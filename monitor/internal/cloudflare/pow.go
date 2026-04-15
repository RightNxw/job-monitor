//go:build solver

package cloudflare

// Parallel SHA-256 proof-of-work solver for Cloudflare managed challenges.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// PowSolution holds the result of a proof-of-work computation.
type PowSolution struct {
	Nonce    string
	Hash     string
	Attempts int64
	Duration time.Duration
}

// SolvePow finds a nonce such that SHA256(e={timestamp}&d={difficulty}&n={nonce}{iv})
// has a hash starting with `difficulty` many zero hex characters.
// It uses parallel goroutines for speed.
func SolvePow(difficulty int, iv string, timestamp int64) (*PowSolution, error) {
	if difficulty <= 0 || difficulty > 64 {
		return nil, fmt.Errorf("invalid difficulty: %d", difficulty)
	}

	target := strings.Repeat("0", difficulty)
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}

	var (
		found    atomic.Bool
		result   atomic.Value // *PowSolution
		totalOps atomic.Int64
		wg       sync.WaitGroup
	)

	start := time.Now()

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Each worker tries nonces in its own range to avoid overlap.
			nonce := int64(workerID)
			step := int64(workers)

			for !found.Load() {
				nonceStr := strconv.FormatInt(nonce, 10)
				input := fmt.Sprintf("e=%d&d=%d&n=%s%s", timestamp, difficulty, nonceStr, iv)

				hash := sha256.Sum256([]byte(input))
				hexHash := hex.EncodeToString(hash[:])

				totalOps.Add(1)

				if strings.HasPrefix(hexHash, target) {
					if found.CompareAndSwap(false, true) {
						result.Store(&PowSolution{
							Nonce:    nonceStr,
							Hash:     hexHash,
							Attempts: totalOps.Load(),
							Duration: time.Since(start),
						})
					}
					return
				}

				nonce += step
			}
		}(w)
	}

	wg.Wait()

	sol, ok := result.Load().(*PowSolution)
	if !ok || sol == nil {
		return nil, fmt.Errorf("failed to find solution (should not happen)")
	}

	return sol, nil
}

// VerifyPow checks that a nonce produces a valid hash for the given parameters.
func VerifyPow(difficulty int, iv string, timestamp int64, nonce string) bool {
	target := strings.Repeat("0", difficulty)
	input := fmt.Sprintf("e=%d&d=%d&n=%s%s", timestamp, difficulty, nonce, iv)
	hash := sha256.Sum256([]byte(input))
	hexHash := hex.EncodeToString(hash[:])
	return strings.HasPrefix(hexHash, target)
}
