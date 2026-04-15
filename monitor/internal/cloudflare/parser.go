//go:build solver

package cloudflare

// Challenge detection and HTML parsing for Cloudflare challenges.

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// Match _cf_chl_opt assignment in inline script (capture everything between { and } inclusive).
	cfChlOptRe = regexp.MustCompile(`window\._cf_chl_opt\s*=\s*\{([^}]+)\}`)

	// Match the full raw assignment including surrounding text up to the semicolon.
	cfChlOptRawRe = regexp.MustCompile(`window\._cf_chl_opt\s*=\s*\{[^}]+\}`)

	// Match individual key-value pairs inside _cf_chl_opt.
	kvStringRe = regexp.MustCompile(`(\w+)\s*:\s*'([^']*)'`)
	kvQuotedRe = regexp.MustCompile(`(\w+)\s*:\s*"([^"]*)"`)

	// Match challenge-platform script tag (HTML src attribute).
	challengeScriptRe = regexp.MustCompile(`<script\s+src="(/cdn-cgi/challenge-platform/[^"]+)"`)

	// Match challenge-platform script URL assigned dynamically in JS.
	// Covers both: a.src = '/cdn-cgi/...'  and  a.src='/cdn-cgi/...'
	dynamicScriptRe = regexp.MustCompile(`\.src\s*=\s*'(/cdn-cgi/challenge-platform/[^']+)'`)

	// Match challenge-platform script injected via JS (JSD pattern).
	// e.g. a.src='/cdn-cgi/challenge-platform/scripts/jsd/main.js'
	jsdScriptRe = regexp.MustCompile(`\.src='(/cdn-cgi/challenge-platform/scripts/[^']+)'`)

	// Match the CF beacon config injected alongside JSD: {r:'ray',t:'timestamp'}
	jsdConfigRe = regexp.MustCompile(`\{r:'([^']+)',t:'([^']+)'\}`)

	// Match turnstile script tag.
	turnstileScriptRe = regexp.MustCompile(`<script\s+src="(https://challenges\.cloudflare\.com/turnstile/[^"]+)"`)
)

// ParseChallengeHTML extracts Cloudflare challenge config from the 403 HTML response.
// It handles both the managed challenge (_cf_chl_opt) and JSD (bot management beacon) patterns.
func ParseChallengeHTML(html, baseURL string) (*ChallengeConfig, error) {
	config := &ChallengeConfig{
		BaseURL: baseURL,
	}

	origin := extractOrigin(baseURL)

	// Try managed challenge first (_cf_chl_opt).
	match := cfChlOptRe.FindStringSubmatch(html)
	if match != nil {
		optBody := match[1]
		kvs := parseKVPairs(optBody)

		config.CRay = kvs["cRay"]
		config.CH = kvs["cH"]
		config.CUPMDTk = kvs["cUPMDTk"]
		config.MD = kvs["md"]
		config.MDRD = kvs["mdrd"]
		config.CType = kvs["cType"]
		config.CvId = kvs["cvId"]
		config.CZone = kvs["cZone"]

		// Extract the raw config assignment for direct V8 injection.
		if rawMatch := cfChlOptRawRe.FindString(html); rawMatch != "" {
			config.RawConfigJS = rawMatch + ";"
		}

		if config.CRay == "" {
			return nil, fmt.Errorf("cRay not found in _cf_chl_opt")
		}

		// Extract challenge-platform script URL.
		// Try static <script src="..."> first, then dynamic a.src = '...' assignment.
		if scriptMatch := challengeScriptRe.FindStringSubmatch(html); scriptMatch != nil {
			config.ScriptURL = origin + scriptMatch[1]
		} else if scriptMatch := dynamicScriptRe.FindStringSubmatch(html); scriptMatch != nil {
			config.ScriptURL = origin + scriptMatch[1]
		}

		return config, nil
	}

	// Try JSD pattern (challenge-platform/scripts/jsd).
	if jsdMatch := jsdScriptRe.FindStringSubmatch(html); jsdMatch != nil {
		config.ScriptURL = origin + jsdMatch[1]
		config.CType = "jsd"

		// Extract ray and timestamp from beacon config.
		if cfgMatch := jsdConfigRe.FindStringSubmatch(html); cfgMatch != nil {
			config.CRay = cfgMatch[1]
			config.CH = cfgMatch[2] // base64 timestamp
		}

		return config, nil
	}

	// Try finding any challenge-platform reference as a fallback.
	if scriptMatch := challengeScriptRe.FindStringSubmatch(html); scriptMatch != nil {
		config.ScriptURL = origin + scriptMatch[1]
		config.CType = "unknown"
		return config, nil
	}

	return nil, fmt.Errorf("no Cloudflare challenge detected in HTML")
}

// DetectChallengeType identifies which Cloudflare protection is present in the response.
func DetectChallengeType(statusCode int, body string, headers map[string]string) ChallengeType {
	// Check Cf-Mitigated header (definitive signal for managed challenge).
	if v, ok := headers["cf-mitigated"]; ok && strings.Contains(strings.ToLower(v), "challenge") {
		return ChallengeTypeManaged
	}

	if strings.Contains(body, "_cf_chl_opt") {
		return ChallengeTypeManaged
	}

	if strings.Contains(body, "challenges.cloudflare.com/turnstile") {
		return ChallengeTypeTurnstile
	}

	if strings.Contains(body, "challenge-platform/scripts/jsd") ||
		strings.Contains(body, "challenge-platform") {
		return ChallengeTypeJSD
	}

	return ChallengeTypeNone
}

// IsChallengeResponse checks if an HTTP response looks like a Cloudflare-protected page.
func IsChallengeResponse(statusCode int, body string) bool {
	if statusCode != 403 {
		return false
	}
	return strings.Contains(body, "_cf_chl_opt") ||
		strings.Contains(body, "challenge-platform") ||
		strings.Contains(body, "cf-challenge") ||
		strings.Contains(body, "cdn-cgi/challenge-platform")
}

// ExtractTurnstileConfig checks if the page uses Turnstile instead of managed challenge.
func ExtractTurnstileConfig(html string) (siteKey string, found bool) {
	match := turnstileScriptRe.FindStringSubmatch(html)
	if match == nil {
		return "", false
	}

	skRe := regexp.MustCompile(`sitekey['":\s]+['"]([0-9a-zA-Z_-]+)['"]`)
	skMatch := skRe.FindStringSubmatch(html)
	if skMatch != nil {
		return skMatch[1], true
	}

	return "", true
}

func parseKVPairs(s string) map[string]string {
	kvs := make(map[string]string)
	for _, m := range kvStringRe.FindAllStringSubmatch(s, -1) {
		kvs[m[1]] = m[2]
	}
	for _, m := range kvQuotedRe.FindAllStringSubmatch(s, -1) {
		if _, exists := kvs[m[1]]; !exists {
			kvs[m[1]] = m[2]
		}
	}
	return kvs
}
