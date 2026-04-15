//go:build solver

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	v8 "github.com/tommie/v8go"
)

// FetchFunc is the signature for the fetch interceptor callback.
type FetchFunc func(url string, opts map[string]interface{}) (body string, statusCode int, headers map[string]string, err error)

// PatchFunc applies optimizations to JS code strings (e.g., Worker eval data).
type PatchFunc func(code string) string

// timerEntry represents a pending setTimeout/setInterval callback.
type timerEntry struct {
	id       int32
	fn       *v8.Function
	delay    time.Duration
	fireAt   time.Time
	interval bool // true for setInterval
}

// ElementState holds the Go-side state for a native DOM element.
// Properties stored here are served by the C++ named property handler (Getter)
// instead of being JS own properties, matching Chrome where elements have
// 0 own properties and all access goes through C++ interceptors.
type ElementState struct {
	TagName      string
	NodeName     string
	NodeType     int32
	ID           string
	ClassName    string
	InnerHTML    string
	OuterHTML    string
	InnerText    string
	TextContent  string
	LocalName    string
	NamespaceURI string
	Prefix       string // null in Chrome, we use ""
	BaseURI      string
	IsConnected  bool
	OffsetWidth  int32
	OffsetHeight int32
	OffsetTop    int32
	OffsetLeft   int32
	ClientWidth  int32
	ClientHeight int32
	ScrollWidth  int32
	ScrollHeight int32
	ScrollTop    int32
	ScrollLeft   int32
	Src          string
	Href         string
	Type         string
	Rel          string
	Media        string
	Nonce        string
	Value        string
	Name         string
	CrossOrigin  string // null in Chrome, we use ""
	Checked      bool
	Disabled     bool
	Width        int32
	Height       int32
}

// Engine wraps a V8 isolate and context with a proper event loop.
type Engine struct {
	iso     *v8.Isolate
	ctx     *v8.Context
	pool    *EnginePool // if non-nil, isolate is returned to pool on Close instead of disposed
	domCfg  *DOMConfig
	timeout time.Duration
	debug   bool

	mu           sync.Mutex
	fetchHandler FetchFunc
	patchHandler PatchFunc
	startTime    time.Time

	// Event loop state, all access must be on the V8 goroutine.
	timers          []*timerEntry
	nextTimerID     int32
	pendingOps      int32 // number of in-flight async operations (fetch, etc.)
	logLines        []string
	reloadRequested bool          // set when location.reload() is called from JS
	exitRequested   bool          // set when caller wants to exit the event loop early
	exitChan        chan struct{} // signaled by RequestExit for instant wakeup

	// Native DOM element support via V8 named property handlers.
	// elementTmpl is the ObjectTemplate with SetNativeDataProperty for creating native-feeling
	// DOM elements. Elements created from this template have C++ level property
	// interception, making them indistinguishable from real browser DOM objects.
	elementTmpl   *v8.ObjectTemplate
	elementStates map[int32]*ElementState // keyed by element ID stored in internal field 0
	nextElementID int32                   // monotonically increasing element ID counter

	// Threaded Web Worker support. Workers run in separate V8 Isolates in
	// goroutines (like real Chrome). The worker's postMessage() sends data
	// through workerMsgChan, which the main event loop reads and delivers
	// to the main context as MessageEvent via window.__workerOnMessage.
	workerMsgChan chan string // worker→main postMessage data (JSON-encoded)
	workerInbox   chan string // main→worker postMessage data
}

func init() {
	// V8 flags: only increase heap size. The --always-turbofan, --max-inlined-bytecode-size,
	// and --interrupt-budget flags were tested and don't help with the megamorphic VM dispatch
	// pattern used by Turnstile's PoW. Let V8 use its default tiering (Ignition→Sparkplug→
	// Maglev→TurboFan) which is actually faster for dynamic dispatch workloads.
	v8.SetFlags("--max-old-space-size=4096")
}

// NewEngine creates a new V8 engine with fake DOM globals injected.
func NewEngine(domCfg *DOMConfig, debug bool) (*Engine, error) {
	return newEngine(domCfg, debug, v8.NewIsolate(), nil)
}

// NewEngineFromPool creates a V8 engine using a pre-warmed isolate from the pool.
// On Close(), the isolate is returned to the pool instead of being disposed.
// This saves ~40-60ms per solve by avoiding isolate creation overhead.
func NewEngineFromPool(domCfg *DOMConfig, debug bool, pool *EnginePool) (*Engine, error) {
	if pool == nil {
		return NewEngine(domCfg, debug)
	}
	return newEngine(domCfg, debug, pool.Get(), pool)
}

func newEngine(domCfg *DOMConfig, debug bool, iso *v8.Isolate, pool *EnginePool) (*Engine, error) {
	e := &Engine{
		iso:           iso,
		pool:          pool,
		domCfg:        domCfg,
		timeout:       180 * time.Second,
		startTime:     time.Now(),
		debug:         debug,
		elementStates: make(map[int32]*ElementState),
		exitChan:      make(chan struct{}, 1),
		workerMsgChan: make(chan string, 16),
	}

	global := v8.NewObjectTemplate(iso)

	// Register Go callback functions.
	if err := e.registerCallbacks(global); err != nil {
		iso.Dispose()
		return nil, fmt.Errorf("register callbacks: %w", err)
	}

	ctx := v8.NewContext(iso, global)
	e.ctx = ctx

	// Pre-define key window properties as CONFIGURABLE getters BEFORE the DOM script.
	// In Chrome, window.document/location/self are accessor properties (getters).
	// V8's `var document = {}` creates non-configurable data properties. By pre-defining
	// them as configurable getters, the DOM script's assignments will update the value
	// captured by the getter instead of creating a new non-configurable data property.
	if _, err := ctx.RunScript(`(function() {
		var _vals = {};
		['document','location','self'].forEach(function(name) {
			_vals[name] = undefined;
			Object.defineProperty(this, name, {
				get: function() { return _vals[name]; },
				set: function(v) { _vals[name] = v; },
				enumerable: true,
				configurable: name !== 'self' // self is non-configurable in Chrome
			});
		}, this);
	}).call(this);`, "pre-define-getters.js"); err != nil {
		e.log("pre-define getters: %v", err)
	}

	// Build native Navigator object with V8 accessor properties on the prototype.
	// This must happen BEFORE the DOM script runs, since it references navigator.
	if err := e.setupNavigator(); err != nil {
		e.Close()
		return nil, fmt.Errorf("setup navigator: %w", err)
	}

	// Build native Screen object with V8 accessor properties on the prototype.
	// This must happen BEFORE the DOM script runs, since it references screen.
	if err := e.setupScreen(); err != nil {
		e.Close()
		return nil, fmt.Errorf("setup screen: %w", err)
	}

	// Build native Performance object with V8 accessor properties on the prototype.
	// This must happen BEFORE the DOM script runs, since it references performance.
	if err := e.setupPerformance(); err != nil {
		e.Close()
		return nil, fmt.Errorf("setup performance: %w", err)
	}

	// Build native Document/HTMLDocument constructors with V8 accessor properties
	// on Document.prototype. This must happen BEFORE the DOM script runs so that
	// dom.go can wire up the prototype chain (Document.prototype → Node.prototype)
	// while preserving the native accessor properties.
	if err := e.setupDocument(); err != nil {
		e.Close()
		return nil, fmt.Errorf("setup document: %w", err)
	}

	// Build native DOM element ObjectTemplate with V8 native data properties.
	// This creates the element template that __goCreateElement uses to make
	// elements with native accessor properties (identical to Chrome's Blink).
	if err := e.setupElementHandler(); err != nil {
		e.Close()
		return nil, fmt.Errorf("setup element handler: %w", err)
	}

	// Inject the fake DOM.
	domScript := BuildDOMScript(domCfg)
	if debug {
		os.WriteFile("/tmp/dom_rendered.js", []byte(domScript), 0644)
	}
	if _, err := ctx.RunScript(domScript, domCfg.URL); err != nil {
		e.Close()
		return nil, fmt.Errorf("inject DOM: %w", err)
	}

	// Create document.all with [[IsHTMLDDA]] semantics.
	// In real browsers, typeof document.all === "undefined" even though it exists.
	// This is a critical browser detection check that the Turnstile VM uses.
	allTmpl := v8.NewObjectTemplate(iso)
	allTmpl.SetCallAsFunctionHandler(func(info *v8.FunctionCallbackInfo) (*v8.Value, error) {
		return info.This().Value, nil
	})
	allTmpl.MarkAsUndetectable()
	if allObj, err := allTmpl.NewInstance(ctx); err == nil {
		// Set document.all on Document.prototype (Chrome has it there, not as own prop)
		// GOPN(document) in Chrome returns only ['location']
		if docProto, err := ctx.Global().Get("Document"); err == nil {
			if docFunc, err := docProto.AsObject(); err == nil {
				if proto, err := docFunc.Get("prototype"); err == nil {
					if protoObj, err := proto.AsObject(); err == nil {
						protoObj.Set("all", allObj)
					}
				}
			}
		}
	}

	// Convert remaining window data properties to GETTERS (Chrome uses getters for all).
	// This must happen AFTER the DOM script sets all values (document, location, etc.).
	if _, err := ctx.RunScript(`(function() {
		var w = this;
		// Properties that should be getters but are currently data properties
		var props = ['document','location','self','screen',
			'innerWidth','innerHeight','outerWidth','outerHeight',
			'devicePixelRatio','origin','isSecureContext','crossOriginIsolated',
			'scrollX','scrollY','pageXOffset','pageYOffset'];
		for (var i = 0; i < props.length; i++) {
			var name = props[i];
			try {
				var desc = Object.getOwnPropertyDescriptor(w, name);
				if (desc && !desc.get) {
					// It's a data property, convert to getter
					var val = w[name];
					Object.defineProperty(w, name, {
						get: (function(v) { return function() { return v; }; })(val),
						set: desc.writable ? (function(n) { return function(v) {
							// Re-define with new value
							Object.defineProperty(w, n, {
								get: (function(nv) { return function() { return nv; }; })(v),
								enumerable: true, configurable: true
							});
						}; })(name) : undefined,
						enumerable: desc.enumerable !== false,
						configurable: true
					});
				}
			} catch(e) { /* non-configurable, skip */ }
		}
	}).call(this);`, "window-getters.js"); err != nil {
		e.log("window getter conversion: %v", err)
	}

	// Delete Go callbacks from V8 global to hide from 'in' operator.
	// dom.go captures them in const variables, so they remain accessible via closures.
	cleanupJS := `(function() {
		var names = ['__goPerformanceNow','__goAtob','__goBtoa','__goDigest',
			'__goPatchWorkerCode','__goSetTimeout','__goSetInterval','__goClearTimer',
			'__goFetch','__goConsoleLog','__goSyncFetch','__goWriteFile','__goLocationReload',
			'__goCreateDocumentAll','__goCreateElement','__goParseHTML',
			'__goCreateWorker','__goWorkerPostMessage',
			'__navObj','__screenObj','__perfObj'];
		var deleted = 0, failed = 0;
		for (var i = 0; i < names.length; i++) {
			try { if (delete globalThis[names[i]]) deleted++; else failed++; } catch(e) { failed++; }
		}
		// Verify: check if originals are truly gone from 'in' operator
		var stillPresent = names.filter(function(n) { return n in globalThis; });
		console.log('[CLEANUP] deleted=' + deleted + ' failed=' + failed + ' stillInGlobal=' + stillPresent.join(','));
	})();`
	if _, err := ctx.RunScript(cleanupJS, domCfg.URL); err != nil {
		log.Printf("[engine] Go callback cleanup failed: %v", err)
	}

	return e, nil
}

// SetFetchHandler sets the callback for when JS calls fetch().
func (e *Engine) SetFetchHandler(fn FetchFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.fetchHandler = fn
}

// SetPatchHandler sets the callback for patching Worker eval code.
func (e *Engine) SetPatchHandler(fn PatchFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.patchHandler = fn
}

// LogLines returns captured console output.
func (e *Engine) LogLines() []string {
	return e.logLines
}

// ExecuteScript runs a JavaScript string in the V8 context.
func (e *Engine) ExecuteScript(ctx context.Context, js, filename string) (*v8.Value, error) {
	done := make(chan struct{})
	var result *v8.Value
	var runErr error

	go func() {
		defer close(done)
		result, runErr = e.ctx.RunScript(js, filename)
	}()

	select {
	case <-done:
		return result, runErr
	case <-ctx.Done():
		e.iso.TerminateExecution()
		<-done
		return nil, fmt.Errorf("script execution timed out: %w", ctx.Err())
	}
}

