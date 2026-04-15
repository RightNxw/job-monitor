//go:build solver

package engine

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

//go:embed chrome_audio_44100.bin
var chromeAudioRaw []byte

// chromeAudioSamplesJS returns a JavaScript array literal string containing
// all 44100 audio samples from a real Chrome OfflineAudioContext rendering.
// The rendering uses: triangle oscillator at 10kHz -> dynamics compressor
// (threshold=-50, knee=40, ratio=12, attack=0, release=0.25) -> destination.
func chromeAudioSamplesJS() string {
	n := len(chromeAudioRaw) / 4
	if n == 0 {
		return "[]"
	}

	var b strings.Builder
	b.Grow(n * 14) // ~13 chars per float on average
	b.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		bits := binary.LittleEndian.Uint32(chromeAudioRaw[i*4 : i*4+4])
		f := math.Float32frombits(bits)
		fmt.Fprintf(&b, "%g", f)
	}
	b.WriteByte(']')
	return b.String()
}
