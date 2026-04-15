//go:build solver

package cloudflare

// V8-based managed challenge solver for Cloudflare.
//
// Flow:
//   1. Fetch challenge page (or use pre-fetched HTML)
//   2. Parse _cf_chl_opt config from HTML
//   3. Fetch the challenge-platform orchestrate script
//   4. Patch the script for resilience
//   5. Set up V8 engine with fake DOM + fetch interceptor
//   6. Inject config, run script with event loop
//   7. Collect cf_clearance cookie

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/RightNxw/job-monitor/monitor/internal/engine"
)

// managedSolver solves Cloudflare managed challenges via V8 JS execution.
type managedSolver struct {
	client *httpClient
	debug  bool
}

// newManagedSolver creates a new V8-based managed challenge solver.
func newManagedSolver(client *httpClient, debug bool) *managedSolver {
	return &managedSolver{
		client: client,
		debug:  debug,
	}
}

// solve attempts to solve a Cloudflare managed challenge using V8.
// If challengeHTML is non-empty, skips the initial page fetch (saves ~200ms).
func (s *managedSolver) solve(ctx context.Context, targetURL, challengeHTML string) (*solveResult, error) {
	start := time.Now()

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

		s.log("Challenge page: status=%d, body=%d bytes, Cf-Mitigated=%s",
			resp.StatusCode, len(body), resp.Header.Get("Cf-Mitigated"))

		// If 200, no challenge needed.
		if resp.StatusCode == 200 && !strings.Contains(body, "_cf_chl_opt") {
			return &solveResult{
				success:     true,
				cookies:     s.client.AllCookies(targetURL),
				userAgent:   s.client.UserAgent(),
				profile:     s.client.ProfileName(),
				solveTimeMs: time.Since(start).Milliseconds(),
			}, nil
		}
	}

	// Step 2: Parse the challenge HTML.
	s.log("Parsing challenge HTML...")
	config, err := ParseChallengeHTML(body, targetURL)
	if err != nil {
		return s.fail(start, "parse challenge: %v", err)
	}
	s.log("Challenge config: cRay=%s, type=%s, scriptURL=%s", config.CRay, config.CType, config.ScriptURL)

	if config.ScriptURL == "" {
		return s.fail(start, "no challenge script URL found")
	}

	// Step 3: Fetch the challenge-platform script.
	s.log("Fetching challenge script: %s", config.ScriptURL)
	scriptResp, err := s.client.Get(ctx, config.ScriptURL)
	if err != nil {
		return s.fail(start, "fetch challenge script: %v", err)
	}
	defer scriptResp.Body.Close()

	scriptBytes, err := io.ReadAll(scriptResp.Body)
	if err != nil {
		return s.fail(start, "read challenge script: %v", err)
	}
	challengeJS := string(scriptBytes)
	s.log("Challenge script length: %d bytes", len(challengeJS))

	if s.debug {
		_ = writeDebugFile("/tmp/orchestrate_script.js", scriptBytes)
	}

	// Patch the challenge script for resilience.
	noPatch := os.Getenv("NO_PATCH") == "1"
	if !noPatch {
		challengeJS = patchCallOpcodeGuard(challengeJS, s.debug)
		challengeJS = patchVMErrorResilience(challengeJS, s.debug)
		if s.debug {
			challengeJS = patchVMInitLogging(challengeJS, s.debug)
			challengeJS = patchCallOpcodeDebug(challengeJS, s.debug)
		}
	} else if s.debug {
		log.Printf("[cf-managed] ALL SCRIPT PATCHES DISABLED (NO_PATCH=1)")
	}

	if s.debug {
		_ = writeDebugFile("/tmp/orchestrate_patched.js", []byte(challengeJS))
	}

	// Step 4: Set up V8 engine with fake DOM.
	domCfg := engine.DefaultDOMConfig(s.client.UserAgent(), targetURL)
	// For managed challenges, compress Date.now() so the parent's overrun checker
	// (10s timeout via Date.now() comparison) doesn't fire during ~150s VM computation.
	if config.CType != "jsd" {
		domCfg.TimeDilation = 0.01
	}
	eng, err := engine.NewEngine(domCfg, s.debug)
	if err != nil {
		return s.fail(start, "create V8 engine: %v", err)
	}
	defer eng.Close()

	// Compute the origin for resolving relative URLs.
	origin := targetURL
	if idx := strings.Index(origin, "//"); idx != -1 {
		if slashIdx := strings.Index(origin[idx+2:], "/"); slashIdx != -1 {
			origin = origin[:idx+2+slashIdx]
		}
	}

	// Step 5: Wire up fetch interceptor (used by both fetch() and XHR).
	eng.SetFetchHandler(func(fetchURL string, opts map[string]interface{}) (string, int, map[string]string, error) {
		// Resolve relative URLs.
		if strings.HasPrefix(fetchURL, "/") {
			fetchURL = origin + fetchURL
		}

		// Determine method.
		method := http.MethodGet
		if m, ok := opts["method"].(string); ok && m != "" {
			method = strings.ToUpper(m)
		} else if raw, ok := opts["raw"].(string); ok {
			if strings.Contains(raw, "POST") || strings.Contains(raw, "post") {
				method = http.MethodPost
			}
		}

		// Determine body.
		var reqBody io.Reader
		var bodyStr string
		if b, ok := opts["body"].(string); ok && b != "" {
			bodyStr = b
			reqBody = strings.NewReader(b)
		}

		s.log("JS %s %s (body=%v, bodyLen=%d)", method, fetchURL, reqBody != nil, len(bodyStr))
		if method == http.MethodPost && len(bodyStr) > 0 {
			preview := bodyStr
			if len(preview) > 500 {
				preview = preview[:500] + "..."
			}
			s.log("JS POST body: %s", preview)

			if s.debug && strings.Contains(fetchURL, "/flow/ov1") {
				dumpFile := fmt.Sprintf("/tmp/flow_post_%d.txt", time.Now().UnixMilli())
				_ = writeDebugFile(dumpFile, []byte(bodyStr))
				s.log("Dumped flow POST body to %s (%d bytes)", dumpFile, len(bodyStr))
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, fetchURL, reqBody)
		if err != nil {
			return "", 0, nil, fmt.Errorf("create request: %w", err)
		}

		// Forward headers from JS.
		contentTypeSet := false
		if hdrs, ok := opts["headers"].(map[string]interface{}); ok {
			for k, v := range hdrs {
				var vs string
				switch tv := v.(type) {
				case string:
					vs = tv
				case float64:
					vs = fmt.Sprintf("%g", tv)
				case bool:
					vs = fmt.Sprintf("%t", tv)
				default:
					vs = fmt.Sprintf("%v", v)
				}
				req.Header.Set(k, vs)
				if strings.EqualFold(k, "content-type") {
					contentTypeSet = true
				}
			}
		}
		if method == http.MethodPost && reqBody != nil && !contentTypeSet {
			req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
		}

		// Chrome adds sec-fetch-storage-access: active for third-party iframe contexts.
		if strings.Contains(fetchURL, "challenges.cloudflare.com") {
			if req.Header.Get("Sec-Fetch-Storage-Access") == "" {
				req.Header.Set("Sec-Fetch-Storage-Access", "active")
			}
		}

		fetchResp, err := s.client.Do(req)
		if err != nil {
			return "", 0, nil, fmt.Errorf("fetch %s: %w", fetchURL, err)
		}
		defer fetchResp.Body.Close()

		fetchBody, err := io.ReadAll(fetchResp.Body)
		if err != nil {
			return "", 0, nil, fmt.Errorf("read response: %w", err)
		}

		headers := make(map[string]string)
		for k, v := range fetchResp.Header {
			headers[strings.ToLower(k)] = strings.Join(v, ", ")
		}

		s.log("JS response: %s %s -> %d (%d bytes)", method, fetchURL, fetchResp.StatusCode, len(fetchBody))

		// Patch and save large Turnstile scripts.
		if strings.Contains(fetchURL, "turnstile") && len(fetchBody) > 100000 && !noPatch {
			bodyStr := string(fetchBody)
			bodyStr = patchVMErrorResilience(bodyStr, s.debug)
			if s.debug {
				bodyStr = patchVMInitLogging(bodyStr, s.debug)
				bodyStr = patchCallOpcodeDebug(bodyStr, s.debug)
			}
			fetchBody = []byte(bodyStr)
			if s.debug {
				os.WriteFile("/tmp/turnstile_iframe_script.js", fetchBody, 0644)
				s.log("Patched + saved Turnstile iframe script (%d bytes)", len(fetchBody))
			}
		}

		if setCookie := fetchResp.Header.Get("Set-Cookie"); setCookie != "" {
			s.log("JS response Set-Cookie: %s", setCookie)
		}
		if fetchResp.StatusCode != 200 && len(fetchBody) > 0 {
			preview := string(fetchBody)
			if len(preview) > 500 {
				preview = preview[:500] + "..."
			}
			s.log("JS error response body: %s", preview)
		}
		if method == "POST" && len(fetchBody) > 0 && len(fetchBody) < 5000 {
			bodyStr := string(fetchBody)
			if len(bodyStr) > 1000 {
				bodyStr = bodyStr[:1000]
			}
			s.log("JS POST response body (%d bytes): %s", len(fetchBody), bodyStr)
		}
		return string(fetchBody), fetchResp.StatusCode, headers, nil
	})

	// Step 5b: Wire up Worker code patcher.
	eng.SetPatchHandler(func(code string) string {
		s.log("Patching Worker eval code (%d bytes)...", len(code))
		if s.debug {
			os.WriteFile("/tmp/worker_eval_raw.js", []byte(code), 0644)
		}
		if !noPatch {
			code = patchVMErrorResilience(code, s.debug)
		}
		if s.debug {
			os.WriteFile("/tmp/worker_eval_patched.js", []byte(code), 0644)
			s.log("Saved patched Worker eval code (%d bytes)", len(code))
		}
		return code
	})

	// Step 6: Inject the challenge config then run the challenge script with event loop.
	var configJS string
	if config.CType == "jsd" {
		configJS = fmt.Sprintf(`window.__CF$cv$params = {r: %q, t: %q};`, config.CRay, config.CH)
		s.log("Injecting JSD config: __CF$cv$params (r=%s, t=%s)", config.CRay, config.CH)
	} else if config.RawConfigJS != "" {
		configJS = config.RawConfigJS
		s.log("Using raw config injection (%d bytes)", len(configJS))
		configJS += `
			window._cf_chl_opt.cOgUHash = location.hash === '' && location.href.indexOf('#') !== -1 ? '#' : location.hash;
			window._cf_chl_opt.cOgUQuery = location.search === '' && location.href.slice(0, location.href.length - (window._cf_chl_opt.cOgUHash || '').length).indexOf('?') !== -1 ? '?' : location.search;
		`
	} else {
		configJS = fmt.Sprintf(`
			window._cf_chl_opt = {
				cvId: %q,
				cZone: %q,
				cType: %q,
				cRay: %q,
				cH: %q,
				cUPMDTk: %q,
				md: %q,
				mdrd: %q
			};
		`, config.CvId, config.CZone, config.CType, config.CRay,
			config.CH, config.CUPMDTk, config.MD, config.MDRD)
		configJS += `
			window._cf_chl_opt.cOgUHash = location.hash === '' && location.href.indexOf('#') !== -1 ? '#' : location.hash;
			window._cf_chl_opt.cOgUQuery = location.search === '' && location.href.slice(0, location.href.length - (window._cf_chl_opt.cOgUHash || '').length).indexOf('?') !== -1 ? '?' : location.search;
		`
	}

	s.log("Injecting challenge config...")
	if _, err := eng.ExecuteScript(ctx, configJS, "config.js"); err != nil {
		return s.fail(start, "inject challenge config: %v", err)
	}

	// Run the challenge script with the event loop to handle setTimeout chains.
	s.log("Executing challenge script with event loop...")
	execCtx, cancel := context.WithTimeout(ctx, 1860*time.Second)
	defer cancel()

	err = eng.RunEventLoop(execCtx, challengeJS, "challenge-platform.js")
	if err != nil {
		s.log("Event loop ended: %v", err)
	}

	// Step 7: Check if we got cf_clearance.
	s.log("All cookies after challenge: %v", s.client.AllCookies(targetURL))

	clearance, ok := s.client.GetCookieValue(targetURL, "cf_clearance")
	if !ok || clearance == "" {
		// Try a follow-up request.
		s.log("No cf_clearance yet, making follow-up request...")
		followResp, err := s.client.Get(ctx, targetURL)
		if err != nil {
			return s.fail(start, "follow-up request: %v", err)
		}
		defer followResp.Body.Close()

		s.log("Follow-up response: status=%d, Cf-Mitigated=%s", followResp.StatusCode, followResp.Header.Get("Cf-Mitigated"))
		s.log("Follow-up cookies after: %v", s.client.AllCookies(targetURL))

		clearance, ok = s.client.GetCookieValue(targetURL, "cf_clearance")
		if !ok || clearance == "" {
			return s.fail(start, "cf_clearance not obtained after challenge execution")
		}
	}

	s.log("Got cf_clearance: %s...", clearance[:min(20, len(clearance))])

	// Verify: re-request the target with cf_clearance to confirm bypass.
	s.log("Verifying cf_clearance with follow-up request...")
	verifyResp, verifyErr := s.client.Get(ctx, targetURL)
	if verifyErr != nil {
		s.log("Verification request failed: %v", verifyErr)
	} else {
		verifyBody, _ := io.ReadAll(verifyResp.Body)
		verifyResp.Body.Close()
		isChal := IsChallengeResponse(verifyResp.StatusCode, string(verifyBody))
		s.log("Verification: status=%d, body=%d bytes, isChallenge=%v, Cf-Mitigated=%s",
			verifyResp.StatusCode, len(verifyBody), isChal, verifyResp.Header.Get("Cf-Mitigated"))
	}

	return &solveResult{
		success:     true,
		cfClearance: clearance,
		cookies:     s.client.AllCookies(targetURL),
		userAgent:   s.client.UserAgent(),
		profile:     s.client.ProfileName(),
		solveTimeMs: time.Since(start).Milliseconds(),
	}, nil
}