// RunEventLoop executes the script and then drives the event loop until all
// timers and async operations complete, or the context expires.
func (e *Engine) RunEventLoop(ctx context.Context, js, filename string) error {
	// Execute the main script.
	_, err := e.ctx.RunScript(js, filename)
	if err != nil {
		// Script may throw, log but continue to drain the event loop
		// since timers/promises may already be queued.
		e.log("Script execution error (continuing event loop): %+v", err)
	} else {
		e.log("Script executed successfully, timers=%d, pending=%d", len(e.timers), e.pendingOps)
	}

	// Drain microtasks after initial execution.
	e.ctx.PerformMicrotaskCheckpoint()

	// Event loop: process timers and wait for async ops.
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(e.timeout)
	}

	loopIter := 0
	totalFired := 0
	for time.Now().Before(deadline) {
		loopIter++
		// Fire any ready timers.
		fired := e.fireReadyTimers()
		totalFired += fired

		// Drain microtasks after each timer batch.
		if fired > 0 {
			e.ctx.PerformMicrotaskCheckpoint()
			e.log("Event loop: iter=%d, fired %d timers (total=%d)", loopIter, fired, totalFired)
		}

		// Deliver any pending worker messages to the main context.
		// Workers run in separate goroutines and post results via workerMsgChan.
		e.deliverWorkerMessages()

		// Check if there's anything left to wait for.
		e.mu.Lock()
		pending := e.pendingOps
		timerCount := len(e.timers)
		e.mu.Unlock()

		if pending == 0 && timerCount == 0 {
			e.log("Event loop: no pending ops or timers, done (iter=%d, totalFired=%d)", loopIter, totalFired)
			break
		}

		// Break out of the loop if the script called location.reload().
		if e.reloadRequested {
			e.log("Event loop: location.reload() requested, breaking out (iter=%d, totalFired=%d)", loopIter, totalFired)
			break
		}

		// Break if caller requested early exit.
		if e.exitRequested {
			e.log("Event loop: exit requested by caller (iter=%d, totalFired=%d)", loopIter, totalFired)
			break
		}

		// Log every ~2 seconds
		if loopIter%200 == 0 {
			e.log("Event loop: iter=%d, pending=%d, timers=%d, totalFired=%d", loopIter, pending, timerCount, totalFired)
		}

		// Small sleep to avoid busy-waiting. exitChan provides instant wakeup
		// when RequestExit() is called (e.g., token captured).
		// workerMsgChan provides instant wakeup when a worker posts a result.
		select {
		case <-ctx.Done():
			e.log("Event loop: context done (iter=%d, totalFired=%d, pending=%d, timers=%d)", loopIter, totalFired, pending, timerCount)
			return ctx.Err()
		case <-e.exitChan:
			e.log("Event loop: exitChan signaled (iter=%d, totalFired=%d)", loopIter, totalFired)
			// Fall through to check exitRequested on next iteration
		case msg := <-e.workerMsgChan:
			// Worker posted a message, deliver immediately to main context.
			e.deliverSingleWorkerMessage(msg)
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Final microtask drain.
	e.ctx.PerformMicrotaskCheckpoint()

	return nil
}

// RequestExit signals the event loop to exit early.
func (e *Engine) RequestExit() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.exitRequested = true
	// Non-blocking send to wake up the event loop immediately.
	select {
	case e.exitChan <- struct{}{}:
	default:
	}
}

// ReloadRequested returns true if the script called location.reload().
func (e *Engine) ReloadRequested() bool {
	return e.reloadRequested
}

// Close disposes of the V8 isolate and context.
func (e *Engine) Close() {
	if e.ctx != nil {
		e.ctx.Close()
		e.ctx = nil
	}
	if e.iso != nil {
		if e.pool != nil {
			// Return isolate to pool for reuse instead of disposing.
			e.pool.Put(e.iso)
		} else {
			e.iso.Dispose()
		}
		e.iso = nil
	}
}

// SetCookie updates document.cookie in the V8 context by parsing a "name=value"
// string and setting it in the internal _cfDc cookie store. Callers use this to
// forward Set-Cookie headers from real responses back into the script's
// environment, so it sees updated cookie values between requests.
func (e *Engine) SetCookie(nameValue string) {
	if e.ctx == nil {
		return
	}
	// Use the document.cookie setter which is already wired up in dom.go
	// to parse "name=value" and update the _ck/_cfDc cookie store.
	escaped := strings.ReplaceAll(nameValue, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	escaped = strings.ReplaceAll(escaped, "\n", "")
	escaped = strings.ReplaceAll(escaped, "\r", "")
	js := fmt.Sprintf(`document.cookie = "%s";`, escaped)
	if _, err := e.ctx.RunScript(js, "set-cookie.js"); err != nil {
		e.log("SetCookie error: %v", err)
	}
}

// elementDefaultDimensions returns realistic offsetWidth, offsetHeight,
// offsetTop, offsetLeft for a given HTML tag. Block elements fill the
// parent width; inline elements are content-sized. Without this, all
// elements return 200x100 which is a trivial bot detection signal.
func elementDefaultDimensions(tag string, screenW, screenH int) (ow, oh, ot, ol int32) {
	parentW := int32(screenW)
	if parentW <= 0 {
		parentW = 1512
	}
	// Default line height for text
	lineH := int32(18 + int32(screenW%5)) // 18-22px, slight variation

	switch tag {
	// Block elements, fill parent width
	case "div", "section", "article", "main", "aside", "header", "footer", "nav", "form", "fieldset":
		return parentW, 0, 0, 0 // 0 height when empty (no content)
	case "p", "h1", "h2", "h3", "h4", "h5", "h6", "li", "dt", "dd", "blockquote", "pre", "figcaption":
		return parentW, lineH, 0, 0 // one line of text
	case "ul", "ol":
		return parentW, lineH * 3, 0, 0 // list with a few items
	case "table":
		return parentW, lineH * 5, 0, 0
	case "tr":
		return parentW, lineH, 0, 0
	case "td", "th":
		return parentW / 4, lineH, 0, 0
	// Inline elements, content-sized
	case "span", "a", "strong", "em", "b", "i", "u", "label", "code", "small", "sub", "sup":
		return int32(40 + screenW%30), lineH, 0, 0 // varies by content
	case "input":
		return int32(173 + screenW%20), 21, 0, 0 // Chrome default input width ~173-193px
	case "button":
		return int32(60 + screenW%20), int32(26 + screenH%4), 0, 0
	case "textarea":
		return int32(300 + screenW%50), int32(50 + screenH%10), 0, 0
	case "select":
		return int32(100 + screenW%30), 21, 0, 0
	// Media elements
	case "img":
		return 300, 150, 0, 0 // default image placeholder
	case "canvas":
		return 300, 150, 0, 0 // HTML spec default
	case "video":
		return 300, 150, 0, 0
	case "iframe":
		return 300, 150, 0, 0
	case "svg":
		return 300, 150, 0, 0
	// Invisible elements
	case "script", "style", "link", "meta", "head", "title", "noscript", "template":
		return 0, 0, 0, 0
	case "br", "hr":
		return parentW, 1, 0, 0
	// Default for unknown tags, block-like
	default:
		return parentW, 0, 0, 0
	}
}

// isInlineTag returns true for HTML tags that are inline-level elements.
// These elements' offsetWidth should be proportional to their text content.
func isInlineTag(tag string) bool {
	switch strings.ToLower(tag) {
	case "span", "a", "strong", "em", "b", "i", "u", "label", "code", "small", "sub", "sup", "abbr", "cite", "q", "var", "samp", "kbd", "mark", "s", "del", "ins":
		return true
	}
	return false
}

// estimateTextWidth computes a pixel width for text using character-width estimation.
// defaultWidth is the fallback if text is empty. This produces widths that vary with
// content, which is critical for font detection probes (BM script compares widths
// of spans with different fonts/text to determine available fonts).
func estimateTextWidth(text string, defaultWidth int32) int {
	if len(text) == 0 {
		return int(defaultWidth)
	}
	// Base font size: 16px (browser default)
	fontSize := 16.0
	width := 0.0
	for _, ch := range text {
		switch {
		case ch == 'i' || ch == 'l' || ch == '!' || ch == '|' || ch == '.' || ch == ',' || ch == ':' || ch == ';' || ch == '\'' || ch == '`':
			width += fontSize * 0.28
		case ch == 'f' || ch == 'j' || ch == 't' || ch == 'r':
			width += fontSize * 0.35
		case ch == 'm' || ch == 'w' || ch == 'M' || ch == 'W':
			width += fontSize * 0.75
		case ch == '@':
			width += fontSize * 0.85
		case ch == ' ':
			width += fontSize * 0.28
		case ch >= 'A' && ch <= 'Z':
			width += fontSize * 0.6
		case ch >= '0' && ch <= '9':
			width += fontSize * 0.5
		case ch > 127:
			width += fontSize * 0.9 // emoji/wide chars
		default:
			width += fontSize * 0.48 // lowercase default
		}
	}
	if width < 1 {
		return int(defaultWidth)
	}
	return int(width + 0.5) // round
}

// stripHTMLTags removes HTML tags from a string, returning only text content.
func stripHTMLTags(html string) string {
	if html == "" || !strings.Contains(html, "<") {
		return html
	}
	var result strings.Builder
	inTag := false
	for _, ch := range html {
		if ch == '<' {
			inTag = true
		} else if ch == '>' {
			inTag = false
		} else if !inTag {
			result.WriteRune(ch)
		}
	}
	return result.String()
}

// fireReadyTimers runs any timers whose fire time has passed.
func (e *Engine) fireReadyTimers() int {
	now := time.Now()
	fired := 0

	// Snapshot the current timer list. Timer callbacks may add new timers
	// (via setTimeout/setInterval calls), so we must not lose those.
	snapshot := e.timers
	e.timers = nil // new timers from callbacks will be appended here

	var remaining []*timerEntry

	for _, t := range snapshot {
		if now.After(t.fireAt) || now.Equal(t.fireAt) {
			// Store the callback in a global, then call it via a try-catch
			// wrapper to capture the stack trace on errors.
			if err := e.ctx.Global().Set("_0", t.fn.Value); err == nil {
				// Log timer source (first 200 chars) for debugging
				e.ctx.Global().Set("_1", t.id)

				// Run the timer callback with a per-callback watchdog.
				// If the callback takes longer than 5s, terminate V8 execution.
				done := make(chan struct{})
				var result *v8.Value
				var runErr error
				go func() {
					result, runErr = e.ctx.RunScript(`
					(function() {
						var _fn = _0, _id = _1;
						try { delete globalThis._0; delete globalThis._1; } catch(x) {}
						try {
							var _s = _fn.toString();
							if (_s.length < 1000) console.log('[TIMER] #' + _id + ' src: ' + _s.substring(0, 200));
							else console.log('[TIMER] #' + _id + ' src(' + _s.length + '): ' + _s.substring(0, 200));
							return _fn();
						} catch(e) {
							console.log('[TIMER ERROR] ' + e.message + '\\n' + (e.stack || ''));
							if (e.__crashFn) console.log('[CRASH-FN] ' + e.__crashFn);
							if (e.__crashThis) console.log('[CRASH-THIS] ' + e.__crashThis);
							if (e.__vmInfo) console.log('[VM-STATE] ' + e.__vmInfo);
							if (e.__vmBC) console.log('[VM-BC] ' + e.__vmBC);
							try {
								if (typeof window !== 'undefined' && window._eventListeners && window._eventListeners.error) {
									var errEv = {type: 'error', message: e.message, error: e, filename: '', lineno: 0, colno: 0, preventDefault: function(){}, stopPropagation: function(){}};
									for (var i = 0; i < window._eventListeners.error.length; i++) {
										window._eventListeners.error[i](errEv);
									}
								}
							} catch(e2) {
								console.log('[TIMER ERROR] error handler failed: ' + e2.message);
							}
						}
					})()
				`, "timer-wrapper.js")
					close(done)
				}()

				select {
				case <-done:
					// Callback completed normally.
				case <-time.After(1800 * time.Second):
					e.log("Timer %d: callback exceeded 1800s timeout, terminating V8", t.id)
					e.iso.TerminateExecution()
					<-done // wait for the goroutine to finish
				}

				_ = result
				if runErr != nil {
					e.log("Timer %d callback error: %v", t.id, runErr)
				}
			} else {
				// Fallback to direct call.
				_, callErr := t.fn.Call(e.ctx.Global())
				if callErr != nil {
					e.log("Timer %d callback error: %v", t.id, callErr)
				}
			}
			fired++

			// Re-queue intervals.
			if t.interval {
				t.fireAt = now.Add(t.delay)
				remaining = append(remaining, t)
			}
		} else {
			remaining = append(remaining, t)
		}
	}

	// Merge: keep remaining old timers + any new timers added during callbacks.
	e.timers = append(remaining, e.timers...)
	return fired
}

// deliverSingleWorkerMessage delivers one worker message to the main V8 context
// via window.__workerOnMessage. Called from the event loop select case.
func (e *Engine) deliverSingleWorkerMessage(msg string) {
	e.log("Delivering worker message to main context (%d bytes)", len(msg))
	deliverJS := fmt.Sprintf(`
		(function() {
			if (typeof window.__workerOnMessage === 'function') {
				var rawData = %q;
				var data;
				try { data = JSON.parse(rawData); } catch(e) { data = rawData; }
				window.__workerOnMessage(data);
			} else {
				console.log('[WORKER-DELIVER] no __workerOnMessage handler set');
			}
		})();
	`, msg)
	if _, err := e.ctx.RunScript(deliverJS, "worker-deliver.js"); err != nil {
		e.log("Failed to deliver worker message: %v", err)
	}
	e.ctx.PerformMicrotaskCheckpoint()
}

// deliverWorkerMessages drains any pending messages from worker goroutines
// and delivers them to the main V8 context via window.__workerOnMessage.
// This is called from the event loop on each iteration.
func (e *Engine) deliverWorkerMessages() {
	for {
		select {
		case msg := <-e.workerMsgChan:
			e.deliverSingleWorkerMessage(msg)
		default:
			return // no more messages
		}
	}
}

func (e *Engine) addPendingOp() {
	e.mu.Lock()
	e.pendingOps++
	e.mu.Unlock()
}

func (e *Engine) removePendingOp() {
	e.mu.Lock()
	e.pendingOps--
	e.mu.Unlock()
}

// setupNavigator creates a native V8 Navigator object with accessor properties
// on the prototype, matching Chrome's internal representation. This ensures:
//   - GOPN(navigator) === 0 (no own properties)
//   - Properties are native V8 accessor properties on Navigator.prototype
//   - navigator instanceof Navigator === true (via FunctionTemplate linkage)
//   - Property descriptors match Chrome (configurable, enumerable, get/set)
//
// The remaining complex properties (plugins, mimeTypes, connection, etc.)
// are added later by dom.go and migrated to the prototype via _m2p().
func (e *Engine) setupNavigator() error {
	iso := e.iso
	ctx := e.ctx
	cfg := e.domCfg

	// Derive appVersion from UserAgent (strip "Mozilla/" prefix).
	appVersion := cfg.UserAgent
	if idx := strings.Index(appVersion, "/"); idx >= 0 {
		appVersion = appVersion[idx+1:]
	}

	// Navigator constructor: throws TypeError("Illegal constructor") like Chrome.
	navigatorCtor := v8.NewFunctionTemplateWithError(iso, func(info *v8.FunctionCallbackInfo) (*v8.Value, error) {
		return nil, v8.NewTypeError(iso, "Illegal constructor")
	})
	// Note: SetClassName is only available in the patched v8go fork.
	// The getter toString names are handled by dom.go's __mark(Navigator.prototype)
	// which adds "get propName" names via __fnNames / __maskedToString.

	proto := navigatorCtor.PrototypeTemplate()

	// Helper: create a getter FunctionTemplate that returns a static value.
	makeGetter := func(val interface{}) *v8.FunctionTemplate {
		return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			v, err := v8.NewValue(iso, val)
			if err != nil {
				return nil
			}
			return v
		})
	}

	// Helper: create a getter that returns null.
	makeNullGetter := func() *v8.FunctionTemplate {
		return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			return v8.Null(iso)
		})
	}

	// Define the 20 key scalar properties as native V8 accessor properties.
	// In Chrome these are getter-only (no setter), configurable, enumerable.
	// v8go's DontEnum=2, DontDelete=4, ReadOnly=1, None=0.
	// Chrome navigator properties are: enumerable=true, configurable=true => None.
	attrs := v8.None

	proto.SetAccessorProperty("userAgent", makeGetter(cfg.UserAgent), nil, attrs)
	proto.SetAccessorProperty("appVersion", makeGetter(appVersion), nil, attrs)
	proto.SetAccessorProperty("appName", makeGetter("Netscape"), nil, attrs)
	proto.SetAccessorProperty("appCodeName", makeGetter("Mozilla"), nil, attrs)
	proto.SetAccessorProperty("platform", makeGetter("MacIntel"), nil, attrs)
	proto.SetAccessorProperty("language", makeGetter("en-US"), nil, attrs)
	proto.SetAccessorProperty("hardwareConcurrency", makeGetter(int32(12)), nil, attrs)
	proto.SetAccessorProperty("cookieEnabled", makeGetter(true), nil, attrs)
	proto.SetAccessorProperty("webdriver", makeGetter(false), nil, attrs)
	proto.SetAccessorProperty("vendor", makeGetter("Google Inc."), nil, attrs)
	proto.SetAccessorProperty("product", makeGetter("Gecko"), nil, attrs)
	proto.SetAccessorProperty("productSub", makeGetter("20030107"), nil, attrs)
	proto.SetAccessorProperty("maxTouchPoints", makeGetter(int32(0)), nil, attrs)
	proto.SetAccessorProperty("onLine", makeGetter(true), nil, attrs)
	proto.SetAccessorProperty("deviceMemory", makeGetter(int32(8)), nil, attrs)
	proto.SetAccessorProperty("pdfViewerEnabled", makeGetter(true), nil, attrs)
	proto.SetAccessorProperty("globalPrivacyControl", makeGetter(false), nil, attrs)
	proto.SetAccessorProperty("doNotTrack", makeNullGetter(), nil, attrs)
	proto.SetAccessorProperty("vendorSub", makeGetter(""), nil, attrs)

	// Create the navigator instance from the FunctionTemplate's InstanceTemplate.
	// This produces an object whose [[Prototype]] is the Navigator.prototype with
	// the native accessor properties we just defined. The instance itself has zero
	// own properties, matching Chrome's GOPN(navigator) === 0.
	navObj, err := navigatorCtor.InstanceTemplate().NewInstance(ctx)
	if err != nil {
		return fmt.Errorf("create navigator instance: %w", err)
	}

	// Set navigator as a GETTER on the global (Chrome uses getters for window properties).
	// Object.getOwnPropertyDescriptor(window, 'navigator') must return {get:fn} not {value:obj}
	if err := ctx.Global().Set("__navObj", navObj); err != nil {
		return fmt.Errorf("set navigator temp: %w", err)
	}
	if _, err := ctx.RunScript(`(function() {
		var _n = __navObj;
		Object.defineProperty(this, 'navigator', {
			get: function() { return _n; },
			enumerable: true, configurable: true
		});
	}).call(this);`, "navigator-getter.js"); err != nil {
		return fmt.Errorf("set navigator getter: %w", err)
	}

	// Set the Navigator constructor on the global (for instanceof checks and dom.go references).
	navFunc := navigatorCtor.GetFunction(ctx)
	if err := ctx.Global().Set("Navigator", navFunc); err != nil {
		return fmt.Errorf("set Navigator constructor: %w", err)
	}

	// Set Symbol.toStringTag on Navigator.prototype so
	// Object.prototype.toString.call(navigator) returns "[object Navigator]".
	// Also set .name on the constructor function.
	_, err = ctx.RunScript(`
		Object.defineProperty(Navigator.prototype, Symbol.toStringTag, {
			value: 'Navigator', configurable: true
		});
		Object.defineProperty(Navigator, 'name', {
			value: 'Navigator', configurable: true
		});
	`, "navigator-setup.js")
	if err != nil {
		return fmt.Errorf("navigator toStringTag: %w", err)
	}

	e.log("Native Navigator created with %d accessor properties on prototype", 19)
	return nil
}

