package glassdoor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RightNxw/job-monitor/monitor/internal/cloudflare"
	"github.com/RightNxw/job-monitor/monitor/internal/httpclient"
	"github.com/RightNxw/job-monitor/monitor/internal/intel"
)

// Company maps a display name to its Glassdoor employer ID and URL slug.
type Company struct {
	Name       string // display name, e.g. "Amazon"
	Slug       string // URL slug, e.g. "Amazon"
	EmployerID string // Glassdoor employer ID, e.g. "6036"
}

// DefaultCompanies is the list of top tech companies to scrape.
var DefaultCompanies = []Company{
	{Name: "Amazon", Slug: "Amazon", EmployerID: "6036"},
	{Name: "Google", Slug: "Google", EmployerID: "9079"},
	{Name: "Meta", Slug: "Meta", EmployerID: "40772"},
	{Name: "Apple", Slug: "Apple", EmployerID: "1138"},
	{Name: "Microsoft", Slug: "Microsoft", EmployerID: "1651"},
	{Name: "NVIDIA", Slug: "NVIDIA", EmployerID: "7633"},
	{Name: "Stripe", Slug: "Stripe", EmployerID: "671932"},
	{Name: "Palantir", Slug: "Palantir-Technologies", EmployerID: "236375"},
	{Name: "Bloomberg", Slug: "Bloomberg-L-P", EmployerID: "3096"},
	{Name: "Goldman Sachs", Slug: "Goldman-Sachs", EmployerID: "2800"},
}

// Scraper monitors Glassdoor interview pages for questions.
type Scraper struct {
	companies   []Company
	store       *intel.Store
	cfCookies   map[string]string // cached cf_clearance cookies
	cfUserAgent string            // UA from the CF solve (must match)
	cfMu        sync.Mutex
}

// NewScraper creates a new Glassdoor interview question scraper.
func NewScraper(companies []Company, store *intel.Store) *Scraper {
	return &Scraper{
		companies: companies,
		store:     store,
		cfCookies: make(map[string]string),
	}
}

// Source returns the scraper identifier.
func (s *Scraper) Source() string {
	return "glassdoor"
}

