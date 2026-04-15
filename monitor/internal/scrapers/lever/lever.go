package lever

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RightNxw/job-monitor/monitor/internal/db"
	"github.com/RightNxw/job-monitor/monitor/internal/httpclient"
	"github.com/RightNxw/job-monitor/monitor/internal/notify"
)

// apiResponse is the paginated Lever API response.
type apiResponse struct {
	Data    []apiPosting `json:"data"`
	HasNext bool         `json:"hasNext"`
	Next    string       `json:"next"`
}

// apiPosting is a single job posting from Lever.
type apiPosting struct {
	ID         string        `json:"id"`
	Text       string        `json:"text"`
	Categories apiCategories `json:"categories"`
	Content    apiContent    `json:"content"`
	URLs       apiURLs       `json:"urls"`
	Tags       []string      `json:"tags"`
	CreatedAt  int64         `json:"createdAt"`
	UpdatedAt  int64         `json:"updatedAt"`
	Country    string        `json:"country"`
}

type apiCategories struct {
	Team         string   `json:"team"`
	Commitment   string   `json:"commitment"`
	Location     string   `json:"location"`
	AllLocations []string `json:"allLocations"`
}

type apiContent struct {
	Description string `json:"description"`
}

type apiURLs struct {
	Apply string `json:"apply"`
	Show  string `json:"show"`
}

// Fields from the public API (api.lever.co/v0/postings/{company})
// These are at the top level of the posting object, not nested under urls.
type apiPostingFlat struct {
	apiPosting
	HostedURL string `json:"hostedUrl"`
	ApplyURL  string `json:"applyUrl"`
}

// parseLeverResponse handles both paginated and flat array formats.
func parseLeverResponse(body []byte) (postings []apiPosting, hasNext bool, next string, err error) {
	// Try paginated format first
	var paginated apiResponse
	if err := json.Unmarshal(body, &paginated); err == nil && paginated.Data != nil {
		return paginated.Data, paginated.HasNext, paginated.Next, nil
	}

	// Fall back to flat array (public API)
	var flat []apiPostingFlat
	if err := json.Unmarshal(body, &flat); err != nil {
		return nil, false, "", fmt.Errorf("unrecognized lever response format: %w", err)
	}

	for _, f := range flat {
		p := f.apiPosting
		if p.URLs.Show == "" {
			p.URLs.Show = f.HostedURL
		}
		if p.URLs.Apply == "" {
			p.URLs.Apply = f.ApplyURL
		}
		postings = append(postings, p)
	}
	return postings, false, "", nil // flat responses aren't paginated
}

// Board defines a Lever-based job board to scrape.
type Board struct {
	Slug    string // e.g. "palantir"
	Company string // display name
	BaseURL string // e.g. "https://www.palantir.com/api/lever/v1/postings"
	Public  bool   // true for api.lever.co (no ?state= param, flat array response)
}

// Scraper monitors Lever-based job boards for new postings.
type Scraper struct {
	board    Board
	database *db.DB
	discord  *notify.Discord
	keywords []string
}

// New creates a Lever scraper for the given board.
func New(board Board, database *db.DB, discord *notify.Discord, keywords []string) *Scraper {
	return &Scraper{
		board:    board,
		database: database,
		discord:  discord,
		keywords: keywords,
	}
}

// Source returns the source identifier for DB records.
func (s *Scraper) Source() string {
	return "lever:" + s.board.Slug
}