// setupScreen creates a native V8 Screen object with accessor properties on the
// prototype, matching Chrome's internal representation. This ensures:
//   - GOPN(screen) === 0 (no own properties)
//   - Properties are native V8 accessor properties on Screen.prototype
//   - screen instanceof Screen === true (via FunctionTemplate linkage)
//   - Property descriptors match Chrome (configurable, enumerable, get/set)
func (e *Engine) setupScreen() error {
	iso := e.iso
	ctx := e.ctx
	cfg := e.domCfg

	// Screen constructor: throws TypeError("Illegal constructor") like Chrome.
	screenCtor := v8.NewFunctionTemplateWithError(iso, func(info *v8.FunctionCallbackInfo) (*v8.Value, error) {
		return nil, v8.NewTypeError(iso, "Illegal constructor")
	})

	proto := screenCtor.PrototypeTemplate()

	// Helper: create a getter FunctionTemplate that returns a static value.
	makeGetter := func(val interface{}) *v8.FunctionTemplate {
		return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			v, err := v8.NewValue(iso, val)
			if err != nil {
				return nil
			}
			return v
		})
	}

	// Helper: create a getter that returns null.
	makeNullGetter := func() *v8.FunctionTemplate {
		return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			return v8.Null(iso)
		})
	}

	// Chrome screen properties are: enumerable=true, configurable=true => None.
	attrs := v8.None

	// Screen dimensions from DOMConfig.
	availHeight := cfg.ScreenHeight - 128 // macOS menu bar + dock (~128px)

	proto.SetAccessorProperty("width", makeGetter(int32(cfg.ScreenWidth)), nil, attrs)
	proto.SetAccessorProperty("height", makeGetter(int32(cfg.ScreenHeight)), nil, attrs)
	proto.SetAccessorProperty("availWidth", makeGetter(int32(cfg.ScreenWidth)), nil, attrs)
	proto.SetAccessorProperty("availHeight", makeGetter(int32(availHeight)), nil, attrs)
	proto.SetAccessorProperty("availLeft", makeGetter(int32(0)), nil, attrs)
	proto.SetAccessorProperty("availTop", makeGetter(int32(33)), nil, attrs)
	colorDepth := int32(cfg.ColorDepth)
	if colorDepth == 0 {
		colorDepth = 30 // default for modern Macs
	}
	pixelDepth := int32(cfg.PixelDepth)
	if pixelDepth == 0 {
		pixelDepth = 30
	}
	proto.SetAccessorProperty("colorDepth", makeGetter(colorDepth), nil, attrs)
	proto.SetAccessorProperty("pixelDepth", makeGetter(pixelDepth), nil, attrs)
	proto.SetAccessorProperty("isExtended", makeGetter(false), nil, attrs)
	proto.SetAccessorProperty("onchange", makeNullGetter(), nil, attrs)

	// Create the screen instance from the FunctionTemplate's InstanceTemplate.
	// This produces an object whose [[Prototype]] is the Screen.prototype with
	// the native accessor properties we just defined. The instance itself has zero
	// own properties, matching Chrome's GOPN(screen) === 0.
	screenObj, err := screenCtor.InstanceTemplate().NewInstance(ctx)
	if err != nil {
		return fmt.Errorf("create screen instance: %w", err)
	}

	// Set screen as a GETTER on the global (Chrome uses getters for window properties).
	if err := ctx.Global().Set("__screenObj", screenObj); err != nil {
		return fmt.Errorf("set screen global: %w", err)
	}

	// Set the Screen constructor on the global (for instanceof checks and dom.go references).
	screenFunc := screenCtor.GetFunction(ctx)
	if err := ctx.Global().Set("Screen", screenFunc); err != nil {
		return fmt.Errorf("set Screen constructor: %w", err)
	}

	// Define screen as getter (Chrome uses getters for window properties)
	if _, err := ctx.RunScript(`(function() {
		var _s = __screenObj;
		Object.defineProperty(this, 'screen', {
			get: function() { return _s; },
			enumerable: true, configurable: true
		});
	}).call(this);`, "screen-getter.js"); err != nil {
		return fmt.Errorf("set screen getter: %w", err)
	}

	// Set Symbol.toStringTag on Screen.prototype so
	// Object.prototype.toString.call(screen) returns "[object Screen]".
	// Also set .name on the constructor function.
	// Add the orientation sub-object as a regular JS object on Screen.prototype.
	_, err = ctx.RunScript(`
		Object.defineProperty(Screen.prototype, Symbol.toStringTag, {
			value: 'Screen', configurable: true
		});
		Object.defineProperty(Screen, 'name', {
			value: 'Screen', configurable: true
		});
		// orientation is a ScreenOrientation-like object on Screen.prototype
		(function() {
			var _orient = {
				type: "landscape-primary",
				angle: 0,
				onchange: null
			};
			Object.defineProperty(_orient, Symbol.toStringTag, {
				value: 'ScreenOrientation', configurable: true
			});
			// In Chrome, addEventListener/removeEventListener/dispatchEvent come from EventTarget
			_orient.addEventListener = function addEventListener() {};
			_orient.removeEventListener = function removeEventListener() {};
			_orient.dispatchEvent = function dispatchEvent() { return true; };
			Object.defineProperty(Screen.prototype, 'orientation', {
				get: function orientation() { return _orient; },
				configurable: true,
				enumerable: true
			});
			try { Object.defineProperty(Screen.prototype.orientation.__lookupGetter__('orientation') || (Object.getOwnPropertyDescriptor(Screen.prototype, 'orientation') || {}).get, 'name', { value: 'get orientation', configurable: true }); } catch(e) {}
		})();
	`, "screen-setup.js")
	if err != nil {
		return fmt.Errorf("screen setup script: %w", err)
	}

	e.log("Native Screen created with 12 accessor properties on prototype (w=%d, h=%d)", cfg.ScreenWidth, cfg.ScreenHeight)
	return nil
}

