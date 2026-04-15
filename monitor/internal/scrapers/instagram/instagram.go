package instagram

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
	"github.com/RightNxw/job-monitor/monitor/internal/intel"
	"github.com/RightNxw/job-monitor/monitor/internal/notify"
)

const (
	userInfoURL = "https://i.instagram.com/api/v1/users/web_profile_info/?username=%s"
	// Web API exposes story_link_stickers (mobile API hides them in Bloks)
	storiesURL = "https://www.instagram.com/api/v1/feed/reels_media/?reel_ids=%s"
	storyLink  = "https://www.instagram.com/stories/%s/%s/"
)

// Scraper monitors an Instagram account's stories.
// Requires cookies from a burner account (INSTAGRAM_COOKIES env var).
type Scraper struct {
	username  string
	sessionID string // full cookie string for auth requests
	csrfToken string // csrftoken cookie value
	userID    string // resolved on first run
	database  *db.DB
	discord   *notify.Discord
	parser    *intel.Parser // Haiku for classifying OCR text
	store     *intel.Store  // for storing hiring signals
}

// New creates an Instagram story scraper.
// cookiesJSON is the JSON array of cookies exported from the browser.
// If empty, the scraper skips gracefully.
func New(username, cookiesJSON string, database *db.DB, discord *notify.Discord, parser *intel.Parser, store *intel.Store) *Scraper {
	sessionID, csrfToken := parseCookies(cookiesJSON)
	return &Scraper{
		username:  username,
		sessionID: sessionID,
		csrfToken: csrfToken,
		database:  database,
		discord:   discord,
		parser:    parser,
		store:     store,
	}
}

// parseCookies extracts the cookie string and csrf token from a JSON cookie array.
func parseCookies(cookiesJSON string) (cookieStr, csrfToken string) {
	if cookiesJSON == "" {
		return "", ""
	}
	type cookie struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	var cookies []cookie
	if err := json.Unmarshal([]byte(cookiesJSON), &cookies); err != nil {
		// Maybe it's just a raw sessionid value
		return "sessionid=" + cookiesJSON, ""
	}
	var parts []string
	for _, c := range cookies {
		parts = append(parts, c.Name+"="+c.Value)
		if c.Name == "csrftoken" {
			csrfToken = c.Value
		}
	}
	return strings.Join(parts, "; "), csrfToken
}

func (s *Scraper) Source() string {
	return "instagram:" + s.username
}