// Run fetches interview questions for all companies.
func (s *Scraper) Run(ctx context.Context, proxyURL string) error {
	// Glassdoor sits behind Cloudflare, so every page needs clearance cookies
	// first. Builds without a solver skip the source instead of failing.
	if err := s.ensureCFCookies(ctx, proxyURL); err != nil {
		if errors.Is(err, cloudflare.ErrSolverUnavailable) {
			log.Printf("[glassdoor] skipped - %v", err)
			return nil
		}
		return err
	}

	totalNew := 0

	for _, company := range s.companies {
		// Fetch up to 3 pages of interview questions per company.
		for page := 1; page <= 3; page++ {
			questions, err := s.fetchInterviewPage(ctx, company, page, proxyURL)
			if err != nil {
				log.Printf("[glassdoor:%s] page %d error: %v", company.Name, page, err)
				break
			}

			if len(questions) == 0 {
				break
			}

			for _, q := range questions {
				sig := &intel.Signal{
					Company:      company.Name,
					EventType:    intel.EventLCQuestion,
					Content:      truncate(q.Question, 500),
					ParsedData:   q.toJSON(),
					Role:         q.JobTitle,
					Questions:    q.Question,
					Round:        q.InterviewType,
					DiscordUser:  "glassdoor",
					DiscordMsgID: fmt.Sprintf("glassdoor:%s:%s", company.EmployerID, q.ID),
					Channel:      "glassdoor",
				}

				if q.Difficulty != "" {
					sig.ParsedData = q.toJSON()
				}

				isNew, err := s.store.InsertSignal(sig)
				if err != nil {
					log.Printf("[glassdoor:%s] insert error: %v", company.Name, err)
					continue
				}
				if isNew {
					totalNew++
				}
			}

			// Rate limit between pages.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}

		// Rate limit between companies.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}

	if totalNew > 0 {
		log.Printf("[glassdoor] %d new interview questions", totalNew)
	} else {
		log.Printf("[glassdoor] no new questions")
	}

	return nil
}

// ensureCFCookies solves Cloudflare JSD if we don't have valid cookies.
func (s *Scraper) ensureCFCookies(ctx context.Context, proxyURL string) error {
	s.cfMu.Lock()
	hasCookies := len(s.cfCookies) > 0
	s.cfMu.Unlock()

	if hasCookies {
		return nil
	}

	log.Printf("[glassdoor] solving Cloudflare challenge...")
	result, err := cloudflare.SolveWithRetry(ctx, "https://www.glassdoor.com/Interview/index.htm", proxyURL, 2)
	if err != nil {
		return err
	}

	s.cfMu.Lock()
	s.cfCookies = result.Cookies
	s.cfUserAgent = result.UserAgent
	s.cfMu.Unlock()

	log.Printf("[glassdoor] Cloudflare solved, got %d cookies", len(result.Cookies))
	return nil
}

// interviewQuestion holds a single parsed interview question.
type interviewQuestion struct {
	ID            string `json:"id"`
	Question      string `json:"question"`
	JobTitle      string `json:"job_title,omitempty"`
	Difficulty    string `json:"difficulty,omitempty"`
	InterviewType string `json:"interview_type,omitempty"`
	Date          string `json:"date,omitempty"`
	Outcome       string `json:"outcome,omitempty"`
	Experience    string `json:"experience,omitempty"`
}

func (q *interviewQuestion) toJSON() string {
	b, _ := json.Marshal(q)
	return string(b)
}

// interviewPageURL builds the Glassdoor interview questions URL.
// Pattern: https://www.glassdoor.com/Interview/{Slug}-Interview-Questions-E{ID}.htm
// Pagination: ...E{ID}_P{page}.htm
func interviewPageURL(c Company, page int) string {
	base := fmt.Sprintf("https://www.glassdoor.com/Interview/%s-Interview-Questions-E%s",
		c.Slug, c.EmployerID)
	if page <= 1 {
		return base + ".htm"
	}
	return fmt.Sprintf("%s_P%d.htm", base, page)
}

// fetchInterviewPage fetches one page of interview questions for a company.
// It tries two extraction strategies:
//  1. Apollo state JSON embedded in the HTML (modern Glassdoor React app)
//  2. Fallback HTML parsing for legacy interview question elements
func (s *Scraper) fetchInterviewPage(ctx context.Context, company Company, page int, proxyURL string) ([]interviewQuestion, error) {
	url := interviewPageURL(company, page)
	log.Printf("[glassdoor:%s] fetching page %d: %s", company.Name, page, url)

	// Solve Cloudflare on first request (or when cookies expire)
	if err := s.ensureCFCookies(ctx, proxyURL); err != nil {
		return nil, fmt.Errorf("cloudflare solve: %w", err)
	}

	// Build cookie header from CF solve
	var cookieParts []string
	s.cfMu.Lock()
	for k, v := range s.cfCookies {
		cookieParts = append(cookieParts, k+"="+v)
	}
	ua := s.cfUserAgent
	s.cfMu.Unlock()

	headers := map[string][]string{
		"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"},
		"accept-language":           {"en-US,en;q=0.9"},
		"sec-fetch-dest":            {"document"},
		"sec-fetch-mode":            {"navigate"},
		"sec-fetch-site":            {"none"},
		"sec-fetch-user":            {"?1"},
		"upgrade-insecure-requests": {"1"},
		"cookie":                    {strings.Join(cookieParts, "; ")},
	}
	if ua != "" {
		headers["user-agent"] = []string{ua}
	}

	resp, err := httpclient.GetWithHeaders(ctx, url, proxyURL, headers)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	body, err := resp.Bytes()
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode == 403 || resp.StatusCode == 429 {
		// CF cookies may have expired, clear and retry next cycle
		s.cfMu.Lock()
		s.cfCookies = make(map[string]string)
		s.cfMu.Unlock()
		return nil, fmt.Errorf("blocked (status %d) - CF cookies expired", resp.StatusCode)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}

	html := string(body)

	// Strategy 1: Extract from Apollo GraphQL cache state embedded in HTML.
	questions := extractFromApolloState(html, company)
	if len(questions) > 0 {
		log.Printf("[glassdoor:%s] extracted %d questions from Apollo state", company.Name, len(questions))
		return questions, nil
	}

	// Strategy 2: Extract from __NEXT_DATA__ JSON blob.
	questions = extractFromNextData(html, company)
	if len(questions) > 0 {
		log.Printf("[glassdoor:%s] extracted %d questions from __NEXT_DATA__", company.Name, len(questions))
		return questions, nil
	}

	// Strategy 3: Fallback regex extraction from raw HTML.
	questions = extractFromHTML(html, company)
	if len(questions) > 0 {
		log.Printf("[glassdoor:%s] extracted %d questions from HTML", company.Name, len(questions))
		return questions, nil
	}

	log.Printf("[glassdoor:%s] no questions extracted (page size=%d bytes)", company.Name, len(body))
	return nil, nil
}

// --- Apollo State Extraction ---

// apolloStateRegex matches the Apollo GraphQL cache state embedded by Glassdoor's React app.
var apolloStateRegex = regexp.MustCompile(`"apolloState"\s*:\s*(\{.+?\})\s*[,}]\s*"`)

// apolloStateRegex2 matches alternative formats.
var apolloStateRegex2 = regexp.MustCompile(`apolloState\s*=\s*(\{.+?\})\s*;`)

func extractFromApolloState(html string, company Company) []interviewQuestion {
	var stateJSON string

	if m := apolloStateRegex.FindStringSubmatch(html); len(m) > 1 {
		stateJSON = m[1]
	} else if m := apolloStateRegex2.FindStringSubmatch(html); len(m) > 1 {
		stateJSON = m[1]
	}

	if stateJSON == "" {
		return nil
	}

	// The Apollo state is a flat map of cache keys to objects.
	// Interview-related keys look like: InterviewReview:12345, InterviewQuestion:67890
	var state map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		log.Printf("[glassdoor] apollo state parse error: %v", err)
		return nil
	}

	var questions []interviewQuestion

	for key, raw := range state {
		// Look for interview review objects.
		if strings.HasPrefix(key, "InterviewReview:") || strings.HasPrefix(key, "InterviewQuestion:") {
			q := parseApolloInterviewObject(key, raw)
			if q != nil {
				questions = append(questions, *q)
			}
			continue
		}

		// Some Apollo caches use numeric keys with a __typename field.
		var obj map[string]interface{}
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}
		typeName, _ := obj["__typename"].(string)
		if typeName == "InterviewReview" || typeName == "InterviewQuestion" {
			q := parseApolloObject(key, obj)
			if q != nil {
				questions = append(questions, *q)
			}
		}
	}

	return questions
}

