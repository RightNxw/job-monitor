package intel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Parser uses a two-layer approach:
// 1. Rules classify the event type (free, instant)
// 2. Haiku extracts details, role, team, questions, location (only on matches)
type Parser struct {
	apiKey string
	model  string
}

func NewParser(apiKey string) *Parser {
	return &Parser{apiKey: apiKey, model: "claude-haiku-4-5-20251001"}
}

// ParsedSignal is the structured output.
type ParsedSignal struct {
	Company    *string `json:"company"`
	Event      string  `json:"event"`
	Details    string  `json:"details"`
	Confidence float64 `json:"confidence"`
	Role       string  `json:"role"`      // "SDE Intern", "SWE New Grad"
	Team       string  `json:"team"`      // "AWS", "Ads", "Search"
	Location   string  `json:"location"`  // "NYC", "Seattle"
	Questions  string  `json:"questions"` // "2 LC mediums, graph + DP"
	Round      string  `json:"round"`     // "OA", "phone screen", "final round"
	Timeline   string  `json:"timeline"`  // "applied 2 weeks ago, OA yesterday"
}

// ParseAlways sends directly to Haiku without rule-based pre-filtering.
// Use for sources like Instagram where OCR text is messy and rules miss companies.
func (p *Parser) ParseAlways(ctx context.Context, message string) (*ParsedSignal, error) {
	event, company := classifyByRules(message)
	if event == "" {
		event = "general"
	}
	signal, err := p.extractDetails(ctx, message, event, company)
	if err != nil {
		return &ParsedSignal{Confidence: 0.0}, err
	}
	if event != "general" {
		signal.Event = event
	}
	if company != "" && (signal.Company == nil || *signal.Company == "") {
		signal.Company = strPtr(company)
	}
	return signal, nil
}

// Parse classifies the message with rules, then extracts details with Haiku.
func (p *Parser) Parse(ctx context.Context, message string) (*ParsedSignal, error) {
	// Layer 1: rule-based classification
	event, company := classifyByRules(message)
	if event == "" {
		// Rules didn't match, skip entirely (don't waste API call)
		return &ParsedSignal{Confidence: 0.0}, nil
	}

	// Layer 2: Haiku extracts the details
	signal, err := p.extractDetails(ctx, message, event, company)
	if err != nil {
		// Haiku failed, return rule-based result without details
		return &ParsedSignal{
			Company:    strPtr(company),
			Event:      event,
			Details:    "",
			Confidence: 0.6,
		}, nil
	}

	// Override event if rules were confident
	if event != "general" {
		signal.Event = event
	}
	if company != "" && (signal.Company == nil || *signal.Company == "") {
		signal.Company = strPtr(company)
	}

	return signal, nil
}

// --- Layer 1: Rule-based classification ---

var companyPattern = regexp.MustCompile(`(?i)\b(amazon|aws|google|meta|facebook|apple|microsoft|nvidia|netflix|uber|lyft|airbnb|stripe|coinbase|palantir|databricks|bloomberg|goldman|citadel|jane\s*street|two\s*sigma|hrt|de\s*shaw|spacex|anduril|cloudflare|roblox|doordash|instacart|salesforce|adobe|oracle|intel|snap|pinterest|spotify|reddit|discord|figma|datadog|mongodb|robinhood|plaid|openai|anthropic|tesla|waymo|scale\s*ai|brex|okta|zscaler|twilio|shopify)\b`)

type rule struct {
	patterns []string
	event    string
}

var classificationRules = []rule{
	{patterns: []string{"got oa", "received oa", "got the oa", "oa today", "online assessment", "coding challenge", "hackerrank", "codesignal", "oa invite"}, event: EventOASent},
	{patterns: []string{"got offer", "received offer", "offer letter", "accepted offer", "got the offer", "offer for", "intern offer", "return offer"}, event: EventOffering},
	{patterns: []string{"got rejected", "rejection", "got the rejection", "rejected after", "no longer being considered", "ghosted"}, event: EventRejecting},
	{patterns: []string{"phone screen", "phone interview", "technical interview", "final round", "onsite", "virtual onsite", "hm round", "behavioral interview", "system design", "coding interview", "interview scheduled", "interview tomorrow", "interview today", "had my interview", "done interview", "just interviewed"}, event: EventInterviewing},
	{patterns: []string{"just applied", "application open", "applications open", "app is open", "portal open", "link to apply", "apply here", "apply now"}, event: EventAppsOpen},
	{patterns: []string{"positions filled", "no longer hiring", "closed the position", "hiring freeze", "not hiring"}, event: EventClosed},
	{patterns: []string{"lc easy", "lc medium", "lc hard", "leetcode", "asked me to solve", "coding question was", "they asked", "interview question"}, event: EventLCQuestion},
}

func classifyByRules(msg string) (event string, company string) {
	lower := strings.ToLower(msg)

	// Extract company name
	if match := companyPattern.FindString(msg); match != "" {
		company = normalizeCompany(match)
	}

	// Match event rules
	for _, r := range classificationRules {
		for _, pattern := range r.patterns {
			if strings.Contains(lower, pattern) {
				return r.event, company
			}
		}
	}

	// !process command is always relevant
	if strings.Contains(lower, "!process") {
		return "general", company
	}

	return "", company
}

func normalizeCompany(raw string) string {
	return NormalizeCompany(raw)
}

// --- Layer 2: Haiku detail extraction ---

const extractionPrompt = `Extract details from this hiring-related message. The event type is already classified as "%s" for company "%s".

Return JSON with:
- "company": normalized company name
- "event": "%s"
- "details": one-line summary
- "confidence": 0.0-1.0
- "role": specific role/position (e.g. "SDE Intern", "SWE New Grad", "MLE PhD Intern")
- "team": team or org if mentioned (e.g. "AWS", "Ads", "Search", "Alexa")
- "location": location if mentioned (e.g. "NYC", "Seattle", "Remote")
- "questions": interview questions or LC problems if mentioned (e.g. "2 LC mediums - graph traversal + DP", "system design for URL shortener")
- "round": interview round (e.g. "OA", "phone screen", "final round", "HM round")
- "timeline": any timing info (e.g. "applied 2 weeks ago", "OA to interview in 3 days")

Use empty string for fields not mentioned. Return ONLY valid JSON.`

func (p *Parser) extractDetails(ctx context.Context, message, event, company string) (*ParsedSignal, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("no API key")
	}

	prompt := fmt.Sprintf(extractionPrompt, event, company, event)

	reqBody := map[string]any{
		"model":      p.model,
		"max_tokens": 300,
		"system":     prompt,
		"messages": []map[string]string{
			{"role": "user", "content": message},
		},
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", p.apiKey)
	req.Header.Set("Anthropic-Version", "2023-06-01")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("anthropic API %d: %s", resp.StatusCode, string(respBody[:min(len(respBody), 300)]))
	}

	var apiResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("parse API response: %w", err)
	}

	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	// Strip markdown code fences if Haiku wraps the JSON
	rawText := strings.TrimSpace(apiResp.Content[0].Text)
	if strings.HasPrefix(rawText, "```") {
		lines := strings.Split(rawText, "\n")
		// Remove first and last lines (```json and ```)
		if len(lines) >= 3 {
			rawText = strings.Join(lines[1:len(lines)-1], "\n")
		}
		rawText = strings.TrimSpace(rawText)
	}

	var signal ParsedSignal
	if err := json.Unmarshal([]byte(rawText), &signal); err != nil {
		return nil, fmt.Errorf("parse output: %w (raw: %s)", err, rawText[:min(len(rawText), 200)])
	}

	return &signal, nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
