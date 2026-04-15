// Package engine is a headless JavaScript execution environment used by the
// Cloudflare solver: a DOM implementation, an HTML parser, canvas and audio
// fingerprint surfaces, and a pooled V8 runtime.
//
// The implementation sits behind the `solver` build tag and is excluded from
// the default build, which is why this package compiles to nothing here. It
// needs a patched build of github.com/tommie/v8go to compile at all.
// See internal/cloudflare/stub.go for the wider picture.
package engine