func (s *managedSolver) fail(start time.Time, format string, args ...interface{}) (*solveResult, error) {
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

func (s *managedSolver) log(format string, args ...interface{}) {
	if s.debug {
		log.Printf("[cf-managed] "+format, args...)
	}
}

func writeDebugFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}

// --- Script patching functions ---
// These patch Cloudflare's obfuscated challenge scripts for resilience and debugging.
// Every patch exists for a reason -- do NOT simplify or remove them.

// callOpcodeFullReV1 captures the FULL CALL opcode ternary including both branches.
var callOpcodeFullReV1 = regexp.MustCompile(
	`(\w+)===void 0\?(\w+)\[(\w+\(\w+\.\w+\))\]\(null,(\w+)\):(\w+)\[(\w+)\]\[(\w+\(\w+\.\w+\))\]\((\w+),(\w+)\)`,
)

var callOpcodeFullReV1Rev = regexp.MustCompile(
	`void 0===(\w+)\?(\w+)\[(\w+\(\w+\.\w+\))\]\(null,(\w+)\):(\w+)\[(\w+)\]\[(\w+\(\w+\.\w+\))\]\((\w+),(\w+)\)`,
)

var callOpcodeFullReV2 = regexp.MustCompile(
	`(\w+)\[(\w+\(\w+\.\w+\))\]\((\w+),void 0\)\?(\w+)\[(\w+\(\w+\.\w+\))\]\(null,(\w+)\):(\w+)\[(\w+)\]\[(\w+\(\w+\.\w+\))\]\((\w+),(\w+)\)`,
)

