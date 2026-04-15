//go:build !solver

// Package cloudflare fronts a Cloudflare challenge solver.
//
// The working solver is not part of the public build. Every file in this
// package and in internal/engine sits behind the `solver` build tag, so a
// default `go build ./...` compiles this stub instead and the Glassdoor
// scraper degrades to a no-op rather than failing to compile.
//
// The tagged implementation needs a patched build of github.com/tommie/v8go,
// so `-tags solver` will not compile as it is. Bring your own solver instead:
// implement SolveWithRetry against
// whatever you use (a hosted solving service, a headless browser, or your
// own challenge runner) and the rest of the pipeline works unchanged.
package cloudflare

import "context"

// SolveResult is what a solver hands back: the cookies and user agent that
// subsequent requests must reuse for the clearance to stay valid.
type SolveResult struct {
	Success     bool
	CfClearance string
	Cookies     map[string]string
	UserAgent   string
}

// Solve always fails in this build.
func Solve(ctx context.Context, targetURL, proxyURL string) (*SolveResult, error) {
	return nil, ErrSolverUnavailable
}

// SolveWithRetry always fails in this build.
func SolveWithRetry(ctx context.Context, targetURL, proxyURL string, maxRetries int) (*SolveResult, error) {
	return nil, ErrSolverUnavailable
}
