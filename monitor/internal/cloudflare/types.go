//go:build solver

package cloudflare

import "strings"

// Cloudflare-specific types for the challenge solver.

// ChallengeType indicates which Cloudflare protection pattern was detected.
type ChallengeType int

const (
	ChallengeTypeNone      ChallengeType = iota
	ChallengeTypeManaged                 // Full managed challenge with _cf_chl_opt
	ChallengeTypeJSD                     // JS Detection beacon (challenge-platform/scripts/jsd)
	ChallengeTypeTurnstile               // Turnstile CAPTCHA
)

// ChallengeConfig holds parameters extracted from the Cloudflare challenge HTML.
type ChallengeConfig struct {
	CRay      string // cf ray id
	CH        string // challenge hash
	CUPMDTk   string // challenge token
	MD        string // metadata
	MDRD      string // metadata redirect
	CType     string // challenge type (managed, interactive, etc.)
	CvId      string // cv id
	CZone     string // zone
	ScriptURL string // challenge-platform script URL
	BaseURL   string // original URL being challenged

	// RawConfigJS is the raw "window._cf_chl_opt = {...}" assignment from the page.
	// Includes ALL fields -- injected directly into V8 for completeness.
	RawConfigJS string
}

// OrchestrateParams holds parameters extracted from the orchestrate script.
type OrchestrateParams struct {
	Alphabet   string // 65-char LZ-String alphabet permutation
	HashTriple string // e.g. "1682918873:1771035073:hash" (without leading/trailing slashes)
	BasePath   string // e.g. "/cdn-cgi/challenge-platform/h/g"
	Ve         string // version: "b" or "g"
}

// DeobfuscateResult holds the extracted values from a deobfuscated JSD script.
type DeobfuscateResult struct {
	Ve       string // version: "b" or "g"
	Path     string // e.g. "/jsd/oneshot/..."
	Alphabet string // 65-char LZ-String alphabet permutation
}

// solveResult is the internal result type used by the JSD solver.
type solveResult struct {
	success     bool
	cfClearance string
	cookies     map[string]string
	userAgent   string
	profile     string
	solveTimeMs int64
	errMsg      string
}

// SolveResult is the public result returned by Solve().
type SolveResult struct {
	Success     bool
	CfClearance string
	Cookies     map[string]string
	UserAgent   string
}

// Helper functions

func extractHost(rawURL string) string {
	if idx := strings.Index(rawURL, "//"); idx != -1 {
		rest := rawURL[idx+2:]
		if slashIdx := strings.Index(rest, "/"); slashIdx != -1 {
			return rest[:slashIdx]
		}
		return rest
	}
	return rawURL
}

func extractOrigin(rawURL string) string {
	if idx := strings.Index(rawURL, "//"); idx != -1 {
		rest := rawURL[idx+2:]
		if slashIdx := strings.Index(rest, "/"); slashIdx != -1 {
			return rawURL[:idx+2+slashIdx]
		}
	}
	return rawURL
}
