package greenhouse

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

const apiBase = "https://boards-api.greenhouse.io/v1/boards"

// apiResponse is the top-level Greenhouse API response.
type apiResponse struct {
	Jobs []apiJob `json:"jobs"`
}

// apiJob is a single job from the Greenhouse API.
type apiJob struct {
	ID             int64       `json:"id"`
	InternalJobID  int64       `json:"internal_job_id"`
	Title          string      `json:"title"`
	AbsoluteURL    string      `json:"absolute_url"`
	Location       apiLocation `json:"location"`
	FirstPublished string      `json:"first_published"`
	UpdatedAt      string      `json:"updated_at"`
	Metadata       []apiMeta   `json:"metadata"`
}

type apiLocation struct {
	Name string `json:"name"`
}

type apiMeta struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Value     any    `json:"value"`
	ValueType string `json:"value_type"`
}

// Scraper monitors a Greenhouse job board for new postings.
type Scraper struct {
	board    string // e.g. "spacex", "figma"
	company  string // display name, e.g. "SpaceX"
	database *db.DB
	discord  *notify.Discord
	keywords []string // filter: only notify if title contains one of these (empty = all)
	useProxy bool     // whether to route through proxy pool
}

// New creates a Greenhouse scraper for the given board slug.
func New(board, company string, database *db.DB, discord *notify.Discord, keywords []string) *Scraper {
	return &Scraper{
		board:    board,
		company:  company,
		database: database,
		discord:  discord,
		keywords: keywords,
		useProxy: false, // Greenhouse is a public API, no proxy needed
	}
}

// Source returns the source identifier for DB records.
func (s *Scraper) Source() string {
	return "greenhouse:" + s.board
}

// Run fetches all jobs and notifies on new ones.
func (s *Scraper) Run(ctx context.Context, proxyURL string) error {
	url := fmt.Sprintf("%s/%s/jobs", apiBase, s.board)
	log.Printf("[greenhouse:%s] fetching %s", s.board, url)

	proxy := ""
	if s.useProxy {
		proxy = proxyURL
	}
	resp, err := httpclient.Get(ctx, url, proxy)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", s.board, err)
	}

	body, err := resp.Bytes()
	if err != nil {
		return fmt.Errorf("read body %s: %w", s.board, err)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("%s returned %d: %s", s.board, resp.StatusCode, string(body[:min(len(body), 200)]))
	}

	var result apiResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parse %s: %w", s.board, err)
	}

	log.Printf("[greenhouse:%s] got %d total jobs", s.board, len(result.Jobs))

	var newJobs []notify.JobNotification
	now := time.Now().UTC()

	for _, j := range result.Jobs {
		if !s.matchesFilter(j.Title) {
			continue
		}

		postedAt := parseTime(j.FirstPublished)

		job := &db.Job{
			Source:       s.Source(),
			ExternalID:   fmt.Sprintf("%d", j.ID),
			Title:        j.Title,
			Company:      s.company,
			Location:     j.Location.Name,
			URL:          j.AbsoluteURL,
			PostedAt:     postedAt,
			DiscoveredAt: now,
			Metadata:     metadataJSON(j.Metadata),
		}

		isNew, err := s.database.InsertJob(job)
		if err != nil {
			log.Printf("[greenhouse:%s] insert error for job %d: %v", s.board, j.ID, err)
			continue
		}

		if isNew {
			newJobs = append(newJobs, notify.JobNotification{
				Title:      j.Title,
				Company:    s.company,
				Location:   j.Location.Name,
				Department: getDepartment(j.Metadata),
				URL:        j.AbsoluteURL,
				Source:     s.Source(),
				PostedAt:   formatPostedAt(j.FirstPublished),
			})
		}
	}

	if len(newJobs) > 0 {
		log.Printf("[greenhouse:%s] %d new jobs found, sending notifications", s.board, len(newJobs))
		if err := s.discord.SendNewJobs(ctx, s.company, newJobs); err != nil {
			log.Printf("[greenhouse:%s] discord error: %v", s.board, err)
		}
	} else {
		log.Printf("[greenhouse:%s] no new jobs", s.board)
	}

	return nil
}

func (s *Scraper) matchesFilter(title string) bool {
	if len(s.keywords) == 0 {
		return true
	}
	lower := strings.ToLower(title)
	for _, kw := range s.keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	// Word-boundary match for "intern", catches "Intern," "Intern/" "Intern "
	// but NOT "Internal" or "International"
	if matchWord(lower, "intern") {
		return true
	}
	return false
}

// matchWord returns true if word appears in s at a word boundary
// (followed by a non-letter or end of string).
func matchWord(s, word string) bool {
	idx := 0
	for {
		i := strings.Index(s[idx:], word)
		if i < 0 {
			return false
		}
		end := idx + i + len(word)
		if end >= len(s) {
			return true // word at end of string
		}
		c := s[end]
		if c < 'a' || c > 'z' {
			return true // followed by non-letter
		}
		// "internship" should also match
		if strings.HasPrefix(s[end:], "ship") {
			return true
		}
		idx = end
	}
}

func getDepartment(meta []apiMeta) string {
	for _, m := range meta {
		if strings.EqualFold(m.Name, "Discipline") || strings.EqualFold(m.Name, "Department") {
			if s, ok := m.Value.(string); ok {
				return s
			}
		}
	}
	return ""
}

func metadataJSON(meta []apiMeta) string {
	m := make(map[string]any)
	for _, item := range meta {
		m[item.Name] = item.Value
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func formatPostedAt(s string) string {
	t := parseTime(s)
	if t.IsZero() {
		return ""
	}
	return t.Format("Jan 2, 2006")
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
