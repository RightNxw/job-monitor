package ghrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RightNxw/job-monitor/monitor/internal/db"
	"github.com/RightNxw/job-monitor/monitor/internal/notify"
)

// Repo defines a GitHub repo with a JSON listings file to monitor.
type Repo struct {
	Slug   string // short name, e.g. "simplify-intern"
	Name   string // display name, e.g. "Simplify Internships"
	RawURL string // raw URL to listings JSON
	// Owner/Repo/Branch/Path for commit-based change detection
	Owner    string
	RepoName string
	Branch   string
	Path     string
}

// listing is a single entry from the Simplify-style listings.json.
type listing struct {
	ID          string   `json:"id"`
	Source      string   `json:"source"`
	CompanyName string   `json:"company_name"`
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	CompanyURL  string   `json:"company_url"`
	Locations   []string `json:"locations"`
	Terms       []string `json:"terms"`
	DatePosted  int64    `json:"date_posted"`
	DateUpdated int64    `json:"date_updated"`
	Active      bool     `json:"active"`
	IsVisible   bool     `json:"is_visible"`
	Sponsorship string   `json:"sponsorship"`
}

// Scraper monitors GitHub repos with JSON listing files.
// Uses ETag-based caching to avoid re-downloading 15MB+ files when unchanged.
type Scraper struct {
	repo     Repo
	database *db.DB
	discord  *notify.Discord
	lastSeen int64  // highest date_posted seen (skip old entries)
	etag     string // GitHub raw ETag for conditional requests
}

func New(repo Repo, database *db.DB, discord *notify.Discord) *Scraper {
	return &Scraper{
		repo:     repo,
		database: database,
		discord:  discord,
	}
}

func (s *Scraper) Source() string {
	return "github:" + s.repo.Slug
}

func (s *Scraper) Run(ctx context.Context, proxyURL string) error {
	body, changed, err := s.fetchIfChanged(ctx)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", s.repo.Slug, err)
	}
	if !changed {
		log.Printf("[github:%s] unchanged (ETag match), skipping", s.repo.Slug)
		return nil
	}

	var listings []listing
	if err := json.Unmarshal(body, &listings); err != nil {
		return fmt.Errorf("parse %s: %w", s.repo.Slug, err)
	}

	log.Printf("[github:%s] got %d total listings", s.repo.Slug, len(listings))

	var newJobs []notify.JobNotification
	now := time.Now().UTC()
	maxPosted := s.lastSeen

	for i := range listings {
		l := &listings[i]

		if !l.Active || !l.IsVisible {
			continue
		}

		// On first run, only ingest listings from the last 24 hours
		// to avoid dumping 20K entries into Discord
		if s.lastSeen == 0 {
			cutoff := time.Now().Add(-24 * time.Hour).Unix()
			if l.DatePosted < cutoff {
				continue
			}
		} else if l.DatePosted <= s.lastSeen {
			continue
		}

		if l.DatePosted > maxPosted {
			maxPosted = l.DatePosted
		}

		location := strings.Join(l.Locations, " | ")

		meta := map[string]any{
			"terms":       l.Terms,
			"sponsorship": l.Sponsorship,
			"company_url": l.CompanyURL,
			"source":      l.Source,
		}
		metaJSON, _ := json.Marshal(meta)

		job := &db.Job{
			Source:       s.Source(),
			ExternalID:   l.ID,
			Title:        l.CompanyName + " - " + l.Title,
			Company:      l.CompanyName,
			Location:     location,
			URL:          l.URL,
			PostedAt:     time.Unix(l.DatePosted, 0),
			DiscoveredAt: now,
			Metadata:     string(metaJSON),
		}

		isNew, err := s.database.InsertJob(job)
		if err != nil {
			continue
		}
		if isNew {
			terms := strings.Join(l.Terms, ", ")
			newJobs = append(newJobs, notify.JobNotification{
				Title:       l.CompanyName + " - " + l.Title,
				Company:     l.CompanyName,
				Location:    location,
				URL:         l.URL,
				Source:      s.Source(),
				Terms:       terms,
				Sponsorship: l.Sponsorship,
				PostedAt:    time.Unix(l.DatePosted, 0).Format("Jan 2, 2006"),
			})
		}
	}

	s.lastSeen = maxPosted

	if len(newJobs) > 0 {
		log.Printf("[github:%s] %d new listings, sending notifications", s.repo.Slug, len(newJobs))
		if err := s.discord.SendNewJobs(ctx, s.repo.Name, newJobs); err != nil {
			log.Printf("[github:%s] discord error: %v", s.repo.Slug, err)
		}
	} else {
		log.Printf("[github:%s] no new listings", s.repo.Slug)
	}

	return nil
}

// fetchIfChanged does a conditional GET using ETag. Returns body only if file changed.
// First call always downloads. Subsequent calls return (nil, false, nil) if unchanged.
func (s *Scraper) fetchIfChanged(ctx context.Context) ([]byte, bool, error) {
	log.Printf("[github:%s] checking for updates", s.repo.Slug)

	req, err := http.NewRequestWithContext(ctx, "GET", s.repo.RawURL, nil)
	if err != nil {
		return nil, false, err
	}
	if s.etag != "" {
		req.Header.Set("If-None-Match", s.etag)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	// 304 Not Modified, file hasn't changed
	if resp.StatusCode == 304 {
		return nil, false, nil
	}

	if resp.StatusCode != 200 {
		return nil, false, fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}

	// Save ETag for next request
	if etag := resp.Header.Get("ETag"); etag != "" {
		s.etag = etag
	}

	log.Printf("[github:%s] downloaded %.1f MB", s.repo.Slug, float64(len(body))/1024/1024)
	return body, true, nil
}
