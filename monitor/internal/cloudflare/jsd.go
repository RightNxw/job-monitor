//go:build solver

package cloudflare

// JSD solver: pure HTTP request-based Cloudflare JSD challenge solver.
// No browser, no V8, no Node.js -- just HTTP requests with proper TLS fingerprinting.
//
// Flow:
//   1. Fetch challenge page -> extract r, t params
//   2. Fetch /cdn-cgi/challenge-platform/scripts/jsd/main.js (follows 302)
//   3. Deobfuscate script -> extract ve, path, alphabet
//   4. Build fingerprint payload -> LZ-compress
//   5. POST to /cdn-cgi/challenge-platform/h/{ve}/jsd/oneshot{path}{r}
//   6. Return cf_clearance cookie

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iancoleman/orderedmap"
)

// deobCacheEntry stores cached JSD deobfuscation results to skip
// redundant script fetches + expensive 5-pass AST deobfuscation.
type deobCacheEntry struct {
	result   *DeobfuscateResult
	cachedAt time.Time
}

var (
	deobCache    sync.Map // host → *deobCacheEntry
	deobCacheTTL = 30 * time.Minute
)

// jsdSolver solves Cloudflare JSD challenges via pure HTTP requests.
type jsdSolver struct {
	client *httpClient
	debug  bool
}

// newJSDSolver creates a new JSD solver using the given TLS client.
func newJSDSolver(client *httpClient, debug bool) *jsdSolver {
	return &jsdSolver{
		client: client,
		debug:  debug,
	}
}

