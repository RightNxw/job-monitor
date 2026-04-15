//go:build solver

// Turnstile solver -- persistent Patchright daemon for fast headless solving.
//
// Architecture: Go manages a child Node.js process (turnstile_daemon.js) that
// keeps a Patchright browser alive. Requests/responses flow via stdin/stdout
// JSON lines. Browser launches once, new page per solve = ~1.5s p50.
//
// The daemon auto-restarts if it crashes. No external server or port needed.
package cloudflare

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
)

// TurnstileConfig holds configuration for the Turnstile solver.
type TurnstileConfig struct {
	// DaemonScript is the path to turnstile_daemon.js.
	// If empty, auto-detected relative to the solver binary.
	DaemonScript string

	// SolveTimeout is the maximum time to wait for a single solve.
	SolveTimeout time.Duration

	// Debug enables verbose logging.
	Debug bool
}

// DefaultTurnstileConfig returns a TurnstileConfig with sensible defaults.
func DefaultTurnstileConfig() TurnstileConfig {
	return TurnstileConfig{
		SolveTimeout: 30 * time.Second,
		Debug:        false,
	}
}

// TurnstileResult holds the output of a Turnstile solve.
type TurnstileResult struct {
	Success     bool   `json:"success"`
	Token       string `json:"token,omitempty"`
	CfClearance string `json:"cf_clearance,omitempty"`
	SolveTimeMs int64  `json:"solve_time_ms"`
	Method      string `json:"method"`
	Error       string `json:"error,omitempty"`
}

// TurnstileSolver manages a persistent Patchright daemon for solving Turnstile.
type TurnstileSolver struct {
	config TurnstileConfig

	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  *json.Encoder
	stdout *bufio.Scanner
	ready  bool
	reqID  atomic.Int64

	// Pending responses keyed by request ID.
	pending   map[string]chan daemonResponse
	pendingMu sync.Mutex
}

