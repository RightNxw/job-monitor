//go:build solver

package cloudflare

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"
)

// Solve bypasses Cloudflare protection on a URL and returns cookies.
// Auto-detects challenge type (JSD / managed / turnstile) and dispatches
// to the appropriate sub-solver. Uses httpcloak for Chrome-identical TLS
// fingerprinting.
//
// For Turnstile challenges, pass a non-nil TurnstileSolver via SolveOptions.
// If no TurnstileSolver is provided, Turnstile challenges return an error.
func Solve(ctx context.Context, targetURL, proxyURL string) (*SolveResult, error) {
	return SolveWithOptions(ctx, targetURL, proxyURL, nil)
}

// SolveOptions holds optional configuration for the solve call.
type SolveOptions struct {
	// TurnstileSolver is an optional pre-configured Turnstile daemon solver.
	// If nil, Turnstile challenges will return an error.
	TurnstileSolver *TurnstileSolver

	// Debug enables verbose logging for all sub-solvers.
	Debug bool
}

// SolveWithOptions bypasses Cloudflare protection with extended options.
func SolveWithOptions(ctx context.Context, targetURL, proxyURL string, opts *SolveOptions) (*SolveResult, error) {
	debug := false
	var turnstile *TurnstileSolver
	if opts != nil {
		debug = opts.Debug
		turnstile = opts.TurnstileSolver
	}

	client, err := newHTTPClient(DefaultPreset, proxyURL, "")
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	defer client.Close()

	if debug {
		log.Printf("[cloudflare] solving %s", targetURL)
	}

	// Fetch the page to detect challenge type.
	resp, err := client.Get(ctx, targetURL)
	if err != nil {
		return nil, fmt.Errorf("fetch page: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	body := string(bodyBytes)

	// Build response headers map for detection.
	respHeaders := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			respHeaders[k] = v[0]
		}
	}

	challengeType := DetectChallengeType(resp.StatusCode, body, respHeaders)
	if debug {
		log.Printf("[cloudflare] detected challenge type: %d (status=%d, body=%d bytes)", challengeType, resp.StatusCode, len(body))
	}

	if challengeType == ChallengeTypeNone {
		// No challenge -- return existing cookies.
		cookies := client.AllCookies(targetURL)
		return &SolveResult{
			Success:   true,
			Cookies:   cookies,
			UserAgent: client.UserAgent(),
		}, nil
	}

	switch challengeType {
	case ChallengeTypeJSD:
		jsd := newJSDSolver(client, debug)
		result, err := jsd.solve(ctx, targetURL, body)
		if err == nil && result.success {
			return &SolveResult{
				Success:     true,
				CfClearance: result.cfClearance,
				Cookies:     result.cookies,
				UserAgent:   result.userAgent,
			}, nil
		}
		// JSD failed, fall through to managed solver as backup.
		if debug {
			if err != nil {
				log.Printf("[cloudflare] JSD failed (%v), trying managed solver", err)
			} else {
				log.Printf("[cloudflare] JSD failed (%s), trying managed solver", result.errMsg)
			}
		}
		// Re-fetch page since context may have advanced.
		resp2, err2 := client.Get(ctx, targetURL)
		if err2 == nil {
			body2, _ := io.ReadAll(resp2.Body)
			resp2.Body.Close()
			if len(body2) > 0 {
				body = string(body2)
			}
		}
		fallthrough

	case ChallengeTypeManaged:
		managed := newManagedSolver(client, debug)
		result, err := managed.solve(ctx, targetURL, body)
		if err != nil {
			return nil, fmt.Errorf("managed solve: %w", err)
		}
		if !result.success {
			return nil, fmt.Errorf("managed failed: %s", result.errMsg)
		}
		return &SolveResult{
			Success:     true,
			CfClearance: result.cfClearance,
			Cookies:     result.cookies,
			UserAgent:   result.userAgent,
		}, nil

	case ChallengeTypeTurnstile:
		if turnstile == nil {
			return nil, fmt.Errorf("turnstile challenge detected but no TurnstileSolver provided")
		}
		sitekey := ExtractSitekey(body)
		result, err := turnstile.SolveTurnstile(ctx, targetURL, sitekey)
		if err != nil {
			return nil, fmt.Errorf("turnstile solve: %w", err)
		}
		if !result.Success {
			return nil, fmt.Errorf("turnstile failed: %s", result.Error)
		}
		return &SolveResult{
			Success:     true,
			CfClearance: result.CfClearance,
			Cookies:     nil, // Turnstile daemon does not return cookies map.
			UserAgent:   client.UserAgent(),
		}, nil

	default:
		return nil, fmt.Errorf("unknown challenge type: %d", challengeType)
	}
}

// SolveWithRetry attempts to solve with retries.
func SolveWithRetry(ctx context.Context, targetURL, proxyURL string, maxRetries int) (*SolveResult, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		result, err := Solve(ctx, targetURL, proxyURL)
		if err == nil {
			return result, nil
		}
		lastErr = err
		time.Sleep(time.Duration(i+1) * time.Second)
	}
	return nil, fmt.Errorf("all %d retries failed: %w", maxRetries, lastErr)
}