// Run fetches all postings (paginated) and notifies on new ones.
func (s *Scraper) Run(ctx context.Context, proxyURL string) error {
	var allPostings []apiPosting
	url := s.board.BaseURL
	if !s.board.Public {
		url += "?state=published"
	}

	for {
		log.Printf("[lever:%s] fetching %s", s.board.Slug, url)

		var resp *httpclient.Response
		var err error
		if s.board.Public {
			resp, err = httpclient.GetJSON(ctx, url, "")
		} else {
			resp, err = httpclient.Get(ctx, url, "")
		}
		if err != nil {
			return fmt.Errorf("fetch %s: %w", s.board.Slug, err)
		}

		body, err := resp.Bytes()
		if err != nil {
			return fmt.Errorf("read body %s: %w", s.board.Slug, err)
		}

		if resp.StatusCode != 200 {
			return fmt.Errorf("%s returned %d: %s", s.board.Slug, resp.StatusCode, string(body[:min(len(body), 200)]))
		}

		// Lever APIs come in two formats:
		// 1. Paginated object: {"data": [...], "hasNext": bool, "next": "cursor"}
		//    (e.g. Palantir's proxy at /api/lever/v1/postings)
		// 2. Flat array: [{"id":...}, ...] with hostedUrl/applyUrl fields
		//    (e.g. api.lever.co/v0/postings/{company})
		postings, hasNext, next, err := parseLeverResponse(body)
		if err != nil {
			return fmt.Errorf("parse %s: %w", s.board.Slug, err)
		}

		allPostings = append(allPostings, postings...)

		if !hasNext || next == "" {
			break
		}
		if s.board.Public {
			url = s.board.BaseURL + "?offset=" + next
		} else {
			url = s.board.BaseURL + "?state=published&offset=" + next
		}
	}

	log.Printf("[lever:%s] got %d total postings", s.board.Slug, len(allPostings))

	var newJobs []notify.JobNotification
	now := time.Now().UTC()

	for _, p := range allPostings {
		if !s.matchesFilter(p) {
			continue
		}

		location := p.Categories.Location
		if len(p.Categories.AllLocations) > 1 {
			location = strings.Join(p.Categories.AllLocations, " | ")
		}

		applyURL := p.URLs.Show
		if applyURL == "" {
			applyURL = p.URLs.Apply
		}

		meta := map[string]any{
			"team":       p.Categories.Team,
			"commitment": p.Categories.Commitment,
			"country":    p.Country,
			"tags":       p.Tags,
		}
		metaJSON, _ := json.Marshal(meta)

		job := &db.Job{
			Source:       s.Source(),
			ExternalID:   p.ID,
			Title:        p.Text,
			Company:      s.board.Company,
			Location:     location,
			URL:          applyURL,
			PostedAt:     time.UnixMilli(p.CreatedAt),
			DiscoveredAt: now,
			Metadata:     string(metaJSON),
		}

		isNew, err := s.database.InsertJob(job)
		if err != nil {
			log.Printf("[lever:%s] insert error for %s: %v", s.board.Slug, p.ID, err)
			continue
		}

		if isNew {
			newJobs = append(newJobs, notify.JobNotification{
				Title:      p.Text,
				Company:    s.board.Company,
				Location:   location,
				Department: p.Categories.Team,
				URL:        applyURL,
				Source:     s.Source(),
				Terms:      p.Categories.Commitment,
			})
		}
	}

	if len(newJobs) > 0 {
		log.Printf("[lever:%s] %d new jobs found, sending notifications", s.board.Slug, len(newJobs))
		if err := s.discord.SendNewJobs(ctx, s.board.Company, newJobs); err != nil {
			log.Printf("[lever:%s] discord error: %v", s.board.Slug, err)
		}
	} else {
		log.Printf("[lever:%s] no new jobs", s.board.Slug)
	}

	return nil
}

func (s *Scraper) matchesFilter(p apiPosting) bool {
	if len(s.keywords) == 0 {
		return true
	}

	// Check commitment field first (Lever has structured data for this)
	commitLower := strings.ToLower(p.Categories.Commitment)
	for _, kw := range s.keywords {
		if strings.Contains(commitLower, strings.ToLower(kw)) {
			return true
		}
	}
	if matchWord(commitLower, "intern") {
		return true
	}

	// Fall back to title match
	titleLower := strings.ToLower(p.Text)
	for _, kw := range s.keywords {
		if strings.Contains(titleLower, strings.ToLower(kw)) {
			return true
		}
	}
	if matchWord(titleLower, "intern") {
		return true
	}

	return false
}

// matchWord returns true if word appears in s at a word boundary.
func matchWord(s, word string) bool {
	idx := 0
	for {
		i := strings.Index(s[idx:], word)
		if i < 0 {
			return false
		}
		end := idx + i + len(word)
		if end >= len(s) {
			return true
		}
		c := s[end]
		if c < 'a' || c > 'z' {
			return true
		}
		if strings.HasPrefix(s[end:], "ship") {
			return true
		}
		idx = end
	}
}
