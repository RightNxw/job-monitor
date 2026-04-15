package cloudflare

import "errors"

// ErrSolverUnavailable reports that this build has no working challenge
// solver. Callers should treat it as "skip this source", not as a failure.
// See internal/cloudflare/stub.go for how to plug in your own.
var ErrSolverUnavailable = errors.New("cloudflare: no solver in this build (see internal/cloudflare/stub.go)")