// setupPerformance creates a native V8 Performance object with accessor properties
// on the prototype, matching Chrome's internal representation. This ensures:
//   - GOPN(performance) === 0 (no own properties)
//   - Properties are native V8 accessor properties on Performance.prototype
//   - performance instanceof Performance === true (via FunctionTemplate linkage)
//   - performance.now() is a Go callback returning real elapsed time
//   - Property descriptors match Chrome (configurable, enumerable, get/set)
//
// The complex getEntriesByType method is overridden later by dom.go once location
// and other DOM globals are available. EventTarget prototype chain wiring also
// happens in dom.go after EventTarget is defined.
func (e *Engine) setupPerformance() error {
	iso := e.iso
	ctx := e.ctx

	// Compute timing values: timeOrigin is ~5 seconds before engine start (simulates page load).
	timeOriginMs := float64(e.startTime.UnixMilli()) - 5000

	// PerformanceTiming offsets from navigationStart (timeOrigin).
	timingOffsets := map[string]float64{
		"navigationStart":            0,
		"fetchStart":                 100,
		"domainLookupStart":          200,
		"domainLookupEnd":            300,
		"connectStart":               400,
		"connectEnd":                 600,
		"requestStart":               700,
		"responseStart":              900,
		"responseEnd":                1000,
		"domLoading":                 1500,
		"domInteractive":             3000,
		"domContentLoadedEventStart": 3500,
		"domContentLoadedEventEnd":   3600,
		"domComplete":                4500,
		"loadEventStart":             4600,
		"loadEventEnd":               4700,
	}

	// Performance constructor: throws TypeError("Illegal constructor") like Chrome.
	perfCtor := v8.NewFunctionTemplateWithError(iso, func(info *v8.FunctionCallbackInfo) (*v8.Value, error) {
		return nil, v8.NewTypeError(iso, "Illegal constructor")
	})

	proto := perfCtor.PrototypeTemplate()

	// Chrome Performance properties are: enumerable=true, configurable=true => None.
	attrs := v8.None

	// --- Accessor properties (getter-only) on prototype ---

	// timeOrigin, epoch milliseconds when the page "started loading"
	timeOriginGetter := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		v, _ := v8.NewValue(iso, timeOriginMs)
		return v
	})
	proto.SetAccessorProperty("timeOrigin", timeOriginGetter, nil, attrs)

	// now(), returns elapsed time since performance start (Go callback for real time)
	nowFn := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		elapsed := time.Since(e.startTime).Seconds() * 1000
		v, _ := v8.NewValue(iso, elapsed)
		return v
	})
	if err := proto.Set("now", nowFn); err != nil {
		return fmt.Errorf("set now: %w", err)
	}

	// onresourcetimingbufferfull, null event handler
	onrtbfGetter := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		return v8.Null(iso)
	})
	proto.SetAccessorProperty("onresourcetimingbufferfull", onrtbfGetter, nil, attrs)

	// --- Stub methods on prototype ---
	makeStub := func(name string) *v8.FunctionTemplate {
		_ = name // name used for documentation only; native toString handled by dom.go _mkFnNat
		fn := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			return v8.Undefined(iso)
		})
		return fn
	}

	for _, name := range []string{
		"mark", "measure", "clearMarks", "clearMeasures",
		"clearResourceTimings", "setResourceTimingBufferSize",
	} {
		if err := proto.Set(name, makeStub(name)); err != nil {
			return fmt.Errorf("set %s: %w", name, err)
		}
	}

	// Create the performance instance from the FunctionTemplate's InstanceTemplate.
	// This produces an object whose [[Prototype]] is the Performance.prototype with
	// the native accessor properties we just defined. The instance itself has zero
	// own properties, matching Chrome's GOPN(performance) === 0.
	perfObj, err := perfCtor.InstanceTemplate().NewInstance(ctx)
	if err != nil {
		return fmt.Errorf("create performance instance: %w", err)
	}

	// Set performance on the global object.
	if err := ctx.Global().Set("__perfObj", perfObj); err != nil {
		return fmt.Errorf("set performance global: %w", err)
	}

	// Set the Performance constructor on the global (for instanceof checks and dom.go references).
	perfFunc := perfCtor.GetFunction(ctx)
	if err := ctx.Global().Set("Performance", perfFunc); err != nil {
		return fmt.Errorf("set Performance constructor: %w", err)
	}

	// Define performance as getter (Chrome uses getters for window properties)
	// Capture value in closure so it survives cleanup of __perfObj
	if _, err := ctx.RunScript(`(function() {
		var _p = __perfObj;
		Object.defineProperty(this, 'performance', {
			get: function() { return _p; },
			enumerable: true, configurable: true
		});
	}).call(this);`, "performance-getter.js"); err != nil {
		return fmt.Errorf("set performance getter: %w", err)
	}

	// Build the PerformanceTiming property values string for injection into JS.
	// We need timing and navigation as JS objects accessible from the prototype getters.
	// Also set up: Symbol.toStringTag, eventCounts (Map), getEntries/getEntriesByName/
	// getEntriesByType (basic versions), toJSON, measureUserAgentSpecificMemory.
	//
	// The memory getter and complex getEntriesByType are set here as JS since they
	// need to create fresh objects or reference closures.
	timingJS := fmt.Sprintf(`(function() {
		var _perfTimeOrigin = %f;
		var _perfTiming = {
			navigationStart: _perfTimeOrigin,
			fetchStart: _perfTimeOrigin + %f,
			domainLookupStart: _perfTimeOrigin + %f,
			domainLookupEnd: _perfTimeOrigin + %f,
			connectStart: _perfTimeOrigin + %f,
			connectEnd: _perfTimeOrigin + %f,
			requestStart: _perfTimeOrigin + %f,
			responseStart: _perfTimeOrigin + %f,
			responseEnd: _perfTimeOrigin + %f,
			domLoading: _perfTimeOrigin + %f,
			domInteractive: _perfTimeOrigin + %f,
			domContentLoadedEventStart: _perfTimeOrigin + %f,
			domContentLoadedEventEnd: _perfTimeOrigin + %f,
			domComplete: _perfTimeOrigin + %f,
			loadEventStart: _perfTimeOrigin + %f,
			loadEventEnd: _perfTimeOrigin + %f
		};
		var _perfNavigation = { type: 0, redirectCount: 0, toJSON: function() { return {type: 0, redirectCount: 0}; } };

		// Store timing data on a hidden global for dom.go's getEntriesByType override
		Object.defineProperty(globalThis, '_cfPt', { value: _perfTiming, configurable: true, writable: true });
		Object.defineProperty(globalThis, '_cfPto', { value: _perfTimeOrigin, configurable: true, writable: true });

		// Override the memory getter to return a fresh empty object
		Object.defineProperty(Performance.prototype, 'memory', {
			get: function() { return {}; },
			enumerable: true, configurable: true
		});

		// timing, PerformanceTiming object
		Object.defineProperty(Performance.prototype, 'timing', {
			get: function() { return _perfTiming; },
			enumerable: true, configurable: true
		});

		// navigation, {type: 0, redirectCount: 0}
		Object.defineProperty(Performance.prototype, 'navigation', {
			get: function() { return _perfNavigation; },
			enumerable: true, configurable: true
		});

		// eventCounts, Map
		Object.defineProperty(Performance.prototype, 'eventCounts', {
			get: function() { return new Map(); },
			enumerable: true, configurable: true
		});

		// Symbol.toStringTag
		Object.defineProperty(Performance.prototype, Symbol.toStringTag, {
			value: 'Performance', configurable: true
		});
		Object.defineProperty(Performance, 'name', {
			value: 'Performance', configurable: true
		});

		// getEntriesByType, basic version (will be overridden by dom.go with location-aware version)
		Performance.prototype.getEntriesByType = function getEntriesByType(type) {
			if (type === 'navigation') {
				return [{
					name: '',
					entryType: 'navigation',
					startTime: 0,
					duration: _perfTiming.loadEventEnd - _perfTiming.navigationStart,
					initiatorType: 'navigation',
					redirectCount: 0,
					type: 'navigate',
					transferSize: 158000,
					encodedBodySize: 154000,
					decodedBodySize: 154000,
					domComplete: _perfTiming.domComplete - _perfTiming.navigationStart,
					domContentLoadedEventEnd: _perfTiming.domContentLoadedEventEnd - _perfTiming.navigationStart,
					domContentLoadedEventStart: _perfTiming.domContentLoadedEventStart - _perfTiming.navigationStart,
					domInteractive: _perfTiming.domInteractive - _perfTiming.navigationStart,
					loadEventEnd: _perfTiming.loadEventEnd - _perfTiming.navigationStart,
					loadEventStart: _perfTiming.loadEventStart - _perfTiming.navigationStart,
					responseEnd: _perfTiming.responseEnd - _perfTiming.navigationStart,
					responseStart: _perfTiming.responseStart - _perfTiming.navigationStart,
					requestStart: _perfTiming.requestStart - _perfTiming.navigationStart,
					connectEnd: _perfTiming.connectEnd - _perfTiming.navigationStart,
					connectStart: _perfTiming.connectStart - _perfTiming.navigationStart,
					domainLookupEnd: _perfTiming.domainLookupEnd - _perfTiming.navigationStart,
					domainLookupStart: _perfTiming.domainLookupStart - _perfTiming.navigationStart,
					fetchStart: _perfTiming.fetchStart - _perfTiming.navigationStart,
					serverTiming: [],
					toJSON: function() { return this; }
				}];
			}
			if (type === 'paint') {
				return [
					{name: 'first-paint', entryType: 'paint', startTime: _perfTiming.domInteractive - _perfTiming.navigationStart - 50, duration: 0},
					{name: 'first-contentful-paint', entryType: 'paint', startTime: _perfTiming.domInteractive - _perfTiming.navigationStart - 30, duration: 0}
				];
			}
			if (type === 'resource') { return []; }
			return [];
		};

		// getEntries
		Performance.prototype.getEntries = function getEntries() {
			var nav = this.getEntriesByType('navigation');
			var res = this.getEntriesByType('resource');
			var paint = this.getEntriesByType('paint');
			return nav.concat(res).concat(paint);
		};

		// getEntriesByName
		Performance.prototype.getEntriesByName = function getEntriesByName(name) {
			return this.getEntries().filter(function(e) { return e.name === name; });
		};

		// measureUserAgentSpecificMemory
		Performance.prototype.measureUserAgentSpecificMemory = function measureUserAgentSpecificMemory() {
			return Promise.resolve({bytes: 24331593, breakdown: []});
		};

		// toJSON
		Performance.prototype.toJSON = function toJSON() {
			return { timeOrigin: this.timeOrigin, timing: this.timing, navigation: this.navigation };
		};
	})();`,
		timeOriginMs,
		timingOffsets["fetchStart"],
		timingOffsets["domainLookupStart"],
		timingOffsets["domainLookupEnd"],
		timingOffsets["connectStart"],
		timingOffsets["connectEnd"],
		timingOffsets["requestStart"],
		timingOffsets["responseStart"],
		timingOffsets["responseEnd"],
		timingOffsets["domLoading"],
		timingOffsets["domInteractive"],
		timingOffsets["domContentLoadedEventStart"],
		timingOffsets["domContentLoadedEventEnd"],
		timingOffsets["domComplete"],
		timingOffsets["loadEventStart"],
		timingOffsets["loadEventEnd"],
	)

	if _, err := ctx.RunScript(timingJS, "performance-setup.js"); err != nil {
		return fmt.Errorf("performance setup script: %w", err)
	}

	e.log("Native Performance created with accessor properties + Go now() callback (timeOrigin=%.0f)", timeOriginMs)
	return nil
}