func parseApolloInterviewObject(key string, raw json.RawMessage) *interviewQuestion {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	return parseApolloObject(key, obj)
}

func parseApolloObject(key string, obj map[string]interface{}) *interviewQuestion {
	// Extract ID from key (e.g. "InterviewReview:12345" -> "12345").
	id := key
	if i := strings.LastIndex(key, ":"); i >= 0 {
		id = key[i+1:]
	}

	// Try various field names Glassdoor might use.
	question := firstString(obj, "interviewQuestion", "question", "questionText", "text", "review")
	if question == "" {
		return nil
	}

	return &interviewQuestion{
		ID:            id,
		Question:      strings.TrimSpace(question),
		JobTitle:      firstString(obj, "jobTitle", "job_title", "position", "roleName"),
		Difficulty:    firstString(obj, "difficulty", "difficultyRating", "difficultyLabel"),
		InterviewType: firstString(obj, "interviewType", "interviewSource", "source"),
		Date:          firstString(obj, "reviewDate", "date", "interviewDate", "createdDate"),
		Outcome:       firstString(obj, "outcome", "offerStatus", "offer"),
		Experience:    firstString(obj, "experience", "overallExperience"),
	}
}

// --- __NEXT_DATA__ Extraction ---

var nextDataRegex = regexp.MustCompile(`<script[^>]*id="__NEXT_DATA__"[^>]*>\s*(\{.+?\})\s*</script>`)

