package reddit

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/RightNxw/job-monitor/monitor/internal/httpclient"
	"github.com/RightNxw/job-monitor/monitor/internal/intel"
	"github.com/RightNxw/job-monitor/monitor/internal/proxy"
)

const redditAPI = "https://www.reddit.com/r/%s/new.json?limit=50"

// Subreddit to monitor.
type Subreddit struct {
	Name string // e.g. "csMajors"
}

// Scraper monitors subreddits for hiring intel and interview questions.
type Scraper struct {
	subreddits []Subreddit
	store      *intel.Store
	interviews *intel.InterviewStore
	parser     *intel.Parser
	pool       *proxy.Pool
	lastSeen   map[string]string // subreddit → last seen post ID
}

func NewScraper(subreddits []Subreddit, store *intel.Store, interviews *intel.InterviewStore, parser *intel.Parser, pool *proxy.Pool) *Scraper {
	return &Scraper{
		subreddits: subreddits,
		store:      store,
		interviews: interviews,
		parser:     parser,
		pool:       pool,
		lastSeen:   make(map[string]string),
	}
}

func (s *Scraper) Source() string {
	return "reddit"
}

func (s *Scraper) Run(ctx context.Context, proxyURL string) error {
	if s.parser == nil {
		log.Printf("[reddit] skipped - no ANTHROPIC_API_KEY set")
		return nil
	}

	totalNew := 0

	for _, sub := range s.subreddits {
		posts, err := s.fetchPosts(ctx, sub.Name)
		if err != nil {
			log.Printf("[reddit:r/%s] fetch error: %v", sub.Name, err)
			continue
		}

		newPosts := s.filterNew(sub.Name, posts)
		if len(newPosts) == 0 {
			continue
		}

		for _, post := range newPosts {
			// Combine title + selftext for parsing
			content := post.Title
			if post.Selftext != "" {
				content += "\n" + post.Selftext
			}

			if len(content) < 15 || !looksHiringRelated(content) {
				continue
			}

			// Truncate long posts for Haiku (save tokens)
			if len(content) > 1000 {
				content = content[:1000]
			}

			parsed, err := s.parser.Parse(ctx, content)
			if err != nil {
				continue
			}

			if parsed.Confidence < 0.5 || parsed.Company == nil || *parsed.Company == "" {
				continue
			}

			parsedJSON, _ := json.Marshal(parsed)

			sig := &intel.Signal{
				Company:      *parsed.Company,
				EventType:    parsed.Event,
				Content:      truncate(content, 500),
				ParsedData:   string(parsedJSON),
				Role:         parsed.Role,
				Team:         parsed.Team,
				Location:     parsed.Location,
				Questions:    parsed.Questions,
				Round:        parsed.Round,
				Timeline:     parsed.Timeline,
				DiscordUser:  "u/" + post.Author,
				DiscordMsgID: "reddit:" + post.ID,
				Channel:      "r/" + sub.Name,
			}

			isNew, err := s.store.InsertSignal(sig)
			if err != nil {
				continue
			}
			if isNew {
				totalNew++
				log.Printf("[reddit:r/%s] %s | %s | %s",
					sub.Name, *parsed.Company, parsed.Event, parsed.Details)

				// Route interview signals to interview_reports with job matching
				if s.interviews != nil && isInterviewEvent(parsed.Event) {
					s.interviews.Insert(&intel.InterviewReport{
						Company:    *parsed.Company,
						Role:       parsed.Role,
						Stage:      parsed.Event,
						Questions:  parsed.Questions,
						Timeline:   parsed.Timeline,
						Location:   parsed.Location,
						Source:     "reddit:" + sub.Name,
						SourceURL:  post.URL,
						SourceID:   "reddit:" + post.ID,
						Author:     "u/" + post.Author,
						Content:    truncate(content, 500),
						ParsedData: string(parsedJSON),
					})
				}
			}

			// Fetch and parse comments for posts that matched and have comments
			if post.NumComments > 0 {
				commentNew := s.processComments(ctx, sub.Name, post)
				totalNew += commentNew
			}
		}
	}

	if totalNew > 0 {
		log.Printf("[reddit] %d new signals", totalNew)
	} else {
		log.Printf("[reddit] no new signals")
	}

	return nil
}

type redditPost struct {
	ID          string
	Title       string
	Selftext    string
	Author      string
	Created     float64
	Score       int
	URL         string
	NumComments int
	Subreddit   string
}