func (s *Scraper) Run(ctx context.Context, proxyURL string) error {
	if s.sessionID == "" {
		log.Printf("[instagram:%s] skipped - no INSTAGRAM_SESSION_ID set (use a burner account)", s.username)
		return nil
	}

	// Resolve user ID on first run
	if s.userID == "" {
		id, err := s.resolveUserID(ctx)
		if err != nil {
			return fmt.Errorf("resolve user ID: %w", err)
		}
		s.userID = id
		log.Printf("[instagram:%s] resolved user ID: %s", s.username, s.userID)
	}

	stories, err := s.fetchStories(ctx)
	if err != nil {
		return fmt.Errorf("fetch stories: %w", err)
	}

	log.Printf("[instagram:%s] got %d active stories", s.username, len(stories))

	var newItems []notify.JobNotification

	for _, st := range stories {
		// OCR first so the title has extracted text before DB insert
		title := storyTitle(st)
		ocrText := ""
		var ocrLinks []string

		if st.ThumbnailURL != "" {
			ocr, err := intel.OCRImage(ctx, st.ThumbnailURL)
			if err != nil {
				log.Printf("[instagram:%s] OCR error: %v", s.username, err)
			} else if isUsableOCR(ocr.Text) {
				ocrText = ocr.Text
				ocrLinks = ocr.Links
				title = truncate(ocrText, 200)
				log.Printf("[instagram:%s] OCR: %d chars, %d links", s.username, len(ocrText), len(ocrLinks))
			} else {
				// OCR failed, send the image directly to Haiku vision
				log.Printf("[instagram:%s] OCR junk (%d chars), falling back to Haiku vision", s.username, len(ocr.Text))
				if s.parser != nil {
					visionResult, err := s.parser.OCRImageVision(ctx, st.ThumbnailURL)
					if err != nil {
						log.Printf("[instagram:%s] Haiku vision error: %v", s.username, err)
					} else if visionResult.Text != "" {
						ocrText = visionResult.Text
						ocrLinks = visionResult.Links
						title = truncate(ocrText, 200)
						log.Printf("[instagram:%s] Haiku vision: %d chars, %d links", s.username, len(ocrText), len(ocrLinks))
					}
				}
			}
		}

		// Haiku: parse OCR text into structured company + role + category
		company := "@" + s.username
		location := "Instagram Story"
		storyCategory := "unknown"

		if s.parser != nil && ocrText != "" {
			parsed, err := s.parser.ParseAlways(ctx, ocrText)
			if err != nil {
				log.Printf("[instagram:%s] Haiku parse error: %v", s.username, err)
			} else if parsed.Confidence >= 0.5 && parsed.Company != nil && *parsed.Company != "" {
				storyCategory = parsed.Event
				company = *parsed.Company
				if parsed.Role != "" {
					title = parsed.Role
				} else if parsed.Details != "" {
					title = parsed.Details
				}
				if parsed.Location != "" {
					location = parsed.Location
				}
				log.Printf("[instagram:%s] Haiku: %s | %s | %s | %.0f%%",
					s.username, *parsed.Company, parsed.Event, parsed.Details, parsed.Confidence*100)

				if s.store != nil {
					parsedJSON, _ := json.Marshal(parsed)
					s.store.InsertSignal(&intel.Signal{
						Company:      *parsed.Company,
						EventType:    parsed.Event,
						Content:      truncate(ocrText, 500),
						ParsedData:   string(parsedJSON),
						Role:         parsed.Role,
						Team:         parsed.Team,
						Location:     parsed.Location,
						Questions:    parsed.Questions,
						Round:        parsed.Round,
						Timeline:     parsed.Timeline,
						DiscordUser:  "@" + s.username,
						DiscordMsgID: "ig:" + st.ID,
						Channel:      "instagram",
					})
				}
			}
		}

		job := &db.Job{
			Source:       s.Source(),
			ExternalID:   st.ID,
			Title:        title,
			Company:      company,
			Location:     location,
			URL:          fmt.Sprintf(storyLink, s.username, st.ID),
			PostedAt:     time.Unix(st.Timestamp, 0),
			DiscoveredAt: time.Now().UTC(),
			Metadata:     st.metadataJSON(),
		}

		isNew, err := s.database.InsertJob(job)
		if err != nil {
			log.Printf("[instagram:%s] insert error: %v", s.username, err)
			continue
		}
		if isNew {

			// 3. Combine sticker links (API) + OCR links (image), deduplicate
			allLinks := append(st.StickerLinks, ocrLinks...)
			seen := make(map[string]bool)
			var uniqueLinks []string
			for _, l := range allLinks {
				if !seen[l] {
					seen[l] = true
					uniqueLinks = append(uniqueLinks, l)
				}
			}
			if len(uniqueLinks) > 0 {
				log.Printf("[instagram:%s] links: %v", s.username, uniqueLinks)
			}

			// Build notification with category tag
			categoryTag := ""
			if storyCategory != "unknown" && storyCategory != "general" {
				categoryTag = " [" + storyCategory + "]"
			}
			linkText := ""
			if len(uniqueLinks) > 0 {
				linkText = "\n" + strings.Join(uniqueLinks, "\n")
			}

			newItems = append(newItems, notify.JobNotification{
				Title:    title + categoryTag + linkText,
				Company:  "@" + s.username,
				URL:      fmt.Sprintf(storyLink, s.username, st.ID),
				Source:   s.Source(),
				ImageURL: st.ThumbnailURL,
				PostedAt: time.Unix(st.Timestamp, 0).Format("Jan 2, 3:04 PM"),
			})
		}
	}

	if len(newItems) > 0 {
		log.Printf("[instagram:%s] %d new stories, sending notifications", s.username, len(newItems))
		if err := s.discord.SendNewJobs(ctx, "@"+s.username, newItems); err != nil {
			log.Printf("[instagram:%s] discord error: %v", s.username, err)
		}
	} else {
		log.Printf("[instagram:%s] no new stories", s.username)
	}

	return nil
}

func storyTitle(st story) string {
	if st.Caption != "" {
		return truncate(st.Caption, 200)
	}
	t := time.Unix(st.Timestamp, 0).Format("Jan 2 3:04 PM")
	return fmt.Sprintf("Story posted %s (%s)", t, st.MediaType)
}

type story struct {
	ID           string
	Caption      string
	Timestamp    int64
	MediaType    string   // image or video
	MediaURL     string   // video URL or full-size image
	ThumbnailURL string   // static image thumbnail (always available, used for OCR)
	StickerLinks []string // URLs from link stickers (extracted from web API)
}