// solve attempts to solve a Cloudflare JSD challenge for the given URL.
// If challengeHTML is non-empty, skips the initial page fetch (saves ~200ms).
func (s *jsdSolver) solve(ctx context.Context, targetURL, challengeHTML string) (*solveResult, error) {
	start := time.Now()

	// Parse host from URL.
	host := extractHost(targetURL)
	origin := extractOrigin(targetURL)

	// Step 1: Use pre-fetched HTML or fetch the challenge page.
	var body string
	if challengeHTML != "" {
		body = challengeHTML
		s.log("Using pre-fetched challenge HTML (%d bytes)", len(body))
	} else {
		s.log("Fetching challenge page: %s", targetURL)
		resp, err := s.client.Get(ctx, targetURL)
		if err != nil {
			return s.fail(start, "fetch challenge page: %v", err)
		}
		defer resp.Body.Close()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return s.fail(start, "read challenge body: %v", err)
		}
		body = string(bodyBytes)
		s.log("Challenge page: status=%d, body=%d bytes", resp.StatusCode, len(body))
	}

	// Extract r and t from the HTML.
	r, t, err := extractRT(body)
	if err != nil {
		if challengeHTML == "" {
			// Fresh fetch with no JSD params, page might already be cleared.
			return &solveResult{
				success:     true,
				cookies:     s.client.AllCookies(targetURL),
				userAgent:   s.client.UserAgent(),
				profile:     s.client.ProfileName(),
				solveTimeMs: time.Since(start).Milliseconds(),
			}, nil
		}
		return s.fail(start, "extract JSD params: %v", err)
	}
	s.log("JSD params: r=%s, t=%s", r, t[:min(20, len(t))])

	// Step 2: Get deobfuscated script params (cached or fresh fetch + deobfuscate).
	// The deob result (ve, path, alphabet) is deployment-specific and changes slowly,
	// so caching saves both the script fetch (~200ms) and 5-pass AST deobfuscation (~100-500ms).
	var deobResult *DeobfuscateResult
	if cached, ok := deobCache.Load(host); ok {
		entry := cached.(*deobCacheEntry)
		if time.Since(entry.cachedAt) < deobCacheTTL {
			deobResult = entry.result
			s.log("Deob cache hit for %s (ve=%s, age=%s)", host, deobResult.Ve, time.Since(entry.cachedAt).Round(time.Second))
		}
	}

	if deobResult == nil {
		scriptURL := fmt.Sprintf("%s/cdn-cgi/challenge-platform/scripts/jsd/main.js", origin)
		s.log("Fetching JSD script: %s", scriptURL)
		scriptResp, err := s.client.Get(ctx, scriptURL)
		if err != nil {
			return s.fail(start, "fetch JSD script: %v", err)
		}
		defer scriptResp.Body.Close()

		scriptBytes, err := io.ReadAll(scriptResp.Body)
		if err != nil {
			return s.fail(start, "read JSD script: %v", err)
		}
		scriptSrc := string(scriptBytes)
		s.log("JSD script: %d bytes, status=%d", len(scriptSrc), scriptResp.StatusCode)

		// Deobfuscate with retry, script rotates and some versions may fail.
		// Wrapped with 10s timeout because go-fast can hang on certain obfuscation patterns.
		for attempt := 0; attempt < 3; attempt++ {
			s.log("Deobfuscating JSD script (attempt %d)...", attempt+1)

			type deobRes struct {
				result *DeobfuscateResult
				err    error
			}
			ch := make(chan deobRes, 1)
			go func() {
				var r *DeobfuscateResult
				var e error
				if s.debug {
					r, e = DeobfuscateAndDump(scriptSrc, "/tmp/jsd_deobfuscated.js")
				} else {
					r, e = Deobfuscate(scriptSrc)
				}
				ch <- deobRes{r, e}
			}()

			select {
			case res := <-ch:
				deobResult, err = res.result, res.err
			case <-time.After(10 * time.Second):
				err = fmt.Errorf("deobfuscation timed out after 10s")
			case <-ctx.Done():
				return s.fail(start, "context cancelled during deobfuscation")
			}

			if err == nil {
				break
			}
			s.log("Deobfuscation attempt %d failed: %v", attempt+1, err)
			if attempt < 2 {
				s.log("Re-fetching JSD script...")
				scriptResp2, err2 := s.client.Get(ctx, scriptURL)
				if err2 != nil {
					return s.fail(start, "re-fetch JSD script: %v", err2)
				}
				scriptBytes2, err2 := io.ReadAll(scriptResp2.Body)
				scriptResp2.Body.Close()
				if err2 != nil {
					return s.fail(start, "re-read JSD script: %v", err2)
				}
				scriptSrc = string(scriptBytes2)
				s.log("Re-fetched JSD script: %d bytes", len(scriptSrc))
			}
		}
		if err != nil {
			return s.fail(start, "deobfuscate JSD script after 3 attempts: %v", err)
		}

		// Cache successful deobfuscation.
		deobCache.Store(host, &deobCacheEntry{result: deobResult, cachedAt: time.Now()})
		s.log("Cached deob params for %s", host)
	}
	s.log("Deob params: ve=%s, path=%s, alphabet=%s...", deobResult.Ve, deobResult.Path, deobResult.Alphabet[:min(20, len(deobResult.Alphabet))])

	// Step 4: Build fingerprint payload.
	s.log("Building fingerprint payload...")
	fp := GenerateFingerprint(host, targetURL, "s")
	payload := buildJSDPayload(t, fp)
	jsonBytes, err := payload.MarshalJSON()
	if err != nil {
		return s.fail(start, "marshal fingerprint: %v", err)
	}
	jsonStr := strings.ReplaceAll(string(jsonBytes), "\n", "")
	s.log("Payload JSON: %d bytes", len(jsonStr))

	// Step 5: LZ-compress and POST.
	compressed := LZCompress(jsonStr, deobResult.Alphabet)
	s.log("Compressed payload: %d chars", len(compressed))

	// Path from deobfuscation already includes /jsd/oneshot/... prefix.
	endpoint := fmt.Sprintf("%s/cdn-cgi/challenge-platform/h/%s%s%s",
		origin, deobResult.Ve, deobResult.Path, r)
	s.log("POSTing to: %s", endpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(compressed))
	if err != nil {
		return s.fail(start, "create POST request: %v", err)
	}

	// Set headers matching Chrome 144 XHR pattern.
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", origin)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Referer", targetURL)

	postResp, err := s.client.Do(req)
	if err != nil {
		return s.fail(start, "POST JSD payload: %v", err)
	}
	defer postResp.Body.Close()

	respBody, _ := io.ReadAll(postResp.Body)
	s.log("POST response: status=%d, body=%d bytes", postResp.StatusCode, len(respBody))

	// Log Set-Cookie if present.
	if sc := postResp.Header.Get("Set-Cookie"); sc != "" {
		s.log("Set-Cookie: %s", sc)
	}

	// Step 6: Check for cf_clearance.
	clearance, ok := s.client.GetCookieValue(targetURL, "cf_clearance")
	if !ok || clearance == "" {
		// Try a follow-up request to pick up the cookie.
		s.log("No cf_clearance yet, making follow-up request...")
		followResp, err := s.client.Get(ctx, targetURL)
		if err != nil {
			return s.fail(start, "follow-up request: %v", err)
		}
		defer followResp.Body.Close()
		io.ReadAll(followResp.Body) // drain

		s.log("Follow-up: status=%d", followResp.StatusCode)
		clearance, ok = s.client.GetCookieValue(targetURL, "cf_clearance")
		if !ok || clearance == "" {
			return s.fail(start, "cf_clearance not obtained after JSD solve (POST status=%d)", postResp.StatusCode)
		}
	}

	s.log("Got cf_clearance: %s... (took %dms)", clearance[:min(20, len(clearance))], time.Since(start).Milliseconds())

	return &solveResult{
		success:     true,
		cfClearance: clearance,
		cookies:     s.client.AllCookies(targetURL),
		userAgent:   s.client.UserAgent(),
		profile:     s.client.ProfileName(),
		solveTimeMs: time.Since(start).Milliseconds(),
	}, nil
}