func patchCallOpcodeDebug(script string, debug bool) string {
	applyDebugFull := func(re *regexp.Regexp, label string) (string, bool) {
		matches := re.FindAllStringSubmatch(script, -1)
		if len(matches) == 0 {
			return script, false
		}
		patched := re.ReplaceAllStringFunc(script, func(match string) string {
			sub := re.FindStringSubmatch(match)
			if sub == nil {
				return match
			}
			b := sub[5]
			c := sub[6]
			lookup := sub[7]
			b3 := sub[8]
			args2 := sub[9]
			oldMethodCall := b + "[" + c + "][" + lookup + "](" + b3 + "," + args2 + ")"
			newMethodCall := fmt.Sprintf(
				`(%s[%s]===void 0&&console.log('[CALL-MISS] b='+typeof %s+' c='+String(%s).substring(0,100)+' keys='+(typeof %s==='object'?Object.keys(%s).slice(0,30).join(','):'N/A')+' b.constructor='+(%s&&%s.constructor?%s.constructor.name:'?')),%s)`,
				b, c, b, c, b, b, b, b, b, oldMethodCall,
			)
			return strings.Replace(match, oldMethodCall, newMethodCall, 1)
		})
		if debug {
			log.Printf("[cf-managed] Added CALL debug logging (%s) to %d opcode(s)", label, len(matches))
		}
		return patched, true
	}

	totalPatched := 0
	if patched, ok := applyDebugFull(callOpcodeFullReV1, "V1"); ok {
		script = patched
		totalPatched++
	}
	if patched, ok := applyDebugFull(callOpcodeFullReV1Rev, "V1Rev"); ok {
		script = patched
		totalPatched++
	}

	if totalPatched == 0 && debug {
		log.Printf("[cf-managed] CALL opcode pattern not found")
	}
	return script
}