func (st story) metadataJSON() string {
	m := map[string]any{
		"type": st.MediaType,
	}
	if len(st.StickerLinks) > 0 {
		m["links"] = st.StickerLinks
	}
	if st.MediaURL != "" {
		m["media_url"] = st.MediaURL
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func (s *Scraper) doRequest(ctx context.Context, url string, needsAuth bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Instagram 317.0.0.34.109 Android")
	req.Header.Set("X-IG-App-ID", "936619743392459")
	req.Header.Set("Accept", "application/json")
	if needsAuth {
		req.Header.Set("Cookie", s.sessionID)
		req.Header.Set("X-CSRFToken", s.csrfToken)
	}

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
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body[:min(len(body), 300)]))
	}
	return body, nil
}

func (s *Scraper) resolveUserID(ctx context.Context) (string, error) {
	url := fmt.Sprintf(userInfoURL, s.username)
	body, err := s.doRequest(ctx, url, false)
	if err != nil {
		return "", err
	}

	var result struct {
		Data struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if result.Data.User.ID == "" {
		return "", fmt.Errorf("user %s not found", s.username)
	}
	return result.Data.User.ID, nil
}

func (s *Scraper) fetchStories(ctx context.Context) ([]story, error) {
	url := fmt.Sprintf(storiesURL, s.userID)
	body, err := s.doRequest(ctx, url, true)
	if err != nil {
		return nil, err
	}

	var result reelsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse stories: %w", err)
	}

	reel, ok := result.Reels[s.userID]
	if !ok {
		return nil, nil // no active stories right now
	}

	var stories []story
	for _, item := range reel.Items {
		caption := ""
		if item.Caption != nil {
			caption = item.Caption.Text
		}
		mediaType := "image"
		mediaURL := ""
		if item.MediaType == 2 {
			mediaType = "video"
			if len(item.VideoVersions) > 0 {
				mediaURL = item.VideoVersions[0].URL
			}
		} else if len(item.ImageVersions.Candidates) > 0 {
			mediaURL = item.ImageVersions.Candidates[0].URL
		}
		// Thumbnail is always available (even for videos)
		thumbnailURL := ""
		if len(item.ImageVersions.Candidates) > 0 {
			thumbnailURL = item.ImageVersions.Candidates[0].URL
		}

		// Extract link sticker URLs
		var stickerLinks []string
		for _, ls := range item.LinkStickers {
			if ls.StoryLink.URL != "" {
				stickerLinks = append(stickerLinks, ls.StoryLink.URL)
			} else if ls.StoryLink.DisplayURL != "" {
				stickerLinks = append(stickerLinks, "https://"+ls.StoryLink.DisplayURL)
			}
		}

		stories = append(stories, story{
			ID:           item.PK,
			Caption:      caption,
			Timestamp:    item.TakenAt,
			MediaType:    mediaType,
			MediaURL:     mediaURL,
			ThumbnailURL: thumbnailURL,
			StickerLinks: stickerLinks,
		})
	}

	return stories, nil
}

// --- API response types ---

type reelsResponse struct {
	Reels  map[string]reelData `json:"reels"`
	Status string              `json:"status"`
}

type reelData struct {
	Items []storyItem `json:"items"`
}

type storyItem struct {
	PK        string `json:"pk"`
	TakenAt   int64  `json:"taken_at"`
	MediaType int    `json:"media_type"` // 1=image, 2=video
	Caption   *struct {
		Text string `json:"text"`
	} `json:"caption"`
	ImageVersions struct {
		Candidates []struct {
			URL string `json:"url"`
		} `json:"candidates"`
	} `json:"image_versions2"`
	VideoVersions []struct {
		URL string `json:"url"`
	} `json:"video_versions"`
	// Link stickers, only available from web API (www.instagram.com/api/v1/)
	LinkStickers []struct {
		StoryLink struct {
			URL        string `json:"url"`
			DisplayURL string `json:"display_url"`
		} `json:"story_link"`
	} `json:"story_link_stickers"`
}

// isUsableOCR checks if OCR text is real content vs junk from bad image recognition.
func isUsableOCR(text string) bool {
	if len(text) < 20 {
		return false
	}
	// Count real words (3+ chars, alphabetic)
	words := 0
	for _, w := range strings.Fields(text) {
		clean := strings.Trim(w, ".,!?;:-()[]{}\"'")
		if len(clean) >= 3 {
			alpha := true
			for _, c := range clean {
				if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
					alpha = false
					break
				}
			}
			if alpha {
				words++
			}
		}
	}
	// Need at least 5 real words to be useful
	return words >= 5
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