func extractFromNextData(html string, company Company) []interviewQuestion {
	m := nextDataRegex.FindStringSubmatch(html)
	if len(m) < 2 {
		return nil
	}

	// Parse the __NEXT_DATA__ blob and walk into props for interview data.
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(m[1]), &data); err != nil {
		return nil
	}

	// Walk: props.pageProps.interviewQuestions or similar paths.
	var questions []interviewQuestion

	walkJSON(data, func(key string, val interface{}) {
		obj, ok := val.(map[string]interface{})
		if !ok {
			return
		}

		// Look for objects that have interview question characteristics.
		qText := firstString(obj, "interviewQuestion", "question", "questionText", "text")
		if qText == "" {
			return
		}

		// Heuristic: must look like an interview question (not random text fields).
		if len(qText) < 10 {
			return
		}

		id := firstString(obj, "id", "reviewId", "questionId")
		if id == "" {
			id = fmt.Sprintf("nd_%d", hashString(qText))
		}

		questions = append(questions, interviewQuestion{
			ID:            id,
			Question:      strings.TrimSpace(qText),
			JobTitle:      firstString(obj, "jobTitle", "position"),
			Difficulty:    firstString(obj, "difficulty", "difficultyRating"),
			InterviewType: firstString(obj, "interviewType", "source"),
			Date:          firstString(obj, "reviewDate", "date"),
			Outcome:       firstString(obj, "outcome", "offerStatus"),
			Experience:    firstString(obj, "experience"),
		})
	})

	return questions
}

// --- HTML Regex Extraction (legacy fallback) ---

// These patterns extract interview questions from Glassdoor's HTML when no JSON is available.
// Glassdoor renders interview questions inside elements with known class names.

var (
	// Matches question text inside interview question spans/divs.
	htmlQuestionRegex = regexp.MustCompile(
		`(?i)class="[^"]*interviewQuestion[^"]*"[^>]*>([^<]+)<`)

	// Matches job title from reviewer spans.
	htmlJobTitleRegex = regexp.MustCompile(
		`(?i)class="[^"]*(?:reviewer|jobTitle)[^"]*"[^>]*>([^<]+)<`)

	// Matches difficulty labels.
	htmlDifficultyRegex = regexp.MustCompile(
		`(?i)class="[^"]*difficulty[^"]*"[^>]*>([^<]+)<`)

	// Matches individual interview review blocks to group questions with metadata.
	htmlReviewBlockRegex = regexp.MustCompile(
		`(?is)<div[^>]*class="[^"]*(?:interviewReview|interview-review|InterviewCard)[^"]*"[^>]*>(.*?)</div>\s*</div>`)

	// Extracts quoted interview questions from freeform text.
	htmlQuotedQuestionRegex = regexp.MustCompile(
		`(?:"|&ldquo;|&quot;)([^"&]{15,300})(?:"|&rdquo;|&quot;)`)
)

