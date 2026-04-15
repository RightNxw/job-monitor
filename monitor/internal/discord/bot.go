package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RightNxw/job-monitor/monitor/internal/intel"
)

const discordAPI = "https://discord.com/api/v9"

// Channel to monitor.
type Channel struct {
	ID   string
	Name string // for logging
}

// Scraper reads messages from Discord channels using a user token.
type Scraper struct {
	token      string // user account token
	channels   []Channel
	store      *intel.Store
	interviews *intel.InterviewStore
	parser     *intel.Parser
	lastMsgIDs map[string]string // channel ID → last seen message ID
}

func NewScraper(token string, channels []Channel, store *intel.Store, interviews *intel.InterviewStore, parser *intel.Parser) *Scraper {
	return &Scraper{
		token:      token,
		channels:   channels,
		store:      store,
		interviews: interviews,
		parser:     parser,
		lastMsgIDs: make(map[string]string),
	}
}

func (s *Scraper) Source() string {
	return "discord:cscareers"
}

func (s *Scraper) Run(ctx context.Context, proxyURL string) error {
	if s.token == "" {
		log.Printf("[discord] skipped - no DISCORD_USER_TOKEN set")
		return nil
	}

	totalNew := 0

	for _, ch := range s.channels {
		msgs, err := s.fetchMessages(ctx, ch.ID)
		if err != nil {
			log.Printf("[discord:%s] fetch error: %v", ch.Name, err)
			continue
		}

		if len(msgs) == 0 {
			continue
		}

		// Track newest message ID for next poll
		s.lastMsgIDs[ch.ID] = msgs[0].ID

		for _, msg := range msgs {
			if msg.Author.Bot {
				continue
			}
			content := strings.TrimSpace(msg.Content)
			if len(content) < 10 || !looksHiringRelated(content) {
				continue
			}

			parsed, err := s.parser.Parse(ctx, content)
			if err != nil {
				log.Printf("[discord:%s] parse error: %v", ch.Name, err)
				continue
			}

			if parsed.Confidence < 0.5 || parsed.Company == nil || *parsed.Company == "" {
				continue
			}

			parsedJSON, _ := json.Marshal(parsed)

			sig := &intel.Signal{
				Company:      *parsed.Company,
				EventType:    parsed.Event,
				Content:      content,
				ParsedData:   string(parsedJSON),
				Role:         parsed.Role,
				Team:         parsed.Team,
				Location:     parsed.Location,
				Questions:    parsed.Questions,
				Round:        parsed.Round,
				Timeline:     parsed.Timeline,
				DiscordUser:  msg.Author.Username,
				DiscordMsgID: msg.ID,
				Channel:      ch.Name,
			}

			isNew, err := s.store.InsertSignal(sig)
			if err != nil {
				continue
			}
			if isNew {
				totalNew++
				log.Printf("[discord:%s] %s | %s | %s | %.0f%%",
					ch.Name, *parsed.Company, parsed.Event, parsed.Details, parsed.Confidence*100)

				// Route interview-related signals to interview_reports with job matching
				if s.interviews != nil && isInterviewEvent(parsed.Event) {
					s.interviews.Insert(&intel.InterviewReport{
						Company:    *parsed.Company,
						Role:       parsed.Role,
						Stage:      parsed.Event,
						Questions:  parsed.Questions,
						Difficulty: "",
						Outcome:    "",
						Timeline:   parsed.Timeline,
						Location:   parsed.Location,
						Source:     "discord:" + ch.Name,
						SourceID:   "discord:" + msg.ID,
						Author:     msg.Author.Username,
						Content:    content,
						ParsedData: string(parsedJSON),
					})
				}
			}
		}
	}

	if totalNew > 0 {
		log.Printf("[discord] %d new signals across %d channels", totalNew, len(s.channels))
	} else {
		log.Printf("[discord] no new signals")
	}

	return nil
}

type discordMessage struct {
	ID        string        `json:"id"`
	Content   string        `json:"content"`
	Timestamp string        `json:"timestamp"`
	Author    discordAuthor `json:"author"`
}

type discordAuthor struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Bot      bool   `json:"bot"`
}

func (s *Scraper) fetchMessages(ctx context.Context, channelID string) ([]discordMessage, error) {
	url := fmt.Sprintf("%s/channels/%s/messages?limit=50", discordAPI, channelID)

	// If we've seen messages before, only fetch newer ones
	if lastID, ok := s.lastMsgIDs[channelID]; ok {
		url += "&after=" + lastID
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", s.token)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 429 {
		log.Printf("[discord] rate limited, will retry next cycle")
		return nil, nil
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}

	var msgs []discordMessage
	if err := json.Unmarshal(body, &msgs); err != nil {
		return nil, err
	}

	return msgs, nil
}

func isInterviewEvent(event string) bool {
	switch event {
	case "oa_sent", "interviewing", "offering", "rejecting", "lc_question":
		return true
	}
	return false
}

func looksHiringRelated(msg string) bool {
	lower := strings.ToLower(msg)
	keywords := []string{
		"intern", "interview", "offer", "reject", "applied", "application",
		"oa", "online assessment", "coding challenge", "phone screen",
		"final round", "behavioral", "technical", "leetcode", "lc",
		"hiring", "position", "role", "swe", "new grad", "newgrad",
		"!process", "timeline", "ghosted", "recruiter", "hm round",
		"hackerrank", "codesignal", "salary", "tc", "yoe",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