// setupDocument creates native V8 Document and HTMLDocument constructors with
// accessor properties on Document.prototype, matching Chrome's internal
// representation. This ensures:
//   - The top 20 fingerprint-sensitive properties are native V8 accessor properties
//   - Property descriptors match Chrome (configurable, enumerable, getter-only)
//   - document instanceof HTMLDocument === true (via FunctionTemplate linkage)
//   - Document.prototype.constructor / HTMLDocument.prototype.constructor work correctly
//
// Properties that depend on JS-side state (body, head, documentElement, cookie)
// use getters that read from global variables set by dom.go after the DOM tree
// is created. The remaining ~231 properties stay as JS stubs on Document.prototype
// populated by dom.go.
//
// dom.go wires the prototype chain: Document.prototype → Node.prototype and
// HTMLDocument.prototype → Document.prototype via Object.setPrototypeOf (which
// preserves the native accessor properties while establishing inheritance).
func (e *Engine) setupDocument() error {
	iso := e.iso
	ctx := e.ctx
	cfg := e.domCfg

	// Parse URL for static property values.
	parsedURL, _ := url.Parse(cfg.URL)
	domain := ""
	if parsedURL != nil {
		domain = parsedURL.Hostname()
	}

	// Document constructor: throws TypeError("Illegal constructor") like Chrome.
	documentCtor := v8.NewFunctionTemplateWithError(iso, func(info *v8.FunctionCallbackInfo) (*v8.Value, error) {
		return nil, v8.NewTypeError(iso, "Illegal constructor")
	})

	proto := documentCtor.PrototypeTemplate()

	// Helper: create a getter FunctionTemplate that returns a static string.
	makeStrGetter := func(val string) *v8.FunctionTemplate {
		return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			v, err := v8.NewValue(iso, val)
			if err != nil {
				return nil
			}
			return v
		})
	}

	// Helper: create a getter that returns a static bool.
	makeBoolGetter := func(val bool) *v8.FunctionTemplate {
		return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			v, err := v8.NewValue(iso, val)
			if err != nil {
				return nil
			}
			return v
		})
	}

	// Helper: create a getter that reads a global variable (for JS-side state).
	// Used for properties like body, head, documentElement that are set by dom.go.
	makeGlobalGetter := func(globalName string) *v8.FunctionTemplate {
		return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			global := e.ctx.Global()
			val, err := global.Get(globalName)
			if err != nil || val == nil || val.IsUndefined() || val.IsNull() {
				return v8.Null(iso)
			}
			return val
		})
	}

	// Helper: create a getter that runs a JS expression (for cookie which needs
	// to serialize the _ck object each time).
	makeScriptGetter := func(script string) *v8.FunctionTemplate {
		return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			val, err := e.ctx.RunScript(script, "doc-getter")
			if err != nil {
				empty, _ := v8.NewValue(iso, "")
				return empty
			}
			return val
		})
	}

	// Chrome Document properties: enumerable=true, configurable=true => None.
	attrs := v8.None

	// --- Static string properties (values known at setup time) ---
	proto.SetAccessorProperty("URL", makeStrGetter(cfg.URL), nil, attrs)
	proto.SetAccessorProperty("documentURI", makeStrGetter(cfg.URL), nil, attrs)
	proto.SetAccessorProperty("domain", makeStrGetter(domain), nil, attrs)
	proto.SetAccessorProperty("referrer", makeStrGetter(cfg.Referrer), nil, attrs)
	proto.SetAccessorProperty("characterSet", makeStrGetter("UTF-8"), nil, attrs)
	proto.SetAccessorProperty("charset", makeStrGetter("UTF-8"), nil, attrs)
	proto.SetAccessorProperty("inputEncoding", makeStrGetter("UTF-8"), nil, attrs)
	proto.SetAccessorProperty("contentType", makeStrGetter("text/html"), nil, attrs)
	proto.SetAccessorProperty("compatMode", makeStrGetter("CSS1Compat"), nil, attrs)
	proto.SetAccessorProperty("readyState", makeStrGetter("complete"), nil, attrs)
	proto.SetAccessorProperty("designMode", makeStrGetter("off"), nil, attrs)
	proto.SetAccessorProperty("dir", makeStrGetter("ltr"), nil, attrs)
	proto.SetAccessorProperty("visibilityState", makeStrGetter("visible"), nil, attrs)
	proto.SetAccessorProperty("title", makeStrGetter("Checking your Browser\u2026"), nil, attrs)

	// --- Boolean properties ---
	proto.SetAccessorProperty("hidden", makeBoolGetter(false), nil, attrs)

	// --- Dynamic properties (read from JS globals set by dom.go) ---
	// dom.go sets _cfDb, _cfDh, _cfDe after the DOM tree is created.
	proto.SetAccessorProperty("body", makeGlobalGetter("_cfDb"), nil, attrs)
	proto.SetAccessorProperty("head", makeGlobalGetter("_cfDh"), nil, attrs)
	proto.SetAccessorProperty("documentElement", makeGlobalGetter("_cfDe"), nil, attrs)

	// cookie, needs to serialize the _ck object each time it's read.
	// The getter runs a small JS snippet that reads the _ck variable from dom.go scope.
	// Note: This native getter will be overridden in dom.go with a proper get/set pair
	// that has access to the _ck closure. We define it here so that the property
	// descriptor shows as a native getter before dom.go overrides it.
	proto.SetAccessorProperty("cookie", makeScriptGetter(`
		(function() {
			try {
				var g = (0, eval)('this');
				var ck = g._cfDc;
				if (!ck) return '';
				var parts = [];
				for (var k in ck) parts.push(k + '=' + ck[k]);
				return parts.join('; ');
			} catch(e) { return ''; }
		})()
	`), nil, attrs)

	// Set the Document constructor on the global.
	docFunc := documentCtor.GetFunction(ctx)
	if err := ctx.Global().Set("Document", docFunc); err != nil {
		return fmt.Errorf("set Document constructor: %w", err)
	}

	// Set Symbol.toStringTag and name on Document.
	_, err := ctx.RunScript(`
		Object.defineProperty(Document.prototype, Symbol.toStringTag, {
			value: 'Document', configurable: true
		});
		Object.defineProperty(Document, 'name', {
			value: 'Document', configurable: true
		});
	`, "document-setup.js")
	if err != nil {
		return fmt.Errorf("document toStringTag: %w", err)
	}

	// HTMLDocument constructor: throws TypeError("Illegal constructor") like Chrome.
	// HTMLDocument inherits from Document (its prototype chain is set up later in
	// dom.go via Object.setPrototypeOf since Node is defined in JS).
	htmlDocCtor := v8.NewFunctionTemplateWithError(iso, func(info *v8.FunctionCallbackInfo) (*v8.Value, error) {
		return nil, v8.NewTypeError(iso, "Illegal constructor")
	})
	// HTMLDocument inherits from Document via FunctionTemplate.Inherit.
	htmlDocCtor.Inherit(documentCtor)

	// Set the HTMLDocument constructor on the global.
	htmlDocFunc := htmlDocCtor.GetFunction(ctx)
	if err := ctx.Global().Set("HTMLDocument", htmlDocFunc); err != nil {
		return fmt.Errorf("set HTMLDocument constructor: %w", err)
	}

	// Set Symbol.toStringTag and name on HTMLDocument.
	_, err = ctx.RunScript(`
		Object.defineProperty(HTMLDocument.prototype, Symbol.toStringTag, {
			value: 'HTMLDocument', configurable: true
		});
		Object.defineProperty(HTMLDocument, 'name', {
			value: 'HTMLDocument', configurable: true
		});
	`, "htmldocument-setup.js")
	if err != nil {
		return fmt.Errorf("htmldocument toStringTag: %w", err)
	}

	e.log("Native Document created with %d native accessor properties on prototype, HTMLDocument inheriting", 20)
	return nil
}

// setupElementHandler creates the ObjectTemplate with a V8 named property handler
// for creating native DOM elements with zero own properties.
//
// Element state (tagName, id, className, etc.) is stored in a Go-side map
// (Engine.elementStates) keyed by an integer ID in internal field 0. The C++
// named property handler serves all reads/writes from this Go map, so the
// element objects have zero JS own properties for intercepted names, matching
// Chrome where Object.getOwnPropertyNames(el) returns [].
//
// Methods and complex objects (style, classList, children, etc.) are NOT
// intercepted, they fall through to JS own properties set by _mkEl in dom.go.
func (e *Engine) setupElementHandler() error {
	iso := e.iso

	// goStateProps lists the Go-side ElementState properties that are served
	// via native data property accessors (SetNativeDataProperty). Each property
	// becomes a V8 native accessor, identical to how Chrome's Blink exposes
	// DOM attributes via C++ accessors rather than generic interceptors.
	//
	// NOTE: "src" is intentionally excluded, iframe elements override it via
	// Object.defineProperty with a custom getter/setter in dom.go.
	// NOTE: innerHTML, outerHTML, innerText, textContent are intentionally
	// excluded, they're now JS-side getters/setters that traverse the DOM tree
	// (see _domGetInnerHTML, _domGetTextContent, _domSetInnerHTML in dom.go).
	goStateProps := []string{
		"tagName", "nodeName", "nodeType",
		"id", "className",
		"localName", "namespaceURI", "prefix",
		"baseURI", "isConnected",
		"offsetWidth", "offsetHeight", "offsetTop", "offsetLeft",
		"clientWidth", "clientHeight",
		"scrollWidth", "scrollHeight", "scrollTop", "scrollLeft",
		"href", "type", "rel", "media",
		"nonce", "value", "name", "crossOrigin",
		"checked", "disabled",
		"width", "height",
	}

	// Node type constants are on Node.prototype in the DOM script (dom.go),
	// NOT as native data properties on each element instance.

	tmpl := v8.NewObjectTemplate(iso)
	// Internal field 0 stores the element's integer ID for Go-side state lookup.
	tmpl.SetInternalFieldCount(1)

	// Install native data property accessors for each Go-state property.
	// V8 treats these identically to Blink's C++ DOM accessors, no interceptor
	// flag is set on the object, making it indistinguishable from real DOM elements.
	for _, prop := range goStateProps {
		propName := prop // capture for closure
		tmpl.SetNativeDataProperty(propName,
			// Getter: read from Go-side ElementState map.
			func(property string, this *v8.Object, info *v8.NativeDataPropertyCallbackInfo) (*v8.Value, error) {
				idVal := this.GetInternalField(0)
				if idVal == nil || idVal.IsUndefined() {
					return nil, nil
				}
				id := idVal.Int32()
				state := e.elementStates[id]
				if state == nil {
					return nil, nil
				}
				return e.getElementProperty(state, propName)
			},
			// Setter: write to Go-side ElementState map.
			func(property string, value *v8.Value, this *v8.Object, info *v8.NativeDataPropertyCallbackInfo) error {
				idVal := this.GetInternalField(0)
				if idVal == nil || idVal.IsUndefined() {
					return nil
				}
				id := idVal.Int32()
				state := e.elementStates[id]
				if state == nil {
					return nil
				}
				e.setElementProperty(state, propName, value)
				// For innerHTML: parse HTML and create child elements.
				if propName == "innerHTML" {
					html := value.String()
					if html != "" && strings.Contains(html, "<") {
						e.parseInnerHTMLChildren(this, html)
					}
				}
				return nil
			},
			v8.DontEnum, // non-enumerable, matching Chrome DOM accessors
		)
	}

	// Install native data property accessors for node type constants.
	// These are read-only, non-enumerable, non-deletable, matching Chrome.
	// NOTE: Node type constants (ELEMENT_NODE, DOCUMENT_NODE, etc.) are NOT
	// registered as native data properties on the element template. In Chrome,
	// they exist on Node.prototype, not as own properties of each element.
	// Having them as own properties is a bot detection signal (elements should
	// have 0 own enumerable properties). The constants are set on Node.prototype
	// in the DOM script (dom.go).

	e.elementTmpl = tmpl
	e.log("Native element ObjectTemplate created with SetNativeDataProperty (Go-state: %d props)", len(goStateProps))
	return nil
}

// parseInnerHTMLChildren parses HTML and creates child elements via JS.
// Called from the native innerHTML setter to populate children/childNodes.
func (e *Engine) parseInnerHTMLChildren(thisObj *v8.Object, html string) {
	// Set the element as a global temp var, run the parser, then clean up.
	global := e.ctx.Global()
	global.Set("__ihTarget", thisObj)

	script := fmt.Sprintf(`(function(){
		var el = __ihTarget;
		if (!el || !el.appendChild) return;
		if (el.children) el.children.length = 0;
		if (el.childNodes) el.childNodes.length = 0;
		var html = %q;
		var re = /<(\w+)([^>]*)(?:\/?>)/g;
		var m;
		while ((m = re.exec(html)) !== null) {
			try {
				var child = document.createElement(m[1]);
				var attrs = m[2] || "";
				var ar = /(\w[\w-]*)=["']([^"']*?)["']/g;
				var am;
				while ((am = ar.exec(attrs)) !== null) {
					try { child.setAttribute(am[1], am[2]); } catch(e) {}
					if (am[1] === 'src') try { child.src = am[2]; } catch(e) {}
					if (am[1] === 'id') try { child.id = am[2]; } catch(e) {}
				}
				el.appendChild(child);
			} catch(e) {}
		}
		delete globalThis.__ihTarget;
	})()`, html)

	if _, err := e.ctx.RunScript(script, "innerHTML-parse"); err != nil {
		e.log("parseInnerHTMLChildren error: %v", err)
	}
}

// getElementProperty reads a property from the Go-side ElementState and returns
// it as a V8 value.
func (e *Engine) getElementProperty(state *ElementState, property string) (*v8.Value, error) {
	iso := e.iso
	switch property {
	case "tagName":
		return v8.NewValue(iso, state.TagName)
	case "nodeName":
		return v8.NewValue(iso, state.NodeName)
	case "nodeType":
		return v8.NewValue(iso, state.NodeType)
	case "id":
		return v8.NewValue(iso, state.ID)
	case "className":
		return v8.NewValue(iso, state.ClassName)
	case "innerHTML":
		return v8.NewValue(iso, state.InnerHTML)
	case "outerHTML":
		return v8.NewValue(iso, state.OuterHTML)
	case "innerText":
		return v8.NewValue(iso, state.InnerText)
	case "textContent":
		return v8.NewValue(iso, state.TextContent)
	case "localName":
		return v8.NewValue(iso, state.LocalName)
	case "namespaceURI":
		if state.NamespaceURI == "" {
			// Chrome returns "http://www.w3.org/1999/xhtml" for HTML elements
			return v8.NewValue(iso, "http://www.w3.org/1999/xhtml")
		}
		return v8.NewValue(iso, state.NamespaceURI)
	case "prefix":
		if state.Prefix == "" {
			// Chrome returns null for prefix on HTML elements
			return v8.Null(iso), nil
		}
		return v8.NewValue(iso, state.Prefix)
	case "baseURI":
		return v8.NewValue(iso, state.BaseURI)
	case "isConnected":
		return v8.NewValue(iso, state.IsConnected)
	case "offsetWidth":
		// Dynamic width for inline elements based on textContent (font detection).
		// BM scripts create spans with test text, then read offsetWidth.
		// Static widths for all spans → instant bot detection.
		if isInlineTag(state.TagName) {
			text := state.TextContent
			if text == "" {
				text = stripHTMLTags(state.InnerHTML)
			}
			if text != "" {
				w := estimateTextWidth(text, state.OffsetWidth)
				return v8.NewValue(iso, int32(w))
			}
		}
		return v8.NewValue(iso, state.OffsetWidth)
	case "offsetHeight":
		// Dynamic height for inline elements
		if isInlineTag(state.TagName) {
			text := state.TextContent
			if text == "" {
				text = stripHTMLTags(state.InnerHTML)
			}
			if text != "" {
				// Height is roughly font-size * line-height (1.2)
				return v8.NewValue(iso, int32(19)) // ~16px * 1.2
			}
		}
		return v8.NewValue(iso, state.OffsetHeight)
	case "offsetTop":
		return v8.NewValue(iso, state.OffsetTop)
	case "offsetLeft":
		return v8.NewValue(iso, state.OffsetLeft)
	case "clientWidth":
		return v8.NewValue(iso, state.ClientWidth)
	case "clientHeight":
		return v8.NewValue(iso, state.ClientHeight)
	case "scrollWidth":
		return v8.NewValue(iso, state.ScrollWidth)
	case "scrollHeight":
		return v8.NewValue(iso, state.ScrollHeight)
	case "scrollTop":
		return v8.NewValue(iso, state.ScrollTop)
	case "scrollLeft":
		return v8.NewValue(iso, state.ScrollLeft)
	case "src":
		return v8.NewValue(iso, state.Src)
	case "href":
		return v8.NewValue(iso, state.Href)
	case "type":
		return v8.NewValue(iso, state.Type)
	case "rel":
		return v8.NewValue(iso, state.Rel)
	case "media":
		return v8.NewValue(iso, state.Media)
	case "nonce":
		return v8.NewValue(iso, state.Nonce)
	case "value":
		return v8.NewValue(iso, state.Value)
	case "name":
		return v8.NewValue(iso, state.Name)
	case "crossOrigin":
		if state.CrossOrigin == "" {
			// Chrome returns null for crossOrigin when not set
			return v8.Null(iso), nil
		}
		return v8.NewValue(iso, state.CrossOrigin)
	case "checked":
		return v8.NewValue(iso, state.Checked)
	case "disabled":
		return v8.NewValue(iso, state.Disabled)
	case "width":
		return v8.NewValue(iso, state.Width)
	case "height":
		return v8.NewValue(iso, state.Height)
	}
	return nil, nil
}