func patchCallOpcodeGuard(script string, debug bool) string {
	type v1Variant struct {
		re    *regexp.Regexp
		label string
	}
	v1Variants := []v1Variant{
		{callOpcodeFullReV1, "V1"},
		{callOpcodeFullReV1Rev, "V1Rev"},
	}

	totalPatched := 0
	for _, v := range v1Variants {
		matches := v.re.FindAllStringSubmatch(script, -1)
		if len(matches) == 0 {
			continue
		}

		script = v.re.ReplaceAllStringFunc(script, func(match string) string {
			sub := v.re.FindStringSubmatch(match)
			if sub == nil {
				return match
			}
			b := sub[1]
			c := sub[2]
			lookup1 := sub[3]
			args1 := sub[4]
			b2 := sub[5]
			c2 := sub[6]
			lookup2 := sub[7]
			b3 := sub[8]
			args2 := sub[9]

			directGuarded := fmt.Sprintf("(typeof %s==='function'?%s[%s](null,%s):void 0)",
				c, c, lookup1, args1)
			methodGuarded := fmt.Sprintf("(typeof %s[%s]==='function'?%s[%s][%s](%s,%s):void 0)",
				b2, c2, b2, c2, lookup2, b3, args2)

			if strings.HasPrefix(match, "void 0===") {
				return fmt.Sprintf("void 0===%s?%s:%s", b, directGuarded, methodGuarded)
			}
			return fmt.Sprintf("%s===void 0?%s:%s", b, directGuarded, methodGuarded)
		})

		if debug {
			log.Printf("[cf-managed] Added CALL opcode guard (%s) to %d call site(s)", v.label, len(matches))
		}
		totalPatched += len(matches)
	}

	// V2: helper function comparison
	if matches := callOpcodeFullReV2.FindAllStringSubmatch(script, -1); len(matches) > 0 {
		script = callOpcodeFullReV2.ReplaceAllStringFunc(script, func(match string) string {
			sub := callOpcodeFullReV2.FindStringSubmatch(match)
			if sub == nil {
				return match
			}
			helper := sub[1]
			helperLookup := sub[2]
			b := sub[3]
			c := sub[4]
			lookup1 := sub[5]
			args1 := sub[6]
			b2 := sub[7]
			c2 := sub[8]
			lookup2 := sub[9]
			b3 := sub[10]
			args2 := sub[11]

			directGuarded := fmt.Sprintf("(typeof %s==='function'?%s[%s](null,%s):void 0)",
				c, c, lookup1, args1)
			methodGuarded := fmt.Sprintf("(typeof %s[%s]==='function'?%s[%s][%s](%s,%s):void 0)",
				b2, c2, b2, c2, lookup2, b3, args2)

			return fmt.Sprintf("%s[%s](%s,void 0)?%s:%s",
				helper, helperLookup, b, directGuarded, methodGuarded)
		})

		if debug {
			log.Printf("[cf-managed] Added CALL opcode guard (V2) to %d call site(s)", len(matches))
		}
		totalPatched += len(matches)
	}

	if totalPatched == 0 && debug {
		log.Printf("[cf-managed] CALL opcode guard pattern not found")
	}
	return script
}