type redditResponse struct {
	Data struct {
		Children []struct {
			Data struct {
				ID          string  `json:"id"`
				Title       string  `json:"title"`
				Selftext    string  `json:"selftext"`
				Author      string  `json:"author"`
				Created     float64 `json:"created_utc"`
				Score       int     `json:"score"`
				URL         string  `json:"url"`
				NumComments int     `json:"num_comments"`
				Subreddit   string  `json:"subreddit"`
			} `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

func (s *Scraper) fetchPosts(ctx context.Context, subreddit string) ([]redditPost, error) {
	url := fmt.Sprintf(redditAPI, subreddit)

	// Use proxy to avoid Reddit IP blocks on JSON API
	proxyURL := ""
	if s.pool != nil {
		proxyURL = s.pool.Get()
	}

	headers := map[string][]string{
		"accept":          {"application/json"},
		"accept-language": {"en-US,en;q=0.9"},
	}

	resp, err := httpclient.GetWithHeaders(ctx, url, proxyURL, headers)
	if err != nil {
		return nil, err
	}

	body, err := resp.Bytes()
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 429 {
		log.Printf("[reddit:r/%s] rate limited", subreddit)
		return nil, nil
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}

	var result redditResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse: %w (body starts with: %s)", err, string(body[:min(len(body), 100)]))
	}

	var posts []redditPost
	for _, child := range result.Data.Children {
		d := child.Data
		posts = append(posts, redditPost{
			ID:          d.ID,
			Title:       d.Title,
			Selftext:    d.Selftext,
			Author:      d.Author,
			Created:     d.Created,
			Score:       d.Score,
			URL:         d.URL,
			NumComments: d.NumComments,
			Subreddit:   d.Subreddit,
		})
	}

	return posts, nil
}

// redditComment represents a single top-level comment.
type redditComment struct {
	ID     string
	Body   string
	Author string
	Score  int
}

// commentsResponse is the JSON shape of /r/{sub}/comments/{id}.json.
// Reddit returns an array of two listings: [0] = post, [1] = comments.
type commentsResponse []struct {
	Data struct {
		Children []struct {
			Kind string `json:"kind"`
			Data struct {
				ID     string `json:"id"`
				Body   string `json:"body"`
				Author string `json:"author"`
				Score  int    `json:"score"`
			} `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

// fetchComments retrieves top-level comments for a post, sorted by score descending, limited to 10.
func (s *Scraper) fetchComments(ctx context.Context, subreddit, postID string) ([]redditComment, error) {
	url := fmt.Sprintf("https://www.reddit.com/r/%s/comments/%s.json?sort=top&limit=10", subreddit, postID)

	proxyURL := ""
	if s.pool != nil {
		proxyURL = s.pool.Get()
	}

	headers := map[string][]string{
		"accept": {"application/json"},
	}

	resp, err := httpclient.GetWithHeaders(ctx, url, proxyURL, headers)
	if err != nil {
		return nil, err
	}

	body, err := resp.Bytes()
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 429 {
		log.Printf("[reddit:r/%s] rate limited on comments for %s", subreddit, postID)
		return nil, nil
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("comments status %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}

	var result commentsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	// The second listing (index 1) contains the comments
	if len(result) < 2 {
		return nil, nil
	}

	var comments []redditComment
	for _, child := range result[1].Data.Children {
		// kind "t1" = comment; skip "more" stubs
		if child.Kind != "t1" {
			continue
		}
		d := child.Data
		if d.Body == "" || d.Body == "[deleted]" || d.Body == "[removed]" {
			continue
		}
		comments = append(comments, redditComment{
			ID:     d.ID,
			Body:   d.Body,
			Author: d.Author,
			Score:  d.Score,
		})
	}

	// Sort by score descending, take top 10
	sort.Slice(comments, func(i, j int) bool {
		return comments[i].Score > comments[j].Score
	})
	if len(comments) > 10 {
		comments = comments[:10]
	}

	return comments, nil
}

// processComments fetches comments for a post and parses them through the Haiku pipeline.
// Returns the number of new signals inserted.
func (s *Scraper) processComments(ctx context.Context, subreddit string, post redditPost) int {
	// Small delay to respect Reddit rate limits
	time.Sleep(500 * time.Millisecond)

	comments, err := s.fetchComments(ctx, subreddit, post.ID)
	if err != nil {
		log.Printf("[reddit:r/%s] comment fetch error for %s: %v", subreddit, post.ID, err)
		return 0
	}

	newSignals := 0
	for _, comment := range comments {
		// Provide post title as context for the comment
		content := fmt.Sprintf("[Post: %s]\n%s", post.Title, comment.Body)

		if len(content) < 15 {
			continue
		}

		// Truncate long comments for Haiku
		if len(content) > 1000 {
			content = content[:1000]
		}

		parsed, err := s.parser.Parse(ctx, content)
		if err != nil {
			continue
		}

		if parsed.Confidence < 0.5 || parsed.Company == nil || *parsed.Company == "" {
			continue
		}

		parsedJSON, _ := json.Marshal(parsed)

		sig := &intel.Signal{
			Company:      *parsed.Company,
			EventType:    parsed.Event,
			Content:      truncate(content, 500),
			ParsedData:   string(parsedJSON),
			Role:         parsed.Role,
			Team:         parsed.Team,
			Location:     parsed.Location,
			Questions:    parsed.Questions,
			Round:        parsed.Round,
			Timeline:     parsed.Timeline,
			DiscordUser:  "u/" + comment.Author,
			DiscordMsgID: "reddit:comment:" + comment.ID,
			Channel:      "r/" + subreddit,
		}

		isNew, err := s.store.InsertSignal(sig)
		if err != nil {
			continue
		}
		if isNew {
			newSignals++
			log.Printf("[reddit:r/%s] comment %s | %s | %s | %s",
				subreddit, comment.ID, *parsed.Company, parsed.Event, parsed.Details)
		}
	}

	return newSignals
}

// filterNew returns posts we haven't seen yet. Updates lastSeen.
func (s *Scraper) filterNew(subreddit string, posts []redditPost) []redditPost {
	lastID := s.lastSeen[subreddit]

	if lastID == "" && len(posts) > 0 {
		// First run, just record the newest and return all
		s.lastSeen[subreddit] = posts[0].ID
		return posts
	}

	var newPosts []redditPost
	for _, p := range posts {
		if p.ID == lastID {
			break
		}
		newPosts = append(newPosts, p)
	}

	if len(newPosts) > 0 {
		s.lastSeen[subreddit] = newPosts[0].ID
	}

	return newPosts
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
		"hiring", "swe", "new grad", "newgrad", "hackerrank", "codesignal",
		"recruiter", "salary", "tc", "hm round", "onsite", "virtual",
		"ghosted", "timeline", "how long", "hear back",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