// setElementProperty writes a property value to the Go-side ElementState.
func (e *Engine) setElementProperty(state *ElementState, property string, value *v8.Value) {
	switch property {
	case "tagName":
		state.TagName = value.String()
	case "nodeName":
		state.NodeName = value.String()
	case "nodeType":
		state.NodeType = value.Int32()
	case "id":
		state.ID = value.String()
	case "className":
		state.ClassName = value.String()
	case "innerHTML":
		state.InnerHTML = value.String()
	case "outerHTML":
		state.OuterHTML = value.String()
	case "innerText":
		state.InnerText = value.String()
	case "textContent":
		state.TextContent = value.String()
	case "localName":
		state.LocalName = value.String()
	case "namespaceURI":
		state.NamespaceURI = value.String()
	case "prefix":
		state.Prefix = value.String()
	case "baseURI":
		state.BaseURI = value.String()
	case "isConnected":
		state.IsConnected = value.Boolean()
	case "offsetWidth":
		state.OffsetWidth = value.Int32()
	case "offsetHeight":
		state.OffsetHeight = value.Int32()
	case "offsetTop":
		state.OffsetTop = value.Int32()
	case "offsetLeft":
		state.OffsetLeft = value.Int32()
	case "clientWidth":
		state.ClientWidth = value.Int32()
	case "clientHeight":
		state.ClientHeight = value.Int32()
	case "scrollWidth":
		state.ScrollWidth = value.Int32()
	case "scrollHeight":
		state.ScrollHeight = value.Int32()
	case "scrollTop":
		state.ScrollTop = value.Int32()
	case "scrollLeft":
		state.ScrollLeft = value.Int32()
	case "src":
		state.Src = value.String()
	case "href":
		state.Href = value.String()
	case "type":
		state.Type = value.String()
	case "rel":
		state.Rel = value.String()
	case "media":
		state.Media = value.String()
	case "nonce":
		state.Nonce = value.String()
	case "value":
		state.Value = value.String()
	case "name":
		state.Name = value.String()
	case "crossOrigin":
		state.CrossOrigin = value.String()
	case "checked":
		state.Checked = value.Boolean()
	case "disabled":
		state.Disabled = value.Boolean()
	case "width":
		state.Width = value.Int32()
	case "height":
		state.Height = value.Int32()
	}
}

