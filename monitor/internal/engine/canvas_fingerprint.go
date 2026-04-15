//go:build solver

package engine

import (
	_ "embed"
	"encoding/base64"
)

// chrome_canvas_125.png contains the exact canvas fingerprint output from
// Chrome 146 on macOS when the Turnstile VM draws its fingerprint pattern
// on a 125x125 canvas. This is machine-specific (GPU/driver dependent).
//
//go:embed chrome_canvas_125.png
var chromeCanvasPNG []byte

// GenerateCanvasFingerprint returns the Chrome canvas fingerprint as base64.
func GenerateCanvasFingerprint() string {
	return base64.StdEncoding.EncodeToString(chromeCanvasPNG)
}