func extractFromHTML(html string, company Company) []interviewQuestion {
	var questions []interviewQuestion
	seen := make(map[string]bool)

	// Try structured extraction from review blocks.
	blocks := htmlReviewBlockRegex.FindAllStringSubmatch(html, -1)
	for i, block := range blocks {
		if len(block) < 2 {
			continue
		}
		content := block[1]

		// Find questions within this block.
		qMatches := htmlQuestionRegex.FindAllStringSubmatch(content, -1)
		for _, qm := range qMatches {
			if len(qm) < 2 {
				continue
			}
			qText := cleanHTMLText(qm[1])
			if qText == "" || seen[qText] {
				continue
			}
			seen[qText] = true

			q := interviewQuestion{
				ID:       fmt.Sprintf("html_%d_%d", i, hashString(qText)),
				Question: qText,
			}

			// Try to find job title in the same block.
			if jtm := htmlJobTitleRegex.FindStringSubmatch(content); len(jtm) > 1 {
				q.JobTitle = cleanHTMLText(jtm[1])
			}

			// Try to find difficulty in the same block.
			if dm := htmlDifficultyRegex.FindStringSubmatch(content); len(dm) > 1 {
				q.Difficulty = cleanHTMLText(dm[1])
			}

			questions = append(questions, q)
		}
	}

	// If structured extraction found nothing, try standalone question elements.
	if len(questions) == 0 {
		qMatches := htmlQuestionRegex.FindAllStringSubmatch(html, -1)
		for _, qm := range qMatches {
			if len(qm) < 2 {
				continue
			}
			qText := cleanHTMLText(qm[1])
			if qText == "" || seen[qText] || len(qText) < 10 {
				continue
			}
			seen[qText] = true
			questions = append(questions, interviewQuestion{
				ID:       fmt.Sprintf("html_%d", hashString(qText)),
				Question: qText,
			})
		}
	}

	// Last resort: look for quoted questions in interview description text.
	if len(questions) == 0 {
		qMatches := htmlQuotedQuestionRegex.FindAllStringSubmatch(html, 50)
		for _, qm := range qMatches {
			if len(qm) < 2 {
				continue
			}
			qText := cleanHTMLText(qm[1])
			if qText == "" || seen[qText] || len(qText) < 15 {
				continue
			}
			// Must look like a question or interview-related content.
			lower := strings.ToLower(qText)
			if !strings.Contains(lower, "?") && !looksLikeQuestion(lower) {
				continue
			}
			seen[qText] = true
			questions = append(questions, interviewQuestion{
				ID:       fmt.Sprintf("quoted_%d", hashString(qText)),
				Question: qText,
			})
		}
	}

	return questions
}

// --- Helpers ---

// firstString returns the first non-empty string value found under any of the given keys.
func firstString(obj map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := obj[k]; ok {
			switch s := v.(type) {
			case string:
				if s != "" {
					return s
				}
			case float64:
				return fmt.Sprintf("%.0f", s)
			}
		}
	}
	return ""
}

// walkJSON recursively walks a JSON object, calling fn for every key-value pair.
func walkJSON(obj interface{}, fn func(string, interface{})) {
	switch v := obj.(type) {
	case map[string]interface{}:
		for k, val := range v {
			fn(k, val)
			walkJSON(val, fn)
		}
	case []interface{}:
		for _, val := range v {
			walkJSON(val, fn)
		}
	}
}

// hashString produces a simple deterministic hash for dedup IDs.
func hashString(s string) uint32 {
	var h uint32
	for _, c := range s {
		h = h*31 + uint32(c)
	}
	return h
}

func cleanHTMLText(s string) string {
	s = strings.TrimSpace(s)
	// Decode common HTML entities.
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
		"&apos;", "'",
		"&ldquo;", "\"",
		"&rdquo;", "\"",
		"&lsquo;", "'",
		"&rsquo;", "'",
		"&mdash;", "--",
		"&ndash;", "-",
		"&nbsp;", " ",
	)
	s = replacer.Replace(s)
	// Collapse whitespace.
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func looksLikeQuestion(s string) bool {
	keywords := []string{
		"tell me", "describe", "explain", "what is", "what are", "how do",
		"how would", "why do", "design", "implement", "algorithm", "code",
		"system", "behavioral", "leadership", "conflict", "challenge",
		"strengths", "weakness", "experience with", "walk me through",
		"give an example", "time when", "most difficult",
	}
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