func (e *Engine) registerCallbacks(global *v8.ObjectTemplate) error {
	// Time compression factor: TimeDilation < 1 compresses perceived time.
	// E.g. 0.02 = 50x compression: 43s real → 0.86s perceived.
	// This prevents time-based overrun checkers from triggering.
	timeCompression := e.domCfg.TimeDilation
	if timeCompression <= 0 || timeCompression > 1 {
		timeCompression = 1 // no compression by default
	}

	_ = timeCompression // Used only by Date.now() in the DOM script

	// __goPerformanceNow, returns REAL elapsed time (no compression).
	// The VM bytecoded computation uses performance.now() for batch sizing.
	// Compressing it would cause the VM to think it has more time budget,
	// creating proportionally more work. Date.now() is compressed separately
	// in the DOM script, the overrun checker uses Date.now(), not performance.now().
	if err := global.Set("__goPerformanceNow",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			elapsed := time.Since(e.startTime).Seconds() * 1000
			val, _ := v8.NewValue(e.iso, elapsed)
			return val
		})); err != nil {
		return err
	}

	// __goAtob - base64 decode
	if err := global.Set("__goAtob",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			if len(info.Args()) < 1 {
				val, _ := v8.NewValue(e.iso, "")
				return val
			}
			s := info.Args()[0].String()
			// Handle URL-safe base64 and missing padding.
			decoded, err := base64.RawStdEncoding.DecodeString(s)
			if err != nil {
				decoded, err = base64.StdEncoding.DecodeString(s)
			}
			if err != nil {
				val, _ := v8.NewValue(e.iso, "")
				return val
			}
			val, _ := v8.NewValue(e.iso, string(decoded))
			return val
		})); err != nil {
		return err
	}

	// __goBtoa - base64 encode
	if err := global.Set("__goBtoa",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			if len(info.Args()) < 1 {
				val, _ := v8.NewValue(e.iso, "")
				return val
			}
			encoded := base64.StdEncoding.EncodeToString([]byte(info.Args()[0].String()))
			val, _ := v8.NewValue(e.iso, encoded)
			return val
		})); err != nil {
		return err
	}

	// __goDigest - crypto.subtle.digest (SHA-256) with PoW detection
	var digestCount int64
	var digestStart time.Time
	var lastDigestLog time.Time
	var firstInput string
	if err := global.Set("__goDigest",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			if len(info.Args()) < 2 {
				val, _ := v8.NewValue(e.iso, "")
				return val
			}
			data := info.Args()[1].String()
			digestCount++
			now := time.Now()
			if digestCount == 1 {
				digestStart = now
				lastDigestLog = now
				firstInput = data
				if len(firstInput) > 100 {
					firstInput = firstInput[:100]
				}
				log.Printf("[engine] First __goDigest call: algo=%s input(%d)=%q",
					info.Args()[0].String(), len(data), firstInput)
			}
			if now.Sub(lastDigestLog) > 5*time.Second {
				elapsed := now.Sub(digestStart).Seconds()
				rate := float64(digestCount) / elapsed
				preview := data
				if len(preview) > 80 {
					preview = preview[:80]
				}
				log.Printf("[engine] __goDigest: count=%d rate=%.0f/s elapsed=%.1fs latest(%d)=%q",
					digestCount, rate, elapsed, len(data), preview)
				lastDigestLog = now
			}
			hash := sha256.Sum256([]byte(data))
			hexHash := hex.EncodeToString(hash[:])
			val, _ := v8.NewValue(e.iso, hexHash)
			return val
		})); err != nil {
		return err
	}

	// __goPatchWorkerCode - apply VM optimizations to Worker eval code.
	// Called from JS when Worker.postMessage receives large code strings.
	if err := global.Set("__goPatchWorkerCode",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			if len(info.Args()) < 1 {
				val, _ := v8.NewValue(e.iso, "")
				return val
			}
			code := info.Args()[0].String()
			e.mu.Lock()
			handler := e.patchHandler
			e.mu.Unlock()
			if handler != nil {
				patched := handler(code)
				if patched != code {
					log.Printf("[engine] __goPatchWorkerCode: patched %d → %d bytes", len(code), len(patched))
				}
				val, _ := v8.NewValue(e.iso, patched)
				return val
			}
			val, _ := v8.NewValue(e.iso, code)
			return val
		})); err != nil {
		return err
	}

	// __goSetTimeout - register a timer callback
	if err := global.Set("__goSetTimeout",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			args := info.Args()
			if len(args) < 1 {
				val, _ := v8.NewValue(e.iso, int32(0))
				return val
			}

			fn, err := args[0].AsFunction()
			if err != nil {
				val, _ := v8.NewValue(e.iso, int32(0))
				return val
			}

			delay := 0
			if len(args) > 1 && args[1].IsNumber() {
				delay = int(args[1].Integer())
			}

			e.nextTimerID++
			id := e.nextTimerID

			e.timers = append(e.timers, &timerEntry{
				id:     id,
				fn:     fn,
				delay:  time.Duration(delay) * time.Millisecond,
				fireAt: time.Now().Add(time.Duration(delay) * time.Millisecond),
			})

			val, _ := v8.NewValue(e.iso, id)
			return val
		})); err != nil {
		return err
	}

	// __goSetInterval - register a repeating timer
	// For managed challenges, dilate interval delays so timeout checker timers
	// fire less often, preventing premature overrun detection.
	if err := global.Set("__goSetInterval",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			args := info.Args()
			if len(args) < 1 {
				val, _ := v8.NewValue(e.iso, int32(0))
				return val
			}

			fn, err := args[0].AsFunction()
			if err != nil {
				val, _ := v8.NewValue(e.iso, int32(0))
				return val
			}

			delay := 0
			if len(args) > 1 && args[1].IsNumber() {
				delay = int(args[1].Integer())
			}
			if delay < 10 {
				delay = 10
			}

			realDelay := time.Duration(delay) * time.Millisecond

			e.nextTimerID++
			id := e.nextTimerID

			e.timers = append(e.timers, &timerEntry{
				id:       id,
				fn:       fn,
				delay:    realDelay,
				fireAt:   time.Now().Add(realDelay),
				interval: true,
			})

			val, _ := v8.NewValue(e.iso, id)
			return val
		})); err != nil {
		return err
	}

	// __goClearTimer - cancel a timer by ID
	if err := global.Set("__goClearTimer",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			args := info.Args()
			if len(args) < 1 {
				return nil
			}
			id := int32(args[0].Integer())

			var remaining []*timerEntry
			for _, t := range e.timers {
				if t.id != id {
					remaining = append(remaining, t)
				}
			}
			e.timers = remaining
			return nil
		})); err != nil {
		return err
	}

	// __goFetch - intercept fetch() calls, return a Promise
	if err := global.Set("__goFetch",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			resolver, err := v8.NewPromiseResolver(info.Context())
			if err != nil {
				e.log("Failed to create promise resolver: %v", err)
				val, _ := v8.NewValue(e.iso, false)
				return val
			}

			e.mu.Lock()
			handler := e.fetchHandler
			e.mu.Unlock()

			if handler == nil {
				errVal, _ := v8.NewValue(e.iso, "no fetch handler configured")
				resolver.Reject(errVal)
				return resolver.GetPromise().Value
			}

			urlStr := ""
			if len(info.Args()) > 0 {
				urlStr = info.Args()[0].String()
			}

			opts := make(map[string]interface{})
			if len(info.Args()) > 1 {
				optsStr := info.Args()[1].String()
				opts["raw"] = optsStr
				// Parse JSON-serialized opts from JS fetch() wrapper.
				// The JS side serializes {method, body, headers, ...} to JSON string.
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(optsStr), &parsed); err == nil {
					for k, v := range parsed {
						opts[k] = v
					}
				}
			}

			e.log("fetch() called: %s opts=%v", urlStr, opts["method"])
			e.addPendingOp()

			// Execute fetch synchronously on the V8 thread.
			// We can't use goroutines because V8 is single-threaded.
			body, status, respHeaders, fetchErr := handler(urlStr, opts)
			e.removePendingOp()

			if fetchErr != nil {
				e.log("fetch() error: %v", fetchErr)
				errVal, _ := v8.NewValue(e.iso, fetchErr.Error())
				resolver.Reject(errVal)
				return resolver.GetPromise().Value
			}

			e.log("fetch() response: status=%d, body=%d bytes", status, len(body))

			// Build the headers entries as a JS array literal for the Response object.
			headersJS := "[]"
			if len(respHeaders) > 0 {
				var pairs []string
				for k, v := range respHeaders {
					pairs = append(pairs, fmt.Sprintf("[%q,%q]", strings.ToLower(k), v))
				}
				headersJS = "[" + strings.Join(pairs, ",") + "]"
			}

			// Build a Response-like object in JS and resolve the promise with it.
			responseJS := fmt.Sprintf(`
				(function() {
					var __body = %q;
					var __status = %d;
					var __headerPairs = %s;
					var __hdrMap = {};
					for (var i = 0; i < __headerPairs.length; i++) {
						__hdrMap[__headerPairs[i][0]] = __headerPairs[i][1];
					}
					var __headers = {
						get: function(name) {
							var n = name.toLowerCase();
							return __hdrMap.hasOwnProperty(n) ? __hdrMap[n] : null;
						},
						has: function(name) {
							return __hdrMap.hasOwnProperty(name.toLowerCase());
						},
						forEach: function(cb, thisArg) {
							var keys = Object.keys(__hdrMap);
							for (var i = 0; i < keys.length; i++) {
								cb.call(thisArg, __hdrMap[keys[i]], keys[i], this);
							}
						},
						entries: function() {
							var keys = Object.keys(__hdrMap);
							var idx = 0;
							var self = this;
							return {
								next: function() {
									if (idx < keys.length) {
										var k = keys[idx++];
										return { value: [k, __hdrMap[k]], done: false };
									}
									return { value: undefined, done: true };
								},
								[Symbol.iterator]: function() { return this; }
							};
						},
						keys: function() {
							var keys = Object.keys(__hdrMap);
							var idx = 0;
							return {
								next: function() {
									if (idx < keys.length) {
										return { value: keys[idx++], done: false };
									}
									return { value: undefined, done: true };
								},
								[Symbol.iterator]: function() { return this; }
							};
						},
						values: function() {
							var keys = Object.keys(__hdrMap);
							var idx = 0;
							return {
								next: function() {
									if (idx < keys.length) {
										return { value: __hdrMap[keys[idx++]], done: false };
									}
									return { value: undefined, done: true };
								},
								[Symbol.iterator]: function() { return this; }
							};
						},
						[Symbol.iterator]: function() { return this.entries(); }
					};
					return {
						ok: __status >= 200 && __status < 300,
						status: __status,
						statusText: "",
						headers: __headers,
						url: %q,
						redirected: false,
						type: "basic",
						bodyUsed: false,
						text: function() { return Promise.resolve(__body); },
						json: function() { return Promise.resolve(JSON.parse(__body)); },
						arrayBuffer: function() {
							var enc = new TextEncoder();
							return Promise.resolve(enc.encode(__body).buffer);
						},
						blob: function() { return Promise.resolve(new Blob([__body])); },
						clone: function() { return this; },
						formData: function() { return Promise.resolve(new FormData()); }
					};
				})()
			`, body, status, headersJS, urlStr)

			respVal, err := e.ctx.RunScript(responseJS, urlStr)
			if err != nil {
				e.log("Failed to create Response object: %v", err)
				errVal, _ := v8.NewValue(e.iso, "failed to create response")
				resolver.Reject(errVal)
				return resolver.GetPromise().Value
			}

			resolver.Resolve(respVal)
			return resolver.GetPromise().Value
		})); err != nil {
		return err
	}

	// __goConsoleLog - capture console output
	if err := global.Set("__goConsoleLog",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			args := info.Args()
			parts := make([]string, len(args))
			for i, a := range args {
				parts[i] = a.String()
			}
			msg := ""
			for i, p := range parts {
				if i > 0 {
					msg += " "
				}
				msg += p
			}
			// Special: dump iframe script to file for analysis (debug only)
			if e.debug && strings.HasPrefix(msg, "[DUMP-IFRAME-SCRIPT]") {
				scriptData := strings.TrimPrefix(msg, "[DUMP-IFRAME-SCRIPT]")
				_ = os.WriteFile("/tmp/turnstile_iframe.js", []byte(scriptData), 0644)
				e.log("[console] [DUMP] Saved iframe script to /tmp/turnstile_iframe.js (%d bytes)", len(scriptData))
				return nil
			}
			if e.debug && strings.HasPrefix(msg, "[DUMP-EVAL-CODE]") {
				evalData := strings.TrimPrefix(msg, "[DUMP-EVAL-CODE]")
				_ = os.WriteFile("/tmp/turnstile_eval.js", []byte(evalData), 0644)
				e.log("[console] [DUMP] Saved eval'd code to /tmp/turnstile_eval.js (%d bytes)", len(evalData))
				return nil
			}
			e.logLines = append(e.logLines, msg)
			e.log("[console] %s", msg)
			return nil
		})); err != nil {
		return err
	}

	// __goSyncFetch - synchronous fetch for XMLHttpRequest
	// Takes (url, method, body, headersJSON) and returns JSON string with response.
	if err := global.Set("__goSyncFetch",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			args := info.Args()
			if len(args) < 2 {
				val, _ := v8.NewValue(e.iso, `{"status":0,"body":"","headers":{}}`)
				return val
			}

			urlStr := args[0].String()
			method := "GET"
			if len(args) > 1 {
				method = args[1].String()
			}
			bodyStr := ""
			if len(args) > 2 {
				bodyStr = args[2].String()
				// Decode base64-encoded binary bodies from Uint8Array/ArrayBuffer.
				// XHR send converts typed arrays to "B64:<base64>" for lossless transfer.
				if strings.HasPrefix(bodyStr, "HEX:") {
					if decoded, err := hex.DecodeString(bodyStr[4:]); err == nil {
						e.log("HEX decode: %d -> %d bytes", len(bodyStr)-4, len(decoded))
						bodyStr = string(decoded)
					} else {
						e.log("HEX decode FAILED: %v (len=%d)", err, len(bodyStr)-4)
					}
				} else if strings.HasPrefix(bodyStr, "B64:") {
					if decoded, err := base64.StdEncoding.DecodeString(bodyStr[4:]); err == nil {
						e.log("B64 decode: %d -> %d bytes", len(bodyStr)-4, len(decoded))
						bodyStr = string(decoded)
					} else {
						e.log("B64 decode FAILED: %v (len=%d)", err, len(bodyStr)-4)
					}
				}
			}
			headersStr := ""
			if len(args) > 3 {
				headersStr = args[3].String()
			}

			e.mu.Lock()
			handler := e.fetchHandler
			e.mu.Unlock()

			if handler == nil {
				val, _ := v8.NewValue(e.iso, `{"status":0,"body":"no fetch handler","headers":{}}`)
				return val
			}

			e.log("XHR %s %s (body=%d bytes, headers=%s)", method, urlStr, len(bodyStr), headersStr)

			opts := map[string]interface{}{
				"raw":    fmt.Sprintf(`{"method":"%s"}`, method),
				"method": method,
				"body":   bodyStr,
			}
			// Parse XHR request headers and include them in opts
			if headersStr != "" && headersStr != "{}" {
				var xhrHeaders map[string]interface{}
				if err := json.Unmarshal([]byte(headersStr), &xhrHeaders); err == nil {
					opts["headers"] = xhrHeaders
				}
			}

			respBody, status, headers, fetchErr := handler(urlStr, opts)
			if fetchErr != nil {
				e.log("XHR error: %v", fetchErr)
				errJSON := fmt.Sprintf(`{"status":0,"body":%q,"headers":{}}`, fetchErr.Error())
				val, _ := v8.NewValue(e.iso, errJSON)
				return val
			}

			e.log("XHR response: %s %s → %d (%d bytes)", method, urlStr, status, len(respBody))

			// Build headers JSON.
			hJSON := "{"
			first := true
			for k, v := range headers {
				if !first {
					hJSON += ","
				}
				hJSON += fmt.Sprintf("%q:%q", k, v)
				first = false
			}
			hJSON += "}"

			// Escape the body for JSON embedding.
			result := fmt.Sprintf(`{"status":%d,"body":%q,"headers":%s}`, status, respBody, hJSON)
			val, _ := v8.NewValue(e.iso, result)
			return val
		})); err != nil {
		return err
	}

	// __goWriteFile - write content to a file from JS (debug only)
	if err := global.Set("__goWriteFile",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			if !e.debug {
				return nil
			}
			args := info.Args()
			if len(args) < 2 {
				return nil
			}
			path := args[0].String()
			content := args[1].String()
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				e.log("__goWriteFile error: %v", err)
			} else {
				e.log("__goWriteFile: wrote %d bytes to %s", len(content), path)
			}
			return nil
		})); err != nil {
		return err
	}

	// __goLocationReload - signal that the script called location.reload()
	if err := global.Set("__goLocationReload",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			e.log("location.reload() signaled from JS")
			e.reloadRequested = true
			return nil
		})); err != nil {
		return err
	}

	// __goCreateDocumentAll, creates an undetectable object for document.all.
	// In real browsers, typeof document.all === "undefined" even though it exists.
	// This is needed for iframe documents which are created dynamically in JS.
	if err := global.Set("__goCreateDocumentAll",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			allTmpl := v8.NewObjectTemplate(e.iso)
			allTmpl.SetCallAsFunctionHandler(func(info2 *v8.FunctionCallbackInfo) (*v8.Value, error) {
				return info2.This().Value, nil
			})
			allTmpl.MarkAsUndetectable()
			obj, err := allTmpl.NewInstance(info.Context())
			if err != nil {
				return nil
			}
			return obj.Value
		})); err != nil {
		return err
	}

	// __goCreateElement, creates a native DOM element using the ObjectTemplate
	// equipped with SetNativeDataProperty accessors. Element state is stored in
	// the Go-side elementStates map, keyed by an integer ID in internal field 0.
	// Native data property accessors serve reads/writes from this Go map,
	// making elements indistinguishable from real Chrome DOM elements.
	//
	// Called from dom.go's _mkEl function: __goCreateElement(tag, id)
	if err := global.Set("__goCreateElement",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			if e.elementTmpl == nil || os.Getenv("DISABLE_SETHANDLER") == "1" {
				return v8.Undefined(e.iso)
			}

			obj, err := e.elementTmpl.NewInstance(info.Context())
			if err != nil {
				e.log("__goCreateElement: NewInstance failed: %v", err)
				return v8.Undefined(e.iso)
			}

			args := info.Args()
			tag := "DIV"
			id := ""
			if len(args) >= 1 && !args[0].IsUndefined() && !args[0].IsNull() {
				tag = args[0].String()
			}
			if len(args) >= 2 && !args[1].IsUndefined() && !args[1].IsNull() {
				id = args[1].String()
			}

			// Assign a unique integer ID and store it in internal field 0.
			elemID := e.nextElementID
			e.nextElementID++
			_ = obj.SetInternalField(0, int32(elemID))

			// Determine baseURI from the DOM config.
			baseURI := ""
			if e.domCfg != nil {
				baseURI = e.domCfg.URL
			}

			e.log("__goCreateElement(%s, %s) → id=%d", tag, id, elemID)
			// Initialize Go-side state with defaults matching _mkEl's _props.
			// Dimensions must be realistic per-tag to avoid bot detection.
			// Block elements fill parent width; inline elements are content-sized.
			tagUpper := strings.ToUpper(tag)
			tagLower := strings.ToLower(tag)
			ow, oh, ot, ol := elementDefaultDimensions(tagLower, e.domCfg.ScreenWidth, e.domCfg.ScreenHeight)
			e.elementStates[elemID] = &ElementState{
				TagName:      tagUpper,
				NodeName:     tagUpper,
				NodeType:     1,
				ID:           id,
				ClassName:    "",
				InnerHTML:    "",
				OuterHTML:    "",
				InnerText:    "",
				TextContent:  "",
				LocalName:    tagLower,
				NamespaceURI: "http://www.w3.org/1999/xhtml",
				Prefix:       "",
				BaseURI:      baseURI,
				IsConnected:  true,
				OffsetWidth:  ow,
				OffsetHeight: oh,
				OffsetTop:    ot,
				OffsetLeft:   ol,
				ClientWidth:  ow,
				ClientHeight: oh,
				ScrollWidth:  ow,
				ScrollHeight: oh,
				ScrollTop:    0,
				ScrollLeft:   0,
				Src:          "",
				Href:         "",
				Type:         "",
				Rel:          "",
				Media:        "",
				Nonce:        "",
				Value:        "",
				Name:         "",
				CrossOrigin:  "",
				Checked:      false,
				Disabled:     false,
				Width:        300,
				Height:       150,
			}

			return obj.Value
		})); err != nil {
		return err
	}

	// __goParseHTML, parses an HTML string using Go's golang.org/x/net/html parser.
	// Returns a JSON array of node trees that the JS side converts to real DOM nodes.
	// Called from dom.go's innerHTML setter.
	if err := global.Set("__goParseHTML",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			args := info.Args()
			if len(args) < 1 {
				val, _ := v8.NewValue(e.iso, "[]")
				return val
			}
			htmlStr := args[0].String()
			result := ParseHTML(htmlStr)
			val, err := v8.NewValue(e.iso, result)
			if err != nil {
				e.log("__goParseHTML: NewValue failed: %v", err)
				val, _ = v8.NewValue(e.iso, "[]")
				return val
			}
			return val
		})); err != nil {
		return err
	}

	// __goCreateWorker, creates a real threaded Worker in a separate V8 Isolate.
	// Called from dom.go's Worker constructor with the blob code string.
	// The Worker runs in a goroutine with its own Isolate+Context, and sends
	// messages back via e.workerMsgChan which the main event loop delivers.
	if err := global.Set("__goCreateWorker",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			args := info.Args()
			if len(args) < 1 {
				return nil
			}
			blobCode := args[0].String()
			e.log("__goCreateWorker: starting threaded worker (%d bytes)", len(blobCode))

			// Get the patch handler (if any) for later use in the worker goroutine.
			e.mu.Lock()
			patchHandler := e.patchHandler
			e.mu.Unlock()

			e.addPendingOp() // keep event loop alive while worker is running

			go e.runWorker(blobCode, patchHandler)

			return nil
		})); err != nil {
		return err
	}

	// __goWorkerPostMessage, sends a message FROM main thread TO the worker.
	// In our threaded implementation, the worker receives messages via its
	// onmessage handler. We store the message and deliver it when the worker
	// checks for messages. For simplicity, we use a Go channel.
	if err := global.Set("__goWorkerPostMessage",
		v8.NewFunctionTemplate(e.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			args := info.Args()
			if len(args) < 1 {
				return nil
			}
			msg := args[0].String()
			e.log("__goWorkerPostMessage: sending %d bytes to worker", len(msg))

			// Store in a channel that the worker goroutine reads.
			e.mu.Lock()
			if e.workerInbox == nil {
				e.workerInbox = make(chan string, 4)
			}
			inbox := e.workerInbox
			e.mu.Unlock()

			// Non-blocking send, the worker will pick it up.
			select {
			case inbox <- msg:
			default:
				e.log("__goWorkerPostMessage: inbox full, dropping message")
			}
			return nil
		})); err != nil {
		return err
	}

	return nil
}