func patchVMInitLogging(script string, debug bool) string {
	evalInitRe := regexp.MustCompile(`(\w+)=\(0,eval\)\((\w+)\((\d+)\)\)`)
	matches := evalInitRe.FindAllStringSubmatchIndex(script, -1)
	if len(matches) == 0 {
		if debug {
			log.Printf("[cf-managed] eval init pattern not found")
		}
		return script
	}

	m := matches[0]
	varName := script[m[2]:m[3]]
	lookupFn := script[m[4]:m[5]]
	lookupArg := script[m[6]:m[7]]

	endPos := m[1]
	logSnippet := fmt.Sprintf(
		`,console.log('[VM-INIT] eval_str='+%s(%s)+', %s='+typeof %s+': '+String(%s).substring(0,200))`+
			`,(function(){var _c=%s(%s);if(_c.length>100){try{__goWriteFile('/tmp/vm_eval_code.js',_c)}catch(e){}}})()`,
		lookupFn, lookupArg, varName, varName, varName,
		lookupFn, lookupArg,
	)

	patched := script[:endPos] + logSnippet + script[endPos:]
	if debug {
		log.Printf("[cf-managed] Added VM init logging for %s=(0,eval)(%s(%s))", varName, lookupFn, lookupArg)
	}
	return patched
}

func patchVMErrorResilience(script string, debug bool) string {
	totalPatched := 0

	throwRe1 := regexp.MustCompile(`else throw (\w+)(\}+)continue;case'(\d+)'`)
	if matches := throwRe1.FindAllStringSubmatch(script, -1); len(matches) > 0 {
		script = throwRe1.ReplaceAllStringFunc(script, func(match string) string {
			sub := throwRe1.FindStringSubmatch(match)
			if sub == nil {
				return match
			}
			errVar := sub[1]
			braces := sub[2]
			caseNum := sub[3]
			return fmt.Sprintf(`else{console.log('[VM-CATCH] unhandled: '+%s.message)}%scontinue;case'%s'`, errVar, braces, caseNum)
		})
		totalPatched += len(matches)
		if debug {
			log.Printf("[cf-managed] Patched %d VM error throw(s) (pattern 1: switch-case)", len(matches))
		}
	}

	if totalPatched == 0 && debug {
		log.Printf("[cf-managed] VM error throw pattern not found")
	} else if debug {
		log.Printf("[cf-managed] Patched %d VM error throw(s) for resilience", totalPatched)
	}
	return script
}