// buildJSDPayload constructs the ordered JSON payload for the JSD oneshot POST.
func buildJSDPayload(t string, fp *orderedmap.OrderedMap) *orderedmap.OrderedMap {
	payload := orderedmap.New()
	payload.Set("t", decodeTimestamp(t))
	payload.Set("lhr", "about:blank")
	payload.Set("api", false)
	payload.Set("payload", fp)
	return payload
}

// decodeTimestamp decodes the base64-encoded timestamp from the challenge page.
func decodeTimestamp(t string) int64 {
	decoded, err := base64.StdEncoding.DecodeString(t)
	if err != nil {
		return 0
	}
	var ts int64
	fmt.Sscanf(string(decoded), "%d", &ts)
	return ts
}

// extractRT finds the r and t parameters from the JSD beacon config in HTML.
func extractRT(html string) (string, string, error) {
	// Match {r:'ray',t:'timestamp'} pattern.
	idx := strings.Index(html, "r:'")
	if idx == -1 {
		return "", "", fmt.Errorf("JSD beacon config not found in HTML")
	}

	// Parse r value.
	rStart := idx + 3
	rEnd := strings.Index(html[rStart:], "'")
	if rEnd == -1 {
		return "", "", fmt.Errorf("r value not terminated")
	}
	r := html[rStart : rStart+rEnd]

	// Parse t value.
	tIdx := strings.Index(html[rStart+rEnd:], "t:'")
	if tIdx == -1 {
		return "", "", fmt.Errorf("t value not found")
	}
	tStart := rStart + rEnd + tIdx + 3
	tEnd := strings.Index(html[tStart:], "'")
	if tEnd == -1 {
		return "", "", fmt.Errorf("t value not terminated")
	}
	t := html[tStart : tStart+tEnd]

	return r, t, nil
}

func (s *jsdSolver) fail(start time.Time, format string, args ...interface{}) (*solveResult, error) {
	msg := fmt.Sprintf(format, args...)
	s.log("FAIL: %s", msg)
	return &solveResult{
		success:     false,
		errMsg:      msg,
		userAgent:   s.client.UserAgent(),
		profile:     s.client.ProfileName(),
		solveTimeMs: time.Since(start).Milliseconds(),
	}, fmt.Errorf("%s", msg)
}

func (s *jsdSolver) log(format string, args ...interface{}) {
	if s.debug {
		log.Printf("[cf-jsd] "+format, args...)
	}
}
