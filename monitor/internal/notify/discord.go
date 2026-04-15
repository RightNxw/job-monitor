package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/sardanioss/httpcloak"
)

type Discord struct {
	webhookURL string
}

func NewDiscord(webhookURL string) *Discord {
	return &Discord{webhookURL: webhookURL}
}

type embed struct {
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	URL         string       `json:"url,omitempty"`
	Color       int          `json:"color"`
	Fields      []embedField `json:"fields,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
	Footer      *embedFooter `json:"footer,omitempty"`
	Thumbnail   *embedImage  `json:"thumbnail,omitempty"`
	Image       *embedImage  `json:"image,omitempty"`
	Author      *embedAuthor `json:"author,omitempty"`
}

type embedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type embedFooter struct {
	Text    string `json:"text"`
	IconURL string `json:"icon_url,omitempty"`
}

type embedImage struct {
	URL string `json:"url"`
}

type embedAuthor struct {
	Name    string `json:"name"`
	URL     string `json:"url,omitempty"`
	IconURL string `json:"icon_url,omitempty"`
}

type webhookPayload struct {
	Content string  `json:"content,omitempty"`
	Embeds  []embed `json:"embeds"`
}

// sourceColors maps scraper source prefixes to embed colors.
var sourceColors = map[string]int{
	"greenhouse": 0x2ea44f, // green
	"lever":      0x6366f1, // indigo
	"github":     0xf97316, // orange
	"instagram":  0xe1306c, // pink
	"discord":    0x5865f2, // discord blurple
	"reddit":     0xff4500, // reddit orange
	"glassdoor":  0x0caa41, // glassdoor green
}

func colorForSource(source string) int {
	for prefix, color := range sourceColors {
		if strings.HasPrefix(source, prefix) {
			return color
		}
	}
	return 0x00b4d8 // default blue
}

// SendNewJobs sends a Discord embed for new job postings.
func (d *Discord) SendNewJobs(ctx context.Context, company string, jobs []JobNotification) error {
	if d.webhookURL == "" {
		log.Printf("[discord] no webhook URL configured, skipping notification for %d %s jobs", len(jobs), company)
		return nil
	}

	// Discord limits 10 embeds per message. Batch if needed.
	for i := 0; i < len(jobs); i += 10 {
		end := i + 10
		if end > len(jobs) {
			end = len(jobs)
		}
		batch := jobs[i:end]

		var embeds []embed
		for _, j := range batch {
			var fields []embedField

			if j.Location != "" {
				fields = append(fields, embedField{Name: "Location", Value: j.Location, Inline: true})
			}
			if j.Department != "" {
				fields = append(fields, embedField{Name: "Department", Value: j.Department, Inline: true})
			}
			if j.Source != "" {
				fields = append(fields, embedField{Name: "Source", Value: j.Source, Inline: true})
			}
			if j.Terms != "" {
				fields = append(fields, embedField{Name: "Term", Value: j.Terms, Inline: true})
			}
			if j.PostedAt != "" {
				fields = append(fields, embedField{Name: "Date Posted", Value: j.PostedAt, Inline: true})
			}
			if j.URL != "" {
				fields = append(fields, embedField{Name: "Apply", Value: fmt.Sprintf("[Click to apply](%s)", j.URL), Inline: false})
			}

			e := embed{
				Title:     truncate(j.Title, 256),
				Color:     colorForSource(j.Source),
				Fields:    fields,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Author: &embedAuthor{
					Name: j.Company,
				},
			}

			if j.PostedAt != "" {
				e.Footer = &embedFooter{Text: "Posted " + j.PostedAt}
			}
			if j.ImageURL != "" {
				if strings.HasPrefix(j.Source, "instagram") {
					e.Image = &embedImage{URL: j.ImageURL} // full-size for stories
				} else {
					e.Thumbnail = &embedImage{URL: j.ImageURL}
				}
			}

			embeds = append(embeds, e)
		}

		payload := webhookPayload{
			Content: fmt.Sprintf("**%d new %s posting(s) found**", len(batch), company),
			Embeds:  embeds,
		}

		body, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal webhook: %w", err)
		}

		client := httpcloak.New("chrome-143")
		defer client.Close()

		resp, err := client.Post(ctx, d.webhookURL, bytes.NewReader(body), "application/json")
		if err != nil {
			return fmt.Errorf("send webhook: %w", err)
		}
		if resp.StatusCode >= 400 {
			respBody, _ := resp.Bytes()
			return fmt.Errorf("webhook returned %d: %s", resp.StatusCode, string(respBody[:min(len(respBody), 200)]))
		}

		// Discord rate limit: 1 message per second
		if end < len(jobs) {
			time.Sleep(1 * time.Second)
		}
	}

	return nil
}

// JobNotification is the data needed to send a Discord notification.
type JobNotification struct {
	Title       string
	Company     string
	Location    string
	Department  string
	URL         string
	Source      string // "greenhouse:spacex", "lever:palantir", etc.
	Terms       string // "Summer 2026", "Fall 2026"
	Sponsorship string // "U.S. Citizenship Required", etc.
	PostedAt    string // human-readable date
	ImageURL    string // story/post image to embed
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