// runWorker executes Worker code in a separate V8 Isolate (goroutine).
// This mirrors real Chrome behavior where Workers run on separate threads,
// allowing the main event loop to continue processing timers during PoW.
//
// The worker gets minimal globals: self, postMessage, crypto.subtle.digest,
// performance.now(), TextEncoder/TextDecoder, and standard JS builtins.
// Worker's postMessage() sends data through e.workerMsgChan → main event loop.
// Main thread's postMessage() sends data through e.workerInbox → worker's onmessage.
func (e *Engine) runWorker(blobCode string, patchHandler PatchFunc) {
	defer e.removePendingOp()

	workerIso := v8.NewIsolate()
	defer workerIso.Dispose()

	workerGlobal := v8.NewObjectTemplate(workerIso)

	startTime := e.startTime

	// postMessage, sends data from worker back to main thread.
	if err := workerGlobal.Set("postMessage",
		v8.NewFunctionTemplate(workerIso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			args := info.Args()
			if len(args) < 1 {
				return nil
			}
			data := args[0].String()
			log.Printf("[worker] postMessage to main: %d bytes", len(data))
			// Non-blocking send to main thread's message channel.
			select {
			case e.workerMsgChan <- data:
			default:
				log.Printf("[worker] workerMsgChan full, blocking send")
				e.workerMsgChan <- data // blocking
			}
			return nil
		})); err != nil {
		log.Printf("[worker] failed to set postMessage: %v", err)
		return
	}

	// __workerDigest, SHA-256 for PoW computation (same as main context).
	var wDigestCount int64
	var wDigestStart time.Time
	var wLastDigestLog time.Time
	if err := workerGlobal.Set("__workerDigest",
		v8.NewFunctionTemplate(workerIso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			if len(info.Args()) < 2 {
				val, _ := v8.NewValue(workerIso, "")
				return val
			}
			data := info.Args()[1].String()
			wDigestCount++
			now := time.Now()
			if wDigestCount == 1 {
				wDigestStart = now
				wLastDigestLog = now
				log.Printf("[worker] First digest call: algo=%s input(%d bytes)",
					info.Args()[0].String(), len(data))
			}
			if now.Sub(wLastDigestLog) > 5*time.Second {
				elapsed := now.Sub(wDigestStart).Seconds()
				rate := float64(wDigestCount) / elapsed
				log.Printf("[worker] digest: count=%d rate=%.0f/s elapsed=%.1fs",
					wDigestCount, rate, elapsed)
				wLastDigestLog = now
			}
			hash := sha256.Sum256([]byte(data))
			hexHash := hex.EncodeToString(hash[:])
			val, _ := v8.NewValue(workerIso, hexHash)
			return val
		})); err != nil {
		log.Printf("[worker] failed to set __workerDigest: %v", err)
		return
	}

	// __workerPerfNow, performance.now() for the worker context.
	if err := workerGlobal.Set("__workerPerfNow",
		v8.NewFunctionTemplate(workerIso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			elapsed := time.Since(startTime).Seconds() * 1000
			val, _ := v8.NewValue(workerIso, elapsed)
			return val
		})); err != nil {
		log.Printf("[worker] failed to set __workerPerfNow: %v", err)
		return
	}

	// __workerConsoleLog, capture worker console output in main engine logs.
	if err := workerGlobal.Set("__workerConsoleLog",
		v8.NewFunctionTemplate(workerIso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			args := info.Args()
			parts := make([]string, len(args))
			for i, a := range args {
				parts[i] = a.String()
			}
			msg := strings.Join(parts, " ")
			e.mu.Lock()
			e.logLines = append(e.logLines, "[worker] "+msg)
			e.mu.Unlock()
			if e.debug {
				log.Printf("[worker] [console] %s", msg)
			}
			return nil
		})); err != nil {
		log.Printf("[worker] failed to set __workerConsoleLog: %v", err)
		return
	}

	// __workerRecvMessage, blocking call that waits for a message from main thread.
	// Returns the message string, or empty string on timeout.
	e.mu.Lock()
	if e.workerInbox == nil {
		e.workerInbox = make(chan string, 4)
	}
	inbox := e.workerInbox
	e.mu.Unlock()

	if err := workerGlobal.Set("__workerRecvMessage",
		v8.NewFunctionTemplate(workerIso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			// Wait for a message from the main thread with a timeout.
			select {
			case msg := <-inbox:
				log.Printf("[worker] received message from main: %d bytes", len(msg))
				val, _ := v8.NewValue(workerIso, msg)
				return val
			case <-time.After(30 * time.Second):
				log.Printf("[worker] __workerRecvMessage: timeout waiting for message")
				val, _ := v8.NewValue(workerIso, "")
				return val
			}
		})); err != nil {
		log.Printf("[worker] failed to set __workerRecvMessage: %v", err)
		return
	}

	// __workerPatchCode, apply VM patches to Worker eval code.
	if err := workerGlobal.Set("__workerPatchCode",
		v8.NewFunctionTemplate(workerIso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			if len(info.Args()) < 1 {
				val, _ := v8.NewValue(workerIso, "")
				return val
			}
			code := info.Args()[0].String()
			if patchHandler != nil {
				patched := patchHandler(code)
				if patched != code {
					log.Printf("[worker] __workerPatchCode: patched %d -> %d bytes", len(code), len(patched))
				}
				val, _ := v8.NewValue(workerIso, patched)
				return val
			}
			val, _ := v8.NewValue(workerIso, code)
			return val
		})); err != nil {
		log.Printf("[worker] failed to set __workerPatchCode: %v", err)
		return
	}

	workerCtx := v8.NewContext(workerIso, workerGlobal)
	defer workerCtx.Close()

	// Set up Worker global scope with JS builtins and crypto/performance wrappers.
	setupScript := `
		var self = globalThis;
		var console = {
			log: function() { __workerConsoleLog.apply(null, arguments); },
			warn: function() { __workerConsoleLog.apply(null, ['WARN:'].concat(Array.prototype.slice.call(arguments))); },
			error: function() { __workerConsoleLog.apply(null, ['ERROR:'].concat(Array.prototype.slice.call(arguments))); },
			info: function() { __workerConsoleLog.apply(null, arguments); },
			debug: function() { __workerConsoleLog.apply(null, arguments); },
			trace: function() {},
			dir: function() {},
			table: function() {},
			time: function() {},
			timeEnd: function() {},
			count: function() {},
			assert: function() {},
			group: function() {},
			groupEnd: function() {},
			groupCollapsed: function() {},
			clear: function() {}
		};

		var performance = {
			now: function now() { return __workerPerfNow(); },
			timeOrigin: Date.now() - __workerPerfNow()
		};

		var crypto = {
			subtle: {
				digest: function(algorithm, data) {
					var hexHash = __workerDigest(algorithm, typeof data === 'string' ? data : String.fromCharCode.apply(null, new Uint8Array(data)));
					var bytes = new Uint8Array(hexHash.length / 2);
					for (var i = 0; i < hexHash.length; i += 2) {
						bytes[i / 2] = parseInt(hexHash.substring(i, i + 2), 16);
					}
					return Promise.resolve(bytes.buffer);
				}
			},
			getRandomValues: function(arr) {
				for (var i = 0; i < arr.length; i++) {
					arr[i] = Math.floor(Math.random() * 256);
				}
				return arr;
			}
		};

		var navigator = { hardwareConcurrency: 12 };

		var _timers = [];
		var _nextTimerId = 0;
		function setTimeout(fn, delay) {
			var id = ++_nextTimerId;
			_timers.push({id: id, fn: fn, at: Date.now() + (delay || 0), interval: false});
			return id;
		}
		function setInterval(fn, delay) {
			var id = ++_nextTimerId;
			_timers.push({id: id, fn: fn, at: Date.now() + (delay || 0), interval: true, delay: delay || 10});
			return id;
		}
		function clearTimeout(id) {
			_timers = _timers.filter(function(t) { return t.id !== id; });
		}
		var clearInterval = clearTimeout;

		function _drainTimers() {
			var now = Date.now();
			var ready = _timers.filter(function(t) { return now >= t.at; });
			_timers = _timers.filter(function(t) { return now < t.at; });
			for (var i = 0; i < ready.length; i++) {
				try { ready[i].fn(); } catch(e) { console.error('[worker timer error]', e.message); }
				if (ready[i].interval) {
					ready[i].at = now + ready[i].delay;
					_timers.push(ready[i]);
				}
			}
		}

		function importScripts() { console.log('[worker] importScripts called (noop)'); }

		var atob = function(s) {
			// Minimal atob for worker context
			var chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=';
			var output = '';
			s = String(s).replace(/=+$/, '');
			for (var bc = 0, bs, buffer, idx = 0; buffer = s.charAt(idx++);
				 ~buffer && (bs = bc % 4 ? bs * 64 + buffer : buffer, bc++ % 4)
				 ? output += String.fromCharCode(255 & bs >> (-2 * bc & 6)) : 0) {
				buffer = chars.indexOf(buffer);
			}
			return output;
		};
		var btoa = function(s) {
			var chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=';
			var output = '';
			for (var block, charCode, idx = 0, map = chars;
				 s.charAt(idx | 0) || (map = '=', idx % 1);
				 output += map.charAt(63 & block >> 8 - idx % 1 * 8)) {
				charCode = s.charCodeAt(idx += 3/4);
				block = block << 8 | charCode;
			}
			return output;
		};

		// TextEncoder/TextDecoder stubs
		if (typeof TextEncoder === 'undefined') {
			globalThis.TextEncoder = function() {};
			TextEncoder.prototype.encode = function(str) {
				var arr = [];
				for (var i = 0; i < str.length; i++) {
					var c = str.charCodeAt(i);
					if (c < 128) arr.push(c);
					else if (c < 2048) { arr.push(192 | (c >> 6)); arr.push(128 | (c & 63)); }
					else { arr.push(224 | (c >> 12)); arr.push(128 | ((c >> 6) & 63)); arr.push(128 | (c & 63)); }
				}
				return new Uint8Array(arr);
			};
		}
		if (typeof TextDecoder === 'undefined') {
			globalThis.TextDecoder = function() {};
			TextDecoder.prototype.decode = function(buf) {
				var bytes = new Uint8Array(buf.buffer || buf);
				var result = '';
				for (var i = 0; i < bytes.length; i++) result += String.fromCharCode(bytes[i]);
				return result;
			};
		}
	`

	if _, err := workerCtx.RunScript(setupScript, "worker-setup.js"); err != nil {
		log.Printf("[worker] setup script failed: %v", err)
		return
	}

	// Execute the blob code to set up the worker's onmessage handler.
	// Wrap it similarly to how dom.go does it, prefix with var onmessage
	// so bare assignment goes to local scope.
	wrappedCode := `
		var onmessage, onerror;
		` + blobCode + `
		if (typeof onmessage === 'function') self.onmessage = onmessage;
		if (typeof onerror === 'function') self.onerror = onerror;
	`

	if _, err := workerCtx.RunScript(wrappedCode, "worker-blob.js"); err != nil {
		log.Printf("[worker] blob code execution failed: %v", err)
		return
	}

	workerCtx.PerformMicrotaskCheckpoint()
	log.Printf("[worker] blob code executed, waiting for messages from main thread")

	// Worker message loop: wait for messages from the main thread,
	// deliver them to onmessage, and send results back.
	// The worker runs until it posts a message back (PoW result) or times out.
	workerLoopScript := `
		(function() {
			// Wait for message from main thread (blocking Go call).
			var rawMsg = __workerRecvMessage();
			if (!rawMsg) {
				console.log('[worker] no message received, exiting');
				return;
			}

			// Parse the message data.
			var data;
			try {
				data = JSON.parse(rawMsg);
			} catch(e) {
				// Not JSON, pass as raw string (e.g., VM code for eval).
				data = rawMsg;
			}

			// Apply Go-side VM patches for large string data (likely VM code).
			if (typeof data === 'string' && data.length > 5000) {
				console.log('[worker] Patching worker eval code (' + data.length + ' chars)...');
				data = __workerPatchCode(data);
			}

			console.log('[worker] received message, delivering to onmessage (type=' + typeof data + ', len=' + (typeof data === 'string' ? data.length : JSON.stringify(data).length) + ')');

			if (typeof self.onmessage === 'function') {
				var evt = {
					data: data,
					type: 'message',
					isTrusted: true,
					origin: '',
					source: null,
					ports: [],
					lastEventId: '',
					bubbles: false,
					cancelable: false,
					defaultPrevented: false,
					timeStamp: Date.now(),
					preventDefault: function() {},
					stopPropagation: function() {},
					stopImmediatePropagation: function() {}
				};
				self.onmessage(evt);
			} else {
				console.log('[worker] no onmessage handler, dropping message');
			}
		})();
	`

	// Run the worker message loop, this blocks until the worker processes the message
	// and calls postMessage to send results back. The main thread's event loop
	// continues running in parallel (timers fire, etc.).
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := workerCtx.RunScript(workerLoopScript, "worker-loop.js")
		if err != nil {
			log.Printf("[worker] message loop error: %v", err)
		}
		workerCtx.PerformMicrotaskCheckpoint()
	}()

	select {
	case <-done:
		log.Printf("[worker] message loop completed (digests=%d)", wDigestCount)
	case <-time.After(120 * time.Second):
		log.Printf("[worker] message loop timed out after 120s, terminating")
		workerIso.TerminateExecution()
		<-done
	}
}

func (e *Engine) log(format string, args ...interface{}) {
	if e.debug {
		log.Printf("[engine] "+format, args...)
	}
}