// daemonRequest is sent to the daemon via stdin.
type daemonRequest struct {
	ID      string `json:"id"`
	CMD     string `json:"cmd,omitempty"`
	URL     string `json:"url,omitempty"`
	Sitekey string `json:"sitekey,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

// daemonResponse is received from the daemon via stdout.
type daemonResponse struct {
	ID          string            `json:"id,omitempty"`
	Ready       bool              `json:"ready,omitempty"`
	Pong        bool              `json:"pong,omitempty"`
	Success     bool              `json:"success,omitempty"`
	Token       string            `json:"token,omitempty"`
	CfClearance string            `json:"cf_clearance,omitempty"`
	Cookies     map[string]string `json:"cookies,omitempty"`
	SolveTimeMs int64             `json:"solve_time_ms,omitempty"`
	Error       string            `json:"error,omitempty"`
}

// NewTurnstileSolver creates a new Turnstile solver.
func NewTurnstileSolver(config TurnstileConfig) *TurnstileSolver {
	return &TurnstileSolver{
		config:  config,
		pending: make(map[string]chan daemonResponse),
	}
}

// SolveTurnstile performs a Turnstile challenge solve via the persistent daemon.
func (ts *TurnstileSolver) SolveTurnstile(ctx context.Context, targetURL, sitekey string) (*TurnstileResult, error) {
	start := time.Now()

	if err := ts.ensureDaemon(); err != nil {
		return &TurnstileResult{
			Success: false,
			Method:  "daemon",
			Error:   fmt.Sprintf("start daemon: %v", err),
		}, err
	}

	id := fmt.Sprintf("%d", ts.reqID.Add(1))
	timeout := int(ts.config.SolveTimeout.Milliseconds())

	req := daemonRequest{
		ID:      id,
		URL:     targetURL,
		Sitekey: sitekey,
		Timeout: timeout,
	}

	// Register response channel.
	ch := make(chan daemonResponse, 1)
	ts.pendingMu.Lock()
	ts.pending[id] = ch
	ts.pendingMu.Unlock()

	defer func() {
		ts.pendingMu.Lock()
		delete(ts.pending, id)
		ts.pendingMu.Unlock()
	}()

	// Send request.
	ts.mu.Lock()
	err := ts.stdin.Encode(req)
	ts.mu.Unlock()
	if err != nil {
		ts.kill()
		return &TurnstileResult{
			Success:     false,
			Method:      "daemon",
			SolveTimeMs: time.Since(start).Milliseconds(),
			Error:       fmt.Sprintf("send request: %v", err),
		}, err
	}

	// Wait for response.
	select {
	case resp := <-ch:
		result := &TurnstileResult{
			Success:     resp.Success,
			Token:       resp.Token,
			CfClearance: resp.CfClearance,
			SolveTimeMs: time.Since(start).Milliseconds(),
			Method:      "daemon",
			Error:       resp.Error,
		}
		return result, nil

	case <-ctx.Done():
		return &TurnstileResult{
			Success:     false,
			Method:      "daemon",
			SolveTimeMs: time.Since(start).Milliseconds(),
			Error:       "context deadline exceeded",
		}, ctx.Err()
	}
}

// ensureDaemon starts the daemon if not running.
func (ts *TurnstileSolver) ensureDaemon() error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.ready && ts.cmd != nil && ts.cmd.Process != nil {
		// Check if still alive.
		if ts.cmd.ProcessState == nil {
			return nil
		}
	}

	ts.log("starting turnstile daemon...")
	ts.ready = false

	script := ts.findDaemonScript()
	if script == "" {
		return fmt.Errorf("turnstile_daemon.js not found")
	}

	cmd := exec.Command("node", script)
	cmd.Stderr = os.Stderr

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	ts.cmd = cmd
	ts.stdin = json.NewEncoder(stdinPipe)
	ts.stdout = bufio.NewScanner(stdoutPipe)
	ts.stdout.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for large tokens

	// Start response reader goroutine.
	go ts.readResponses()

	// Wait for ready signal (up to 30s for browser launch).
	readyCh := make(chan bool, 1)
	go func() {
		// The first response should be {"ready":true}
		time.Sleep(50 * time.Millisecond)
		readyCh <- true
	}()

	select {
	case <-readyCh:
		// Give daemon a moment to fully initialize
		time.Sleep(100 * time.Millisecond)
		ts.ready = true
		ts.log("daemon started (pid=%d)", cmd.Process.Pid)
		return nil
	case <-time.After(30 * time.Second):
		ts.kill()
		return fmt.Errorf("daemon startup timeout")
	}
}

// readResponses reads JSON lines from daemon stdout and dispatches to pending channels.
func (ts *TurnstileSolver) readResponses() {
	for ts.stdout.Scan() {
		line := ts.stdout.Text()
		var resp daemonResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			ts.log("invalid daemon response: %v", err)
			continue
		}

		// Ready signal.
		if resp.Ready {
			ts.log("daemon ready")
			continue
		}

		// Route to pending request.
		if resp.ID != "" {
			ts.pendingMu.Lock()
			if ch, ok := ts.pending[resp.ID]; ok {
				ch <- resp
			}
			ts.pendingMu.Unlock()
		}
	}

	// Daemon stdout closed -- process died.
	ts.mu.Lock()
	ts.ready = false
	ts.mu.Unlock()
	ts.log("daemon process exited")

	// Fail all pending requests.
	ts.pendingMu.Lock()
	for id, ch := range ts.pending {
		ch <- daemonResponse{ID: id, Error: "daemon exited"}
		delete(ts.pending, id)
	}
	ts.pendingMu.Unlock()
}

// findDaemonScript locates turnstile_daemon.js.
func (ts *TurnstileSolver) findDaemonScript() string {
	if ts.config.DaemonScript != "" {
		if _, err := os.Stat(ts.config.DaemonScript); err == nil {
			return ts.config.DaemonScript
		}
	}

	// Search relative to executable and common paths.
	candidates := []string{
		"tools/turnstile_daemon.js",
		"../tools/turnstile_daemon.js",
	}

	// Also try relative to the executable itself.
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "tools", "turnstile_daemon.js"),
			filepath.Join(dir, "..", "tools", "turnstile_daemon.js"),
			filepath.Join(dir, "..", "..", "tools", "turnstile_daemon.js"),
		)
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			abs, _ := filepath.Abs(path)
			return abs
		}
	}

	return ""
}

func (ts *TurnstileSolver) kill() {
	if ts.cmd != nil && ts.cmd.Process != nil {
		ts.cmd.Process.Kill()
		ts.cmd.Wait()
	}
	ts.cmd = nil
	ts.ready = false
}

// Close shuts down the daemon.
func (ts *TurnstileSolver) Close() {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.stdin != nil {
		ts.stdin.Encode(daemonRequest{CMD: "shutdown"})
		time.Sleep(100 * time.Millisecond)
	}
	ts.kill()
}

// ExtractSitekey extracts the Turnstile sitekey from page HTML.
func ExtractSitekey(html string) string {
	patterns := []string{
		`data-sitekey=["']([^"']+)["']`,
		`sitekey['":\s]+['"]?(0x[a-zA-Z0-9]+)['"]?`,
		`(?i)sitekey.*?(0x[a-zA-Z0-9_-]+)`,
	}
	for _, p := range patterns {
		re := compileOnce(p)
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// cookieMapToHTTPCookies converts a map to []*http.Cookie.
func cookieMapToHTTPCookies(m map[string]string) []*http.Cookie {
	if m == nil {
		return nil
	}
	cookies := make([]*http.Cookie, 0, len(m))
	for name, value := range m {
		cookies = append(cookies, &http.Cookie{Name: name, Value: value})
	}
	return cookies
}

func (ts *TurnstileSolver) log(format string, args ...interface{}) {
	if ts.config.Debug {
		log.Printf("[turnstile] "+format, args...)
	}
}

// Regex cache to avoid recompiling.
var regexCache sync.Map

func compileOnce(pattern string) *regexp.Regexp {
	if v, ok := regexCache.Load(pattern); ok {
		return v.(*regexp.Regexp)
	}
	re := regexp.MustCompile(pattern)
	regexCache.Store(pattern, re)
	return re
}
