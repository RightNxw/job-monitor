//go:build solver

package engine

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// DOMConfig holds configuration for the fake browser DOM.
type DOMConfig struct {
	UserAgent         string
	URL               string
	Referrer          string
	ScreenWidth       int
	ScreenHeight      int
	ColorDepth        int               // screen.colorDepth (default 30 for modern Macs)
	PixelDepth        int               // screen.pixelDepth (default 30 for modern Macs)
	CanvasFingerprint string            // base64-encoded PNG for toDataURL
	TimeDilation      float64           // <1 compresses time (e.g. 0.1 = 10x compression, 43s appears as 4.3s)
	FastTimers        bool              // fire all timers immediately regardless of delay (for capture mode)
	Cookies           map[string]string // initial document.cookie values (e.g. from Set-Cookie headers)
}

// DefaultDOMConfig returns a default Chrome-like DOM configuration.
func DefaultDOMConfig(userAgent, targetURL string) *DOMConfig {
	return &DOMConfig{
		UserAgent:         userAgent,
		URL:               targetURL,
		Referrer:          "",
		ScreenWidth:       1512,
		ScreenHeight:      982,
		ColorDepth:        30,
		PixelDepth:        30,
		CanvasFingerprint: GenerateCanvasFingerprint(),
	}
}

// BuildDOMScript generates the JavaScript that sets up fake browser globals.
func BuildDOMScript(cfg *DOMConfig) string {
	parsedURL, _ := url.Parse(cfg.URL)
	hostname := ""
	origin := ""
	pathname := "/"
	protocol := "https:"
	if parsedURL != nil {
		hostname = parsedURL.Hostname()
		origin = fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)
		pathname = parsedURL.Path
		if pathname == "" {
			pathname = "/"
		}
		protocol = parsedURL.Scheme + ":"
	}

	script := fmt.Sprintf(`
// ============= Fake DOM for Cloudflare Challenge =============

// --- Capture Go callbacks in script-scope const variables ---
// Go sets __go* functions on the V8 global before this script runs.
// const at script top level creates script-scope bindings (NOT global properties),
// so these are invisible to 'in' operator and property enumeration.
// After this script finishes, engine.go deletes the __go* globals.
const _goPerformanceNow = globalThis.__goPerformanceNow || function(){return 0};
const _goAtob = globalThis.__goAtob || function(){return ''};
const _goBtoa = globalThis.__goBtoa || function(){return ''};
const _goDigest = globalThis.__goDigest || function(){return ''};
const _goPatchWorkerCode = globalThis.__goPatchWorkerCode || function(c){return c};
const _goSetTimeout = globalThis.__goSetTimeout || function(){return 0};
const _goSetInterval = globalThis.__goSetInterval || function(){return 0};
const _goClearTimer = globalThis.__goClearTimer || function(){};
const _goFetch = globalThis.__goFetch || function(){return '{}'};
const _goConsoleLog = globalThis.__goConsoleLog || function(){};
const _goSyncFetch = globalThis.__goSyncFetch || function(){return '{}'};
const _goWriteFile = globalThis.__goWriteFile || function(){};
const _goLocationReload = globalThis.__goLocationReload || function(){};
const _goCreateDocumentAll = globalThis.__goCreateDocumentAll || function(){return []};
const _goCreateElement = globalThis.__goCreateElement || function(){return undefined};
const _goParseHTML = globalThis.__goParseHTML || function(){return '[]'};
const _goCreateWorker = globalThis.__goCreateWorker || function(){};
const _goWorkerPostMessage = globalThis.__goWorkerPostMessage || function(){};

// --- Save original JSON.stringify before challenge script can override it ---
// Using const at script top level creates a script-scope binding (NOT a global property),
// so it's invisible to 'in' operator, Object.keys, and property enumeration.
const _safeStringify = JSON.stringify;

// Symbol key for event listeners, invisible to Object.getOwnPropertyNames
// and string-keyed property enumeration. Defined early so iframe code can use it.
const _sEL = Symbol('el');

// Pre-populated behavioral event buffer.
// When addEventListener is called for mouse/key/scroll events, the buffered events
// are immediately replayed to the handler. This ensures the BM script's fingerprint
// collectors have data BEFORE the first sensor POST (which fires synchronously).
var _preBufferedEvents = {};
(function() {
	var ts = Date.now() - 2000; // simulate events from ~2s before page load
	var cx = 500, cy = 400;
	var events = [];
	// Mouse movement path (20 moves)
	for (var i = 0; i < 20; i++) {
		ts += 15 + Math.floor(Math.random() * 35);
		cx += Math.floor((Math.random() - 0.3) * 60);
		cy += Math.floor((Math.random() - 0.3) * 40);
		if (cx < 10) cx = 10; if (cx > 1400) cx = 1400;
		if (cy < 10) cy = 10; if (cy > 900) cy = 900;
		var me = {type:'mousemove',clientX:cx,clientY:cy,pageX:cx,pageY:cy,screenX:cx,screenY:cy+100,
			movementX:Math.floor((Math.random()-0.5)*8),movementY:Math.floor((Math.random()-0.5)*6),
			button:0,buttons:0,timeStamp:ts,isTrusted:true,bubbles:true,cancelable:true,composed:true,
			target:null,currentTarget:null,eventPhase:2,detail:0,ctrlKey:false,shiftKey:false,altKey:false,metaKey:false,
			preventDefault:function(){},stopPropagation:function(){},stopImmediatePropagation:function(){}};
		events.push(me);
		// Also create pointer version
		var pe = {};
		for (var k in me) pe[k] = me[k];
		pe.type = 'pointermove';
		pe.pointerId = 1;
		pe.pointerType = 'mouse';
		pe.width = 1;
		pe.height = 1;
		pe.pressure = 0;
		pe.tiltX = 0;
		pe.tiltY = 0;
		pe.isPrimary = true;
		events.push(pe);
	}
	// Click sequence
	ts += 80 + Math.floor(Math.random() * 120);
	['pointerdown','mousedown','pointerup','mouseup','click'].forEach(function(type) {
		ts += 2 + Math.floor(Math.random() * 5);
		var e = {type:type,clientX:cx,clientY:cy,pageX:cx,pageY:cy,screenX:cx,screenY:cy+100,
			button:0,buttons:type.indexOf('down')>=0?1:0,timeStamp:ts,isTrusted:true,bubbles:true,cancelable:true,
			composed:true,target:null,currentTarget:null,eventPhase:2,detail:1,
			ctrlKey:false,shiftKey:false,altKey:false,metaKey:false,
			preventDefault:function(){},stopPropagation:function(){},stopImmediatePropagation:function(){}};
		if (type.indexOf('pointer') >= 0) { e.pointerId=1;e.pointerType='mouse';e.width=1;e.height=1;e.pressure=type.indexOf('down')>=0?0.5:0;e.isPrimary=true; }
		events.push(e);
	});
	// Keyboard
	ts += 200 + Math.floor(Math.random() * 100);
	['keydown','keypress','keyup'].forEach(function(type) {
		ts += 5 + Math.floor(Math.random() * 15);
		events.push({type:type,key:'a',code:'KeyA',keyCode:65,which:65,charCode:type==='keypress'?97:0,
			timeStamp:ts,isTrusted:true,bubbles:true,cancelable:true,composed:true,
			target:null,currentTarget:null,eventPhase:2,repeat:false,isComposing:false,
			ctrlKey:false,shiftKey:false,altKey:false,metaKey:false,
			preventDefault:function(){},stopPropagation:function(){},stopImmediatePropagation:function(){}});
	});
	// Scroll/wheel
	ts += 50;
	events.push({type:'wheel',deltaX:0,deltaY:-100,deltaZ:0,deltaMode:0,clientX:cx,clientY:cy,
		timeStamp:ts,isTrusted:true,bubbles:true,cancelable:true,composed:true,
		target:null,currentTarget:null,eventPhase:2,
		preventDefault:function(){},stopPropagation:function(){},stopImmediatePropagation:function(){}});
	events.push({type:'scroll',timeStamp:ts+1,isTrusted:true,bubbles:true,cancelable:false,
		target:null,currentTarget:null,eventPhase:2,
		preventDefault:function(){},stopPropagation:function(){},stopImmediatePropagation:function(){}});
	// Focus/visibility
	events.push({type:'focus',timeStamp:ts-500,isTrusted:true,bubbles:false,cancelable:false,
		target:null,currentTarget:null,eventPhase:2,
		preventDefault:function(){},stopPropagation:function(){},stopImmediatePropagation:function(){}});
	events.push({type:'focusin',timeStamp:ts-500,isTrusted:true,bubbles:true,cancelable:false,
		target:null,currentTarget:null,eventPhase:2,
		preventDefault:function(){},stopPropagation:function(){},stopImmediatePropagation:function(){}});

	// Index events by type
	for (var i = 0; i < events.length; i++) {
		var ev = events[i];
		if (!_preBufferedEvents[ev.type]) _preBufferedEvents[ev.type] = [];
		_preBufferedEvents[ev.type].push(ev);
	}
})();

// --- V8 14.x polyfills ---
// Chrome 146 uses V8 14.6 which has builtins our V8 13.6 lacks.
// The Turnstile VM enumerates all window properties, missing builtins
// change the fingerprint hash. These stubs ensure typeof matches Chrome.
if (typeof Temporal === 'undefined') {
	globalThis.Temporal = Object.create(null);
	Temporal.Now = {instant:function(){return{}},plainDateISO:function(){return{}},plainTimeISO:function(){return{}},plainDateTimeISO:function(){return{}},zonedDateTimeISO:function(){return{}},timeZoneId:function(){return 'UTC'}};
	Temporal.Duration = function(){};
	Temporal.Instant = function(){};
	Temporal.PlainDate = function(){};
	Temporal.PlainTime = function(){};
	Temporal.PlainDateTime = function(){};
	Temporal.PlainMonthDay = function(){};
	Temporal.PlainYearMonth = function(){};
	Temporal.ZonedDateTime = function(){};
	Temporal.Calendar = function(){};
	Temporal.TimeZone = function(){};
}
if (typeof Float16Array === 'undefined') { globalThis.Float16Array = function Float16Array(){}; Float16Array.BYTES_PER_ELEMENT = 2; Float16Array.prototype = Object.create(null); }
if (typeof Math.f16round !== 'function') { Math.f16round = function f16round(x) { return Math.fround(x); }; }
if (typeof Error.isError !== 'function') { Error.isError = function isError(v) { return v instanceof Error; }; }
if (typeof RegExp.escape !== 'function') { RegExp.escape = function escape(s) { return String(s).replace(/[.*+?^${}()|[\]\\]/g, '\\$&'); }; }
// SharedArrayBuffer must exist (Chrome has it). V8 13.6 exposes it natively,
// but if cross-origin isolation flags disable it, stub it so typeof matches Chrome.
if (typeof SharedArrayBuffer === 'undefined') {
	globalThis.SharedArrayBuffer = function SharedArrayBuffer(byteLength) {
		return new ArrayBuffer(byteLength);
	};
	SharedArrayBuffer.prototype = Object.create(ArrayBuffer.prototype);
	Object.defineProperty(SharedArrayBuffer.prototype, Symbol.toStringTag, { value: 'SharedArrayBuffer', configurable: true });
}
// Atomics must also exist alongside SharedArrayBuffer (Chrome always has both)
if (typeof Atomics === 'undefined') {
	globalThis.Atomics = Object.create(null);
	Atomics.add = function(){return 0}; Atomics.and = function(){return 0};
	Atomics.compareExchange = function(){return 0}; Atomics.exchange = function(){return 0};
	Atomics.isLockFree = function(size){return size===1||size===2||size===4};
	Atomics.load = function(){return 0}; Atomics.or = function(){return 0};
	Atomics.store = function(ta,i,v){return v}; Atomics.sub = function(){return 0};
	Atomics.wait = function(){return 'ok'}; Atomics.notify = function(){return 0};
	Atomics.xor = function(){return 0};
	Object.defineProperty(Atomics, Symbol.toStringTag, { value: 'Atomics', configurable: true });
}
// V8 14.8+ builtins: DisposableStack, AsyncDisposableStack, SuppressedError
if (typeof DisposableStack === 'undefined') {
	globalThis.DisposableStack = function DisposableStack() { throw new TypeError("Illegal constructor"); };
	DisposableStack.prototype[Symbol.dispose] = function() {};
	Object.defineProperty(DisposableStack.prototype, Symbol.toStringTag, { value: 'DisposableStack', configurable: true });
}
if (typeof AsyncDisposableStack === 'undefined') {
	globalThis.AsyncDisposableStack = function AsyncDisposableStack() { throw new TypeError("Illegal constructor"); };
	AsyncDisposableStack.prototype[Symbol.asyncDispose] = function() { return Promise.resolve(); };
	Object.defineProperty(AsyncDisposableStack.prototype, Symbol.toStringTag, { value: 'AsyncDisposableStack', configurable: true });
}
if (typeof SuppressedError === 'undefined') {
	globalThis.SuppressedError = function SuppressedError(error, suppressed, message) {
		var e = new Error(message);
		e.error = error; e.suppressed = suppressed; e.name = 'SuppressedError';
		Object.setPrototypeOf(e, SuppressedError.prototype);
		return e;
	};
	SuppressedError.prototype = Object.create(Error.prototype);
	SuppressedError.prototype.constructor = SuppressedError;
	SuppressedError.prototype.name = 'SuppressedError';
}
// V8 14.2+ Iterator helpers
if (typeof Iterator !== 'undefined' && !Iterator.from) {
	Iterator.from = function(obj) { return obj[Symbol.iterator](); };
	var ip = Iterator.prototype;
	if (!ip.map) ip.map = function(fn) { return this; };
	if (!ip.filter) ip.filter = function(fn) { return this; };
	if (!ip.take) ip.take = function(n) { return this; };
	if (!ip.drop) ip.drop = function(n) { return this; };
	if (!ip.flatMap) ip.flatMap = function(fn) { return this; };
	if (!ip.reduce) ip.reduce = function(fn, init) { return init; };
	if (!ip.toArray) ip.toArray = function() { return []; };
	if (!ip.forEach) ip.forEach = function(fn) {};
	if (!ip.some) ip.some = function(fn) { return false; };
	if (!ip.every) ip.every = function(fn) { return true; };
	if (!ip.find) ip.find = function(fn) { return undefined; };
}
// V8 14.4+ Set methods
if (typeof Set !== 'undefined') {
	var sp = Set.prototype;
	if (!sp.union) sp.union = function(other) { var s = new Set(this); other.forEach(function(v){s.add(v)}); return s; };
	if (!sp.intersection) sp.intersection = function(other) { var s = new Set(); this.forEach(function(v){if(other.has(v))s.add(v)}); return s; };
	if (!sp.difference) sp.difference = function(other) { var s = new Set(); this.forEach(function(v){if(!other.has(v))s.add(v)}); return s; };
	if (!sp.symmetricDifference) sp.symmetricDifference = function(other) { var s = new Set(this); other.forEach(function(v){if(s.has(v))s.delete(v);else s.add(v)}); return s; };
	if (!sp.isSubsetOf) sp.isSubsetOf = function(other) { var r=true; this.forEach(function(v){if(!other.has(v))r=false}); return r; };
	if (!sp.isSupersetOf) sp.isSupersetOf = function(other) { var r=true; other.forEach(function(v){if(!this.has(v))r=false}.bind(this)); return r; };
	if (!sp.isDisjointFrom) sp.isDisjointFrom = function(other) { var r=true; this.forEach(function(v){if(other.has(v))r=false}); return r; };
}
// Wrap Promise to allow calling without 'new', Kasada ips.js calls Promise() directly
(function() {
	var _OrigPromise = Promise;
	globalThis.Promise = function(executor) {
		return new _OrigPromise(executor);
	};
	Object.setPrototypeOf(Promise, _OrigPromise);
	Promise.prototype = _OrigPromise.prototype;
	Promise.prototype.constructor = Promise;
	Promise.resolve = _OrigPromise.resolve.bind(_OrigPromise);
	Promise.reject = _OrigPromise.reject.bind(_OrigPromise);
	Promise.all = _OrigPromise.all.bind(_OrigPromise);
	Promise.allSettled = _OrigPromise.allSettled.bind(_OrigPromise);
	Promise.any = _OrigPromise.any.bind(_OrigPromise);
	Promise.race = _OrigPromise.race.bind(_OrigPromise);
	// Preserve Symbol.species if it exists
	if (_OrigPromise[Symbol.species]) {
		Object.defineProperty(Promise, Symbol.species, { get: function() { return Promise; } });
	}
})();
// V8 14.0+ Promise.withResolvers
if (!Promise.withResolvers) {
	Promise.withResolvers = function() {
		var resolve, reject;
		var promise = new Promise(function(res, rej) { resolve = res; reject = rej; });
		return { promise: promise, resolve: resolve, reject: reject };
	};
}
// V8 14.0+ Object.groupBy / Map.groupBy
if (!Object.groupBy) {
	Object.groupBy = function(items, fn) {
		var result = Object.create(null);
		var i = 0;
		for (var item of items) {
			var key = fn(item, i++);
			if (!result[key]) result[key] = [];
			result[key].push(item);
		}
		return result;
	};
}
if (!Map.groupBy) {
	Map.groupBy = function(items, fn) {
		var map = new Map();
		var i = 0;
		for (var item of items) {
			var key = fn(item, i++);
			if (!map.has(key)) map.set(key, []);
			map.get(key).push(item);
		}
		return map;
	};
}
// V8 14.0+ Array.fromAsync
if (!Array.fromAsync) {
	Array.fromAsync = function(items) {
		return Promise.resolve(Array.from(items));
	};
}
// V8 14.0+ String.prototype.isWellFormed / toWellFormed
if (!String.prototype.isWellFormed) {
	String.prototype.isWellFormed = function() { return true; };
	String.prototype.toWellFormed = function() { return this.toString(); };
}
// V8 14.0+ ArrayBuffer.prototype.transfer
if (!ArrayBuffer.prototype.transfer) {
	ArrayBuffer.prototype.transfer = function(newLen) {
		var buf = new ArrayBuffer(newLen || this.byteLength);
		new Uint8Array(buf).set(new Uint8Array(this).subarray(0, Math.min(this.byteLength, buf.byteLength)));
		return buf;
	};
	ArrayBuffer.prototype.transferToFixedLength = ArrayBuffer.prototype.transfer;
}
// V8 14.0+ Atomics.waitAsync
if (typeof Atomics !== 'undefined' && !Atomics.waitAsync) {
	Atomics.waitAsync = function() { return { async: true, value: Promise.resolve('ok') }; };
}

// JSON.stringify is saved as _safeStringify above for internal use

// Debug wrappers removed, they modify native prototypes (push, bind) and add
// global vars (_r5Tracing, _zfBindLog) that the challenge script can detect.

// --- Real Date.now reference + time compression (hoisted for performance.now()) ---
// Saved before any patching so both Date.now() and performance.now() can use
// pure JS without CGO calls. This is critical for PoW hot-loop performance.
let _rdn = Date.now;
let _pst = _rdn.call(Date);
let _tcomp = %f;
let _ifrST = [];
let _iframeScriptUrl = '';
let _mkNat = function(){};
let _mkFnNat = function(){};

// --- Time compression ---
// When TimeDilation < 1 (e.g. 0.01), we compress BOTH Date.now() AND
// performance.now() so the VM's batch sizer creates larger batches (each
// batch runs longer in wall-clock time). This prevents the computation from
// yielding too frequently, which would allow the orchestrator to interleave
// forceFail/reload messages. Total work is identical, just batched differently.
(function() {
	if (_tcomp > 0 && _tcomp < 1) {
		Date.now = function() {
			var real = _rdn.call(Date);
			var elapsed = real - _pst;
			return Math.floor(_pst + elapsed * _tcomp);
		};
	}
})();

// --- _rDate: a Date constructor with uncompressed Date.now() ---
// The parent orchestrator needs compressed Date.now() to fool its overrun timer,
// but the Turnstile iframe VM and PoW Worker need real time for batch sizing.
// _rDate is a drop-in Date replacement with .now() = real time.
let _rDate = (function() {
	var RD = function() { return new (Function.prototype.bind.apply(Date, [null].concat(Array.prototype.slice.call(arguments)))); };
	RD.prototype = Date.prototype;
	RD.now = _rdn;
	RD.parse = Date.parse;
	RD.UTC = Date.UTC;
	RD.length = Date.length;
	Object.defineProperty(RD, 'name', {value: 'Date', configurable: true});
	RD.toString = function() { return Date.toString(); };
	return RD;
})();

// --- _iDate: Date constructor with inflated Date.now() for iframe context ---
// The Turnstile iframe script computes timing deltas (e.g. Date.now() - baseTimestamp).
// In V8, script execution is ~30-50x faster than in a real browser (no DOM overhead).
// This makes timing deltas unrealistically small (11ms vs ~400ms in browser),
// which the server detects as bot-like. We inflate elapsed time for the iframe
// context to simulate real browser execution overhead.
let _iCO = 0;
let _iLRN = _rdn.call(Date);
let _iDNCC = 0;
let _iDNFn = function now() {
	var real = _rdn.call(Date);
	var sinceLast = real - _iLRN;
	_iLRN = real;
	if (sinceLast < 10 && _iCO < 2000) {
		_iCO += Math.floor(sinceLast * 35 + Math.random() * 5);
	}
	_iDNCC++;
	return real + _iCO;
};
// Native Date.now has no .prototype, remove ours to match
delete _iDNFn.prototype;
let _iDate = (function() {
	var RD = function() {
		// For "new Date()" with no args, use inflated time
		if (arguments.length === 0) {
			return new Date(_iDNFn());
		}
		return new (Function.prototype.bind.apply(Date, [null].concat(Array.prototype.slice.call(arguments))));
	};
	RD.prototype = Date.prototype;
	RD.now = _iDNFn;
	RD.parse = Date.parse;
	RD.UTC = Date.UTC;
	RD.length = Date.length;
	Object.defineProperty(RD, 'name', {value: 'Date', configurable: true});
	RD.toString = function() { return Date.toString(); };
	return RD;
})();

// --- location ---
// Use assignment instead of var, var creates non-configurable global binding
// which prevents Object.defineProperty from converting to getter later.
location = {
	href: %q,
	hostname: %q,
	origin: %q,
	pathname: %q,
	protocol: %q,
	host: %q,
	search: %q,
	hash: "",
	port: "",
	assign: function(url) { console.log('[DOM] location.assign(' + url + ')'); location.href = url; _goLocationReload(); },
	replace: function(url) { console.log('[DOM] location.replace(' + url + ')'); location.href = url; _goLocationReload(); },
	reload: function() { console.log('[DOM] location.reload()'); _goLocationReload(); }
};
// Make location.href settable with logging so we detect CF page reloads
(function() {
	var _href = location.href;
	Object.defineProperty(location, 'href', {
		get: function() { return _href; },
		set: function(v) {
			console.log('[DOM] location.href = ' + v);
			_href = v;
		},
		configurable: true
	});
})();

// --- canvas fingerprint (generated in Go, deterministic per browser profile) ---
let _cvsFP = "data:image/png;base64,%s";

// --- navigator ---
// PluginArray/MimeTypeArray constructors (needed before navigator.plugins/mimeTypes)
// Must use class syntax so 'class X extends PluginArray {}' works in the VM.
// Chrome's native PluginArray/MimeTypeArray are class-like constructors.
class PluginArray { constructor() { throw new TypeError("Illegal constructor"); } }
Object.defineProperty(PluginArray.prototype, Symbol.toStringTag, { value: 'PluginArray', configurable: true });
class MimeTypeArray { constructor() { throw new TypeError("Illegal constructor"); } }
Object.defineProperty(MimeTypeArray.prototype, Symbol.toStringTag, { value: 'MimeTypeArray', configurable: true });
// Permissions constructor (so Permissions.prototype.query exists for VM detection)
class Permissions { constructor() { throw new TypeError("Illegal constructor"); } }
Permissions.prototype.query = function(desc) { return Promise.resolve({state: 'prompt', name: desc ? desc.name : '', onchange: null, addEventListener: function(){}, removeEventListener: function(){}}); };
Object.defineProperty(Permissions.prototype, Symbol.toStringTag, { value: 'Permissions', configurable: true });

var navigator = {
	userAgent: %q,
	platform: "MacIntel",
	language: "en-US",
	languages: Object.freeze(["en-US"]),
	hardwareConcurrency: 12,
	cookieEnabled: true,
	webdriver: false,
	vendor: "Google Inc.",
	appVersion: %q,
	appName: "Netscape",
	appCodeName: "Mozilla",
	product: "Gecko",
	productSub: "20030107",
	maxTouchPoints: 0,
	onLine: true,
	doNotTrack: null,
	deviceMemory: 8,
	pdfViewerEnabled: true,
	globalPrivacyControl: false,
	plugins: (function() {
		// Each plugin has indexed MimeType sub-items (plugin[0], plugin[1]).
		// Chrome has 5 plugins × 2 mimes each = 10 total.
		var _mkMime = function(type, desc, suffixes) {
			return {type: type, description: desc, suffixes: suffixes, enabledPlugin: null};
		};
		var _mkPlugin = function(name, fn) {
			var m0 = _mkMime('application/pdf', 'Portable Document Format', 'pdf');
			var m1 = _mkMime('text/pdf', 'Portable Document Format', 'pdf');
			var pl = {name: name, filename: fn, description: 'Portable Document Format', type: 'application/pdf', length: 2, 0: m0, 1: m1};
			// Chrome supports named MIME type access: plugin['application/pdf'] → MimeType
			pl['application/pdf'] = m0;
			pl['text/pdf'] = m1;
			pl.item = function(i) { return this[i] || null; };
			pl.namedItem = function(n) { for (var j=0;j<this.length;j++) if(this[j].type===n) return this[j]; return null; };
			pl[Symbol.iterator] = function() { var idx=0,self=this; return {next:function(){return idx<self.length?{value:self[idx++],done:false}:{done:true}}}; };
			m0.enabledPlugin = pl;
			m1.enabledPlugin = pl;
			return pl;
		};
		var items = [
			_mkPlugin("PDF Viewer", "internal-pdf-viewer"),
			_mkPlugin("Chrome PDF Plugin", "internal-pdf-viewer"),
			_mkPlugin("Chrome PDF Viewer", "internal-pdf-viewer"),
			_mkPlugin("Microsoft Edge PDF Viewer", "internal-pdf-viewer"),
			_mkPlugin("WebKit built-in PDF", "internal-pdf-viewer")
		];
		// PluginArray is NOT an Array, Array.isArray must return false.
		// Must have PluginArray.prototype so 'class Blah extends navigator.plugins {}'
		// throws the CORRECT error ("not a constructor" not "extends undefined").
		var p = {};
		for (var i = 0; i < items.length; i++) p[i] = items[i];
		p.length = items.length;
		p.namedItem = function(name) { for (var j = 0; j < this.length; j++) { if (this[j].name === name) return this[j]; } return null; };
		p.item = function(i) { return this[i] || null; };
		p.refresh = function() {};
		p[Symbol.iterator] = function() { var idx = 0, self = this; return { next: function() { return idx < self.length ? { value: self[idx++], done: false } : { done: true }; } }; };
		Object.setPrototypeOf(p, PluginArray.prototype);
		// Debug: verify plugin items have indexed MimeTypes
		console.log('[PLUGIN-CHECK] p[0].length=' + p[0].length + ' p[0][0]=' + (p[0][0] ? p[0][0].type : 'UNDEF') + ' p[0][1]=' + (p[0][1] ? p[0][1].type : 'UNDEF'));
		return p;
	})(),
	mimeTypes: (function() {
		var mt0 = {type: 'application/pdf', description: 'Portable Document Format', suffixes: 'pdf'};
		var mt1 = {type: 'text/pdf', description: 'Portable Document Format', suffixes: 'pdf'};
		var m = {0: mt0, 1: mt1, length: 2};
		// Chrome supports named MIME type access: mimeTypes['application/pdf'] → MimeType
		m['application/pdf'] = mt0;
		m['text/pdf'] = mt1;
		m.item = function(i) { return m[i] || null; };
		m.namedItem = function(name) { for (var i = 0; i < m.length; i++) { if (m[i] && m[i].type === name) return m[i]; } return null; };
		m[Symbol.iterator] = function() { var idx = 0; return { next: function() { return idx < m.length ? { value: m[idx++], done: false } : { done: true }; } }; };
		Object.setPrototypeOf(m, MimeTypeArray.prototype);
		return m;
	})(),
	connection: {
		effectiveType: "4g",
		rtt: 100,
		downlink: 5.65,
		saveData: false,
		onchange: null,
		ontypechange: null,
		addEventListener: function() {},
		removeEventListener: function() {},
		dispatchEvent: function() { return true; }
	},
	getBattery: function() { return Promise.resolve({charging: true, chargingTime: 0, dischargingTime: Infinity, level: 1}); },
	mediaDevices: {
		enumerateDevices: function() {
			return Promise.resolve([
				{deviceId: '', groupId: 'default', kind: 'audioinput', label: '', toJSON: function() { return {deviceId: this.deviceId, groupId: this.groupId, kind: this.kind, label: this.label}; }},
				{deviceId: '', groupId: 'default', kind: 'audiooutput', label: '', toJSON: function() { return {deviceId: this.deviceId, groupId: this.groupId, kind: this.kind, label: this.label}; }},
				{deviceId: '', groupId: '', kind: 'videoinput', label: '', toJSON: function() { return {deviceId: this.deviceId, groupId: this.groupId, kind: this.kind, label: this.label}; }}
			]);
		},
		getUserMedia: function() { return Promise.reject(new DOMException('NotAllowedError')); },
		getDisplayMedia: function() { return Promise.reject(new DOMException('NotAllowedError')); }
	},
	credentials: { get: function() { return Promise.resolve(null); } },
	clipboard: {
		readText: function() { return Promise.reject(new DOMException('NotAllowedError')); },
		read: function() { return Promise.reject(new DOMException('NotAllowedError')); },
		writeText: function(text) { return Promise.resolve(); },
		write: function(data) { return Promise.resolve(); }
	},
	permissions: { query: function(desc) { return Promise.resolve({state: "prompt", name: desc ? desc.name : "", onchange: null, addEventListener: function(){}, removeEventListener: function(){}}); } },
	permission: { query: function(desc) { return Promise.resolve({state: "granted"}); } },
	storage: { estimate: function() { return Promise.resolve({quota: 1073741824, usage: 0}); }, getDirectory: function() { return Promise.resolve({}); }, persist: function() { return Promise.resolve(false); }, persisted: function() { return Promise.resolve(false); } },
	serviceWorker: { controller: null, ready: Promise.resolve(null), register: function() { return Promise.reject(new DOMException('SecurityError')); }, getRegistrations: function() { return Promise.resolve([]); } },
	locks: { request: function() { return Promise.resolve(); }, query: function() { return Promise.resolve({held: [], pending: []}); } },
	geolocation: { getCurrentPosition: function(s,e) { if(e) e({code:1, message:'denied'}); }, watchPosition: function() { return 0; }, clearWatch: function() {} },
	keyboard: {
		getLayoutMap: function() {
			var map = new Map();
			var keys = {'KeyA':'a','KeyB':'b','KeyC':'c','KeyD':'d','KeyE':'e','KeyF':'f','KeyG':'g','KeyH':'h',
				'KeyI':'i','KeyJ':'j','KeyK':'k','KeyL':'l','KeyM':'m','KeyN':'n','KeyO':'o','KeyP':'p',
				'KeyQ':'q','KeyR':'r','KeyS':'s','KeyT':'t','KeyU':'u','KeyV':'v','KeyW':'w','KeyX':'x',
				'KeyY':'y','KeyZ':'z','Digit0':'0','Digit1':'1','Digit2':'2','Digit3':'3','Digit4':'4',
				'Digit5':'5','Digit6':'6','Digit7':'7','Digit8':'8','Digit9':'9',
				'Minus':'-','Equal':'=','BracketLeft':'[','BracketRight':']','Backslash':'\\',
				'Semicolon':';','Quote':"'", 'Comma':',','Period':'.','Slash':'/',
				'Backquote':String.fromCharCode(96),'Space':' ','Enter':'\r','Tab':'\t'};
			for (var k in keys) map.set(k, keys[k]);
			return Promise.resolve(map);
		},
		lock: function() { return Promise.resolve(); },
		unlock: function() { return Promise.resolve(); },
		addEventListener: function() {},
		removeEventListener: function() {}
	},
	mediaCapabilities: { decodingInfo: function() { return Promise.resolve({supported: true, smooth: true, powerEfficient: true}); } },
	scheduling: { isInputPending: function() { return false; } },
	sendBeacon: function(url, data) { return true; },
	wakeLock: { request: function(type) { return Promise.reject(new DOMException('NotAllowedError')); } },
	usb: { getDevices: function() { return Promise.resolve([]); }, requestDevice: function() { return Promise.reject(new DOMException('NotFoundError')); } },
	hid: { getDevices: function() { return Promise.resolve([]); }, requestDevice: function() { return Promise.reject(new DOMException('NotFoundError')); }, addEventListener: function() {}, removeEventListener: function() {} },
	serial: { getPorts: function() { return Promise.resolve([]); }, requestPort: function() { return Promise.reject(new DOMException('NotFoundError')); }, addEventListener: function() {}, removeEventListener: function() {} },
	bluetooth: { getAvailability: function() { return Promise.resolve(false); }, requestDevice: function() { return Promise.reject(new DOMException('NotFoundError')); }, addEventListener: function() {}, removeEventListener: function() {} },
	xr: { isSessionSupported: function() { return Promise.resolve(false); }, requestSession: function() { return Promise.reject(new DOMException('NotSupportedError')); }, addEventListener: function() {}, removeEventListener: function() {} },
	ink: { requestPresenter: function() { return Promise.resolve({updateInkTrailStartPoint: function(){}, expectedImprovement: 0}); } },
	login: { setStatus: function() { return Promise.resolve(); } },
	managed: { getManagedConfiguration: function() { return Promise.resolve({}); }, addEventListener: function() {}, removeEventListener: function() {} },
	virtualKeyboard: { show: function(){}, hide: function(){}, overlaysContent: false, boundingRect: {x:0,y:0,width:0,height:0}, addEventListener: function(){}, removeEventListener: function(){} },
	windowControlsOverlay: { visible: false, getTitlebarAreaRect: function() { return {x:0,y:0,width:0,height:0}; }, addEventListener: function(){}, removeEventListener: function(){} },
	presentation: { defaultRequest: null, receiver: null },
	mediaSession: { metadata: null, playbackState: "none", setActionHandler: function(){}, setPositionState: function(){} },
	userActivation: { hasBeenActive: true, isActive: false },
	gpu: { requestAdapter: function() { return Promise.resolve(null); } },
	vendorSub: "",
	javaEnabled: function() { return false; },
	getGamepads: function() { return [null, null, null, null]; },
	vibrate: function() { return true; },
	share: function() { return Promise.reject(new DOMException('NotAllowedError')); },
	canShare: function() { return false; },
	requestMIDIAccess: function() { return Promise.reject(new DOMException('NotSupportedError')); },
	requestMediaKeySystemAccess: function() { return Promise.reject(new DOMException('NotSupportedError')); },
	getInstalledRelatedApps: function() { return Promise.resolve([]); },
	setAppBadge: function() { return Promise.resolve(); },
	clearAppBadge: function() { return Promise.resolve(); },
	getUserMedia: function(c, s, e) { if (e) e(new DOMException('NotAllowedError')); },
	webkitGetUserMedia: function(c, s, e) { if (e) e(new DOMException('NotAllowedError')); },
	webkitPersistentStorage: { queryUsageAndQuota: function(s,e){if(s)s(0,1073741824);}, requestQuota: function(s,cb){if(cb)cb(s);} },
	webkitTemporaryStorage: { queryUsageAndQuota: function(s,e){if(s)s(0,1073741824);}, requestQuota: function(s,cb){if(cb)cb(s);} },
	storageBuckets: { open: function() { return Promise.reject(new DOMException('NotSupportedError')); } },
	registerProtocolHandler: function() {},
	unregisterProtocolHandler: function() {},
	devicePosture: { type: "continuous", addEventListener: function(){}, removeEventListener: function(){} },
	protectedAudience: { queryFeatureSupport: function() { return false; } },
	adAuctionComponents: function() { return []; },
	joinAdInterestGroup: function() { return Promise.resolve(); },
	leaveAdInterestGroup: function() { return Promise.resolve(); },
	runAdAuction: function() { return Promise.resolve(null); },
	updateAdInterestGroups: function() {},
	canLoadAdAuctionFencedFrame: function() { return false; },
	clearOriginJoinedAdInterestGroups: function() { return Promise.resolve(); },
	createAuctionNonce: function() { return Promise.resolve(''); },
	deprecatedReplaceInURN: function() { return Promise.resolve(); },
	deprecatedRunAdAuctionEnforcesKAnonymity: function() { return false; },
	deprecatedURNToURL: function() { return Promise.resolve(''); },
	getInterestGroupAdAuctionData: function() { return Promise.resolve(null); },
	userAgentData: {
		brands: [
			{brand: "Chromium", version: "146"},
			{brand: "Not-A.Brand", version: "24"},
			{brand: "Google Chrome", version: "146"}
		],
		mobile: false,
		platform: "macOS",
		getHighEntropyValues: function() {
			return Promise.resolve({
				architecture: "arm",
				bitness: "64",
				brands: this.brands,
				fullVersionList: [{brand: "Chromium", version: "146.0.7689.90"}, {brand: "Not-A.Brand", version: "24.0.0.0"}, {brand: "Google Chrome", version: "146.0.7689.90"}],
				mobile: false,
				model: "",
				platform: "macOS",
				platformVersion: "15.7.3",
				uaFullVersion: "146.0.7689.90",
				wow64: false
			});
		},
		// toJSON() is required, Chrome calls it via JSON.stringify(navigator.userAgentData).
		// Without it, JSON.stringify returns all enumerable properties including methods,
		// which differs from Chrome's output: {"brands":[...],"mobile":false,"platform":"macOS"}
		toJSON: function() {
			return { brands: this.brands, mobile: this.mobile, platform: this.platform };
		}
	}
};

// Wire up enabledPlugin back-references (mimeType items -> plugin)
if (navigator.mimeTypes && navigator.plugins) {
	for (var _mi = 0; _mi < navigator.mimeTypes.length; _mi++) {
		if (navigator.mimeTypes[_mi] && !navigator.mimeTypes[_mi].enabledPlugin) {
			navigator.mimeTypes[_mi].enabledPlugin = navigator.plugins[0] || null;
		}
	}
}

// --- screen is created natively in engine.go setupScreen() ---

// --- Browser constructor chains ---
// Real browsers have prototype hierarchies so Object.prototype.toString.call()
// returns correct type strings (e.g. "[object Window]" not "[object Object]").
function EventTarget() { throw new TypeError("Illegal constructor"); }
Object.defineProperty(EventTarget.prototype, Symbol.toStringTag, { value: 'EventTarget', configurable: true });

function Node() { throw new TypeError("Illegal constructor"); }
Node.prototype = Object.create(EventTarget.prototype);
Node.prototype.constructor = Node;
Object.defineProperty(Node.prototype, Symbol.toStringTag, { value: 'Node', configurable: true });
Node.ELEMENT_NODE = 1; Node.TEXT_NODE = 3; Node.COMMENT_NODE = 8;
Node.DOCUMENT_NODE = 9; Node.DOCUMENT_FRAGMENT_NODE = 11;
// Also on prototype so element instances inherit via prototype chain (Chrome behavior)
Node.prototype.ELEMENT_NODE = 1; Node.prototype.ATTRIBUTE_NODE = 2;
Node.prototype.TEXT_NODE = 3; Node.prototype.CDATA_SECTION_NODE = 4;
Node.prototype.PROCESSING_INSTRUCTION_NODE = 7; Node.prototype.COMMENT_NODE = 8;
Node.prototype.DOCUMENT_NODE = 9; Node.prototype.DOCUMENT_TYPE_NODE = 10;
Node.prototype.DOCUMENT_FRAGMENT_NODE = 11;

function Element() { throw new TypeError("Illegal constructor"); }
Element.prototype = Object.create(Node.prototype);
Element.prototype.constructor = Element;
Object.defineProperty(Element.prototype, Symbol.toStringTag, { value: 'Element', configurable: true });

function HTMLElement() {
	// Allow super() calls from custom element subclasses
	if (new.target && new.target !== HTMLElement) return;
	throw new TypeError("Illegal constructor");
}
HTMLElement.prototype = Object.create(Element.prototype);
HTMLElement.prototype.constructor = HTMLElement;
Object.defineProperty(HTMLElement.prototype, Symbol.toStringTag, { value: 'HTMLElement', configurable: true });

// Document and HTMLDocument are created natively in engine.go setupDocument()
// with native accessor properties on Document.prototype. Here we wire up the
// prototype chain to inherit from Node while preserving those native accessors.
// Object.setPrototypeOf changes [[Prototype]] without replacing the prototype object,
// so native V8 accessor properties (URL, body, hidden, etc.) survive.
Object.setPrototypeOf(Document.prototype, Node.prototype);
Document.prototype.constructor = Document;
// Symbol.toStringTag already set by engine.go

Object.setPrototypeOf(HTMLDocument.prototype, Document.prototype);
HTMLDocument.prototype.constructor = HTMLDocument;
// Symbol.toStringTag already set by engine.go

// Navigator constructor is created natively in engine.go setupNavigator()

// Screen constructor is created natively in engine.go setupScreen()

// WindowProperties sits between Window.prototype and EventTarget.prototype in Chrome
var WindowProperties = Object.create(EventTarget.prototype);
Object.defineProperty(WindowProperties, Symbol.toStringTag, { value: 'WindowProperties', configurable: true });

function Window() { throw new TypeError("Illegal constructor"); }
Window.prototype = Object.create(WindowProperties);
Window.prototype.constructor = Window;
Window.prototype.TEMPORARY = 0;
Window.prototype.PERSISTENT = 1;
Object.defineProperty(Window.prototype, Symbol.toStringTag, { value: 'Window', configurable: true });

function CSSStyleDeclaration() { throw new TypeError("Illegal constructor"); }
Object.defineProperty(CSSStyleDeclaration.prototype, Symbol.toStringTag, { value: 'CSSStyleDeclaration', configurable: true });
// BM scripts check CSSStyleDeclaration.prototype for these methods via computed property names.
// They must exist on the prototype (not just instances) for feature detection to work.
CSSStyleDeclaration.prototype.getPropertyValue = function(p) { return this[p] !== undefined ? this[p] : ''; };
CSSStyleDeclaration.prototype.setProperty = function(p, v) { this[p] = String(v); };
CSSStyleDeclaration.prototype.removeProperty = function(p) { var old = this[p] || ''; delete this[p]; return old; };
CSSStyleDeclaration.prototype.getPropertyPriority = function() { return ''; };
CSSStyleDeclaration.prototype.item = function(i) { var keys = Object.keys(this); return keys[i] || ''; };
Object.defineProperty(CSSStyleDeclaration.prototype, 'cssText', { get: function() { return this._cssText || ''; }, set: function(v) { this._cssText = v; }, configurable: true });
Object.defineProperty(CSSStyleDeclaration.prototype, 'length', { get: function() { return Object.keys(this).filter(function(k){return k[0]!=='_'&&typeof this[k]==='string';}.bind(this)).length; }, configurable: true });
Object.defineProperty(CSSStyleDeclaration.prototype, 'parentRule', { get: function() { return null; }, configurable: true });

// Factory for per-element CSSStyleDeclaration.
// Each element gets its own style object (not a shared singleton).
// Includes common CSS properties initialized to '' (Chrome behavior for unset properties).
function _mkStyle() {
	var s = {cssText: '', length: 0,
		display: '', visibility: '', opacity: '', position: '', overflow: '',
		width: '', height: '', top: '', right: '', bottom: '', left: '',
		margin: '', marginTop: '', marginRight: '', marginBottom: '', marginLeft: '',
		padding: '', paddingTop: '', paddingRight: '', paddingBottom: '', paddingLeft: '',
		border: '', borderWidth: '', borderStyle: '', borderColor: '', borderRadius: '',
		borderTop: '', borderRight: '', borderBottom: '', borderLeft: '',
		background: '', backgroundColor: '', backgroundImage: '', backgroundPosition: '', backgroundSize: '',
		color: '', fontSize: '', fontFamily: '', fontWeight: '', fontStyle: '',
		lineHeight: '', textAlign: '', textDecoration: '', textTransform: '',
		transform: '', transition: '', animation: '', cursor: '', pointerEvents: '',
		zIndex: '', float: '', clear: '', boxSizing: '',
		flex: '', flexDirection: '', flexWrap: '', flexGrow: '', flexShrink: '', flexBasis: '',
		justifyContent: '', alignItems: '', alignSelf: '', alignContent: '',
		grid: '', gridTemplate: '', gridTemplateColumns: '', gridTemplateRows: '',
		gap: '', rowGap: '', columnGap: '',
		overflowX: '', overflowY: '', whiteSpace: '', wordBreak: '', wordWrap: '',
		outline: '', outlineWidth: '', outlineStyle: '', outlineColor: '',
		boxShadow: '', textShadow: '', filter: '', backdropFilter: '',
		userSelect: '', touchAction: '', appearance: '', resize: '',
		objectFit: '', objectPosition: '', verticalAlign: '', direction: '',
		content: '', listStyle: '', tableLayout: '', clipPath: '',
		willChange: '', perspective: '', transformOrigin: '', transformStyle: '',
		webkitAppearance: '', webkitTextFillColor: '', webkitFontSmoothing: ''
	};
	s.setProperty = function(p, v) { s[p] = String(v); };
	s.removeProperty = function(p) { var old = s[p] || ''; delete s[p]; return old; };
	s.getPropertyValue = function(p) { return s[p] !== undefined ? s[p] : ''; };
	s.getPropertyPriority = function() { return ''; };
	s.item = function(i) { var keys = Object.keys(s); return keys[i] || ''; };
	Object.setPrototypeOf(s, CSSStyleDeclaration.prototype);
	return s;
}

function ShadowRoot() { throw new TypeError("Illegal constructor"); }
ShadowRoot.prototype = Object.create(Node.prototype);
ShadowRoot.prototype.constructor = ShadowRoot;
Object.defineProperty(ShadowRoot.prototype, Symbol.toStringTag, { value: 'ShadowRoot', configurable: true });

// --- Early collection constructor stubs (needed BEFORE _mkEl) ---
function NodeList() { throw new TypeError("Illegal constructor"); }
NodeList.prototype = Object.create(Array.prototype);
NodeList.prototype.constructor = NodeList;
Object.defineProperty(NodeList.prototype, Symbol.toStringTag, { value: 'NodeList', configurable: true });
NodeList.prototype.item = function(i) { return this[i] || null; };
// Chrome's NodeList.prototype has: item, entries, forEach, keys, values (own properties)
NodeList.prototype.forEach = Array.prototype.forEach;
NodeList.prototype.entries = Array.prototype.entries;
NodeList.prototype.keys = Array.prototype.keys;
NodeList.prototype.values = Array.prototype.values;
// Helper: create a NodeList-typed array (keeps Array methods like push/splice)
function _mkNodeList() { var a = []; Object.setPrototypeOf(a, NodeList.prototype); return a; }

function HTMLCollection() { throw new TypeError("Illegal constructor"); }
HTMLCollection.prototype = Object.create(Array.prototype);
HTMLCollection.prototype.constructor = HTMLCollection;
Object.defineProperty(HTMLCollection.prototype, Symbol.toStringTag, { value: 'HTMLCollection', configurable: true });
HTMLCollection.prototype.item = function(i) { return this[i] || null; };
HTMLCollection.prototype.namedItem = function(name) { return null; };
function _mkHTMLCollection() { var a = []; Object.setPrototypeOf(a, HTMLCollection.prototype); return a; }

function DOMTokenList() { throw new TypeError("Illegal constructor"); }
DOMTokenList.prototype = Object.create(Object.prototype);
DOMTokenList.prototype.constructor = DOMTokenList;
Object.defineProperty(DOMTokenList.prototype, Symbol.toStringTag, { value: 'DOMTokenList', configurable: true });
DOMTokenList.prototype.add = function() {};
DOMTokenList.prototype.remove = function() {};
DOMTokenList.prototype.contains = function() { return false; };
DOMTokenList.prototype.toggle = function() { return false; };
DOMTokenList.prototype.item = function(i) { return null; };
DOMTokenList.prototype.supports = function() { return false; };

function NamedNodeMap() { throw new TypeError("Illegal constructor"); }
NamedNodeMap.prototype = Object.create(Object.prototype);
NamedNodeMap.prototype.constructor = NamedNodeMap;
Object.defineProperty(NamedNodeMap.prototype, Symbol.toStringTag, { value: 'NamedNodeMap', configurable: true });
NamedNodeMap.prototype.item = function(i) { return null; };
NamedNodeMap.prototype.getNamedItem = function(name) { return null; };
NamedNodeMap.prototype.setNamedItem = function(attr) {};
NamedNodeMap.prototype.removeNamedItem = function(name) {};

// Tag name -> element constructor name mapping
let _eTM = {
	'div': 'HTMLDivElement', 'span': 'HTMLSpanElement', 'p': 'HTMLParagraphElement',
	'a': 'HTMLAnchorElement', 'input': 'HTMLInputElement', 'form': 'HTMLFormElement',
	'script': 'HTMLScriptElement', 'style': 'HTMLStyleElement', 'link': 'HTMLLinkElement',
	'meta': 'HTMLMetaElement', 'img': 'HTMLImageElement', 'iframe': 'HTMLIFrameElement',
	'canvas': 'HTMLCanvasElement', 'button': 'HTMLButtonElement', 'select': 'HTMLSelectElement',
	'textarea': 'HTMLTextAreaElement', 'table': 'HTMLTableElement',
	'tr': 'HTMLTableRowElement', 'td': 'HTMLTableCellElement', 'th': 'HTMLTableCellElement',
	'h1': 'HTMLHeadingElement', 'h2': 'HTMLHeadingElement', 'h3': 'HTMLHeadingElement',
	'h4': 'HTMLHeadingElement', 'h5': 'HTMLHeadingElement', 'h6': 'HTMLHeadingElement',
	'ul': 'HTMLUListElement', 'ol': 'HTMLOListElement', 'li': 'HTMLLIElement',
	'br': 'HTMLBRElement', 'hr': 'HTMLHRElement',
	'head': 'HTMLHeadElement', 'body': 'HTMLBodyElement', 'html': 'HTMLHtmlElement',
	'video': 'HTMLVideoElement', 'audio': 'HTMLAudioElement', 'source': 'HTMLSourceElement',
	'label': 'HTMLLabelElement'
};

// --- DOM tree mutation helpers ---
// These maintain the childNodes/children arrays consistently.
// firstChild/lastChild/nextSibling/previousSibling are computed via getters
// from these arrays, so keeping the arrays correct is sufficient.
function _domRemoveFromParent(child) {
	if (!child || typeof child !== 'object') return;
	var parent = child._parentNode;
	if (!parent) return;
	if (parent.childNodes) {
		var i = parent.childNodes.indexOf(child);
		if (i !== -1) parent.childNodes.splice(i, 1);
	}
	if (parent.children && child.nodeType === 1) {
		var j = parent.children.indexOf(child);
		if (j !== -1) parent.children.splice(j, 1);
	}
	child._parentNode = null;
	child._parentElement = null;
}

function _domAppendChild(parent, child) {
	if (!child) return child;
	// Handle DocumentFragment: append all its children
	if (child.nodeType === 11 && child.childNodes) {
		var kids = child.childNodes.slice(); // copy since we mutate
		for (var i = 0; i < kids.length; i++) _domAppendChild(parent, kids[i]);
		return child;
	}
	// Handle string/number: create text node
	if (typeof child === 'string' || typeof child === 'number') {
		child = {nodeType: 3, nodeName: '#text', textContent: String(child), data: String(child), nodeValue: String(child), _parentNode: null, _parentElement: null};
	}
	// Remove from old parent if moving
	_domRemoveFromParent(child);
	// Add to new parent
	if (parent.childNodes) parent.childNodes.push(child);
	if (parent.children && child.nodeType === 1) parent.children.push(child);
	child._parentNode = parent;
	child._parentElement = (parent.nodeType === 1) ? parent : null;
	// Notify MutationObserver
	if (typeof _domNotifyMutation === 'function') _domNotifyMutation(parent, 'childList', {addedNodes: [child]});
	// Fire connectedCallback for custom elements when appended to DOM
	if (child.nodeType === 1 && typeof child.connectedCallback === 'function') {
		try { child.connectedCallback(); } catch(e) {}
	}
	return child;
}

function _domRemoveChild(parent, child) {
	if (!child) return child;
	if (parent.childNodes) {
		var i = parent.childNodes.indexOf(child);
		if (i !== -1) parent.childNodes.splice(i, 1);
	}
	if (parent.children && child.nodeType === 1) {
		var j = parent.children.indexOf(child);
		if (j !== -1) parent.children.splice(j, 1);
	}
	child._parentNode = null;
	child._parentElement = null;
	// Notify MutationObserver
	if (typeof _domNotifyMutation === 'function') _domNotifyMutation(parent, 'childList', {removedNodes: [child]});
	return child;
}

function _domInsertBefore(parent, node, ref) {
	if (!node) return node;
	if (!ref) return _domAppendChild(parent, node);
	// Handle DocumentFragment
	if (node.nodeType === 11 && node.childNodes) {
		var kids = node.childNodes.slice();
		for (var i = 0; i < kids.length; i++) _domInsertBefore(parent, kids[i], ref);
		return node;
	}
	// Remove from old parent
	_domRemoveFromParent(node);
	// Insert in childNodes before ref
	if (parent.childNodes) {
		var ri = parent.childNodes.indexOf(ref);
		if (ri !== -1) parent.childNodes.splice(ri, 0, node);
		else parent.childNodes.push(node);
	}
	// Insert in children (element-only) before ref
	if (parent.children && node.nodeType === 1) {
		var ci = parent.children.indexOf(ref);
		if (ci !== -1) parent.children.splice(ci, 0, node);
		else parent.children.push(node);
	}
	node._parentNode = parent;
	node._parentElement = (parent.nodeType === 1) ? parent : null;
	// Notify MutationObserver
	if (typeof _domNotifyMutation === 'function') _domNotifyMutation(parent, 'childList', {addedNodes: [node]});
	return node;
}

function _domReplaceChild(parent, newChild, oldChild) {
	if (!oldChild) return oldChild;
	_domInsertBefore(parent, newChild, oldChild);
	_domRemoveChild(parent, oldChild);
	return oldChild;
}

// MutationObserver notification placeholder (replaced in Task 6)
var _domMutationObservers = [];
function _domNotifyMutation(target, type, data) {
	for (var i = 0; i < _domMutationObservers.length; i++) {
		var obs = _domMutationObservers[i];
		if (!obs._targets) continue;
		for (var j = 0; j < obs._targets.length; j++) {
			var entry = obs._targets[j];
			if (entry.target !== target) continue;
			if (type === 'childList' && !entry.options.childList) continue;
			if (type === 'attributes' && !entry.options.attributes) continue;
			if (type === 'characterData' && !entry.options.characterData) continue;
			var record = {type: type, target: target, addedNodes: (data && data.addedNodes) || [], removedNodes: (data && data.removedNodes) || [], attributeName: (data && data.attributeName) || null, oldValue: (data && data.oldValue) || null};
			obs._records.push(record);
			if (!obs._scheduled) {
				obs._scheduled = true;
				var o = obs;
				Promise.resolve().then(function() {
					o._scheduled = false;
					var records = o._records.splice(0);
					if (records.length > 0 && o._callback) {
						try { o._callback(records, o); } catch(e) {}
					}
				});
			}
			break;
		}
	}
}

// --- innerHTML parser: calls Go's __goParseHTML, creates real DOM nodes ---
function _domBuildNodes(parsedArray, ownerDoc) {
	var result = [];
	if (!parsedArray) return result;
	for (var i = 0; i < parsedArray.length; i++) {
		var n = parsedArray[i];
		if (n.type === 'text') {
			var tn = {nodeType: 3, textContent: n.text, data: n.text, _parentNode: null, _parentElement: null, nodeName: '#text', nodeValue: n.text};
			result.push(tn);
		} else if (n.type === 'comment') {
			var cn = {nodeType: 8, textContent: n.text, data: n.text, _parentNode: null, _parentElement: null, nodeName: '#comment'};
			result.push(cn);
		} else if (n.type === 'element') {
			var el = _mkEl(n.tag);
			el.ownerDocument = ownerDoc || (typeof document !== 'undefined' ? document : null);
			// Set attributes
			if (n.attrs) {
				for (var k in n.attrs) {
					if (n.attrs.hasOwnProperty(k)) {
						el.setAttribute(k, n.attrs[k]);
						// Sync reflected properties
						if (k === 'id') el.id = n.attrs[k];
						if (k === 'class') el.className = n.attrs[k];
						if (k === 'src') try { el.src = n.attrs[k]; } catch(e) {}
						if (k === 'href') try { el.href = n.attrs[k]; } catch(e) {}
						if (k === 'value') try { el.value = n.attrs[k]; } catch(e) {}
						if (k === 'type') try { el.type = n.attrs[k]; } catch(e) {}
						if (k === 'name') try { el.name = n.attrs[k]; } catch(e) {}
					}
				}
			}
			// Recursively build children
			if (n.children && n.children.length > 0) {
				var kids = _domBuildNodes(n.children, ownerDoc);
				for (var j = 0; j < kids.length; j++) {
					_domAppendChild(el, kids[j]);
				}
			}
			result.push(el);
		}
	}
	return result;
}

function _domSetInnerHTML(el, html) {
	// Clear existing children
	if (el.childNodes) {
		while (el.childNodes.length > 0) {
			_domRemoveChild(el, el.childNodes[el.childNodes.length - 1]);
		}
	}
	if (!html || html === '') return;
	// Parse HTML via Go
	var jsonStr = _goParseHTML(html);
	var parsed;
	try { parsed = JSON.parse(jsonStr); } catch(e) { return; }
	if (!parsed || !parsed.length) return;
	// Build and attach DOM nodes
	var ownerDoc = el.ownerDocument || (typeof document !== 'undefined' ? document : null);
	var nodes = _domBuildNodes(parsed, ownerDoc);
	for (var i = 0; i < nodes.length; i++) {
		_domAppendChild(el, nodes[i]);
	}
}

function _domGetTextContent(node) {
	if (!node) return '';
	if (node.nodeType === 3 || node.nodeType === 8) return node.textContent || node.data || '';
	if (!node.childNodes || node.childNodes.length === 0) return '';
	var text = '';
	for (var i = 0; i < node.childNodes.length; i++) {
		text += _domGetTextContent(node.childNodes[i]);
	}
	return text;
}

function _domGetInnerHTML(node) {
	if (!node || !node.childNodes) return '';
	var html = '';
	for (var i = 0; i < node.childNodes.length; i++) {
		var child = node.childNodes[i];
		if (child.nodeType === 3) {
			html += (child.textContent || child.data || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
		} else if (child.nodeType === 8) {
			html += '<!--' + (child.textContent || child.data || '') + '-->';
		} else if (child.nodeType === 1) {
			var tag = (child.tagName || 'div').toLowerCase();
			html += '<' + tag;
			// Serialize attributes
			if (child.attributes) {
				var keys = Object.keys(child.attributes);
				for (var j = 0; j < keys.length; j++) {
					var k = keys[j];
					if (k === 'length' || k === 'item' || k === 'getNamedItem' || k === 'setNamedItem' || k === 'removeNamedItem') continue;
					if (typeof child.attributes[k] === 'function') continue;
					html += ' ' + k + '="' + String(child.attributes[k]).replace(/"/g, '&quot;') + '"';
				}
			}
			// Void elements
			var voids = {area:1,base:1,br:1,col:1,embed:1,hr:1,img:1,input:1,link:1,meta:1,param:1,source:1,track:1,wbr:1};
			if (voids[tag]) {
				html += '>';
			} else {
				html += '>';
				html += _domGetInnerHTML(child);
				html += '</' + tag + '>';
			}
		}
	}
	return html;
}

// --- CSS selector engine ---
// Parses CSS selectors and matches against real DOM tree.
// Supports: tag, #id, .class, [attr], [attr=value], [attr~=value], [attr|=value],
// [attr^=value], [attr$=value], [attr*=value], tag.class, #id .class tag,
// >, +, ~, :first-child, :last-child, :nth-child(n), :not(sel), *, comma groups

// Parse a simple selector (no combinators) into a matcher object
function _parseSingleSelector(sel) {
	var m = {tag: null, id: null, classes: [], attrs: [], pseudos: [], not: null};
	var i = 0, len = sel.length;
	while (i < len) {
		var ch = sel.charAt(i);
		if (ch === '#') {
			i++;
			var start = i;
			while (i < len && sel.charAt(i) !== '.' && sel.charAt(i) !== '#' && sel.charAt(i) !== '[' && sel.charAt(i) !== ':' && sel.charAt(i) !== ' ') i++;
			m.id = sel.substring(start, i);
		} else if (ch === '.') {
			i++;
			var start = i;
			while (i < len && sel.charAt(i) !== '.' && sel.charAt(i) !== '#' && sel.charAt(i) !== '[' && sel.charAt(i) !== ':' && sel.charAt(i) !== ' ') i++;
			m.classes.push(sel.substring(start, i));
		} else if (ch === '[') {
			i++;
			var start = i;
			while (i < len && sel.charAt(i) !== ']') i++;
			var attrExpr = sel.substring(start, i);
			i++; // skip ]
			// Parse attr expression: name, name=value, name~=value, etc.
			var eqIdx = attrExpr.indexOf('=');
			if (eqIdx === -1) {
				m.attrs.push({name: attrExpr.trim(), op: 'exists'});
			} else {
				var op = '=';
				var aName = attrExpr.substring(0, eqIdx);
				if (aName.charAt(aName.length - 1) === '~') { op = '~='; aName = aName.slice(0, -1); }
				else if (aName.charAt(aName.length - 1) === '|') { op = '|='; aName = aName.slice(0, -1); }
				else if (aName.charAt(aName.length - 1) === '^') { op = '^='; aName = aName.slice(0, -1); }
				else if (aName.charAt(aName.length - 1) === '$') { op = '$='; aName = aName.slice(0, -1); }
				else if (aName.charAt(aName.length - 1) === '*') { op = '*='; aName = aName.slice(0, -1); }
				var aVal = attrExpr.substring(eqIdx + 1).trim();
				// Remove quotes
				if ((aVal.charAt(0) === '"' && aVal.charAt(aVal.length-1) === '"') || (aVal.charAt(0) === "'" && aVal.charAt(aVal.length-1) === "'"))
					aVal = aVal.substring(1, aVal.length - 1);
				m.attrs.push({name: aName.trim(), op: op, value: aVal});
			}
		} else if (ch === ':') {
			i++;
			var start = i;
			if (sel.substring(i, i+4) === 'not(') {
				i += 4;
				var depth = 1;
				var ns = i;
				while (i < len && depth > 0) { if (sel.charAt(i) === '(') depth++; if (sel.charAt(i) === ')') depth--; i++; }
				m.not = sel.substring(ns, i - 1);
			} else {
				while (i < len && sel.charAt(i) !== '.' && sel.charAt(i) !== '#' && sel.charAt(i) !== '[' && sel.charAt(i) !== ':' && sel.charAt(i) !== ' ' && sel.charAt(i) !== '(' && sel.charAt(i) !== ')') i++;
				var pseudo = sel.substring(start, i);
				if (i < len && sel.charAt(i) === '(') {
					i++;
					var ps = i;
					while (i < len && sel.charAt(i) !== ')') i++;
					pseudo += '(' + sel.substring(ps, i) + ')';
					i++;
				}
				m.pseudos.push(pseudo);
			}
		} else if (ch === '*') {
			m.tag = '*';
			i++;
		} else if (ch !== ' ') {
			// Tag name
			var start = i;
			while (i < len && sel.charAt(i) !== '.' && sel.charAt(i) !== '#' && sel.charAt(i) !== '[' && sel.charAt(i) !== ':' && sel.charAt(i) !== ' ') i++;
			m.tag = sel.substring(start, i).toUpperCase();
		} else {
			i++;
		}
	}
	return m;
}

function _matchesSingle(el, matcher) {
	if (!el || el.nodeType !== 1) return false;
	if (matcher.tag && matcher.tag !== '*' && (el.tagName || '').toUpperCase() !== matcher.tag) return false;
	if (matcher.id && (el.id || '') !== matcher.id) return false;
	for (var i = 0; i < matcher.classes.length; i++) {
		var cn = el.className || '';
		if (typeof cn !== 'string') cn = cn.toString ? cn.toString() : '';
		var classes = cn.split(/\s+/);
		if (classes.indexOf(matcher.classes[i]) === -1) return false;
	}
	for (var i = 0; i < matcher.attrs.length; i++) {
		var a = matcher.attrs[i];
		var val = null;
		if (el.getAttribute) val = el.getAttribute(a.name);
		else if (el.attributes && el.attributes[a.name] !== undefined) val = el.attributes[a.name];
		if (a.op === 'exists') { if (val === null) return false; }
		else if (a.op === '=') { if (val !== a.value) return false; }
		else if (a.op === '~=') { if (!val || val.split(/\s+/).indexOf(a.value) === -1) return false; }
		else if (a.op === '|=') { if (!val || (val !== a.value && val.indexOf(a.value + '-') !== 0)) return false; }
		else if (a.op === '^=') { if (!val || val.indexOf(a.value) !== 0) return false; }
		else if (a.op === '$=') { if (!val || val.indexOf(a.value, val.length - a.value.length) === -1) return false; }
		else if (a.op === '*=') { if (!val || val.indexOf(a.value) === -1) return false; }
	}
	for (var i = 0; i < matcher.pseudos.length; i++) {
		var p = matcher.pseudos[i];
		if (p === 'first-child') {
			var parent = el._parentNode;
			if (!parent || !parent.children || parent.children[0] !== el) return false;
		} else if (p === 'last-child') {
			var parent = el._parentNode;
			if (!parent || !parent.children || parent.children[parent.children.length - 1] !== el) return false;
		} else if (p.indexOf('nth-child(') === 0) {
			var nthExpr = p.substring(10, p.length - 1);
			var parent = el._parentNode;
			if (!parent || !parent.children) return false;
			var idx = parent.children.indexOf(el) + 1; // 1-based
			if (!_matchNth(nthExpr, idx)) return false;
		}
	}
	if (matcher.not) {
		var notMatcher = _parseSingleSelector(matcher.not);
		if (_matchesSingle(el, notMatcher)) return false;
	}
	return true;
}

function _matchNth(expr, idx) {
	expr = expr.trim();
	if (expr === 'odd') return idx %% 2 === 1;
	if (expr === 'even') return idx %% 2 === 0;
	var n = parseInt(expr, 10);
	if (!isNaN(n)) return idx === n;
	// Parse An+B
	var match = expr.match(/^([+-]?\d*)n([+-]\d+)?$/);
	if (match) {
		var a = match[1] === '' || match[1] === '+' ? 1 : match[1] === '-' ? -1 : parseInt(match[1], 10);
		var b = match[2] ? parseInt(match[2], 10) : 0;
		if (a === 0) return idx === b;
		return (idx - b) %% a === 0 && (idx - b) / a >= 0;
	}
	return false;
}

// Parse a full selector string into groups of combinator chains
// e.g. "div > .foo .bar" → [{sel: "div", combinator: null}, {sel: ".foo", combinator: ">"}, {sel: ".bar", combinator: " "}]
function _parseSelector(sel) {
	sel = sel.trim();
	var parts = [];
	var current = '';
	var combinator = null;
	var i = 0, len = sel.length;
	while (i < len) {
		var ch = sel.charAt(i);
		if (ch === ' ' || ch === '>' || ch === '+' || ch === '~') {
			if (current.trim()) {
				parts.push({sel: current.trim(), combinator: combinator});
				current = '';
			}
			// Skip whitespace and find combinator
			while (i < len && sel.charAt(i) === ' ') i++;
			if (i < len && (sel.charAt(i) === '>' || sel.charAt(i) === '+' || sel.charAt(i) === '~')) {
				combinator = sel.charAt(i);
				i++;
				while (i < len && sel.charAt(i) === ' ') i++;
			} else {
				combinator = ' '; // descendant
			}
		} else if (ch === '[') {
			// Consume the entire bracket expression
			current += ch;
			i++;
			while (i < len && sel.charAt(i) !== ']') { current += sel.charAt(i); i++; }
			if (i < len) { current += sel.charAt(i); i++; }
		} else if (ch === '(') {
			current += ch;
			i++;
			var depth = 1;
			while (i < len && depth > 0) {
				if (sel.charAt(i) === '(') depth++;
				if (sel.charAt(i) === ')') depth--;
				current += sel.charAt(i);
				i++;
			}
		} else {
			current += ch;
			i++;
		}
	}
	if (current.trim()) parts.push({sel: current.trim(), combinator: combinator});
	return parts;
}

function _domQuerySelectorAll(root, sel) {
	if (!sel || !root) return [];
	sel = sel.trim();
	// Handle comma-separated selectors
	if (sel.indexOf(',') !== -1) {
		var groups = sel.split(',');
		var results = [];
		var seen = [];
		for (var g = 0; g < groups.length; g++) {
			var matches = _domQuerySelectorAll(root, groups[g].trim());
			for (var m = 0; m < matches.length; m++) {
				if (seen.indexOf(matches[m]) === -1) {
					results.push(matches[m]);
					seen.push(matches[m]);
				}
			}
		}
		return results;
	}
	// Parse the selector into combinator chain
	var chain = _parseSelector(sel);
	if (chain.length === 0) return [];
	// Start matching
	var candidates = _getAllDescendants(root);
	// Filter by the last selector in chain, then walk up for combinators
	var lastPart = chain[chain.length - 1];
	var lastMatcher = _parseSingleSelector(lastPart.sel);
	var results = [];
	for (var ci = 0; ci < candidates.length; ci++) {
		var el = candidates[ci];
		if (!_matchesSingle(el, lastMatcher)) continue;
		// Walk backwards through the chain verifying combinators
		if (_verifyCombinatorChain(el, chain, chain.length - 2, root)) {
			results.push(el);
		}
	}
	return results;
}

function _domQuerySelector(root, sel) {
	if (!sel || !root) return null;
	sel = sel.trim();
	// Handle comma-separated
	if (sel.indexOf(',') !== -1) {
		var groups = sel.split(',');
		for (var g = 0; g < groups.length; g++) {
			var result = _domQuerySelector(root, groups[g].trim());
			if (result) return result;
		}
		return null;
	}
	var chain = _parseSelector(sel);
	if (chain.length === 0) return null;
	var lastMatcher = _parseSingleSelector(chain[chain.length - 1].sel);
	// DFS search for first match
	return _findFirstMatch(root, lastMatcher, chain, root);
}

function _findFirstMatch(node, lastMatcher, chain, root) {
	if (!node) return null;
	var kids = node.childNodes || [];
	for (var i = 0; i < kids.length; i++) {
		var child = kids[i];
		if (child.nodeType === 1 && _matchesSingle(child, lastMatcher) && _verifyCombinatorChain(child, chain, chain.length - 2, root)) {
			return child;
		}
		var found = _findFirstMatch(child, lastMatcher, chain, root);
		if (found) return found;
	}
	return null;
}

function _verifyCombinatorChain(el, chain, idx, root) {
	if (idx < 0) return true; // all parts verified
	var part = chain[idx];
	var matcher = _parseSingleSelector(part.sel);
	var nextCombinator = chain[idx + 1].combinator;

	if (nextCombinator === ' ') {
		// Descendant: any ancestor must match
		var ancestor = el._parentNode;
		while (ancestor && ancestor !== root) {
			if (_matchesSingle(ancestor, matcher) && _verifyCombinatorChain(ancestor, chain, idx - 1, root)) return true;
			ancestor = ancestor._parentNode;
		}
		return false;
	} else if (nextCombinator === '>') {
		// Child: direct parent must match
		var parent = el._parentNode;
		return parent && parent !== root && _matchesSingle(parent, matcher) && _verifyCombinatorChain(parent, chain, idx - 1, root);
	} else if (nextCombinator === '+') {
		// Adjacent sibling
		var prev = _getPrevElementSibling(el);
		return prev && _matchesSingle(prev, matcher) && _verifyCombinatorChain(prev, chain, idx - 1, root);
	} else if (nextCombinator === '~') {
		// General sibling
		var parent = el._parentNode;
		if (!parent || !parent.children) return false;
		var myIdx = parent.children.indexOf(el);
		for (var i = 0; i < myIdx; i++) {
			if (_matchesSingle(parent.children[i], matcher) && _verifyCombinatorChain(parent.children[i], chain, idx - 1, root)) return true;
		}
		return false;
	}
	return false;
}

function _getPrevElementSibling(el) {
	var parent = el._parentNode;
	if (!parent || !parent.children) return null;
	var idx = parent.children.indexOf(el);
	return idx > 0 ? parent.children[idx - 1] : null;
}

function _getAllDescendants(root) {
	var result = [];
	function walk(node) {
		var kids = node.childNodes || [];
		for (var i = 0; i < kids.length; i++) {
			if (kids[i].nodeType === 1) result.push(kids[i]);
			walk(kids[i]);
		}
	}
	walk(root);
	return result;
}

// --- Element factory ---
let _mkEl = function(tag, id) {
	// Try to create a native handler-equipped V8 object via Go callback.
	// This makes the element's property access go through C++ level interception,
	// matching how real browser DOM elements behave at the V8 bytecode level.
	// If the Go callback is not available, fall back to a plain JS object.
	var _nativeBase = _goCreateElement(tag, id);
	var _useNative = (typeof _nativeBase === 'object' && _nativeBase !== null);

	// --- Properties managed by Go-side ElementState (NOT set as JS own properties) ---
	// These are served by the C++ named property handler Getter/Setter, so they
	// appear as interceptor-resolved properties with 0 own property footprint,
	// matching Chrome's native DOM elements.
	// Go-state properties: nodeType, tagName, nodeName, id, className,
	//   innerHTML, outerHTML, innerText, textContent, localName, namespaceURI,
	//   prefix, baseURI, isConnected, offsetWidth, offsetHeight, offsetTop,
	//   offsetLeft, clientWidth, clientHeight, scrollWidth, scrollHeight,
	//   scrollTop, scrollLeft, src, href, type, rel, media, nonce, value,
	//   name, crossOrigin, checked, disabled, width, height

	// --- Properties that remain as JS own properties (methods, complex objects) ---
	// These fall through the C++ handler (Setter returns false) and become
	// real JS own properties on the element object.
	var _jsProps = {
		style: _mkStyle(),
		attributes: (function() {
			var a = {};
			a.length = 0;
			a.item = function(i) { return null; };
			a.getNamedItem = function(name) { return null; };
			a.setNamedItem = function(attr) {};
			a.removeNamedItem = function(name) {};
			Object.setPrototypeOf(a, NamedNodeMap.prototype);
			return a;
		})(),
		children: _mkHTMLCollection(),
		childNodes: _mkNodeList(),
		_parentNode: null,
		_parentElement: null,
		ownerDocument: null, // set after document is created
		classList: (function(owner) {
			var cl = {
				_el: owner,
				_classes: [],
				_sync: function() { this._el.className = this._classes.join(" "); },
				add: function() { for (var i = 0; i < arguments.length; i++) { if (this._classes.indexOf(arguments[i]) === -1) this._classes.push(arguments[i]); } this._sync(); },
				remove: function() { for (var i = 0; i < arguments.length; i++) { var idx = this._classes.indexOf(arguments[i]); if (idx !== -1) this._classes.splice(idx, 1); } this._sync(); },
				contains: function(c) { return this._classes.indexOf(c) !== -1; },
				toggle: function(c) { if (this.contains(c)) { this.remove(c); return false; } else { this.add(c); return true; } },
				get length() { return this._classes.length; },
				item: function(i) { return this._classes[i] || null; },
				toString: function() { return this._classes.join(" "); }
			};
			Object.setPrototypeOf(cl, DOMTokenList.prototype);
			return cl;
		})(el),
		dataset: {},
		setAttribute: function(k, v) {
			this.attributes[k] = String(v);
			if (k === "id") this.id = String(v);
			if (k === "class") this.className = String(v);
			// Sync data-* attributes to dataset
			if (k.indexOf('data-') === 0) {
				var prop = k.slice(5).replace(/-([a-z])/g, function(m, c) { return c.toUpperCase(); });
				if (this.dataset) this.dataset[prop] = String(v);
			}
			// Sync property setters for known reflected attributes
			if (k === "src" || k === "href" || k === "value" || k === "name" || k === "type" || k === "rel" || k === "media" || k === "nonce" || k === "crossOrigin") {
				try { this[k] = String(v); } catch(e) {}
			}
		},
		getAttribute: function(k) { return this.attributes.hasOwnProperty(k) ? this.attributes[k] : null; },
		removeAttribute: function(k) { delete this.attributes[k]; },
		hasAttribute: function(k) { return k in this.attributes; },
		addEventListener: function(ev, fn, opts) {
			if (!this[_sEL]) this[_sEL] = {};
			if (!this[_sEL][ev]) this[_sEL][ev] = [];
			var entry = {fn: fn, capture: false, once: false, passive: false};
			if (opts === true) entry.capture = true;
			else if (opts && typeof opts === 'object') { entry.capture = !!opts.capture; entry.once = !!opts.once; entry.passive = !!opts.passive; }
			this[_sEL][ev].push(entry);
			// Replay pre-buffered behavioral events to this handler.
			// The BM script registers mouse/key/scroll handlers on document/window
			// during initialization, BEFORE its first POST. By replaying events
			// here, the handler accumulates data into the BM script's internal buffers,
			// making POST #1 as comprehensive as POST #6.
			if (_preBufferedEvents[ev]) {
				var buffered = _preBufferedEvents[ev];
				var self = this;
				for (var bi = 0; bi < buffered.length; bi++) {
					try {
						var be = buffered[bi];
						be.target = self;
						be.currentTarget = self;
						fn.call(self, be);
					} catch(e) {}
				}
			}
		},
		removeEventListener: function(ev, fn, opts) {
			if (this[_sEL] && this[_sEL][ev]) {
				var capture = (opts === true) || (opts && typeof opts === 'object' && opts.capture);
				this[_sEL][ev] = this[_sEL][ev].filter(function(e) { return typeof e === 'function' ? e !== fn : !(e.fn === fn && e.capture === !!capture); });
			}
		},
		dispatchEvent: function(ev) { return EventTarget.prototype.dispatchEvent.call(this, ev); },
		appendChild: function(child) { return _domAppendChild(this, child); },
		removeChild: function(child) { return _domRemoveChild(this, child); },
		insertBefore: function(node, ref) { return _domInsertBefore(this, node, ref); },
		replaceChild: function(newNode, oldNode) { return _domReplaceChild(this, newNode, oldNode); },
		remove: function() { _domRemoveFromParent(this); },
		after: function(node) { if (this._parentNode) _domInsertBefore(this._parentNode, node, this.nextSibling); },
		before: function(node) { if (this._parentNode) _domInsertBefore(this._parentNode, node, this); },
		replaceWith: function(node) { if (this._parentNode) { _domInsertBefore(this._parentNode, node, this); _domRemoveFromParent(this); } },
		append: function() { for (var i = 0; i < arguments.length; i++) _domAppendChild(this, arguments[i]); },
		prepend: function() { for (var i = arguments.length - 1; i >= 0; i--) { if (this.childNodes.length > 0) _domInsertBefore(this, arguments[i], this.childNodes[0]); else _domAppendChild(this, arguments[i]); } },
		cloneNode: function(deep) {
			var clone = _mkEl(this.tagName, this.id);
			if (deep && this.childNodes) {
				for (var i = 0; i < this.childNodes.length; i++) {
					var child = this.childNodes[i];
					if (child && child.cloneNode) _domAppendChild(clone, child.cloneNode(true));
					else if (child && child.nodeType === 3) _domAppendChild(clone, {nodeType: 3, nodeName: '#text', textContent: child.textContent, data: child.data, nodeValue: child.textContent, _parentNode: null, _parentElement: null});
				}
			}
			return clone;
		},
		contains: function(node) {
			if (node === this) return true;
			if (!this.childNodes) return false;
			for (var i = 0; i < this.childNodes.length; i++) {
				if (this.childNodes[i] === node) return true;
				if (this.childNodes[i] && this.childNodes[i].contains && this.childNodes[i].contains(node)) return true;
			}
			return false;
		},
		compareDocumentPosition: function(other) {
			// DOM Level 3 compareDocumentPosition.
			// Returns bitmask: 1=DISCONNECTED, 2=PRECEDING, 4=FOLLOWING,
			// 8=CONTAINS, 16=CONTAINED_BY, 32=IMPLEMENTATION_SPECIFIC
			if (this === other) return 0;
			// Simple heuristic: assume 'other' follows 'this' (FOLLOWING)
			return 4;
		},
		matches: function(sel) { return false; },
		closest: function(sel) { return null; },
		getBoundingClientRect: function() { return {top: 100, left: 100, bottom: 200, right: 300, width: 200, height: 100, x: 100, y: 100}; },
		getClientRects: function() { return [this.getBoundingClientRect()]; },
		focus: function() {},
		blur: function() {},
		click: function() {},
		querySelector: function(sel) { return _domQuerySelector(this, sel); },
		querySelectorAll: function(sel) { return _domQuerySelectorAll(this, sel); },
		matches: function(sel) {
			try {
				var matcher = _parseSingleSelector(sel);
				return _matchesSingle(this, matcher);
			} catch(e) { return false; }
		},
		closest: function(sel) {
			var el = this;
			while (el && el.nodeType === 1) {
				try {
					var matcher = _parseSingleSelector(sel);
					if (_matchesSingle(el, matcher)) return el;
				} catch(e) {}
				el = el._parentNode;
			}
			return null;
		},
		getElementsByTagName: function(tag) {
			tag = tag.toUpperCase();
			var r = _domQuerySelectorAll(this, tag === '*' ? '*' : tag.toLowerCase());
			Object.setPrototypeOf(r, HTMLCollection.prototype);
			return r;
		},
		getElementsByClassName: function(cls) {
			var r = _domQuerySelectorAll(this, '.' + cls.split(/\s+/).join('.'));
			Object.setPrototypeOf(r, HTMLCollection.prototype);
			return r;
		},
		attachShadow: function(opts) {
			var host = this;
			var root = {
				nodeType: 11,
				mode: (opts && opts.mode) || "open",
				host: host,
				delegatesFocus: (opts && opts.delegatesFocus) || false,
				slotAssignment: (opts && opts.slotAssignment) || "named",
				childNodes: _mkNodeList(),
				children: _mkHTMLCollection(),
				adoptedStyleSheets: [],
				styleSheets: [],
				innerHTML: "",
				textContent: "",
				ownerDocument: (typeof document !== 'undefined') ? document : null,
				appendChild: function(child) { return _domAppendChild(this, child); },
				removeChild: function(child) { return _domRemoveChild(this, child); },
				insertBefore: function(node, ref) { return _domInsertBefore(this, node, ref); },
				replaceChild: function(newN, oldN) { return _domReplaceChild(this, newN, oldN); },
				append: function() { for (var i = 0; i < arguments.length; i++) _domAppendChild(this, arguments[i]); },
				prepend: function() { for (var i = arguments.length - 1; i >= 0; i--) { if (this.childNodes.length > 0) _domInsertBefore(this, arguments[i], this.childNodes[0]); else _domAppendChild(this, arguments[i]); } },
				querySelector: function(sel) {
					return _domQuerySelector(this, sel);
				},
				querySelectorAll: function(sel) {
					return _domQuerySelectorAll(this, sel);
				},
				getElementById: function(id) {
					return _domQuerySelector(this, '#' + id);
				},
				getElementsByTagName: function(tag) { var r = []; Object.setPrototypeOf(r, HTMLCollection.prototype); return r; },
				getElementsByClassName: function(cls) { var r = []; Object.setPrototypeOf(r, HTMLCollection.prototype); return r; },
				contains: function(node) { return this.children.indexOf(node) !== -1; },
				cloneNode: function() { return host.attachShadow(opts); },
				addEventListener: function() {},
				removeEventListener: function() {},
				dispatchEvent: function() { return true; },
				getRootNode: function() { return this; },
				getSelection: function() { return null; }
			};
			Object.defineProperty(root, 'firstChild', {
				get: function() { return this.childNodes.length > 0 ? this.childNodes[0] : null; },
				configurable: true
			});
			Object.defineProperty(root, 'lastChild', {
				get: function() { return this.childNodes.length > 0 ? this.childNodes[this.childNodes.length - 1] : null; },
				configurable: true
			});
			Object.defineProperty(root, 'firstElementChild', {
				get: function() { return this.children.length > 0 ? this.children[0] : null; },
				configurable: true
			});
			Object.defineProperty(root, 'childElementCount', {
				get: function() { return this.children.length; },
				configurable: true
			});
			host.shadowRoot = (root.mode === "open") ? root : null;
			host._shadowRoot = root;
			Object.setPrototypeOf(root, ShadowRoot.prototype);
			Object.defineProperty(root, Symbol.toStringTag, { value: 'ShadowRoot', configurable: true });
			return root;
		},
		shadowRoot: null,
		getContext: function(type) {
			console.log('[DOM] canvas.getContext(' + type + ')');
			if (type === "2d") {
				return _mk2DC(this);
			}
			if (type === "webgl" || type === "experimental-webgl") {
				return _mkWGL(this, false);
			}
			if (type === "webgl2" || type === "experimental-webgl2") {
				return _mkWGL(this, true);
			}
			return null;
		},
		toDataURL: function(type) {
			var _smallPNG = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==';
			var result = (this.width <= 2 || this.height <= 2) ? _smallPNG : _cvsFP;
			console.log('[DOM] canvas.toDataURL(' + (type||'image/png') + ') w=' + this.width + ' h=' + this.height + ' returning ' + result.length + ' chars');
			return result;
		},
		toBlob: function(callback, type, quality) {
			var _smallPNG = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==';
			var dataUrl = (this.width <= 2 || this.height <= 2) ? _smallPNG : _cvsFP;
			console.log('[DOM] canvas.toBlob w=' + this.width + ' h=' + this.height);
			var b64 = dataUrl.split(',')[1] || '';
			var blob = new Blob([atob(b64)], {type: type || 'image/png'});
			if (typeof callback === 'function') setTimeout(function() { callback(blob); }, 0);
		},
		sheet: { insertRule: function(){}, deleteRule: function(){}, cssRules: [] },
		onload: null,
		onerror: null,
		getRootNode: function() { return (typeof document !== 'undefined') ? document : this; }
	};

	// For non-native fallback, also include Go-state properties as own properties.
	var _allProps = _jsProps;
	if (!_useNative) {
		_allProps = {};
		// Copy JS props first
		var jk = Object.keys(_jsProps);
		for (var ji = 0; ji < jk.length; ji++) _allProps[jk[ji]] = _jsProps[jk[ji]];
		// Add Go-state properties as own properties (fallback path)
		_allProps.nodeType = 1;
		_allProps.tagName = (tag || "DIV").toUpperCase();
		_allProps.nodeName = (tag || "DIV").toUpperCase();
		_allProps.id = id || "";
		_allProps.className = "";
		_allProps.innerHTML = "";
		_allProps.outerHTML = "";
		_allProps.innerText = "";
		_allProps.textContent = "";
		_allProps.isConnected = true;
		_allProps.nextSibling = null;
		_allProps.previousSibling = null;
		_allProps.offsetWidth = 200;
		_allProps.offsetHeight = 100;
		_allProps.offsetTop = 100;
		_allProps.offsetLeft = 100;
		_allProps.clientWidth = 200;
		_allProps.clientHeight = 100;
		_allProps.scrollWidth = 200;
		_allProps.scrollHeight = 100;
		_allProps.scrollTop = 0;
		_allProps.scrollLeft = 0;
		_allProps.src = "";
		_allProps.href = "";
		_allProps.type = "";
		_allProps.rel = "";
		_allProps.media = "";
		_allProps.crossOrigin = null;
		_allProps.nonce = "";
		_allProps.value = "";
		_allProps.checked = false;
		_allProps.disabled = false;
		_allProps.name = "";
		_allProps.width = 300;
		_allProps.height = 150;
	}

	// Use native handler-equipped object if available, otherwise use plain _allProps.
	// For native elements, only JS-side properties (methods, complex objects) are
	// set as own properties. Go-state properties (tagName, id, etc.) are served
	// by the C++ handler from the Go map, zero own property footprint.
	var el;
	if (_useNative) {
		el = _nativeBase;
		// Copy all _jsProps as own properties for now.
		// TODO: move method implementations to prototypes for zero-own-property goal.
		var keys = Object.keys(_jsProps);
		for (var ki = 0; ki < keys.length; ki++) {
			el[keys[ki]] = _jsProps[keys[ki]];
		}
		// Each element needs its own style (not the shared _jsProps reference)
		el.style = _mkStyle();
	} else {
		el = _allProps;
		el.style = _mkStyle();
	}
	// Fix classList back-reference: the IIFE that creates classList runs during
	// _jsProps construction before el is assigned, so _el is undefined. Patch it now.
	if (el.classList && el.classList._el === undefined) {
		el.classList._el = el;
	}

	// Dynamic child accessors (based on children array)
	Object.defineProperty(el, 'firstChild', {
		get: function() { return this.childNodes.length > 0 ? this.childNodes[0] : null; },
		configurable: true
	});
	Object.defineProperty(el, 'lastChild', {
		get: function() { return this.childNodes.length > 0 ? this.childNodes[this.childNodes.length - 1] : null; },
		configurable: true
	});
	Object.defineProperty(el, 'firstElementChild', {
		get: function() {
			for (var i = 0; i < this.children.length; i++) {
				if (this.children[i].nodeType === 1) return this.children[i];
			}
			return null;
		},
		configurable: true
	});
	Object.defineProperty(el, 'lastElementChild', {
		get: function() {
			for (var i = this.children.length - 1; i >= 0; i--) {
				if (this.children[i].nodeType === 1) return this.children[i];
			}
			return null;
		},
		configurable: true
	});
	Object.defineProperty(el, 'childElementCount', {
		get: function() { return this.children.filter(function(c) { return c.nodeType === 1; }).length; },
		configurable: true
	});
	Object.defineProperty(el, 'previousElementSibling', {
		get: function() {
			var p = this._parentNode;
			if (!p || !p.children) return null;
			var idx = p.children.indexOf(this);
			return idx > 0 ? p.children[idx - 1] : null;
		},
		configurable: true
	});
	Object.defineProperty(el, 'nextElementSibling', {
		get: function() {
			var p = this._parentNode;
			if (!p || !p.children) return null;
			var idx = p.children.indexOf(this);
			return idx >= 0 && idx < p.children.length - 1 ? p.children[idx + 1] : null;
		},
		configurable: true
	});
	Object.defineProperty(el, 'previousSibling', {
		get: function() {
			var p = this._parentNode;
			if (!p || !p.childNodes) return null;
			var idx = p.childNodes.indexOf(this);
			return idx > 0 ? p.childNodes[idx - 1] : null;
		},
		configurable: true
	});
	Object.defineProperty(el, 'nextSibling', {
		get: function() {
			var p = this._parentNode;
			if (!p || !p.childNodes) return null;
			var idx = p.childNodes.indexOf(this);
			return idx >= 0 && idx < p.childNodes.length - 1 ? p.childNodes[idx + 1] : null;
		},
		configurable: true
	});
	// parentNode/parentElement, return null for detached nodes (React checks this)
	Object.defineProperty(el, 'parentNode', {
		get: function() { return this._parentNode || null; },
		set: function(v) { this._parentNode = v; },
		configurable: true
	});
	Object.defineProperty(el, 'parentElement', {
		get: function() { return this._parentElement || null; },
		set: function(v) { this._parentElement = v; },
		configurable: true
	});
	// className getter/setter, syncs with classList bidirectionally
	// Wrapped in try-catch because Go-native elements have non-configurable className
	try {
		(function(elem) {
			var _cn = elem.className || "";
			Object.defineProperty(elem, 'className', {
				get: function() { return _cn; },
				set: function(v) {
					_cn = String(v || "");
					// Sync classList from className
					if (this.classList && this.classList._classes) {
						this.classList._classes = _cn ? _cn.split(/\s+/).filter(function(c) { return c; }) : [];
					}
					// Sync to attributes
					this.attributes = this.attributes || {};
					this.attributes['class'] = _cn;
				},
				configurable: true
			});
		})(el);
	} catch(e) {}
	// innerHTML getter/setter, parses HTML into real DOM nodes
	(function(elem) {
		var _rawHTML = "";
		try { Object.defineProperty(elem, 'innerHTML', {
			get: function() {
				// If we have child nodes, serialize them
				if (this.childNodes && this.childNodes.length > 0) return _domGetInnerHTML(this);
				return _rawHTML;
			},
			set: function(v) {
				_rawHTML = String(v || '');
				_domSetInnerHTML(this, _rawHTML);
			},
			configurable: true,
			enumerable: true
		});
	} catch(_) {} })(el);
	// textContent getter/setter
	try { Object.defineProperty(el, 'textContent', {
		get: function() { return _domGetTextContent(this); },
		set: function(v) {
			// Clear children and set a single text node
			if (this.childNodes) {
				while (this.childNodes.length > 0) _domRemoveChild(this, this.childNodes[this.childNodes.length - 1]);
			}
			if (v !== null && v !== undefined && v !== '') {
				_domAppendChild(this, {nodeType: 3, textContent: String(v), data: String(v), _parentNode: null, _parentElement: null, nodeName: '#text', nodeValue: String(v)});
			}
			if (typeof _domNotifyMutation === 'function') _domNotifyMutation(this, 'characterData', {});
		},
		configurable: true,
		enumerable: true
	}); } catch(_) {}
	// innerText getter/setter (same as textContent for our purposes)
	try { Object.defineProperty(el, 'innerText', {
		get: function() { return _domGetTextContent(this); },
		set: function(v) { this.textContent = v; },
		configurable: true,
		enumerable: true
	}); } catch(_) {}
	// outerHTML getter
	try { Object.defineProperty(el, 'outerHTML', {
		get: function() {
			var tag = (this.tagName || 'div').toLowerCase();
			var html = '<' + tag;
			if (this.attributes) {
				var keys = Object.keys(this.attributes);
				for (var j = 0; j < keys.length; j++) {
					var k = keys[j];
					if (k === 'length' || k === 'item' || k === 'getNamedItem' || k === 'setNamedItem' || k === 'removeNamedItem') continue;
					if (typeof this.attributes[k] === 'function') continue;
					html += ' ' + k + '="' + String(this.attributes[k]).replace(/"/g, '&quot;') + '"';
				}
			}
			var voids = {area:1,base:1,br:1,col:1,embed:1,hr:1,img:1,input:1,link:1,meta:1,param:1,source:1,track:1,wbr:1};
			if (voids[tag]) return html + '>';
			return html + '>' + (this.innerHTML || '') + '</' + tag + '>';
		},
		set: function(v) {
			if (this._parentNode) {
				var parsed = JSON.parse(_goParseHTML(String(v || '')));
				var ownerDoc = this.ownerDocument || (typeof document !== 'undefined' ? document : null);
				var nodes = _domBuildNodes(parsed, ownerDoc);
				for (var i = 0; i < nodes.length; i++) {
					_domInsertBefore(this._parentNode, nodes[i], this);
				}
				_domRemoveFromParent(this);
			}
		},
		configurable: true,
		enumerable: true
	}); } catch(_) {}

	// Iframe support, add contentDocument/contentWindow for iframe elements
	// Also add src setter that triggers onload for iframes
	if ((tag || "").toLowerCase() === "iframe") {
		var _iframeSrc = "";
		Object.defineProperty(el, "src", {
			get: function() { return _iframeSrc; },
			set: function(v) {
				_iframeSrc = v;
				console.log('[DOM] iframe.src = ' + v);
				var self = this;
				// For Turnstile challenge iframes: fetch and execute the content
				if (v.indexOf('challenges.cloudflare.com') !== -1 && v.indexOf('turnstile') !== -1) {
					setTimeout(function() {
						console.log('[DOM] Turnstile iframe: fetching content from ' + v);
						_iframeScriptUrl = v;
						try {
							// Set the iframe's location to match the iframe URL
							var iframeUrl = v;
							var iframeOrigin = 'https://challenges.cloudflare.com';
							var iframePathname = '/';
							try {
								var pu = new URL(iframeUrl);
								iframeOrigin = pu.origin;
								iframePathname = pu.pathname;
							} catch(e) {}
							self.contentWindow.location = {
								href: iframeUrl,
								origin: iframeOrigin,
								hostname: 'challenges.cloudflare.com',
								host: 'challenges.cloudflare.com',
								protocol: 'https:',
								pathname: iframePathname,
								search: '',
								hash: '',
								port: '',
								assign: function() {},
								replace: function() {},
								reload: function() {},
								toString: function() { return this.href; },
								[Symbol.toStringTag]: 'Location'
							};
							// window.origin must match location.origin in a real browser
							self.contentWindow.origin = iframeOrigin;
							if (self.contentDocument) {
								self.contentDocument.location = self.contentWindow.location;
								// Set domain and referrer to match real browser behavior.
								// Use Object.defineProperty because these are getter-only on the prototype.
								var _ifrDocProto = Object.getPrototypeOf(self.contentDocument);
								if (_ifrDocProto) {
									Object.defineProperty(_ifrDocProto, 'domain', {
										get: function() { return 'challenges.cloudflare.com'; },
										set: undefined, configurable: true, enumerable: true
									});
									var _refUrl = location.href || '';
									Object.defineProperty(_ifrDocProto, 'referrer', {
										get: function() { return _refUrl; },
										set: undefined, configurable: true, enumerable: true
									});
								}
							}

							var result = JSON.parse(_goSyncFetch(v, 'GET', '', '{}'));
							if (result.status === 200 && result.body) {
								console.log('[DOM] Turnstile iframe HTML: ' + result.body.length + ' chars');

								// --- Inject performance resource entry for the iframe URL ---
								// The Turnstile VM extracts CF-Ray from serverTiming to construct POST URLs.
								// Parse server-timing header: "cf-ray;desc=\"abc123-EWR\""
								// Debug: log all response headers from iframe fetch
								if (result.headers) {
									var _hdrKeys = Object.keys(result.headers);
									console.log('[DOM] iframe response headers (' + _hdrKeys.length + '): ' + _hdrKeys.join(', '));
									for (var hi = 0; hi < _hdrKeys.length; hi++) {
										console.log('[DOM] iframe hdr: ' + _hdrKeys[hi] + '=' + String(result.headers[_hdrKeys[hi]]).substring(0, 100));
									}
								}
								var _stHeader = (result.headers && (result.headers['server-timing'] || result.headers['Server-Timing'])) || '';
								var _serverTimingEntries = [];
								if (_stHeader) {
									console.log('[DOM] iframe server-timing header: ' + _stHeader);
									var _stParts = _stHeader.split(',');
									for (var sti = 0; sti < _stParts.length; sti++) {
										var _stPart = _stParts[sti].trim();
										var _stName = _stPart.split(';')[0].trim();
										var _stDesc = '';
										var _descMatch = _stPart.match(/desc="([^"]+)"/);
										if (_descMatch) _stDesc = _descMatch[1];
										var _stDur = 0;
										var _durMatch = _stPart.match(/dur=([0-9.]+)/);
										if (_durMatch) _stDur = parseFloat(_durMatch[1]);
										_serverTimingEntries.push({name: _stName, description: _stDesc, duration: _stDur});
									}
									console.log('[DOM] iframe serverTiming entries: ' + JSON.stringify(_serverTimingEntries));
								}
								// Add resource entry for the iframe URL to performance timeline
								var _iframeLoadStart = performance.now() - 200;
								var _iframeEntry = {
									name: iframeUrl,
									entryType: 'resource',
									startTime: _iframeLoadStart,
									duration: 180 + Math.random() * 30,
									initiatorType: 'iframe',
									nextHopProtocol: 'h2',
									workerStart: 0, redirectStart: 0, redirectEnd: 0,
									fetchStart: _iframeLoadStart,
									domainLookupStart: _iframeLoadStart,
									domainLookupEnd: _iframeLoadStart + 5,
									connectStart: _iframeLoadStart + 5,
									connectEnd: _iframeLoadStart + 30,
									secureConnectionStart: _iframeLoadStart + 10,
									requestStart: _iframeLoadStart + 32,
									responseStart: _iframeLoadStart + 100,
									responseEnd: _iframeLoadStart + 180,
									transferSize: result.body.length + 400,
									encodedBodySize: result.body.length,
									decodedBodySize: result.body.length,
									serverTiming: _serverTimingEntries,
									toJSON: function() { return this; }
								};
								// Patch performance.getEntriesByType to include the iframe resource entry
								var _origGetEntriesByType = performance.getEntriesByType.bind(performance);
								performance.getEntriesByType = function(type) {
									var entries = _origGetEntriesByType(type);
									if (type === 'resource') entries.push(_iframeEntry);
									return entries;
								};
								// Store in script-scoped var (not on window) to avoid detection
								_ifrST = _serverTimingEntries;
								// Parse script tags from the HTML
								var scriptRe = /<script[^>]*?(?:src=["']([^"']+)["'][^>]*)?>([^]*?)<\/script>/gi;
								var scripts = [];
								var m;
								while ((m = scriptRe.exec(result.body)) !== null) {
									if (m[1]) {
										scripts.push({type: 'external', src: m[1]});
									} else if (m[2] && m[2].trim()) {
										scripts.push({type: 'inline', content: m[2]});
									}
								}
								console.log('[DOM] Turnstile iframe: found ' + scripts.length + ' scripts');
								// Execute each script
								for (var si = 0; si < scripts.length; si++) {
									var s = scripts[si];
									try {
										var code = '';
										if (s.type === 'external') {
											var scriptUrl = s.src;
											// Resolve relative URLs
											if (scriptUrl.indexOf('//') === 0) scriptUrl = 'https:' + scriptUrl;
											else if (scriptUrl.indexOf('/') === 0) scriptUrl = 'https://challenges.cloudflare.com' + scriptUrl;
											console.log('[DOM] Turnstile iframe: fetching script ' + scriptUrl);
											var sr = JSON.parse(_goSyncFetch(scriptUrl, 'GET', '', '{}'));
											if (sr.status === 200) code = sr.body;
											else console.log('[DOM] Turnstile iframe: script fetch failed: ' + sr.status);
										} else {
											code = s.content;
										}
										if (code) {
											console.log('[DOM] Turnstile iframe: executing script (' + code.length + ' chars)');
											// Diagnostics: analyze iframe script structure
											var hasEvalThis = code.indexOf('eval') !== -1 && code.indexOf("'this'") !== -1;
											var hasPostMsg = (code.match(/postMessage/g) || []).length;
											var hasInit = code.indexOf('"init"') !== -1 || code.indexOf("'init'") !== -1;
											var hasReady = code.indexOf('"ready"') !== -1 || code.indexOf("'ready'") !== -1;
											var hasComplete = code.indexOf('"complete"') !== -1 || code.indexOf("'complete'") !== -1;
											var hasMeow = code.indexOf('"meow"') !== -1 || code.indexOf("'meow'") !== -1;
											var hasFood = code.indexOf('"food"') !== -1 || code.indexOf("'food'") !== -1;
											var hasRcV = code.indexOf('"rcV"') !== -1 || code.indexOf("'rcV'") !== -1;
											console.log('[IFRAME-ANALYSIS] eval+this=' + hasEvalThis + ' postMessage=' + hasPostMsg + ' init=' + hasInit + ' ready=' + hasReady + ' complete=' + hasComplete + ' meow=' + hasMeow + ' food=' + hasFood + ' rcV=' + hasRcV);
											console.log('[IFRAME-ANALYSIS] first 500: ' + code.substring(0, 500));
											// --- Patch 1: global-access patterns ---
											// (0, eval)('this') and Function('return this')() escape to V8 global.
											// Replace with 'window' which our Function params shadow to iframeWin.
											// MUST run before Patch 0 (eval interceptor) so 'this' cases are handled first.
											var origLen = code.length;
											code = code.replace(/\(0,\s*eval\)\s*\(\s*(['"])this\1\s*\)/g, 'window');
											code = code.replace(/\(0,eval\)\s*\(\s*(['"])this\1\s*\)/g, 'window');

											// --- Patch 0: Replace remaining (0,eval)( with _iEv( ---
											var preLen0 = code.length;
											code = code.replace(/\(0,\s*eval\)\s*\(/g, '_iEv(');
											var patch0Diff = code.length - preLen0;
											if (patch0Diff !== 0) {
												console.log('[DOM] Turnstile iframe: Patch 0 replaced (0,eval) (' + patch0Diff + ' chars diff)');
											} else {
												console.log('[DOM] Turnstile iframe: Patch 0 found NO (0,eval) to replace');
											}
											code = code.replace(/Function\s*\(\s*(['"])return this\1\s*\)\s*\(\s*\)/g, 'window');
											code = code.replace(/new\s+Function\s*\(\s*(['"])return this\1\s*\)\s*\(\s*\)/g, 'window');

											// VM-CALL guard removed: prototype chain (iframeWin → _v8Global)
											// handles all global lookups now, no need for per-instruction typeof check.
											if (code.length !== origLen) {
												console.log('[DOM] Turnstile iframe: patched global-access patterns (' + (origLen - code.length) + ' chars diff)');
											}

											// --- Patch 2: IIFE .call(window) ---
											// The iframe script is (function(){...VM code...})() or }())
											// In sloppy mode, this inside IIFE = V8 global (NOT iframeWin).
											// Patch the invocation so this = window = iframeWin.
											var iifePatched = false;
											// Try multiple IIFE ending patterns:
											// })() , standard:  (function(){...})()
											// }()) , Crockford: (function(){...}())
											// }}()), nested:    (function(){...{...}}())
											var iifePatterns = [
												{ re: /\}\)\s*\(\s*\)\s*;?\s*$/, rep: '}).call(window);' },
												{ re: /\}\s*\(\s*\)\s*\)\s*;?\s*$/, rep: '}.call(window));' },
												{ re: /\}\}\s*\(\s*\)\s*\)\s*;?\s*$/, rep: '}}.call(window));' },
												{ re: /\}\}\s*\(\s*\)\s*;?\s*$/, rep: '}}.call(window);' }
											];
											for (var pi = 0; pi < iifePatterns.length && !iifePatched; pi++) {
												var pat = iifePatterns[pi];
												var newCode = code.replace(pat.re, function() { iifePatched = true; return pat.rep; });
												if (iifePatched) {
													code = newCode;
													console.log('[DOM] Turnstile iframe: patched IIFE pattern #' + pi + ' to .call(window)');
												}
											}
											if (!iifePatched) {
												console.log('[DOM] Turnstile iframe: WARNING - no IIFE pattern matched');
												console.log('[DOM] Turnstile iframe: last 100 chars: ' + JSON.stringify(code.substring(code.length - 100)));
											}

											// Patch 3 removed, was payload IIFE wrapper causing side effects.

											// --- Setup ---
											var iframeWin = self.contentWindow;
											var iframeEval = function(x) {
												if (x === 'this') return iframeWin;
												return eval(x);
											};

											// Cross-origin proxy for 'top' and 'parent':
											// iframe at challenges.cloudflare.com can only postMessage to cross-origin parent.
											// IMPORTANT: capture the real parent postMessage function from el.contentWindow.parent
											// so it works inside _iframeCtxCall where globals are swapped.
											var _realParentPostMessage = self.contentWindow.parent ? self.contentWindow.parent.postMessage : null;
											// Cross-origin window simulation WITHOUT Proxy.
											// Proxy objects are detectable via Proxy.revocable() tricks,
											// Object.getPrototypeOf differences, and toString leaks.
											// Use a plain object with defineProperty + throws instead.
											var crossOriginWindow = function(target, label, pmFn) {
												var _secErr = function() {
													throw new DOMException(
														"Blocked a frame with origin \"https://challenges.cloudflare.com\" from accessing a cross-origin frame.",
														"SecurityError"
													);
												};
												var obj = Object.create(null);
												Object.defineProperty(obj, 'postMessage', {
													get: function() { return pmFn || target.postMessage.bind(target); },
													enumerable: true, configurable: false
												});
												Object.defineProperty(obj, 'closed', {
													get: function() { return false; },
													enumerable: true, configurable: false
												});
												Object.defineProperty(obj, 'frames', {
													get: function() { return obj; },
													enumerable: true, configurable: false
												});
												Object.defineProperty(obj, 'length', {
													get: function() { return 0; },
													enumerable: true, configurable: false
												});
												Object.defineProperty(obj, 'location', {
													get: function() { _secErr(); },
													set: function(v) { target.location.href = v; },
													enumerable: true, configurable: false
												});
												// NO Symbol.toStringTag, Chrome cross-origin windows return [object Object]
												// then must return undefined (for Promise detection)
												Object.defineProperty(obj, 'then', {
													get: function() { return undefined; },
													enumerable: false, configurable: false
												});
												// Additional properties Chrome exposes on cross-origin windows (14 total)
												Object.defineProperty(obj, 'self', { get: function() { return obj; }, enumerable: true, configurable: false });
												Object.defineProperty(obj, 'window', { get: function() { return obj; }, enumerable: true, configurable: false });
												Object.defineProperty(obj, 'top', { get: function() { return obj; }, enumerable: true, configurable: false });
												Object.defineProperty(obj, 'parent', { get: function() { return obj; }, enumerable: true, configurable: false });
												Object.defineProperty(obj, 'opener', { get: function() { return null; }, enumerable: true, configurable: false });
												Object.defineProperty(obj, 'blur', { get: function() { return function(){}; }, enumerable: true, configurable: false });
												Object.defineProperty(obj, 'focus', { get: function() { return function(){}; }, enumerable: true, configurable: false });
												Object.defineProperty(obj, 'close', { get: function() { return function(){}; }, enumerable: true, configurable: false });
												return obj;
											};
											var crossTop = crossOriginWindow(window, 'top');
											var crossParent = crossOriginWindow(window, 'parent', _realParentPostMessage);

											// --- Timer diagnostics ---
											// Wrap setTimeout/setInterval as Function params so we can detect
											// if the iframe VM registers ANY async work.
											// Timer counter removed, no longer logging.
											var _origSetTimeout = setTimeout;
											var _origSetInterval = setInterval;

											// Context-switching wrapper: saves parent globals, sets iframe
											// globals, calls fn, restores parent globals.  This is necessary
											// because after the iframe eval finishes, V8 globals are restored
											// to the parent page's values.  Timer callbacks registered by the
											// iframe script fire later and need the iframe context.
											var _iframeCtxCall = function(fn, fnArgs) {
												var _v8g = _realIndirectEval('this');
												var _gn = ['window','document','self','parent','top','location',
													'navigator','globalThis','frames','setTimeout','setInterval',
													'XMLHttpRequest','fetch','Date','Function','eval'];
												var _sv = {};
												for (var i = 0; i < _gn.length; i++) _sv[_gn[i]] = _v8g[_gn[i]];
												_v8g.window = iframeWin;
												_v8g.document = self.contentDocument;
												_v8g.self = iframeWin;
												_v8g.parent = crossParent;
												_v8g.top = crossTop;
												// Also set on window directly (V8 global proxy may differ from eval('this'))
												try { window.parent = crossParent; } catch(e) {}
												try { window.top = crossTop; } catch(e) {}
												_v8g.location = iframeWin.location;
												_v8g.navigator = navigator;
												_v8g.globalThis = iframeWin;
												_v8g.frames = [];
												_v8g.setTimeout = _wrappedST;
												_v8g.setInterval = _wrappedSI;
												_v8g.XMLHttpRequest = _iframeXHR;
												_v8g.fetch = _iframeFetch;
												_v8g.Date = _iDate;
												_v8g.Function = _iframeFunction;
												_v8g.eval = _iframeEvalWrapper;
												Object.defineProperty(_realFunction.prototype, 'constructor', {
													value: _iframeFunction, writable: true, configurable: true
												});
												try {
													if (typeof fn === 'function') return (fnArgs && fnArgs.length) ? fn.apply(null, fnArgs) : fn();
													else if (typeof fn === 'string') return (0, eval)(fn);
												} catch(ctxErr) {
													throw ctxErr;
												} finally {
													for (var i = 0; i < _gn.length; i++) _v8g[_gn[i]] = _sv[_gn[i]];
													Object.defineProperty(_realFunction.prototype, 'constructor', {
														value: _origFPC, writable: true, configurable: true
													});
												}
											};

											var _wrappedST = function(fn, delay) {
												var d = delay || 0;
												var origFn = fn;
												var extraArgs = Array.prototype.slice.call(arguments, 2);
												// Inject realistic behavioral event counts into the Wkaxa6 field
												// of the flow POST payload. The challenge script initializes
												// Wkaxa6 counters to 0 AFTER registering event listeners, so
												// dispatched events can't increment them in time. Instead,
												// we directly set plausible values.
												if (extraArgs.length >= 2 && typeof extraArgs[0] === 'string' && extraArgs[0].indexOf('/flow/') !== -1) {
													var _pl = extraArgs[1];
													if (_pl && _pl.Wkaxa6 && typeof _pl.Wkaxa6 === 'object') {
														var _mm = 30 + Math.floor(Math.random() * 60);
														_pl.Wkaxa6.DYHLP0 = _mm;
														_pl.Wkaxa6.LxhDK1 = _mm + Math.floor(Math.random() * 5);
														_pl.Wkaxa6.mAFmZ1 = 1 + Math.floor(Math.random() * 3);
														_pl.Wkaxa6.aTaV9 = 1;
														_pl.Wkaxa6.LdwTe2 = _mm * 2 + 3 + Math.floor(Math.random() * 10);
													}
													// NO overrides, just trace
												// Log BYNjT6 comparison (config vs payload)
												try {
													var _cfgByn = window._cf_chl_opt ? window._cf_chl_opt.BYNjT6 || '' : '';
													var _plKeys = Object.keys(_pl);
													// Find the BYNjT6-equivalent key in the payload
													for (var _bi = 0; _bi < _plKeys.length; _bi++) {
														var _bv = _pl[_plKeys[_bi]];
														if (typeof _bv === 'string' && _bv.length > 1000 && _bv.length < 2000) {
															console.log('[BYN-COMPARE] key=' + _plKeys[_bi] + ' configLen=' + _cfgByn.length + ' payloadLen=' + _bv.length + ' diff=' + (_bv.length - _cfgByn.length));
															// Log the ADDED portion (last N chars that are extra)
															if (_bv.length > _cfgByn.length) {
																var _extra = _bv.length - _cfgByn.length;
																console.log('[BYN-EXTRA] last ' + _extra + ' chars: ' + _bv.substring(_bv.length - _extra));
															}
															break;
														}
													}
												} catch(e) {}
												// Log payload summary and dump to file for analysis
												try {
													var _keys = Object.keys(_pl);
													console.log('[PAYLOAD] keys=' + _keys.length);
													var _fullJSON = _safeStringify(_pl);
													console.log('[PAYLOAD-SIZE] JSON=' + _fullJSON.length + ' bytes');
													_goWriteFile('/tmp/solver_payload.json', _fullJSON);
												} catch(e) { console.log('[PAYLOAD-JSON-ERR] ' + e.message); }
												}
												var wrappedFn = function() {
													var timerArgs = arguments.length > 0 ? Array.prototype.slice.call(arguments) : extraArgs;
													_iframeCtxCall(origFn, timerArgs);
												};
												return _origSetTimeout(wrappedFn, d);
											};
											var _wrappedSI = function(fn, delay) {
												var d = delay || 0;
												var origFn = fn;
												var extraArgs = Array.prototype.slice.call(arguments, 2);
												var wrappedFn = function() {
													var timerArgs = arguments.length > 0 ? Array.prototype.slice.call(arguments) : extraArgs;
													_iframeCtxCall(origFn, timerArgs);
												};
												return _origSetInterval(wrappedFn, d);
											};

											// --- Shadow Function to prevent global escape ---
											// Function("return this")() returns the V8 global, not iframeWin.
											// [].constructor.constructor("return this")() also escapes.
											// We shadow Function in the parameter list AND patch
											// Function.prototype.constructor so ALL escape routes are covered.
											var _realFunction = Function;
											var _iframeFunction = function Function() {
												var args = Array.prototype.slice.call(arguments);
												var body = args.length > 0 ? String(args[args.length - 1]) : '';
												if (/^\s*return\s+this\s*;?\s*$/.test(body)) {
													return function() { return iframeWin; };
												}
												return _realFunction.apply(this, args);
											};
											_iframeFunction.prototype = _realFunction.prototype;
											Object.defineProperty(_iframeFunction, 'length', { value: 1, configurable: true });
											// Mask toString to look native
											_iframeFunction.toString = function() { return 'function Function() { [native code] }'; };
											// Save original Function.prototype.constructor for save/restore in _iframeCtxCall.
											// We do NOT patch it globally, that leaks into the main window context
											// and is detectable (Function !== Function.prototype.constructor).
											var _origFPC = _realFunction.prototype.constructor;

											// Also update iframeWin.Function so window.Function resolves to wrapper
											iframeWin.Function = _iframeFunction;

											// Override XHR origin for iframe context
											// Real browser: XHR from challenges.cloudflare.com iframe
											// sends Origin/Referer = challenges.cloudflare.com
											var _iframeOrigin = 'https://challenges.cloudflare.com';
											var _iframeRefUrl = iframeWin.location.href || _iframeOrigin + '/';
											var _origXHR = XMLHttpRequest;
											var _iframeXHR = function() {
												_origXHR.call(this);
												this._overrideOrigin = _iframeOrigin;
												this._overrideReferer = _iframeRefUrl;
											};
											_iframeXHR.prototype = _origXHR.prototype;
											Object.defineProperty(_iframeXHR, 'name', {value: 'XMLHttpRequest', configurable: true});
											iframeWin.XMLHttpRequest = _iframeXHR;

											// Override fetch for iframe context, sec-fetch-site
											// must be computed relative to the iframe's origin.
											var _iframeFetch = function(url, opts) {
												console.log('[IFRAME-FETCH] fetch(' + url + ')');
												// Resolve relative URLs against iframe origin.
												// In a real browser, fetch('/path') inside an iframe
												// resolves against the iframe's origin.
												var resolvedUrl = (typeof url === 'string') ? url : (url.url || '');
												if (resolvedUrl.charAt(0) === '/') {
													resolvedUrl = _iframeOrigin + resolvedUrl;
												}
												opts = opts || {};
												if (!opts.headers) opts.headers = {};
												var h = opts.headers;
												if (!h['Sec-Fetch-Site'] && !h['sec-fetch-site']) {
													var urlOrigin = '';
													try { var u = new URL(resolvedUrl); urlOrigin = u.origin; } catch(e) {}
													h['Sec-Fetch-Site'] = (urlOrigin === _iframeOrigin) ? 'same-origin' : 'cross-site';
												}
												if (!h['Sec-Fetch-Dest'] && !h['sec-fetch-dest']) h['Sec-Fetch-Dest'] = 'empty';
												if (!h['Sec-Fetch-Mode'] && !h['sec-fetch-mode']) h['Sec-Fetch-Mode'] = opts.mode || 'cors';
												if (!h['Origin'] && !h['origin']) h['Origin'] = _iframeOrigin;
												if (!h['Referer'] && !h['referer']) h['Referer'] = iframeWin.location.href || _iframeOrigin + '/';
												return _mainWindow.fetch(resolvedUrl, opts);
											};
											iframeWin.fetch = _iframeFetch;

											// --- Execute via global eval ---
											// (0, eval)(code) so function declarations land on V8 global.
											// After eval, set prototype chain so non-enumerable eval'd
											// functions (like z) are accessible through iframeWin.
											var _v8Global = (0, eval)('this');

											// Set iframeWin's prototype to V8 global so that
											// dynamically-defined functions (e.g., z) that the VM
											// stores on the V8 global via (0,eval) are accessible
											// through iframeWin. Own properties (document, location,
											// etc.) take precedence over prototype properties.
											try {
												Object.setPrototypeOf(iframeWin, _v8Global);
											} catch(e) {}

											// --- Eval wrapper: make (0,eval)('this') return iframeWin ---
											// In Chrome, window === (0,eval)('this') is true.
											// In our V8 env, (0,eval)('this') returns the V8 global, not iframeWin.
											// Override eval to intercept 'this' lookups during iframe context.
											var _realIndirectEval = (0, eval); // capture real indirect eval
											var _evalCallCount = 0;
											var _iframeEvalWrapper = function eval(code) {
												_evalCallCount++;
												if (_evalCallCount === 1 && typeof code === 'string') {
													// Save the full challenge script for analysis
													try { _goWriteFile('/tmp/turnstile_v2_challenge_eval.js', code); } catch(e) {}
													// Extract and log _cf_chl_opt initialization
													var optMatch = code.match(/window\._cf_chl_opt\s*=\s*\{[^}]+\}/);
													if (optMatch) console.log('[EVAL-WRAP] _cf_chl_opt: ' + optMatch[0].substring(0, 300));
												}
												if (_evalCallCount <= 50) {
													var preview = typeof code === 'string' ? code.substring(0, 80) : String(code);
													console.log('[EVAL-WRAP] #' + _evalCallCount + ' code=' + JSON.stringify(preview) + (typeof code === 'string' && code.length > 80 ? '... (' + code.length + ' chars)' : ''));
												}
												if (typeof code === 'string' && code.trim() === 'this') return iframeWin;
												return _realIndirectEval(code);
											};
											if (typeof _mkFnNat === 'function') _mkFnNat(_iframeEvalWrapper, 'eval');

											// Save V8 globals we're about to override
											var _globalNames = [
												'window', 'document', 'self', 'parent', 'top',
												'location', 'navigator', 'globalThis', 'frames',
												'setTimeout', 'setInterval',
												'XMLHttpRequest', 'fetch', 'Date', 'eval'
											];
											var _saved = {};
											for (var _gi = 0; _gi < _globalNames.length; _gi++) {
												var _gn = _globalNames[_gi];
												_saved[_gn] = _v8Global[_gn];
											}

											// Override V8 globals with iframe values for the eval scope
											_v8Global.window = iframeWin;
											_v8Global.document = self.contentDocument;
											_v8Global.self = iframeWin;
											_v8Global.parent = crossParent;
											_v8Global.top = crossTop;
											try { window.parent = crossParent; } catch(e) {}
											try { window.top = crossTop; } catch(e) {}
											_v8Global.location = iframeWin.location;
											_v8Global.navigator = navigator;
											_v8Global.globalThis = iframeWin;
											_v8Global.frames = [];
											_v8Global.setTimeout = _wrappedST;
											_v8Global.setInterval = _wrappedSI;
											_v8Global.XMLHttpRequest = _iframeXHR;
											_v8Global.fetch = _iframeFetch;
											_v8Global._iEv = iframeEval;
											// Use inflated Date for iframe, simulates real browser execution
											// overhead so timing deltas look realistic to the server.
											_v8Global.Date = _iDate;
											_v8Global.eval = _iframeEvalWrapper;

											// Snapshot pre-eval property names on V8 global
											var _preKeys = {};
											var _preNames = Reflect.ownKeys(_v8Global);
											for (var _pi = 0; _pi < _preNames.length; _pi++) {
												_preKeys[_preNames[_pi]] = true;
											}

											// Store context-switch on contentWindow so postMessage
											// dispatch can wrap handlers in the iframe context.
											Object.defineProperty(self.contentWindow, _SYM_CTX, {value: _iframeCtxCall, writable: true, enumerable: false, configurable: true});

											// Mark iframe XHR, fetch, and other functions as native
											// BEFORE the VM runs, so toString() checks pass.
											if (typeof _mkNat === 'function') {
												_mkNat(iframeWin);
												_mkNat(iframeDoc);
												_mkFnNat(_iframeXHR, 'XMLHttpRequest');
												_mkFnNat(_iframeFetch, 'fetch');
												_mkFnNat(_wrappedST, 'setTimeout');
												_mkFnNat(_wrappedSI, 'setInterval');
												_mkFnNat(_iframeCtxCall, '');
												// Mark iframe Date.now as native, __mark(iframeWin)
												// only goes one level deep, missing Date.now and
												// performance methods. Without this, toString() leaks
												// implementation code (105060 tampering).
												_mkFnNat(_iDNFn, 'now');
												if (iframeWin.Date) {
													_mkFnNat(iframeWin.Date.toString, 'toString');
												}
												if (iframeWin.performance) {
													_mkNat(iframeWin.performance);
												}
											}

											// Debug proxy removed, Proxy objects are detectable by CF

											// --- AMhCb8 setter trap on V8 global ---
											// Debug traps removed, defineProperty on V8 global adds detectable
											// enumerable properties that the VM can enumerate and fingerprint.

											// TEMPORARY: iframe context diagnostics
											(function() {
												var ir = {};
												// What does window resolve to?
												ir['window===iframeWin'] = (window === iframeWin);
												// parent checks (bare identifier vs window.parent)
												ir['parent===crossParent'] = (parent === crossParent);
												ir['window.parent===crossParent'] = (window.parent === crossParent);
												ir['parent===window.parent'] = (parent === window.parent);
												ir['typeof_parent'] = typeof parent;
												ir['typeof_window.parent'] = typeof window.parent;
												try { ir['toString_parent'] = Object.prototype.toString.call(parent); } catch(e) { ir['toString_parent'] = 'ERR:' + e.message; }
												try { ir['toString_window.parent'] = Object.prototype.toString.call(window.parent); } catch(e) { ir['toString_window.parent'] = 'ERR:' + e.message; }
												// top checks
												ir['top===crossTop'] = (top === crossTop);
												ir['window.top===crossTop'] = (window.top === crossTop);
												ir['top===window.top'] = (top === window.top);
												// self/window identity
												ir['self===window'] = (self === window);
												ir['self===iframeWin'] = (self === iframeWin);
												// parent instanceof Window
												try { ir['parent_instanceof_Window'] = (parent instanceof Window); } catch(e) { ir['parent_instanceof_Window'] = 'ERR:' + e.message; }
												try { ir['window.parent_instanceof_Window'] = (window.parent instanceof Window); } catch(e) { ir['window.parent_instanceof_Window'] = 'ERR:' + e.message; }
												// GOPN(parent)
												try { ir['GOPN_parent'] = Object.getOwnPropertyNames(parent).length; } catch(e) { ir['GOPN_parent'] = 'ERR:' + e.message; }
												try { ir['GOPN_window.parent'] = Object.getOwnPropertyNames(window.parent).length; } catch(e) { ir['GOPN_window.parent'] = 'ERR:' + e.message; }
												// cross-origin checks
												try { ir['parent.location'] = String(parent.location); ir['parent.location_threw'] = false; } catch(e) { ir['parent.location'] = e.message.substring(0,80); ir['parent.location_threw'] = true; }
												try { ir['parent.document'] = typeof parent.document; ir['parent.document_threw'] = false; } catch(e) { ir['parent.document'] = e.message.substring(0,80); ir['parent.document_threw'] = true; }
												try { ir['window.parent.location'] = String(window.parent.location); ir['window.parent.location_threw'] = false; } catch(e) { ir['window.parent.location'] = e.message.substring(0,80); ir['window.parent.location_threw'] = true; }
												try { ir['window.parent.document'] = typeof window.parent.document; ir['window.parent.document_threw'] = false; } catch(e) { ir['window.parent.document'] = e.message.substring(0,80); ir['window.parent.document_threw'] = true; }
												// Property descriptors on window
												var pd = Object.getOwnPropertyDescriptor(window, 'parent');
												ir['desc_window.parent'] = pd ? _safeStringify({val:typeof pd.value, get:!!pd.get, set:!!pd.set, enum:pd.enumerable, conf:pd.configurable}) : 'NOT_FOUND';
												var td = Object.getOwnPropertyDescriptor(window, 'top');
												ir['desc_window.top'] = td ? _safeStringify({val:typeof td.value, get:!!td.get, set:!!td.set, enum:td.enumerable, conf:td.configurable}) : 'NOT_FOUND';
												var sd = Object.getOwnPropertyDescriptor(window, 'self');
												ir['desc_window.self'] = sd ? _safeStringify({val:typeof sd.value, get:!!sd.get, set:!!sd.set, enum:sd.enumerable, conf:sd.configurable}) : 'NOT_FOUND';
												// SharedArrayBuffer & fence
												ir['typeof_SharedArrayBuffer'] = typeof SharedArrayBuffer;
												ir['typeof_fence'] = typeof window.fence;
												// document.all
												ir['typeof_document.all'] = typeof document.all;
												// Array.isArray(plugins)
												ir['Array.isArray_plugins'] = Array.isArray(navigator.plugins);
												// window GOPN count
												ir['GOPN_window'] = Object.getOwnPropertyNames(window).length;
												// frameElement
												ir['typeof_frameElement'] = typeof window.frameElement;
												ir['frameElement_tagName'] = window.frameElement ? window.frameElement.tagName : 'null';
												// window.parent === window
												ir['window.parent===window'] = (window.parent === window);
												// eval('this') check
												try { ir['eval_this_is_window'] = ((0, eval)('this') === window); } catch(e) { ir['eval_this'] = 'ERR'; }
												// window.name
												ir['window.name'] = window.name;
												ir['window.length'] = window.length;

												console.log('[IFRAME-DIAG] ' + _safeStringify(ir));
											})();

											try {
												(0, eval)(code);
											} finally {
												// Copy new properties from V8 global to iframeWin.
												// Uses Reflect.ownKeys (captures strings + symbols).
												var _postNames = Reflect.ownKeys(_v8Global);
												var _copied = [];
												for (var _qi = 0; _qi < _postNames.length; _qi++) {
													var _qk = _postNames[_qi];
													if (!_preKeys[_qk] && typeof _qk === 'string') {
														try {
															iframeWin[_qk] = _v8Global[_qk];
															_copied.push(_qk);
														} catch(e) {}
													}
												}

												// Also probe common single-letter function names via eval.
												// V8's global object proxy may not enumerate all declarations.
												var _probeChars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ';
												var _probed = [];
												for (var _ci = 0; _ci < _probeChars.length; _ci++) {
													var _ch = _probeChars.charAt(_ci);
													if (!_preKeys[_ch]) {
														try {
															var _fval = (0, eval)(_ch);
															if (_fval !== undefined && typeof iframeWin[_ch] === 'undefined') {
																iframeWin[_ch] = _fval;
																_probed.push(_ch + ':' + typeof _fval);
															}
														} catch(e) {} // ReferenceError = not defined
													}
												}

												// Accessor tracing DISABLED for production.
												// Converting data properties to getters changes
												// Object.getOwnPropertyDescriptor results which
												// the VM can detect as non-standard.

												// Restore V8 globals
												for (var _ri = 0; _ri < _globalNames.length; _ri++) {
													var _rn = _globalNames[_ri];
													_v8Global[_rn] = _saved[_rn];
												}
											}

											// Restore Function.prototype.constructor
											Object.defineProperty(_realFunction.prototype, 'constructor', {
												value: _origFPC,
												writable: true,
												configurable: true
											});

											// --- Post-execution sync ---
											// Copy _cf_chl_opt from iframeWin to real V8 global
											// (outer scope eval is NOT shadowed, so (0,eval)("this") works)
											var _v8g = (0, eval)('this');
											if (iframeWin._cf_chl_opt && !_v8g._cf_chl_opt) {
												_v8g._cf_chl_opt = iframeWin._cf_chl_opt;
												console.log('[IFRAME-SYNC] _cf_chl_opt synced to V8 global');
												// Track BYNjT6 modifications
												var _origByn = iframeWin._cf_chl_opt.BYNjT6;
												if (_origByn) {
													console.log('[BYN-SEED] initial len=' + _origByn.length);
													var _bynKey = Object.keys(iframeWin._cf_chl_opt).find(function(k) { return iframeWin._cf_chl_opt[k] === _origByn; });
													if (_bynKey) {
														var _bynVal = _origByn;
														Object.defineProperty(iframeWin._cf_chl_opt, _bynKey, {
															get: function() { return _bynVal; },
															set: function(v) {
																if (typeof v === 'string' && v.length !== _bynVal.length) {
																	console.log('[BYN-GROW] ' + _bynVal.length + ' -> ' + v.length + ' (+' + (v.length - _bynVal.length) + ')');
																}
																_bynVal = v;
															},
															configurable: true
														});
													}
												}
											}
											console.log('[DOM] Turnstile iframe: script executed OK');
										}
									} catch(e) {
										console.log('[DOM] Turnstile iframe script error: ' + e.message);
										console.log('[DOM] Turnstile iframe script stack: ' + (e.stack || '').split('\n').slice(0, 5).join(' <- '));
									}
								}
							} else {
								console.log('[DOM] Turnstile iframe fetch failed: status=' + result.status);
							}
						} catch(e) {
							console.log('[DOM] Turnstile iframe fetch error: ' + e.message);
						}
						// Fire onload
						console.log('[DOM] iframe onload firing for ' + v);
						if (self.onload) self.onload();
						if (self._listeners && self._listeners['load']) {
							self._listeners['load'].forEach(function(fn) { fn({type: 'load', target: self}); });
						}

						// --- Behavioral event simulation ---
						// Turnstile VM collects mouse/pointer events for bot detection.
						// Events are dispatched when the challenge script registers its
						// event listeners (triggered from addEventListener hook below).
						// The simulation runs inside _iframeCtxCall so handlers can
						// access the challenge script's global variables (Wkaxa6 etc.).
					}, 50);
				} else {
					// Normal iframe, just simulate onload
					setTimeout(function() {
						console.log('[DOM] iframe onload firing for ' + v);
						if (self.onload) self.onload();
						if (self._listeners && self._listeners['load']) {
							self._listeners['load'].forEach(function(fn) { fn({type: 'load', target: self}); });
						}
					}, 50);
				}
			},
			configurable: true
		});
		// Symbol keys for internal properties, invisible to Object.getOwnPropertyNames()
		var _SYM_EVT = Symbol('evtLs');
		var _SYM_BEHAV = Symbol('behavSent');
		var _SYM_CTX = Symbol('iCtx');
		var iframeDoc = {
			nodeType: 9,
			documentElement: _mkEl("html"),
			head: _mkEl("head"),
			body: _mkEl("body"),
			cookie: "",
			readyState: "complete",
			hidden: false,
			visibilityState: "visible",
			domain: "",
			referrer: "",
			title: "Checking your Browser\u2026",
			characterSet: "UTF-8",
			contentType: "text/html",
			compatMode: "CSS1Compat",
			currentScript: null,
			adoptedStyleSheets: [],
			createElement: function(t) { console.log('[IFRAME-DOM] createElement(' + t + ')'); return _mkEl(t); },
			createElementNS: function(ns, t) { return _mkEl(t); },
			createTextNode: function(text) { return {nodeType: 3, nodeName: '#text', textContent: text, data: text, nodeValue: text, _parentNode: null, _parentElement: null}; },
			createDocumentFragment: function() { var f = _mkEl("fragment"); f.nodeType = 11; return f; },
			createComment: function(text) { return {nodeType: 8, nodeName: '#comment', textContent: text, data: text, nodeValue: text, _parentNode: null, _parentElement: null}; },
			_elCache: {},
			getElementById: function(id) {
				// Return cached or new stub element, Turnstile script expects UI elements
				if (!this._elCache[id]) {
					this._elCache[id] = _mkEl('div', id);
					this._elCache[id].ownerDocument = this;
					this._elCache[id].parentNode = this.body;
					this.body.appendChild(this._elCache[id]);
				}
				return this._elCache[id];
			},
			getElementsByTagName: function(t) { var r; if (t === "head") r = [this.head]; else if (t === "body") r = [this.body]; else r = []; Object.setPrototypeOf(r, HTMLCollection.prototype); return r; },
			getElementsByClassName: function() { var r = []; Object.setPrototypeOf(r, HTMLCollection.prototype); return r; },
			querySelector: function(sel) {
				if (sel === "head") return this.head;
				if (sel === "body") return this.body;
				// Return a stub element for any selector, Turnstile expects iframe UI elements
				var key = 'qs:' + sel;
				if (!this._elCache[key]) {
					this._elCache[key] = _mkEl('div');
					this._elCache[key].ownerDocument = this;
					this._elCache[key].parentNode = this.body;
					this.body.appendChild(this._elCache[key]);
				}
				return this._elCache[key];
			},
			querySelectorAll: function(sel) {
				// Return array with one stub element
				var el = this.querySelector(sel);
				return el ? [el] : [];
			},
			hasFocus: function() { return false; },
			addEventListener: function(ev, fn) {
				if (!this[_SYM_EVT][ev]) this[_SYM_EVT][ev] = [];
				this[_SYM_EVT][ev].push(fn);
				// DOMContentLoaded/load fire immediately since we are "complete"
				if (ev === "DOMContentLoaded" || ev === "readystatechange" || ev === "load") {
					setTimeout(function() { try { fn(new Event(ev)); } catch(e) {} }, 0);
				}
				// Trigger behavioral events when the click listener registers
				// (the 7th and last behavioral listener). This ensures ALL
				// handlers are in place before we dispatch events.
				if (false && ev === "click" && !this[_SYM_BEHAV] && this[_SYM_EVT]['mousemove']) {
					this[_SYM_BEHAV] = true;
					var doc = this;
					var iWin = el.contentWindow;
					console.log('[BEHAV] All 7 behavioral listeners registered - dispatching events NOW');

					function mkMouse(type, x, y) {
						return {
							type: type, clientX: x, clientY: y,
							screenX: x + 50, screenY: y + 150,
							pageX: x, pageY: y, offsetX: x, offsetY: y,
							movementX: 0, movementY: 0,
							button: 0, buttons: 0,
							which: (type === 'click') ? 1 : 0,
							detail: (type === 'click') ? 1 : 0,
							altKey: false, ctrlKey: false, metaKey: false, shiftKey: false,
							bubbles: true, cancelable: true, composed: true,
							isTrusted: true, timeStamp: Date.now(),
							target: doc.body, currentTarget: doc,
							preventDefault: function() {},
							stopPropagation: function() {},
							stopImmediatePropagation: function() {}
						};
					}
					function mkPointer(type, x, y) {
						var ev = mkMouse(type, x, y);
						ev.pointerId = 1; ev.pointerType = 'mouse';
						ev.width = 1; ev.height = 1;
						ev.pressure = (type === 'pointermove') ? 0.5 : 0;
						ev.tiltX = 0; ev.tiltY = 0; ev.twist = 0; ev.isPrimary = true;
						return ev;
					}
					function dispatchEv(ev) {
						// Dispatch directly, we're already inside the iframe context
						// (this runs during the challenge script's init, inside _iframeCtxCall)
						doc.dispatchEvent(ev);
						if (iWin && iWin.dispatchEvent) iWin.dispatchEvent(ev);
					}

					// Curved mouse path: top-right → center-left (widget checkbox)
					var steps = 15 + Math.floor(Math.random() * 10);
					var sx = 280 + Math.random() * 40, sy = 10 + Math.random() * 15;
					var ex = 22 + Math.random() * 8, ey = 20 + Math.random() * 6;
					dispatchEv(mkMouse('mouseenter', sx, sy));
					dispatchEv(mkPointer('pointerover', sx, sy));
					for (var s = 0; s <= steps; s++) {
						var t = s / steps;
						var cx = 150 + (Math.random() - 0.5) * 30;
						var cy = 5 + Math.random() * 10;
						var x = Math.round((1-t)*(1-t)*sx + 2*(1-t)*t*cx + t*t*ex + (Math.random()-0.5)*3);
						var y = Math.round((1-t)*(1-t)*sy + 2*(1-t)*t*cy + t*t*ey + (Math.random()-0.5)*2);
						dispatchEv(mkMouse('mousemove', x, y));
						dispatchEv(mkPointer('pointermove', x, y));
					}
					console.log('[BEHAV] Dispatched ' + (steps+1) + ' mousemove + pointermove events');
				}
			},
			removeEventListener: function(ev, fn) {
				if (this[_SYM_EVT][ev]) {
					this[_SYM_EVT][ev] = this[_SYM_EVT][ev].filter(function(f) { return f !== fn; });
				}
			},
			dispatchEvent: function(ev) {
				var type = ev.type || ev;
				var handlers = this[_SYM_EVT][type] || [];
				if (handlers.length > 0 && (type === 'mousemove' || type === 'pointermove' || type === 'pointerover' || type === 'click')) {
					console.log('[IFRAME-DOC] dispatchEvent(' + type + ') to ' + handlers.length + ' handler(s)');
				}
				for (var i = 0; i < handlers.length; i++) {
					try { handlers[i](ev); } catch(e) {
						console.log('[IFRAME-DOC] dispatchEvent(' + type + ') handler[' + i + '] THREW: ' + e.message);
					}
				}
				return true;
			},
			write: function() {},
			writeln: function() {},
			open: function() {},
			close: function() {},
			importNode: function(node) { return node; },
			createRange: function() { return {setStart:function(){},setEnd:function(){},collapse:function(){},selectNode:function(){},selectNodeContents:function(){},createContextualFragment:function(html){return document.createDocumentFragment();}}; },
			createTreeWalker: function() { return {nextNode: function() { return null; }, currentNode: null}; },
			createNodeIterator: function(root, whatToShow, filter) { return { nextNode: function() { return null; }, previousNode: function() { return null; }, referenceNode: root || null, whatToShow: whatToShow || 0xFFFFFFFF, detach: function() {} }; }
		};
		iframeDoc.all = _goCreateDocumentAll();
		iframeDoc[_SYM_EVT] = {};
		iframeDoc[_SYM_BEHAV] = false;
		iframeDoc.doctype = {name: "html", publicId: "", systemId: "", nodeName: "html", nodeType: 10};
		iframeDoc.head.parentNode = iframeDoc.documentElement;
		iframeDoc.body.parentNode = iframeDoc.documentElement;
		iframeDoc.documentElement.appendChild(iframeDoc.head);
		iframeDoc.documentElement.appendChild(iframeDoc.body);
		// Populate iframe head with elements matching Chrome's Turnstile iframe DOM
		// Chrome has 10 head children and 13 total elements
		var _mkIfrEl = function(tag) { var e = _mkEl(tag); e.ownerDocument = iframeDoc; return e; };
		var _ifrMeta1 = _mkIfrEl('meta'); _ifrMeta1.setAttribute('http-equiv', 'Content-Type'); _ifrMeta1.setAttribute('content', 'text/html; charset=UTF-8');
		var _ifrMeta2 = _mkIfrEl('meta'); _ifrMeta2.setAttribute('name', 'robots'); _ifrMeta2.setAttribute('content', 'noindex,nofollow');
		var _ifrTitle = _mkIfrEl('title'); _ifrTitle.textContent = 'Checking your Browser\u2026';
		var _ifrStyle1 = _mkIfrEl('style'); _ifrStyle1.textContent = 'body{margin:0;padding:0}';
		var _ifrStyle2 = _mkIfrEl('style'); _ifrStyle2.textContent = '.cb-lb{display:none}';
		var _ifrLink = _mkIfrEl('link'); _ifrLink.setAttribute('rel', 'stylesheet');
		var _ifrMeta3 = _mkIfrEl('meta'); _ifrMeta3.setAttribute('name', 'viewport'); _ifrMeta3.setAttribute('content', 'width=device-width, initial-scale=1');
		iframeDoc.head.appendChild(_ifrMeta1);
		iframeDoc.head.appendChild(_ifrMeta2);
		iframeDoc.head.appendChild(_ifrTitle);
		iframeDoc.head.appendChild(_ifrStyle1);
		iframeDoc.head.appendChild(_ifrStyle2);
		iframeDoc.head.appendChild(_ifrLink);
		iframeDoc.head.appendChild(_ifrMeta3);
		// URL, documentURI, baseURI must be dynamically resolved from location.
		// The inner iframe's VM uses document.URL to construct POST URLs.
		Object.defineProperty(iframeDoc, 'URL', {
			get: function() { return (this.location && this.location.href) || ''; },
			configurable: true
		});
		Object.defineProperty(iframeDoc, 'documentURI', {
			get: function() { return (this.location && this.location.href) || ''; },
			configurable: true
		});
		Object.defineProperty(iframeDoc, 'baseURI', {
			get: function() { return (this.location && this.location.href) || ''; },
			configurable: true
		});
		// Set iframe document prototype chain: iframeDoc → iframeDocProto → HTMLDocument.prototype
		// Then migrate all own properties so getOwnPropertyNames returns ~0 (like real Chrome)
		var iframeDocProto = Object.create(HTMLDocument.prototype);
		Object.setPrototypeOf(iframeDoc, iframeDocProto);
		if (typeof _m2p === 'function') {
			_m2p(iframeDoc, iframeDocProto);
		}
		el.contentDocument = iframeDoc;
		// Capture the main window reference before _iframeCtxCall can swap globals.
		var _mainWindow = window;
		el.contentWindow = {
			document: iframeDoc,
			location: location,
			navigator: navigator,
			parent: {
				postMessage: function(data, origin) {
					var _pmJson = JSON.stringify(data);
					console.log('[IFRAME-MSG] postMessage to parent: ' + _pmJson.substring(0, 300));
					if (data && data.md) console.log('[IFRAME-MSG] md field (' + data.md.length + ' chars): ' + data.md.substring(0, 500));
					var _dispatchData = data;
					// Dispatch to main window's message listeners.
					// IMPORTANT: use _mainWindow (captured) not window (may be swapped during _iframeCtxCall).
					var ev = new Event('message');
					ev.data = _dispatchData;
					ev.origin = 'https://challenges.cloudflare.com';
					ev.source = el.contentWindow;
					ev.ports = [];
					if (_mainWindow[_sEL] && _mainWindow[_sEL]['message']) {
						var handlers = _mainWindow[_sEL]['message'].slice();
						for (var mi = 0; mi < handlers.length; mi++) {
							try { handlers[mi](ev); } catch(e) { console.log('[IFRAME-MSG] handler error: ' + e.message); }
						}
					}
				},
				location: location
			},
			top: window,
			self: null,
			postMessage: function(data, origin) {
				// Log all keys including undefined to detect missing fields
				var allKeys = data && typeof data === 'object' ? Object.keys(data).map(function(k) { return k + '=' + (data[k] === undefined ? 'UNDEF' : typeof data[k] === 'string' ? '"' + data[k].substring(0, 30) + '"' : String(data[k]).substring(0, 30)); }).join(', ') : '';
				console.log('[IFRAME] received postMessage: {' + allKeys + '}');
				// Suppress forceFail from parent, our VM is slow but will complete.
				// The parent's iteration-count-based watchdog fires this too early.
				if (data && data.event === 'forceFail') {
					console.log('[IFRAME] SUPPRESSED forceFail - VM still computing');
					return;
				}
				// Dispatch to iframe's own message listeners
				if (el.contentWindow[_SYM_EVT] && el.contentWindow[_SYM_EVT]['message']) {
					var ev = new Event('message');
					ev.data = data;
					ev.origin = location.origin;
					ev.source = window;
					ev.ports = [];
					var handlers = el.contentWindow[_SYM_EVT]['message'].slice();
					for (var mi = 0; mi < handlers.length; mi++) {
						try {
							var handler = handlers[mi];
							// Wrap in iframe context if available, the handlers
							// were registered by the iframe script and need iframe globals.
							if (el.contentWindow[_SYM_CTX]) {
								(function(h, e) { el.contentWindow[_SYM_CTX](function() { h(e); }); })(handler, ev);
							} else {
								handler(ev);
							}
						} catch(e) {
							console.log('[IFRAME] msg handler #' + mi + ' error: ' + e.message);
							if (e.stack) console.log('[IFRAME] stack: ' + e.stack.split('\n').slice(0, 4).join(' <- '));
						}
					}
				}
			},
			addEventListener: function(type, fn) {
				if (!this[_SYM_EVT][type]) this[_SYM_EVT][type] = [];
				this[_SYM_EVT][type].push(fn);
				console.log('[IFRAME] addEventListener(' + type + ')');
			},
			removeEventListener: function(type, fn) {
				if (this[_SYM_EVT][type]) {
					this[_SYM_EVT][type] = this[_SYM_EVT][type].filter(function(f) { return f !== fn; });
				}
			},
			dispatchEvent: function(ev) {
				var type = ev.type || ev;
				var handlers = (this[_SYM_EVT][type] || []).slice();
				for (var i = 0; i < handlers.length; i++) {
					try {
						if (this[_SYM_CTX]) {
							(function(h, e) { el.contentWindow[_SYM_CTX](function() { h(e); }); })(handlers[i], ev);
						} else {
							handlers[i](ev);
						}
					} catch(e) {}
				}
				return true;
			},
			// Standard global functions that scripts expect on any window object
			eval: eval,
			parseInt: parseInt,
			parseFloat: parseFloat,
			isNaN: isNaN,
			isFinite: isFinite,
			encodeURIComponent: encodeURIComponent,
			decodeURIComponent: decodeURIComponent,
			encodeURI: encodeURI,
			decodeURI: decodeURI,
			atob: atob,
			btoa: btoa,
			setTimeout: setTimeout,
			setInterval: setInterval,
			clearTimeout: clearTimeout,
			clearInterval: clearInterval,
			fetch: (typeof fetch !== 'undefined' ? fetch : undefined),
			console: console,
			JSON: JSON,
			Math: Math,
			Date: Date,
			Array: Array,
			Object: Object,
			String: String,
			Number: Number,
			Boolean: Boolean,
			RegExp: RegExp,
			Error: Error,
			TypeError: TypeError,
			RangeError: RangeError,
			Map: Map,
			Set: Set,
			WeakMap: WeakMap,
			WeakSet: WeakSet,
			Promise: Promise,
			Symbol: Symbol,
			Proxy: Proxy,
			Reflect: Reflect,
			ArrayBuffer: ArrayBuffer,
			Uint8Array: Uint8Array,
			Int8Array: Int8Array,
			Uint16Array: Uint16Array,
			Int16Array: Int16Array,
			Uint32Array: Uint32Array,
			Int32Array: Int32Array,
			Float32Array: Float32Array,
			Float64Array: Float64Array,
			DataView: DataView,
			BigInt: BigInt,
			crypto: crypto,
			performance: performance,
			screen: screen,
			innerWidth: 300,
			innerHeight: 65,
			outerWidth: 1922,
			outerHeight: 854,
			devicePixelRatio: 2,
			XMLHttpRequest: (typeof XMLHttpRequest !== 'undefined' ? XMLHttpRequest : undefined),
			Worker: (typeof Worker !== 'undefined' ? Worker : undefined),
			ReadableStream: (typeof ReadableStream !== 'undefined' ? ReadableStream : undefined),
			MutationObserver: (typeof MutationObserver !== 'undefined' ? MutationObserver : undefined),
			requestAnimationFrame: (typeof requestAnimationFrame !== 'undefined' ? requestAnimationFrame : undefined),
			cancelAnimationFrame: (typeof cancelAnimationFrame !== 'undefined' ? cancelAnimationFrame : undefined),
			// Web APIs needed by Turnstile iframe script
			TextEncoder: (typeof TextEncoder !== 'undefined' ? TextEncoder : window.TextEncoder),
			TextDecoder: (typeof TextDecoder !== 'undefined' ? TextDecoder : window.TextDecoder),
			URL: (typeof URL !== 'undefined' ? URL : window.URL),
			URLSearchParams: (typeof URLSearchParams !== 'undefined' ? URLSearchParams : window.URLSearchParams),
			Blob: (typeof Blob !== 'undefined' ? Blob : window.Blob),
			Event: (typeof Event !== 'undefined' ? Event : window.Event),
			CustomEvent: (typeof CustomEvent !== 'undefined' ? CustomEvent : window.CustomEvent),
			MessageChannel: (typeof MessageChannel !== 'undefined' ? MessageChannel : window.MessageChannel),
			MessagePort: (typeof MessagePort !== 'undefined' ? MessagePort : window.MessagePort),
			queueMicrotask: (typeof queueMicrotask !== 'undefined' ? queueMicrotask : window.queueMicrotask),
			structuredClone: (typeof structuredClone !== 'undefined' ? structuredClone : window.structuredClone),
			ShadowRoot: (typeof ShadowRoot !== 'undefined' ? ShadowRoot : window.ShadowRoot),
			DOMParser: (typeof DOMParser !== 'undefined' ? DOMParser : window.DOMParser),
			AbortController: (typeof AbortController !== 'undefined' ? AbortController : window.AbortController),
			AbortSignal: (typeof AbortSignal !== 'undefined' ? AbortSignal : window.AbortSignal),
			Headers: (typeof Headers !== 'undefined' ? Headers : window.Headers),
			Request: (typeof Request !== 'undefined' ? Request : window.Request),
			Response: (typeof Response !== 'undefined' ? Response : window.Response),
			FormData: (typeof FormData !== 'undefined' ? FormData : window.FormData),
			Function: Function,
			WeakRef: (typeof WeakRef !== 'undefined' ? WeakRef : undefined),
			FinalizationRegistry: (typeof FinalizationRegistry !== 'undefined' ? FinalizationRegistry : undefined),
			Uint8ClampedArray: (typeof Uint8ClampedArray !== 'undefined' ? Uint8ClampedArray : undefined),
			Image: (typeof Image !== 'undefined' ? Image : function() { return _mkEl('img'); }),
			PerformanceObserver: window.PerformanceObserver,
			NodeFilter: window.NodeFilter,
			DOMException: (typeof DOMException !== 'undefined' ? DOMException : window.DOMException),
			// DOM constructors, needed for prototype checks (bot fingerprinting)
			Document: window.Document,
			HTMLDocument: window.HTMLDocument,
			HTMLElement: window.HTMLElement,
			Element: window.Element,
			Node: window.Node,
			EventTarget: window.EventTarget,
			DocumentFragment: window.DocumentFragment,
			ShadowRoot: window.ShadowRoot,
			Comment: window.Comment,
			Text: window.Text,
			CharacterData: window.CharacterData,
			MutationObserver: window.MutationObserver,
			IntersectionObserver: window.IntersectionObserver,
			ResizeObserver: window.ResizeObserver,
			// frameElement: the iframe element that contains this window.
			// Chrome allows access even cross-origin (returns the element).
			frameElement: el,
			// Web APIs that Turnstile accesses on iframe window
			BroadcastChannel: window.BroadcastChannel,
			ReadableStream: window.ReadableStream,
			WritableStream: window.WritableStream,
			TransformStream: window.TransformStream,
			Blob: window.Blob,
			Worker: window.Worker,
			AudioContext: window.AudioContext,
			OfflineAudioContext: window.OfflineAudioContext
		};
		el.contentWindow[_SYM_EVT] = {};
		// Copy V8 built-in globals to iframe window (no Proxy, Proxy objects are detectable)
		var _v8Global = (0, eval)('this');
		var _builtins = [
			'NaN','Infinity','undefined','parseInt','parseFloat','isNaN','isFinite',
			'encodeURI','decodeURI','encodeURIComponent','decodeURIComponent','escape','unescape',
			'Object','Function','Array','String','Number','Boolean','RegExp','Date',
			'Error','TypeError','RangeError','SyntaxError','ReferenceError','URIError','EvalError',
			'Symbol','Map','Set','WeakMap','WeakSet','Promise','Proxy','Reflect',
			'ArrayBuffer','DataView','SharedArrayBuffer',
			'Int8Array','Uint8Array','Uint8ClampedArray','Int16Array','Uint16Array',
			'Int32Array','Uint32Array','Float32Array','Float64Array','BigInt64Array','BigUint64Array',
			'BigInt','Math','JSON','Intl','Atomics','console','queueMicrotask',
			'EventTarget','Node','Element','HTMLElement','Document','HTMLDocument',
			'Window','Navigator','Screen','ShadowRoot'
		];
		for (var bi = 0; bi < _builtins.length; bi++) {
			var bk = _builtins[bi];
			if (el.contentWindow[bk] === undefined && _v8Global[bk] !== undefined) {
				el.contentWindow[bk] = _v8Global[bk];
			}
		}
		// Copy ALL main window properties to iframe window (Chrome API stubs, etc.)
		// This ensures GOPN(window) inside iframe matches real Chrome (~1200+).
		var _mainKeys = Object.getOwnPropertyNames(_mainWindow);
		var _copiedCount = 0;
		for (var ki = 0; ki < _mainKeys.length; ki++) {
			var mk = _mainKeys[ki];
			if (el.contentWindow[mk] === undefined) {
				try { el.contentWindow[mk] = _mainWindow[mk]; _copiedCount++; } catch(e) {}
			}
		}
		el.contentWindow.self = el.contentWindow;
		el.contentWindow.window = el.contentWindow;
		el.contentWindow.globalThis = el.contentWindow;
		// Set Window prototype on iframe window for toString checks
		Object.setPrototypeOf(el.contentWindow, Window.prototype);
		Object.defineProperty(el.contentWindow, Symbol.toStringTag, { value: 'Window', configurable: true });
		// document.defaultView, returns the window object (needed by Turnstile VM)
		iframeDoc.defaultView = el.contentWindow;
		// isSecureContext, true for https: origins
		el.contentWindow.isSecureContext = true;

		// Mark iframe document/window functions as native so toString() returns
		// "[native code]" instead of source code. Without this, the VM detects
		// our fake DOM via Function.prototype.toString checks.
		if (typeof _mkNat === 'function') {
			_mkNat(iframeDoc);
			_mkNat(el.contentWindow);
			_mkNat(el.contentWindow.parent);
		}
	}
	// Track script elements so querySelectorAll("script") can find them
	if ((tag || "").toLowerCase() === "script") {
		_sEls.push(el);
	}
	// Set prototype chain to tag-specific constructor (HTMLDivElement, HTMLBodyElement, etc.)
	// Chrome's createElement returns objects with tag-specific prototypes.
	var __tsTag = _eTM[(tag || '').toLowerCase()] || 'HTMLElement';
	// If a tag-specific constructor exists on window, use its prototype
	var __tagCtor = (typeof window !== 'undefined') ? window[__tsTag] : null;
	if (__tagCtor && __tagCtor.prototype) {
		Object.setPrototypeOf(el, __tagCtor.prototype);
	} else {
		Object.setPrototypeOf(el, HTMLElement.prototype);
	}
	Object.defineProperty(el, Symbol.toStringTag, { value: __tsTag, configurable: true });
	return el;
};

let _mk2DC = function(canvas) {
	var _ops = []; // track drawing operations for fingerprint
	var _fillStyle = "#000000";
	var _strokeStyle = "#000000";
	var _font = "10px sans-serif";
	var _textAlign = "start";
	var _textBaseline = "alphabetic";
	var _globalAlpha = 1;
	var _globalCompositeOperation = "source-over";

	// Simple hash function for operations -> pixel seed
	function __hashOps(ops) {
		var h = 0x811c9dc5;
		var s = ops.join('|');
		for (var i = 0; i < s.length; i++) {
			h ^= s.charCodeAt(i);
			h = Math.imul(h, 0x01000193);
		}
		return h >>> 0;
	}

	// Font-aware character width estimation
	function __charWidth(ch, fontSize) {
		var code = ch.charCodeAt(0);
		// Narrow characters
		if (ch === 'i' || ch === 'l' || ch === '!' || ch === '|' || ch === '.' || ch === ',' || ch === ':' || ch === ';' || ch === "'" || code === 96) return fontSize * 0.28;
		if (ch === 'f' || ch === 'j' || ch === 't' || ch === 'r') return fontSize * 0.35;
		// Wide characters
		if (ch === 'm' || ch === 'w' || ch === 'M' || ch === 'W') return fontSize * 0.75;
		if (ch === '@') return fontSize * 0.85;
		// Space
		if (ch === ' ') return fontSize * 0.28;
		// Uppercase
		if (code >= 65 && code <= 90) return fontSize * 0.6;
		// Digits
		if (code >= 48 && code <= 57) return fontSize * 0.5;
		// Emoji (wide)
		if (code > 127) return fontSize * 0.9;
		// Default lowercase
		return fontSize * 0.48;
	}

	var ctx = {
		get fillStyle() { return _fillStyle; },
		set fillStyle(v) { _fillStyle = v; },
		get strokeStyle() { return _strokeStyle; },
		set strokeStyle(v) { _strokeStyle = v; },
		get font() { return _font; },
		set font(v) { _font = v; },
		get textAlign() { return _textAlign; },
		set textAlign(v) { _textAlign = v; },
		get textBaseline() { return _textBaseline; },
		set textBaseline(v) { _textBaseline = v; },
		get globalAlpha() { return _globalAlpha; },
		set globalAlpha(v) { _globalAlpha = v; },
		get globalCompositeOperation() { return _globalCompositeOperation; },
		set globalCompositeOperation(v) { _globalCompositeOperation = v; },
		imageSmoothingEnabled: true,
		lineWidth: 1,
		lineCap: "butt",
		lineJoin: "miter",
		miterLimit: 10,
		shadowBlur: 0,
		shadowColor: "rgba(0, 0, 0, 0)",
		shadowOffsetX: 0,
		shadowOffsetY: 0,
		lineDashOffset: 0,
		direction: "ltr",
		fontKerning: "auto",
		fontStretch: "normal",
		fontVariantCaps: "normal",
		letterSpacing: "0px",
		wordSpacing: "0px",
		fillRect: function(x, y, w, h) {
			_ops.push('fr:' + x + ':' + y + ':' + w + ':' + h + ':' + _fillStyle);
			console.log('[DOM] canvas.fillRect(' + x + ',' + y + ',' + w + ',' + h + ') fill=' + _fillStyle);
		},
		clearRect: function(x, y, w, h) { _ops.push('cr:' + x + ':' + y + ':' + w + ':' + h); },
		strokeRect: function(x, y, w, h) { _ops.push('sr:' + x + ':' + y + ':' + w + ':' + h); },
		fillText: function(text, x, y, maxWidth) {
			_ops.push('ft:' + text + ':' + x + ':' + y + ':' + _font + ':' + _fillStyle);
			console.log('[DOM] canvas.fillText("' + (text||'').substring(0,50) + '", ' + x + ', ' + y + ') font=' + _font + ' fill=' + _fillStyle);
		},
		strokeText: function(text, x, y) {
			_ops.push('st:' + text + ':' + x + ':' + y + ':' + _font + ':' + _strokeStyle);
		},
		measureText: function(text) {
			// Parse font size from font string (e.g., "14px Arial", "11pt no-real-font-123")
			var fontSize = 10;
			var m = _font.match(/(\d+(?:\.\d+)?)\s*(px|pt|em|rem)/);
			if (m) {
				fontSize = parseFloat(m[1]);
				if (m[2] === 'pt') fontSize *= 1.333; // pt -> px
				if (m[2] === 'em' || m[2] === 'rem') fontSize *= 16;
			}
			// Calculate width character by character
			var width = 0;
			var str = String(text || '');
			for (var i = 0; i < str.length; i++) {
				width += __charWidth(str[i], fontSize);
			}
			// Add slight variation based on font name for font detection
			var fontKey = _font.toLowerCase();
			if (fontKey.indexOf('arial') >= 0) width *= 1.0;
			else if (fontKey.indexOf('times') >= 0) width *= 0.92;
			else if (fontKey.indexOf('courier') >= 0) width *= 1.05;
			else if (fontKey.indexOf('georgia') >= 0) width *= 0.98;
			else if (fontKey.indexOf('verdana') >= 0) width *= 1.08;
			else if (fontKey.indexOf('helvetica') >= 0) width *= 1.0;
			else if (fontKey.indexOf('monospace') >= 0) width *= 1.02;
			else width *= 0.99; // fallback font

			var ascent = fontSize * 0.8;
			var descent = fontSize * 0.2;
			return {
				width: Math.round(width * 100) / 100,
				actualBoundingBoxAscent: Math.round(ascent * 10) / 10,
				actualBoundingBoxDescent: Math.round(descent * 10) / 10,
				actualBoundingBoxLeft: 0,
				actualBoundingBoxRight: Math.round(width * 100) / 100,
				fontBoundingBoxAscent: Math.round(ascent * 1.2 * 10) / 10,
				fontBoundingBoxDescent: Math.round(descent * 1.5 * 10) / 10,
				emHeightAscent: Math.round(ascent * 10) / 10,
				emHeightDescent: Math.round(descent * 10) / 10,
				alphabeticBaseline: 0,
				hangingBaseline: Math.round(ascent * 0.8 * 10) / 10,
				ideographicBaseline: Math.round(-descent * 10) / 10
			};
		},
		beginPath: function(){ _ops.push('bp'); },
		closePath: function(){ _ops.push('cp'); },
		moveTo: function(x,y){ _ops.push('mt:'+x+':'+y); },
		lineTo: function(x,y){ _ops.push('lt:'+x+':'+y); },
		bezierCurveTo: function(){ _ops.push('bct'); },
		quadraticCurveTo: function(){ _ops.push('qct'); },
		arc: function(x,y,r){ _ops.push('arc:'+x+':'+y+':'+r); },
		arcTo: function(){ _ops.push('at'); },
		ellipse: function(){ _ops.push('ell'); },
		rect: function(x,y,w,h){ _ops.push('rect:'+x+':'+y+':'+w+':'+h); },
		fill: function(){ _ops.push('fill'); },
		stroke: function(){ _ops.push('stroke'); },
		clip: function(){},
		isPointInPath: function() { return false; },
		isPointInStroke: function() { return false; },
		save: function(){},
		restore: function(){},
		translate: function(){},
		rotate: function(){},
		scale: function(){},
		transform: function(){},
		setTransform: function(){},
		resetTransform: function(){},
		getTransform: function() { return {a:1,b:0,c:0,d:1,e:0,f:0, is2D:true, isIdentity:true}; },
		createLinearGradient: function() { return {addColorStop: function(){}}; },
		createRadialGradient: function() { return {addColorStop: function(){}}; },
		createConicGradient: function() { return {addColorStop: function(){}}; },
		createPattern: function() { return {}; },
		drawImage: function(){ _ops.push('di'); },
		getImageData: function(x, y, w, h) {
			console.log('[DOM] canvas.getImageData(' + x + ',' + y + ',' + w + ',' + h + ') ops=' + _ops.length);
			var data = new Uint8ClampedArray(w * h * 4);
			// Generate deterministic pixel data based on operations
			var seed = __hashOps(_ops);
			for (var i = 0; i < data.length; i += 4) {
				// Most pixels white (background), some colored (text/shapes)
				seed = (seed * 1103515245 + 12345) & 0x7fffffff;
				if ((seed & 0xff) < 20) {
					// "Rendered" pixel
					data[i] = (seed >> 8) & 0xff;   // R
					data[i+1] = (seed >> 16) & 0xff; // G
					data[i+2] = (seed >> 24) & 0x7f; // B
					data[i+3] = 255;
				} else {
					// White background
					data[i] = 255; data[i+1] = 255; data[i+2] = 255; data[i+3] = 255;
				}
			}
			// Log first 20 bytes of pixel data for fingerprint debugging
			var sample = [];
			for (var j = 0; j < Math.min(20, data.length); j++) sample.push(data[j]);
			console.log('[VMTRACE] getImageData result: ' + w + 'x' + h + ' hash=0x' + __hashOps(_ops).toString(16) + ' first20px=[' + sample.join(',') + '] ops=[' + _ops.slice(0,5).join('; ') + ']');
			return {data: data, width: w, height: h, colorSpace: "srgb"};
		},
		createImageData: function(w, h) { return {data: new Uint8ClampedArray(w * h * 4), width: w, height: h}; },
		putImageData: function(){},
		getLineDash: function() { return []; },
		setLineDash: function(){},
		canvas: canvas || {width: 300, height: 150}
	};
	return ctx;
};

// --- document ---
// The following properties are native V8 accessor properties on Document.prototype
// (created by engine.go setupDocument): URL, documentURI, domain, referrer, cookie,
// title, readyState, characterSet, charset, inputEncoding, contentType, compatMode,
// designMode, dir, hidden, visibilityState, body, head, documentElement.
// They must NOT appear as own properties on the document object, or _m2p would
// overwrite the native getters.
let _ck = {};
// Expose _ck to the native cookie getter via a global (non-enumerable).
Object.defineProperty(globalThis, '_cfDc', { value: _ck, writable: true, configurable: true, enumerable: false });
let _els = {}; // track created elements by ID
let _sEls = []; // track script elements for querySelectorAll("script")
document = {
	nodeType: 9,
	// cookie: native getter on Document.prototype reads _cfDc.
	// Override with proper get/set pair that has closure access to _ck.
	// This own accessor will be migrated to Document.prototype by _m2p,
	// replacing the native cookie getter (which is fine, cookie needs the setter).
	get cookie() {
		var parts = [];
		for (var k in _ck) {
			parts.push(k + "=" + _ck[k]);
		}
		return parts.join("; ");
	},
	set cookie(val) {
		var idx = val.indexOf("=");
		if (idx === -1) return;
		var name = val.substring(0, idx).trim();
		var rest = val.substring(idx + 1);
		var semiIdx = rest.indexOf(";");
		var value = semiIdx !== -1 ? rest.substring(0, semiIdx) : rest;
		_ck[name] = value.trim();
		console.log('[DOM] document.cookie SET: ' + name + '=' + value.trim().substring(0, 40) + (value.trim().length > 40 ? '...' : ''));
	},
	// Static properties removed, now native getters on Document.prototype:
	// readyState, title, referrer, domain, URL, documentURI, characterSet,
	// charset, inputEncoding, contentType, compatMode, designMode, dir,
	// hidden, visibilityState
	doctype: {name: "html", publicId: "", systemId: "", nodeName: "html", nodeType: 10},
	lastModified: (function() { var d = new Date(); return (d.getMonth()+1).toString().padStart(2,'0') + '/' + d.getDate().toString().padStart(2,'0') + '/' + d.getFullYear() + ' ' + d.getHours().toString().padStart(2,'0') + ':' + d.getMinutes().toString().padStart(2,'0') + ':' + d.getSeconds().toString().padStart(2,'0'); })(),
	hasFocus: function() { return false; },
	createElement: function(tag) {
		if (tag.toLowerCase() === 'iframe') console.log('[CE] createElement(iframe) called');
		var el = _mkEl(tag);
		el.ownerDocument = document;
		// Check customElements registry, set prototype so methods are available
		if (tag.indexOf('-') !== -1 && window.customElements) {
			var ctor = window.customElements.get(tag);
			if (ctor && ctor.prototype) {
				try { Object.setPrototypeOf(el, ctor.prototype); } catch(e) {}
				// Try running constructor (works for function constructors, not class constructors)
				try { ctor.call(el); } catch(e) {
					// Class constructors can't be .call()'d, try Reflect.construct
					try {
						var instance = Reflect.construct(ctor, [], ctor);
						// Copy own properties from constructed instance to our element
						var ownKeys = Object.getOwnPropertyNames(instance);
						for (var ki = 0; ki < ownKeys.length; ki++) {
							try { el[ownKeys[ki]] = instance[ownKeys[ki]]; } catch(e2) {}
						}
					} catch(e2) {}
				}
			}
		}
		// NOTE: parentNode starts as null (standard DOM behavior).
		// The parentNode getter in _mkEl falls back to document.body for CF compat,
		// so CF scripts that do el.parentNode.insertBefore() still work.
		// Add img-specific properties (complete, naturalWidth, naturalHeight)
		if (tag.toLowerCase() === "img") {
			el.complete = false;
			el.naturalWidth = 0;
			el.naturalHeight = 0;
			el.width = 0;
			el.height = 0;
			el.crossOrigin = null;
			el.decode = function() { return Promise.resolve(); };
		}
		// HTMLMediaElement properties for video/audio, Kasada calls canPlayType()
		if (tag.toLowerCase() === "video" || tag.toLowerCase() === "audio") {
			el.canPlayType = function(mime) {
				if (!mime) return '';
				mime = mime.toLowerCase();
				if (mime.indexOf('video/mp4') !== -1 || mime.indexOf('audio/mpeg') !== -1 ||
					mime.indexOf('audio/mp4') !== -1 || mime.indexOf('video/webm') !== -1 ||
					mime.indexOf('audio/webm') !== -1 || mime.indexOf('audio/ogg') !== -1 ||
					mime.indexOf('video/ogg') !== -1 || mime.indexOf('audio/wav') !== -1 ||
					mime.indexOf('audio/x-wav') !== -1 || mime.indexOf('audio/flac') !== -1 ||
					mime.indexOf('audio/aac') !== -1) {
					return mime.indexOf('codecs') !== -1 ? 'probably' : 'maybe';
				}
				return '';
			};
			el.play = function() { return Promise.resolve(); };
			el.pause = function() {};
			el.load = function() {};
			el.currentTime = 0;
			el.duration = NaN;
			el.paused = true;
			el.ended = false;
			el.muted = false;
			el.volume = 1;
			el.playbackRate = 1;
			el.defaultPlaybackRate = 1;
			el.readyState = 0;
			el.networkState = 0;
			el.error = null;
			el.preload = 'auto';
			el.autoplay = false;
			el.loop = false;
			el.controls = false;
			el.crossOrigin = null;
			el.buffered = { length: 0, start: function() { return 0; }, end: function() { return 0; } };
			el.seekable = { length: 0, start: function() { return 0; }, end: function() { return 0; } };
			el.played = { length: 0, start: function() { return 0; }, end: function() { return 0; } };
			el.textTracks = { length: 0 };
			el.audioTracks = { length: 0 };
			el.videoTracks = { length: 0 };
			el.addTextTrack = function() { return {}; };
			el.captureStream = function() { return { getTracks: function() { return []; }, getAudioTracks: function() { return []; }, getVideoTracks: function() { return []; } }; };
		}
		// <template> element, content is a DocumentFragment
		if (tag.toLowerCase() === "template") {
			var frag = _mkEl("fragment");
			frag.nodeType = 11;
			Object.defineProperty(el, 'content', {
				get: function() { return frag; },
				configurable: true
			});
			// Override innerHTML to populate the fragment instead
			var _origInnerHTMLDesc = Object.getOwnPropertyDescriptor(el, 'innerHTML');
			Object.defineProperty(el, 'innerHTML', {
				get: function() { return _domGetInnerHTML ? _domGetInnerHTML(frag) : ''; },
				set: function(v) {
					if (typeof _domSetInnerHTML === 'function') _domSetInnerHTML(frag, String(v || ''));
				},
				configurable: true
			});
		}
		// Fire onload for script/img/link elements asynchronously and log URL
		if (tag.toLowerCase() === "script" || tag.toLowerCase() === "img" || tag.toLowerCase() === "link") {
			var _src = "";
			Object.defineProperty(el, "src", {
				get: function() { return _src; },
				set: function(v) {
					_src = v;
					console.log('[DOM] ' + tag + '.src = ' + v);
					var self = this;
					// For img elements: fetch the URL to populate complete/naturalWidth
					// and fire onload, Turnstile checks img.complete and encodedBodySize
					if (tag.toLowerCase() === "img" && v) {
						self.complete = false;
						setTimeout(function() {
							try {
								var result = JSON.parse(_goSyncFetch(v, 'GET', '', '{}'));
								if (result.status >= 200 && result.status < 400) {
									self.complete = true;
									self.naturalWidth = 1;
									self.naturalHeight = 1;
									self.width = 1;
									self.height = 1;
									console.log('[DOM] img loaded: ' + v + ' (' + (result.body ? result.body.length : 0) + ' bytes)');
								} else {
									self.complete = true;
									self.naturalWidth = 0;
									self.naturalHeight = 0;
									console.log('[DOM] img load failed: ' + v + ' status=' + result.status);
								}
							} catch(e) {
								self.complete = true;
								console.log('[DOM] img fetch error: ' + v + ' ' + e.message);
							}
							if (self.onload) self.onload();
						}, 0);
					}
					// For turnstile scripts: fetch and execute the real Turnstile API
					else if (tag.toLowerCase() === "script" && v.indexOf('/turnstile/') !== -1) {
						var scriptUrl = v;
						var scriptEl = self; // the script element being created
						// Add this script element to document.head so Turnstile can find itself
						scriptEl._src = v;
						scriptEl.getAttribute = function(k) {
							if (k === 'src') return scriptEl._src;
							if (k === 'nonce') return '';
							return null;
						};
						scriptEl.hasAttribute = function(k) { return k === 'src'; };
						document.head.appendChild(scriptEl);
						setTimeout(function() {
							console.log('[DOM] Fetching real Turnstile script: ' + scriptUrl);
							try {
								var result = JSON.parse(_goSyncFetch(scriptUrl, 'GET', '', '{}'));
								if (result.status === 200 && result.body) {
									console.log('[DOM] Turnstile script fetched: ' + result.body.length + ' chars');
									try {
										// Remove the stub so the real Turnstile can initialize properly
										var savedStub = window.turnstile;
										delete window.turnstile;
										// Set document.currentScript so Turnstile Le() finds its script tag
										document.currentScript = scriptEl;
										eval(result.body);
										document.currentScript = null;
										console.log('[DOM] Turnstile script executed OK, turnstile=' + typeof window.turnstile);
										// If the real Turnstile failed to set itself up, restore our stub
										if (!window.turnstile || typeof window.turnstile.render !== 'function') {
											console.log('[DOM] Real Turnstile did not initialize, restoring stub');
											window.turnstile = savedStub;
										} else {
											// Wrap real turnstile.render with logging and iframe interception
											var __realRender = window.turnstile.render.bind(window.turnstile);
											window.turnstile.render = function(container, options) {
												console.log('[REAL-TS] render called: container=' + String(container) + ' sitekey=' + (options && options.sitekey) + ' action=' + (options && options.action));
												console.log('[REAL-TS] options: ' + JSON.stringify(options || {}).substring(0, 300));
												var result = __realRender(container, options);
												console.log('[REAL-TS] render returned: ' + String(result));
												return result;
											};
										}
									} catch(e) {
										document.currentScript = null;
										console.log('[DOM] Turnstile script execution error: ' + e.message);
										console.log('[DOM] Turnstile error stack: ' + (e.stack || '').split('\n').slice(0, 5).join(' <- '));
										// Restore stub on error
										if (savedStub) window.turnstile = savedStub;
									}
								} else {
									console.log('[DOM] Turnstile script fetch failed: status=' + result.status);
								}
							} catch(e) {
								console.log('[DOM] Turnstile fetch error: ' + e.message);
							}
							if (self.onload) self.onload();
						}, 50);
					} else {
						setTimeout(function() { if (self.onload) self.onload(); }, 0);
					}
				},
				configurable: true
			});
		}
		return el;
	},
	createElementNS: function(ns, tag) { return this.createElement(tag); },
	createTextNode: function(text) { return {nodeType: 3, nodeName: '#text', textContent: text, data: text, nodeValue: text, _parentNode: null, _parentElement: null}; },
	createDocumentFragment: function() {
		var frag = _mkEl("fragment");
		frag.nodeType = 11;
		return frag;
	},
	createComment: function(text) { return {nodeType: 8, nodeName: '#comment', textContent: text, data: text, nodeValue: text, _parentNode: null, _parentElement: null}; },
	createTreeWalker: function(root, whatToShow, filter) {
		whatToShow = whatToShow || 0xFFFFFFFF;
		var walker = {
			root: root,
			currentNode: root,
			whatToShow: whatToShow,
			filter: filter || null,
			_accept: function(node) {
				if (!node) return false;
				var show = false;
				if (whatToShow === 0xFFFFFFFF) show = true;
				else if ((whatToShow & 1) && node.nodeType === 1) show = true;
				else if ((whatToShow & 4) && node.nodeType === 3) show = true;
				else if ((whatToShow & 128) && node.nodeType === 8) show = true;
				if (!show) return false;
				if (filter && typeof filter.acceptNode === 'function') return filter.acceptNode(node) === 1;
				return true;
			},
			nextNode: function() {
				var node = this.currentNode;
				// Try first child
				if (node.childNodes && node.childNodes.length > 0) {
					for (var i = 0; i < node.childNodes.length; i++) {
						if (this._accept(node.childNodes[i])) { this.currentNode = node.childNodes[i]; return this.currentNode; }
						// Even if not accepted, try its children
						var saved = this.currentNode;
						this.currentNode = node.childNodes[i];
						var next = this.nextNode();
						if (next) return next;
						this.currentNode = saved;
					}
				}
				// Try siblings and parent's siblings
				var cur = node;
				while (cur && cur !== this.root) {
					var parent = cur._parentNode || cur.parentNode;
					if (!parent || !parent.childNodes) break;
					var idx = parent.childNodes.indexOf(cur);
					for (var j = idx + 1; j < parent.childNodes.length; j++) {
						if (this._accept(parent.childNodes[j])) { this.currentNode = parent.childNodes[j]; return this.currentNode; }
						var saved2 = this.currentNode;
						this.currentNode = parent.childNodes[j];
						var next2 = this.nextNode();
						if (next2) return next2;
						this.currentNode = saved2;
					}
					cur = parent;
				}
				return null;
			},
			previousNode: function() { return null; },
			firstChild: function() { if (this.currentNode.childNodes) { for (var i=0;i<this.currentNode.childNodes.length;i++) { if (this._accept(this.currentNode.childNodes[i])) { this.currentNode = this.currentNode.childNodes[i]; return this.currentNode; } } } return null; },
			parentNode: function() { var p = this.currentNode._parentNode || this.currentNode.parentNode; if (p && p !== this.root && this._accept(p)) { this.currentNode = p; return p; } return null; }
		};
		return walker;
	},
	createNodeIterator: function(root, whatToShow, filter) {
		return { nextNode: function() { return null; }, previousNode: function() { return null; }, referenceNode: root || null, whatToShow: whatToShow || 0xFFFFFFFF, pointerBeforeReferenceNode: true, detach: function() {} };
	},
	createRange: function() {
		return {
			startContainer: null, startOffset: 0, endContainer: null, endOffset: 0, collapsed: true, commonAncestorContainer: null,
			setStart: function(node, offset) { this.startContainer = node; this.startOffset = offset; this.collapsed = false; },
			setEnd: function(node, offset) { this.endContainer = node; this.endOffset = offset; },
			collapse: function(toStart) { if (toStart) { this.endContainer = this.startContainer; this.endOffset = this.startOffset; } else { this.startContainer = this.endContainer; this.startOffset = this.endOffset; } this.collapsed = true; },
			selectNode: function(node) { this.startContainer = node._parentNode; this.endContainer = node._parentNode; },
			selectNodeContents: function(node) { this.startContainer = node; this.startOffset = 0; this.endContainer = node; this.endOffset = node.childNodes ? node.childNodes.length : 0; },
			cloneContents: function() { var frag = document.createDocumentFragment(); return frag; },
			deleteContents: function() {},
			extractContents: function() { return document.createDocumentFragment(); },
			insertNode: function(node) { if (this.startContainer) { try { _domInsertBefore(this.startContainer, node, this.startContainer.childNodes ? this.startContainer.childNodes[this.startOffset] : null); } catch(e) {} } },
			cloneRange: function() { var r = document.createRange(); r.startContainer = this.startContainer; r.startOffset = this.startOffset; r.endContainer = this.endContainer; r.endOffset = this.endOffset; r.collapsed = this.collapsed; return r; },
			createContextualFragment: function(html) { var frag = document.createDocumentFragment(); var tmp = _mkEl('div'); if (typeof _domSetInnerHTML === 'function') _domSetInnerHTML(tmp, html); while (tmp.childNodes && tmp.childNodes.length > 0) _domAppendChild(frag, tmp.childNodes[0]); return frag; },
			getBoundingClientRect: function() { return {x:0,y:0,width:0,height:0,top:0,right:0,bottom:0,left:0}; },
			getClientRects: function() { return []; },
			toString: function() { return ''; }
		};
	},
	getElementById: function(id) {
		// Search the real DOM tree
		var found = _domQuerySelector(document.documentElement || document, '#' + id);
		if (found) return found;
		// Check the element cache (CF challenge compatibility)
		if (_els[id]) return _els[id];
		// Standard DOM behavior: return null if not found
		return null;
	},
	getElementsByTagName: function(tag) {
		tag = tag.toLowerCase();
		// Fast paths for structural elements
		if (tag === "head") { var r = [document.head]; Object.setPrototypeOf(r, HTMLCollection.prototype); return r; }
		if (tag === "body") { var r = [document.body]; Object.setPrototypeOf(r, HTMLCollection.prototype); return r; }
		if (tag === "html") { var r = [document.documentElement]; Object.setPrototypeOf(r, HTMLCollection.prototype); return r; }
		// Search real DOM tree
		var r = _domQuerySelectorAll(document.documentElement || document, tag === "*" ? "*" : tag);
		// Also include script tracking
		if (tag === "script") {
			for (var i = 0; i < _sEls.length; i++) {
				if (r.indexOf(_sEls[i]) === -1) r.push(_sEls[i]);
			}
		}
		Object.setPrototypeOf(r, HTMLCollection.prototype);
		return r;
	},
	getElementsByClassName: function(cls) {
		var r = _domQuerySelectorAll(document.documentElement || document, '.' + cls.split(/\s+/).join('.'));
		Object.setPrototypeOf(r, HTMLCollection.prototype);
		return r;
	},
	getElementsByName: function(name) {
		return _domQuerySelectorAll(document.documentElement || document, '[name="' + name + '"]');
	},
	querySelector: function(sel) {
		// Fast paths
		if (sel === "head") return document.head;
		if (sel === "body") return document.body;
		if (sel === "html") return document.documentElement;
		// Search real DOM tree, return null if not found (standard DOM behavior)
		return _domQuerySelector(document.documentElement || document, sel);
	},
	querySelectorAll: function(sel) {
		// Search real DOM tree
		var results = _domQuerySelectorAll(document.documentElement || document, sel);
		// Also include tracked scripts
		if (sel === "script" || sel === "script[src]") {
			for (var i = 0; i < _sEls.length; i++) {
				if (results.indexOf(_sEls[i]) === -1) {
					if (sel === "script[src]" && (!_sEls[i].src || _sEls[i].src === "")) continue;
					results.push(_sEls[i]);
				}
			}
		}
		return results;
	},
	// head, body, documentElement: native getters on Document.prototype read
	// _cfDh, _cfDb, _cfDe globals. No own properties here.
	addEventListener: function(ev, fn) {
		var _s = Symbol.for('docEvtLs');
		if (!this[_s]) this[_s] = {};
		if (!this[_s][ev]) this[_s][ev] = [];
		this[_s][ev].push(fn);
		if (ev === "DOMContentLoaded" || ev === "readystatechange") {
			setTimeout(function() { fn(new Event(ev)); }, 0);
		}
	},
	removeEventListener: function(ev, fn) {
		var _s = Symbol.for('docEvtLs');
		if (this[_s] && this[_s][ev]) {
			this[_s][ev] = this[_s][ev].filter(function(f) { return f !== fn; });
		}
	},
	dispatchEvent: function(ev) {
		var type = ev.type || ev;
		// Check both storage locations: Symbol.for('docEvtLs') (legacy) and _sEL (element prototype)
		var _s = Symbol.for('docEvtLs');
		var handlers = (this[_s] && this[_s][type]) || [];
		for (var i = 0; i < handlers.length; i++) {
			try { handlers[i](ev); } catch(e) {}
		}
		// Also dispatch to handlers stored via addEventListener (element prototype path)
		var elHandlers = this[_sEL] && this[_sEL][type];
		if (elHandlers) {
			for (var j = 0; j < elHandlers.length; j++) {
				try {
					var h = elHandlers[j];
					var fn = typeof h === 'function' ? h : h.fn;
					if (fn) fn.call(this, ev);
				} catch(e) {}
			}
		}
		return true;
	},
	createEvent: function(type) { return {initEvent: function(){}, type: "", bubbles: false, cancelable: false}; },
	importNode: function(node) { return node; },
	adoptNode: function(node) { return node; },
	open: function() {},
	close: function() {},
	write: function() {},
	writeln: function() {}
};

// DOM collections, used by Turnstile fingerprinting
Object.defineProperty(document, 'scripts', {
	get: function() { return _sEls.slice(); },
	configurable: true
});
document.styleSheets = (function() {
	// Fake stylesheets, Chrome typically has several. Each must have .type, .href, .cssRules.
	var _iterFn = function() { var idx=0, self=this; return {next:function(){return idx<self.length?{value:self[idx],done:false,idx:idx++}:{done:true}}}; };
	var mkSheet = function(href) {
		var rules = {length: 0, item: function(){return null;}};
		rules[Symbol.iterator] = _iterFn;
		var media = {length: 0, mediaText: '', item: function(){return null;}};
		media[Symbol.iterator] = _iterFn;
		return {
			type: 'text/css', href: href, ownerNode: null, disabled: false,
			media: media,
			cssRules: rules,
			rules: rules,
			insertRule: function(){return 0;}, deleteRule: function(){},
			addRule: function(){return -1;}, removeRule: function(){}
		};
	};
	var ss = [mkSheet(null), mkSheet(null), mkSheet(null), mkSheet(null), mkSheet(null), mkSheet(null), mkSheet(null), mkSheet(null)];
	ss.item = function(i) { return ss[i] || null; };
	return ss;
})();
var _emptyIterFn = function(){return{next:function(){return{done:true}}}};
document.images = {length: 0, item: function(i){return null;}, namedItem: function(){return null;}, [Symbol.iterator]: _emptyIterFn};
document.forms = {length: 0, item: function(i){return null;}, namedItem: function(){return null;}, [Symbol.iterator]: _emptyIterFn};
document.links = {length: 0, item: function(i){return null;}, namedItem: function(){return null;}, [Symbol.iterator]: _emptyIterFn};
document.embeds = {length: 0, item: function(i){return null;}, namedItem: function(){return null;}, [Symbol.iterator]: _emptyIterFn};
document.plugins = {length: 0, item: function(i){return null;}, namedItem: function(){return null;}};
document.scripts = {length: 0, item: function(i){return null;}, namedItem: function(){return null;}};
// document.all set from Go with MarkAsUndetectable (typeof === "undefined")
document.anchors = {length: 0, item: function(i){return null;}, namedItem: function(){return null;}};

// Set document.location to the same location object as window.location
document.location = location;
// document.defaultView, returns the window object
document.defaultView = window;

// Create proper head/body/documentElement via globals that native getters read.
// In Chrome, document has NO own body/head/documentElement, they're prototype getters.
// The native getters on Document.prototype (from engine.go setupDocument) read
// _cfDh, _cfDb, _cfDe globals, so we set those here.
var _cfDh = _mkEl("head");
_cfDh.ownerDocument = document;
var _cfDb = _mkEl("body");
_cfDb.ownerDocument = document;
_cfDb.clientWidth = 1496;
_cfDb.clientHeight = 0;
_cfDb.offsetWidth = 1512;
_cfDb.offsetHeight = 0;
var _cfDe = _mkEl("html");
_cfDe.ownerDocument = document;
_cfDe.clientWidth = 1512;
_cfDe.clientHeight = 774;
_cfDe.lang = "en";
_cfDe.appendChild(_cfDh);
_cfDe.appendChild(_cfDb);

// Wire document's prototype chain EARLY so document.body/head/documentElement
// work via the native getters on Document.prototype immediately. The native
// getters read _cfDb, _cfDh, _cfDe globals we just set.
// This is done again at the canonical spot (~line 4625) but is harmless to repeat.
Object.setPrototypeOf(document, HTMLDocument.prototype);

// Pre-populate body with the challenge page's DOM structure.
// CF's orchestrator script expects these elements to exist.
(function() {
	var mainWrapper = _mkEl("div");
	mainWrapper.className = "main-wrapper";
	mainWrapper.id = "main-wrapper";
	mainWrapper.ownerDocument = document;

	var mainContent = _mkEl("div");
	mainContent.className = "main-content";
	mainContent.ownerDocument = document;
	mainWrapper.appendChild(mainContent);

	// The challenge container where CF injects the turnstile widget
	var challengeStage = _mkEl("div");
	challengeStage.id = "challenge-stage";
	challengeStage.ownerDocument = document;
	mainContent.appendChild(challengeStage);

	// Challenge form element
	var challengeForm = _mkEl("div");
	challengeForm.id = "challenge-form";
	challengeForm.ownerDocument = document;
	challengeStage.appendChild(challengeForm);

	document.body.appendChild(mainWrapper);

	// Also create #cf-chl-widget-* container that the script might look for
	var widgetContainer = _mkEl("div");
	widgetContainer.id = "cf-chl-widget";
	widgetContainer.ownerDocument = document;
	challengeStage.appendChild(widgetContainer);
})();

// --- window ---
let _nid = 0;
var self = this;

var window = self;
// Chrome exposes window properties as GETTERS, not data properties.
// Object.getOwnPropertyDescriptor(window, 'document') returns {get:fn, ...} not {value:obj}
// The VM checks this descriptor shape.
let _wDoc = document, _wNav = navigator, _wLoc = location;
var _wParent = window, _wTop = window, _wSelf = window;
var _wName = "", _wClosed = false, _wOpener = null, _wFrameElement = null;
var _wFrames = [], _wLength = 0;
// document, navigator, screen are set natively by engine.go, don't redefine
// But convert location, clientInformation to getters
try { Object.defineProperty(window, 'document', { get: function() { return _wDoc; }, enumerable: true, configurable: false }); } catch(e) {}
try { Object.defineProperty(window, 'navigator', { get: function() { return _wNav; }, enumerable: true, configurable: true }); } catch(e) {}
Object.defineProperty(window, 'clientInformation', { get: function() { return _wNav; }, enumerable: true, configurable: true });
try { Object.defineProperty(window, 'location', { get: function() { return _wLoc; }, set: function(v) { _wLoc = v; }, enumerable: true, configurable: false }); } catch(e) {}
// window.screen is set natively by engine.go setupScreen()
// Wrap all in try-catch, some may already be defined by engine.go
var _winProps = [
    ['self', function() { return window; }, null, true],
    ['parent', function() { return _wParent; }, function(v) { _wParent = v; }, true],
    ['top', function() { return _wTop; }, function(v) { _wTop = v; }, false],
    ['frameElement', function() { return _wFrameElement; }, function(v) { _wFrameElement = v; }, true],
    ['frames', function() { return _wFrames; }, function(v) { _wFrames = v; }, true],
    ['name', function() { return _wName; }, function(v) { _wName = v; }, true],
    ['closed', function() { return _wClosed; }, null, true],
    ['opener', function() { return _wOpener; }, null, true],
    ['length', function() { return _wLength; }, function(v) { _wLength = v; }, true]
];
for (var _wi = 0; _wi < _winProps.length; _wi++) {
    try {
        var _wp = _winProps[_wi];
        var _desc = { get: _wp[1], enumerable: true, configurable: _wp[3] };
        if (_wp[2]) _desc.set = _wp[2];
        Object.defineProperty(window, _wp[0], _desc);
    } catch(e) {}
}

// Window dimensions, match Chrome's /fp page (full browser window, not iframe).
// The /fp page is a top-level page, not a Turnstile iframe.
window.innerWidth = 1512;
window.innerHeight = 774;
window.outerWidth = 1512;
window.outerHeight = 861;
window.devicePixelRatio = 2;
window.scrollX = 0;
window.scrollY = 0;
window.pageXOffset = 0;
window.pageYOffset = 0;

window.isSecureContext = true;
window.origin = %q;
window.crossOriginIsolated = false;

// --- Window instance objects (stub objects) ---
window.scheduler = { postTask: function(fn) { return Promise.resolve(fn()); }, yield: function() { return Promise.resolve(); } };
window.navigation = { currentEntry: null, entries: function() { return []; }, canGoBack: false, canGoForward: false, addEventListener: function(){}, removeEventListener: function(){} };
window.cookieStore = { get: function() { return Promise.resolve(null); }, getAll: function() { return Promise.resolve([]); }, set: function() { return Promise.resolve(); }, delete: function() { return Promise.resolve(); }, addEventListener: function(){}, removeEventListener: function(){} };
window.trustedTypes = { createPolicy: function(n,r) { return r; }, isHTML: function() { return false; }, isScript: function() { return false; }, isScriptURL: function() { return false; }, emptyHTML: '', emptyScript: '' };
window.sharedStorage = { set: function() { return Promise.resolve(); }, append: function() { return Promise.resolve(); }, delete: function() { return Promise.resolve(); }, clear: function() { return Promise.resolve(); } };
window.launchQueue = { setConsumer: function(){} };
window.documentPictureInPicture = { requestWindow: function() { return Promise.resolve(window); }, addEventListener: function(){}, removeEventListener: function(){} };
window.customElements = (function() {
	var _registry = {};
	var _waiting = {};
	return {
		define: function(name, constructor, options) {
			_registry[name.toLowerCase()] = { constructor: constructor, options: options };
			if (_waiting[name]) { _waiting[name].forEach(function(r) { r(constructor); }); delete _waiting[name]; }
			try { var existing = document.querySelectorAll(name); for (var i = 0; i < existing.length; i++) { try { constructor.call(existing[i]); } catch(e) {} } } catch(e) {}
		},
		get: function(name) { var r = _registry[name.toLowerCase()]; return r ? r.constructor : undefined; },
		whenDefined: function(name) {
			if (_registry[name.toLowerCase()]) return Promise.resolve(_registry[name.toLowerCase()].constructor);
			return new Promise(function(resolve) { if (!_waiting[name]) _waiting[name] = []; _waiting[name].push(resolve); });
		},
		upgrade: function() {},
		getName: function(constructor) { for (var name in _registry) { if (_registry[name].constructor === constructor) return name; } return null; }
	};
})();
window.fence = undefined;
window.external = { AddSearchProvider: function(){}, IsSearchProviderInstalled: function() { return false; } };
window.styleMedia = { type: 'screen', matchMedium: function() { return false; } };
window.viewport = null;
window.crashReport = null;

// --- BarProp objects ---
window.locationbar = { visible: true };
window.menubar = { visible: true };
window.personalbar = { visible: true };
window.scrollbars = { visible: true };
window.statusbar = { visible: true };
window.toolbar = { visible: true };

// --- Screen position + status ---
// Chrome has screenY=33 (macOS menu bar height)
window.screenX = 0;
window.screenY = 33;
window.screenLeft = 0;
window.screenTop = 33;
window.status = '';
window.offscreenBuffering = true;
window.originAgentCluster = true;
window.credentialless = false;
window.event = undefined;

// --- Missing window functions ---
window.find = function() { return false; };
window.captureEvents = function() {};
window.releaseEvents = function() {};
window.moveBy = function() {};
window.moveTo = function() {};
window.resizeBy = function() {};
window.resizeTo = function() {};
window.reportError = function(e) { throw e; };
window.createImageBitmap = function() { return Promise.resolve({}); };
window.fetchLater = function() { return { activated: false }; };
window.getScreenDetails = function() { return Promise.resolve({}); };
window.queryLocalFonts = function() { return Promise.resolve([]); };
window.showDirectoryPicker = function() { return Promise.resolve({}); };
window.showOpenFilePicker = function() { return Promise.resolve([]); };
window.showSaveFilePicker = function() { return Promise.resolve({}); };

// Timers, backed by Go event loop via _goSetTimeout/_goSetInterval/_goClearTimer
window.setTimeout = function(fn, ms) {
	if (typeof fn === "string") {
		var code = fn;
		fn = function() { eval(code); };
	}
	if (typeof fn !== "function") return 0;
	// Pass extra arguments to the callback (standard setTimeout behavior)
	var extraArgs = Array.prototype.slice.call(arguments, 2);
	if (extraArgs.length > 0) {
		var origFn = fn;
		fn = function() { origFn.apply(null, extraArgs); };
	}
	return _goSetTimeout(fn, ms || 0);
};
window.clearTimeout = function(id) { if (id) _goClearTimer(id); };
window.setInterval = function(fn, ms) {
	if (typeof fn === "string") {
		var code = fn;
		fn = function() { eval(code); };
	}
	if (typeof fn !== "function") return 0;
	// Pass extra arguments to the callback (standard setInterval behavior)
	var extraArgs = Array.prototype.slice.call(arguments, 2);
	if (extraArgs.length > 0) {
		var origFn = fn;
		fn = function() { origFn.apply(null, extraArgs); };
	}
	return _goSetInterval(fn, ms || 0);
};
window.clearInterval = function(id) { if (id) _goClearTimer(id); };
window.requestAnimationFrame = function(fn) {
	if (typeof fn !== "function") return 0;
	return _goSetTimeout(function() { fn(performance.now()); }, 16);
};
window.cancelAnimationFrame = function(id) { if (id) _goClearTimer(id); };
window.queueMicrotask = function(fn) { Promise.resolve().then(fn); };

// RTCPeerConnection, Kasada uses this for WebRTC fingerprinting (local IP, ICE candidates).
window.RTCPeerConnection = function(config) {
	this.localDescription = null;
	this.remoteDescription = null;
	this.signalingState = 'stable';
	this.iceConnectionState = 'new';
	this.iceGatheringState = 'new';
	this.connectionState = 'new';
	this.onicecandidate = null;
	this.onicegatheringstatechange = null;
	this.onconnectionstatechange = null;
	this.onsignalingstatechange = null;
	this.ondatachannel = null;
	this.createDataChannel = function(label, opts) {
		return {label: label || '', readyState: 'connecting', bufferedAmount: 0,
			send: function() {}, close: function() {},
			addEventListener: function() {}, removeEventListener: function() {},
			onopen: null, onclose: null, onmessage: null, onerror: null};
	};
	this.createOffer = function() { return Promise.resolve({type: 'offer', sdp: 'v=0\\r\\no=- 0 0 IN IP4 0.0.0.0\\r\\ns=-\\r\\nt=0 0\\r\\n'}); };
	this.createAnswer = function() { return Promise.resolve({type: 'answer', sdp: ''}); };
	this.setLocalDescription = function(desc) { this.localDescription = desc; return Promise.resolve(); };
	this.setRemoteDescription = function(desc) { this.remoteDescription = desc; return Promise.resolve(); };
	this.addIceCandidate = function() { return Promise.resolve(); };
	this.getStats = function() { return Promise.resolve(new Map()); };
	this.getSenders = function() { return []; };
	this.getReceivers = function() { return []; };
	this.getTransceivers = function() { return []; };
	this.close = function() { this.connectionState = 'closed'; this.signalingState = 'closed'; };
	this.addEventListener = function() {};
	this.removeEventListener = function() {};
};
Object.defineProperty(window.RTCPeerConnection, 'name', { value: 'RTCPeerConnection', configurable: true });
window.RTCPeerConnection.prototype = Object.create(EventTarget.prototype);
window.RTCPeerConnection.prototype.constructor = window.RTCPeerConnection;
window.RTCPeerConnection.generateCertificate = function() { return Promise.resolve({}); };

// --- Webkit aliases (must be after requestAnimationFrame/cancelAnimationFrame) ---
window.webkitCancelAnimationFrame = window.cancelAnimationFrame;
window.webkitRequestAnimationFrame = window.requestAnimationFrame;
window.webkitURL = (typeof URL !== 'undefined') ? URL : function() {};
window.webkitMediaStream = (typeof MediaStream !== 'undefined') ? MediaStream : function() {};
window.webkitRTCPeerConnection = (typeof RTCPeerConnection !== 'undefined') ? RTCPeerConnection : function() {};
window.webkitRequestFileSystem = function() {};
window.webkitResolveLocalFileSystemURL = function() {};
window.webkitSpeechGrammar = function() {};
window.webkitSpeechGrammarList = function() {};
window.webkitSpeechRecognition = function() {};
window.webkitSpeechRecognitionError = function() {};
window.webkitSpeechRecognitionEvent = function() {};

// Event handling, initialize the Symbol-keyed handler store on window
// so EventTarget.prototype.addEventListener/removeEventListener/dispatchEvent
// (inherited via prototype chain) work correctly for window.
// In Chrome, these three methods are NOT own properties of window, they're
// inherited from EventTarget.prototype.
window[_sEL] = {};

// Misc globals
// CRITICAL: atob must return a latin1 "binary string" where each byte maps to
// exactly one char (charCodeAt 0-255). The Go _goAtob callback passes decoded
// bytes through v8.NewValue(iso, string) which uses V8's NewFromUtf8, corrupting
// any byte > 127 (invalid UTF-8 sequences become U+FFFD = 65533, which truncates
// to 253 when written to a Uint8Array). This breaks Cloudflare's VM bytecode
// decoder (AS function) which relies on charCodeAt returning exact byte values.
// Fix: implement atob entirely in JS so the binary string never crosses the
// Go→V8 UTF-8 boundary.
window.atob = (function() {
	var chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
	var lookup = new Uint8Array(256);
	for (var i = 0; i < chars.length; i++) lookup[chars.charCodeAt(i)] = i;
	return function(input) {
		// Remove whitespace and validate (match browser behavior)
		input = String(input).replace(/[\t\n\f\r ]+/g, "");
		// Add padding if needed (browser atob auto-pads)
		var rem = input.length %% 4;
		if (rem === 1) throw new DOMException("The string to be decoded is not correctly encoded.", "InvalidCharacterError");
		if (rem === 2) input += "==";
		else if (rem === 3) input += "=";
		var len = input.length;
		// Count padding
		var pad = 0;
		if (input.charAt(len - 1) === "=") pad++;
		if (input.charAt(len - 2) === "=") pad++;
		var outLen = (len / 4) * 3 - pad;
		var result = new Array(outLen);
		var j = 0;
		for (var i = 0; i < len; i += 4) {
			var a = lookup[input.charCodeAt(i)];
			var b = lookup[input.charCodeAt(i + 1)];
			var c = lookup[input.charCodeAt(i + 2)];
			var d = lookup[input.charCodeAt(i + 3)];
			var bits = (a << 18) | (b << 12) | (c << 6) | d;
			if (j < outLen) result[j++] = String.fromCharCode((bits >> 16) & 0xFF);
			if (j < outLen) result[j++] = String.fromCharCode((bits >> 8) & 0xFF);
			if (j < outLen) result[j++] = String.fromCharCode(bits & 0xFF);
		}
		return result.join("");
	};
})();
// btoa is safe to keep in Go since it only receives JS strings (already UTF-16)
// and returns ASCII base64, no binary corruption risk.
window.btoa = function(s) { return _goBtoa(s); };

// Chrome 146 getComputedStyle property names (421 properties, hyphenated, alphabetical).
// Used by item() and indexed access to match Chrome's exact enumeration order.
const _cssPropNames = ("accent-color,align-content,align-items,align-self,alignment-baseline,all,animation,animation-composition,animation-delay,animation-direction,animation-duration,animation-fill-mode,animation-iteration-count,animation-name,animation-play-state,animation-range,animation-range-end,animation-range-start,animation-timeline,animation-timing-function,appearance,backdrop-filter,backface-visibility,background,background-attachment,background-blend-mode,background-clip,background-color,background-image,background-origin,background-position,background-position-x,background-position-y,background-repeat,background-size,baseline-shift,baseline-source,block-size,border,border-block,border-block-color,border-block-end,border-block-end-color,border-block-end-style,border-block-end-width,border-block-start,border-block-start-color,border-block-start-style,border-block-start-width,border-block-style,border-block-width,border-bottom,border-bottom-color,border-bottom-left-radius,border-bottom-right-radius,border-bottom-style,border-bottom-width,border-collapse,border-color,border-end-end-radius,border-end-start-radius,border-image,border-image-outset,border-image-repeat,border-image-slice,border-image-source,border-image-width,border-inline,border-inline-color,border-inline-end,border-inline-end-color,border-inline-end-style,border-inline-end-width,border-inline-start,border-inline-start-color,border-inline-start-style,border-inline-start-width,border-inline-style,border-inline-width,border-left,border-left-color,border-left-style,border-left-width,border-radius,border-right,border-right-color,border-right-style,border-right-width,border-spacing,border-start-end-radius,border-start-start-radius,border-style,border-top,border-top-color,border-top-left-radius,border-top-right-radius,border-top-style,border-top-width,border-width,bottom,box-decoration-break,box-shadow,box-sizing,break-after,break-before,break-inside,buffered-rendering,caption-side,caret-color,clear,clip,clip-path,clip-rule,color,color-interpolation,color-interpolation-filters,color-scheme,column-count,column-fill,column-gap,column-rule,column-rule-color,column-rule-style,column-rule-width,column-span,column-width,columns,contain,contain-intrinsic-block-size,contain-intrinsic-height,contain-intrinsic-inline-size,contain-intrinsic-size,contain-intrinsic-width,container,container-name,container-type,content,content-visibility,counter-increment,counter-reset,counter-set,cursor,cx,cy,d,direction,display,dominant-baseline,empty-cells,field-sizing,fill,fill-opacity,fill-rule,filter,flex,flex-basis,flex-direction,flex-flow,flex-grow,flex-shrink,flex-wrap,float,flood-color,flood-opacity,font,font-display,font-family,font-feature-settings,font-kerning,font-optical-sizing,font-palette,font-size,font-size-adjust,font-stretch,font-style,font-synthesis,font-synthesis-small-caps,font-synthesis-style,font-synthesis-weight,font-variant,font-variant-alternates,font-variant-caps,font-variant-east-asian,font-variant-emoji,font-variant-ligatures,font-variant-numeric,font-variant-position,font-variation-settings,font-weight,forced-color-adjust,gap,grid,grid-area,grid-auto-columns,grid-auto-flow,grid-auto-rows,grid-column,grid-column-end,grid-column-gap,grid-column-start,grid-gap,grid-row,grid-row-end,grid-row-gap,grid-row-start,grid-template,grid-template-areas,grid-template-columns,grid-template-rows,height,hyphenate-character,hyphenate-limit-chars,hyphens,image-orientation,image-rendering,initial-letter,inline-size,inset,inset-block,inset-block-end,inset-block-start,inset-inline,inset-inline-end,inset-inline-start,isolation,justify-content,justify-items,justify-self,left,letter-spacing,lighting-color,line-break,line-height,list-style,list-style-image,list-style-position,list-style-type,margin,margin-block,margin-block-end,margin-block-start,margin-bottom,margin-inline,margin-inline-end,margin-inline-start,margin-left,margin-right,margin-top,marker,marker-end,marker-mid,marker-start,mask,mask-clip,mask-composite,mask-image,mask-mode,mask-origin,mask-position,mask-repeat,mask-size,mask-type,math-depth,math-shift,math-style,max-block-size,max-height,max-inline-size,max-width,min-block-size,min-height,min-inline-size,min-width,mix-blend-mode,object-fit,object-position,object-view-box,offset,offset-anchor,offset-distance,offset-path,offset-position,offset-rotate,opacity,order,orphans,outline,outline-color,outline-offset,outline-style,outline-width,overflow,overflow-anchor,overflow-clip-margin,overflow-wrap,overflow-x,overflow-y,overscroll-behavior,overscroll-behavior-block,overscroll-behavior-inline,overscroll-behavior-x,overscroll-behavior-y,padding,padding-block,padding-block-end,padding-block-start,padding-bottom,padding-inline,padding-inline-end,padding-inline-start,padding-left,padding-right,padding-top,page,page-break-after,page-break-before,page-break-inside,paint-order,perspective,perspective-origin,place-content,place-items,place-self,pointer-events,position,print-color-adjust,r,resize,right,rotate,row-gap,ruby-align,ruby-position,rx,ry,scale,scroll-behavior,scroll-margin,scroll-margin-block,scroll-margin-block-end,scroll-margin-block-start,scroll-margin-bottom,scroll-margin-inline,scroll-margin-inline-end,scroll-margin-inline-start,scroll-margin-left,scroll-margin-right,scroll-margin-top,scroll-padding,scroll-padding-block,scroll-padding-block-end,scroll-padding-block-start,scroll-padding-bottom,scroll-padding-inline,scroll-padding-inline-end,scroll-padding-inline-start,scroll-padding-left,scroll-padding-right,scroll-padding-top,scroll-snap-align,scroll-snap-stop,scroll-snap-type,scroll-timeline,scroll-timeline-axis,scroll-timeline-name,scrollbar-color,scrollbar-gutter,scrollbar-width,shape-image-threshold,shape-margin,shape-outside,shape-rendering,stop-color,stop-opacity,stroke,stroke-dasharray,stroke-dashoffset,stroke-linecap,stroke-linejoin,stroke-miterlimit,stroke-opacity,stroke-width,tab-size,table-layout,text-align,text-align-last,text-anchor,text-combine-upright,text-decoration,text-decoration-color,text-decoration-line,text-decoration-skip-ink,text-decoration-style,text-decoration-thickness,text-emphasis,text-emphasis-color,text-emphasis-position,text-emphasis-style,text-indent,text-orientation,text-overflow,text-rendering,text-shadow,text-size-adjust,text-transform,text-underline-offset,text-underline-position,text-wrap,text-wrap-mode,text-wrap-style,timeline-scope,top,touch-action,transform,transform-origin,transform-style,transition,transition-behavior,transition-delay,transition-duration,transition-property,transition-timing-function,translate,unicode-bidi,user-select,vector-effect,vertical-align,view-timeline,view-timeline-axis,view-timeline-inset,view-timeline-name,view-transition-class,view-transition-name,visibility,white-space,white-space-collapse,widows,width,will-change,word-break,word-spacing,word-wrap,writing-mode,x,y,z-index,zoom").split(",");

window.getComputedStyle = function(el, pseudo) {
	var defaults = {
		display: "block", visibility: "visible", opacity: "1",
		position: "static", overflow: "visible", zIndex: "auto",
		width: "200px", height: "100px", margin: "0px", padding: "0px",
		border: "0px none rgb(0, 0, 0)", borderWidth: "0px",
		fontSize: "16px", fontFamily: "Arial, sans-serif", fontWeight: "400",
		lineHeight: "normal", color: "rgb(0, 0, 0)",
		backgroundColor: "rgba(0, 0, 0, 0)", backgroundImage: "none",
		textAlign: "start", textDecoration: "none", textTransform: "none",
		cursor: "auto", pointerEvents: "auto", userSelect: "auto",
		transform: "none", transition: "all 0s ease 0s",
		boxSizing: "content-box", float: "none", clear: "none",
		// margin variants
		marginTop: "0px", marginRight: "0px", marginBottom: "0px", marginLeft: "0px",
		marginBlock: "0px", marginBlockStart: "0px", marginBlockEnd: "0px",
		marginInline: "0px", marginInlineStart: "0px", marginInlineEnd: "0px",
		// padding variants
		paddingTop: "0px", paddingRight: "0px", paddingBottom: "0px", paddingLeft: "0px",
		paddingBlock: "0px", paddingBlockStart: "0px", paddingBlockEnd: "0px",
		paddingInline: "0px", paddingInlineStart: "0px", paddingInlineEnd: "0px",
		// border variants
		borderStyle: "none", borderColor: "rgb(0, 0, 0)", borderRadius: "0px",
		borderTop: "0px none rgb(0, 0, 0)", borderRight: "0px none rgb(0, 0, 0)",
		borderBottom: "0px none rgb(0, 0, 0)", borderLeft: "0px none rgb(0, 0, 0)",
		borderTopWidth: "0px", borderRightWidth: "0px", borderBottomWidth: "0px", borderLeftWidth: "0px",
		borderTopStyle: "none", borderRightStyle: "none", borderBottomStyle: "none", borderLeftStyle: "none",
		borderTopColor: "rgb(0, 0, 0)", borderRightColor: "rgb(0, 0, 0)", borderBottomColor: "rgb(0, 0, 0)", borderLeftColor: "rgb(0, 0, 0)",
		borderTopLeftRadius: "0px", borderTopRightRadius: "0px", borderBottomLeftRadius: "0px", borderBottomRightRadius: "0px",
		borderImage: "none", borderImageSource: "none", borderImageSlice: "100%%",
		borderImageWidth: "1", borderImageOutset: "0", borderImageRepeat: "stretch",
		borderCollapse: "separate", borderSpacing: "0px 0px",
		// flexbox
		flex: "0 1 auto", flexDirection: "row", flexWrap: "nowrap", flexFlow: "row nowrap",
		flexGrow: "0", flexShrink: "1", flexBasis: "auto",
		justifyContent: "normal", alignItems: "normal", alignSelf: "auto", alignContent: "normal",
		order: "0",
		// grid
		grid: "none", gridTemplate: "none", gridTemplateColumns: "none", gridTemplateRows: "none",
		gridTemplateAreas: "none", gridArea: "auto / auto / auto / auto",
		gridColumn: "auto / auto", gridColumnStart: "auto", gridColumnEnd: "auto",
		gridRow: "auto / auto", gridRowStart: "auto", gridRowEnd: "auto",
		gridAutoFlow: "row", gridAutoColumns: "auto", gridAutoRows: "auto",
		gap: "normal", rowGap: "normal", columnGap: "normal",
		// transform / animation / transition
		transformOrigin: "100px 50px", transformStyle: "flat",
		animation: "none 0s ease 0s 1 normal none running",
		animationName: "none", animationDuration: "0s", animationTimingFunction: "ease",
		animationDelay: "0s", animationIterationCount: "1", animationDirection: "normal",
		animationFillMode: "none", animationPlayState: "running",
		transitionProperty: "all", transitionDuration: "0s", transitionTimingFunction: "ease", transitionDelay: "0s",
		willChange: "auto", perspective: "none", perspectiveOrigin: "100px 50px",
		backfaceVisibility: "visible",
		// text properties
		textIndent: "0px", textShadow: "none", textOverflow: "clip",
		textRendering: "auto", textDecorationLine: "none", textDecorationStyle: "solid",
		textDecorationColor: "rgb(0, 0, 0)", textDecorationThickness: "auto",
		textUnderlineOffset: "auto", textUnderlinePosition: "auto",
		letterSpacing: "normal", wordSpacing: "0px", wordBreak: "normal", wordWrap: "normal",
		whiteSpace: "normal", overflowWrap: "normal", tabSize: "8",
		fontStyle: "normal", fontVariant: "normal", fontStretch: "100%%",
		fontSizeAdjust: "none", fontKerning: "auto",
		// dimension / sizing
		minWidth: "0px", maxWidth: "none", minHeight: "0px", maxHeight: "none",
		top: "auto", right: "auto", bottom: "auto", left: "auto",
		inset: "auto", insetBlock: "auto", insetBlockStart: "auto", insetBlockEnd: "auto",
		insetInline: "auto", insetInlineStart: "auto", insetInlineEnd: "auto",
		// outline
		outline: "rgb(0, 0, 0) none 0px", outlineWidth: "0px", outlineStyle: "none",
		outlineColor: "rgb(0, 0, 0)", outlineOffset: "0px",
		// list
		listStyle: "outside none disc", listStyleType: "disc", listStylePosition: "outside", listStyleImage: "none",
		// overflow variants
		overflowX: "visible", overflowY: "visible", overflowAnchor: "auto",
		overflowClipMargin: "0px",
		// clip / mask
		clip: "auto", clipPath: "none", clipRule: "nonzero",
		mask: "none", maskImage: "none", maskPosition: "0%% 0%%", maskSize: "auto", maskRepeat: "repeat",
		// misc
		objectFit: "fill", objectPosition: "50%% 50%%",
		verticalAlign: "baseline", direction: "ltr", unicodeBidi: "normal",
		writingMode: "horizontal-tb", isolation: "auto", mixBlendMode: "normal",
		filter: "none", backdropFilter: "none", resize: "none", appearance: "none",
		contain: "none", contentVisibility: "visible",
		scrollBehavior: "auto", scrollMargin: "0px", scrollPadding: "auto",
		scrollSnapType: "none", scrollSnapAlign: "none",
		touchAction: "auto", caretColor: "auto", accentColor: "auto",
		colorScheme: "normal", forcedColorAdjust: "auto",
		containerType: "normal", containerName: "none",
		aspectRatio: "auto",
		// background variants
		backgroundPosition: "0%% 0%%", backgroundSize: "auto", backgroundRepeat: "repeat",
		backgroundAttachment: "scroll", backgroundClip: "border-box", backgroundOrigin: "padding-box",
		backgroundBlendMode: "normal",
		// box shadow
		boxShadow: "none",
		// table
		tableLayout: "auto", captionSide: "top", emptyCells: "show",
		// columns
		columns: "auto auto", columnCount: "auto", columnWidth: "auto",
		columnFill: "balance", columnRule: "0px none rgb(0, 0, 0)",
		columnRuleWidth: "0px", columnRuleStyle: "none", columnRuleColor: "rgb(0, 0, 0)",
		columnSpan: "none",
		// counters
		counterIncrement: "none", counterReset: "none", counterSet: "none",
		// content
		content: "normal", quotes: "auto",
		// page break
		pageBreakAfter: "auto", pageBreakBefore: "auto", pageBreakInside: "auto",
		breakAfter: "auto", breakBefore: "auto", breakInside: "auto",
		// orphans/widows
		orphans: "2", widows: "2",
		// image rendering
		imageRendering: "auto",
		// shape
		shapeOutside: "none", shapeMargin: "0px", shapeImageThreshold: "0",
		// all / initial / unset helpers (values that Chrome reports)
		all: "",
		// webkit prefixed (Chrome still reports these)
		webkitAppearance: "none", webkitBoxShadow: "none",
		webkitFilter: "none", webkitBackdropFilter: "none",
		webkitTextFillColor: "rgb(0, 0, 0)", webkitTextStrokeColor: "rgb(0, 0, 0)", webkitTextStrokeWidth: "0px",
		webkitFontSmoothing: "antialiased",
		webkitBoxAlign: "stretch", webkitBoxDirection: "normal", webkitBoxFlex: "0",
		webkitBoxOrdinalGroup: "1", webkitBoxOrient: "horizontal", webkitBoxPack: "start"
	};
	// getPropertyValue handles both camelCase and hyphenated CSS property names.
	defaults.getPropertyValue = function(p) {
		if (defaults[p] !== undefined) return defaults[p];
		// Convert hyphenated to camelCase: "margin-top" → "marginTop"
		var camel = p.replace(/-([a-z])/g, function(m, c) { return c.toUpperCase(); });
		return defaults[camel] || "";
	};
	Object.defineProperty(defaults, 'length', { value: _cssPropNames.length, writable: false, configurable: true });
	// item() returns hyphenated CSS property names (matching Chrome behavior).
	defaults.item = function(i) { return _cssPropNames[i] || ""; };
	defaults.setProperty = function() {};
	defaults.removeProperty = function() {};
	// Add indexed access (cs[0], cs[1], ...), Chrome supports this.
	for (var _ci = 0; _ci < _cssPropNames.length; _ci++) {
		Object.defineProperty(defaults, _ci, {value: _cssPropNames[_ci], enumerable: false, configurable: true});
	}
	Object.setPrototypeOf(defaults, CSSStyleDeclaration.prototype);
	return defaults;
};
window.matchMedia = function(query) {
	var q = query.replace(/\s+/g, ' ').trim().toLowerCase();
	var m = false;
	// prefers-color-scheme
	if (q.indexOf('prefers-color-scheme: light') !== -1 || q.indexOf('prefers-color-scheme:light') !== -1) m = true;
	// prefers-reduced-motion: no-preference (default)
	if (q.indexOf('prefers-reduced-motion: no-preference') !== -1 || q.indexOf('prefers-reduced-motion:no-preference') !== -1) m = true;
	// min-width / max-width evaluation against innerWidth (300 for iframe)
	var minW = q.match(/\(min-width:\s*(\d+)px\)/);
	if (minW && parseInt(minW[1], 10) <= window.innerWidth) m = true;
	var maxW = q.match(/\(max-width:\s*(\d+)px\)/);
	if (maxW && parseInt(maxW[1], 10) >= window.innerWidth) m = true;
	// min-height / max-height evaluation against innerHeight (65 for iframe)
	var minH = q.match(/\(min-height:\s*(\d+)px\)/);
	if (minH && parseInt(minH[1], 10) <= window.innerHeight) m = true;
	var maxH = q.match(/\(max-height:\s*(\d+)px\)/);
	if (maxH && parseInt(maxH[1], 10) >= window.innerHeight) m = true;
	// color / color-gamut
	if (q === '(color)' || q === 'all' || q === 'screen') m = true;
	if (q.indexOf('color-gamut: srgb') !== -1 || q.indexOf('color-gamut:srgb') !== -1) m = true;
	// display-mode: browser (default)
	if (q.indexOf('display-mode: browser') !== -1 || q.indexOf('display-mode:browser') !== -1) m = true;
	// forced-colors: none (default)
	if (q.indexOf('forced-colors: none') !== -1 || q.indexOf('forced-colors:none') !== -1) m = true;
	// prefers-contrast: no-preference (default)
	if (q.indexOf('prefers-contrast: no-preference') !== -1 || q.indexOf('prefers-contrast:no-preference') !== -1) m = true;
	// inverted-colors: none (default)
	if (q.indexOf('inverted-colors: none') !== -1 || q.indexOf('inverted-colors:none') !== -1) m = true;
	// Not-matched queries
	if (q.indexOf('prefers-color-scheme: dark') !== -1 || q.indexOf('prefers-color-scheme:dark') !== -1) m = false;
	if (q.indexOf('prefers-reduced-motion: reduce') !== -1 || q.indexOf('prefers-reduced-motion:reduce') !== -1) m = false;
	var result = {
		matches: m,
		media: query,
		onchange: null,
		addListener: function(){},
		removeListener: function(){},
		addEventListener: function(){},
		removeEventListener: function(){},
		dispatchEvent: function() { return true; }
	};
	Object.setPrototypeOf(result, EventTarget.prototype);
	Object.defineProperty(result, Symbol.toStringTag, { value: 'MediaQueryList', configurable: true });
	return result;
};
window.requestIdleCallback = function(fn) { fn({timeRemaining: function(){ return 50; }, didTimeout: false}); return ++_nid; };
window.cancelIdleCallback = function() {};
window.scroll = function() {};
window.scrollTo = function() {};
window.scrollBy = function() {};
window.focus = function() {};
window.blur = function() {};
window.close = function() {};
window.stop = function() {};
window.open = function() { return null; };
window.print = function() {};
window.alert = function() {};
window.confirm = function() { return true; };
window.prompt = function() { return null; };
window.postMessage = function() {};

// crypto.subtle, must return Promise<ArrayBuffer> like real browsers
window.crypto = {
	subtle: {
		digest: function(algorithm, data) {
			// _goDigest returns a hex string. Convert to ArrayBuffer.
			var hexHash = _goDigest(algorithm, typeof data === 'string' ? data : String.fromCharCode.apply(null, new Uint8Array(data)));
			// Convert hex string to ArrayBuffer
			var bytes = new Uint8Array(hexHash.length / 2);
			for (var i = 0; i < hexHash.length; i += 2) {
				bytes[i / 2] = parseInt(hexHash.substring(i, i + 2), 16);
			}
			return Promise.resolve(bytes.buffer);
		},
		importKey: function(format, keyData, algorithm, extractable, usages) {
			var algoName = (algorithm && algorithm.name) || algorithm || 'unknown';
			var dataLen = keyData ? (keyData.byteLength || keyData.length || 0) : 0;
			console.log('[CRYPTO] importKey(' + format + ', ' + dataLen + 'B, ' + algoName + ', ' + extractable + ', [' + (usages||[]).join(',') + '])');
			return Promise.resolve({type: 'secret', extractable: extractable, algorithm: {name: algoName}, usages: usages || []});
		},
		exportKey: function(format, key) {
			console.log('[CRYPTO] exportKey(' + format + ')');
			return Promise.resolve(new ArrayBuffer(0));
		},
		sign: function(algorithm, key, data) {
			console.log('[CRYPTO] sign(' + ((algorithm && algorithm.name) || algorithm) + ', data=' + (data ? data.byteLength : 0) + 'B)');
			return Promise.resolve(new ArrayBuffer(32));
		},
		verify: function() { return Promise.resolve(true); },
		encrypt: function(algorithm, key, data) {
			var algoName = (algorithm && algorithm.name) || algorithm || 'unknown';
			var dataLen = data ? (data.byteLength || data.length || 0) : 0;
			var ivLen = (algorithm && algorithm.iv) ? (algorithm.iv.byteLength || algorithm.iv.length || 0) : 0;
			console.log('[CRYPTO] encrypt(' + algoName + ', iv=' + ivLen + 'B, data=' + dataLen + 'B)');
			return Promise.resolve(new ArrayBuffer(0));
		},
		decrypt: function(algorithm, key, data) {
			var algoName = (algorithm && algorithm.name) || algorithm || 'unknown';
			console.log('[CRYPTO] decrypt(' + algoName + ', data=' + (data ? data.byteLength : 0) + 'B)');
			return Promise.resolve(new ArrayBuffer(0));
		},
		deriveBits: function(algorithm, baseKey, length) {
			console.log('[CRYPTO] deriveBits(' + ((algorithm && algorithm.name) || algorithm) + ', ' + length + ')');
			return Promise.resolve(new ArrayBuffer(0));
		},
		deriveKey: function() { return Promise.resolve({}); }
	},
	getRandomValues: (function() {
		// xorshift128+ PRNG, same algorithm V8 uses for Math.random().
		// Seeded from Math.random() but generates full 64-bit state to fill
		// typed arrays with uniformly distributed values matching Chrome's
		// crypto.getRandomValues() statistical distribution.
		// This avoids the bias of Math.floor(Math.random() * max) which
		// only has 52 bits of mantissa and can't fill Uint32 uniformly.
		var _s0 = (Math.random() * 0xFFFFFFFF) >>> 0;
		var _s1 = (Math.random() * 0xFFFFFFFF) >>> 0;
		var _s2 = (Math.random() * 0xFFFFFFFF) >>> 0;
		var _s3 = (Math.random() * 0xFFFFFFFF) >>> 0;
		if (_s0 === 0 && _s1 === 0 && _s2 === 0 && _s3 === 0) _s0 = 1;
		function _xrand32() {
			// xorshift128 with 32-bit words
			var t = _s3;
			var s = _s0;
			_s3 = _s2; _s2 = _s1; _s1 = s;
			t ^= (t << 11) | 0;
			t ^= (t >>> 8) | 0;
			_s0 = t ^ s ^ (s >>> 19);
			return (_s0 >>> 0);
		}
		return function(arr) {
			if (!(arr && arr.buffer instanceof ArrayBuffer)) {
				throw new TypeError("Failed to execute 'getRandomValues' on 'Crypto': parameter 1 is not of type 'ArrayBufferView'.");
			}
			if (arr.byteLength > 65536) {
				throw new DOMException("The ArrayBufferView's byte length (" + arr.byteLength + ") exceeds the number of bytes of entropy available via this API (65536).", "QuotaExceededError");
			}
			// Fill using a Uint8Array view for uniform byte distribution,
			// then let the typed array view interpret the bytes correctly.
			var bytes = new Uint8Array(arr.buffer, arr.byteOffset, arr.byteLength);
			for (var i = 0; i < bytes.length; i += 4) {
				var r = _xrand32();
				bytes[i] = r & 0xFF;
				if (i + 1 < bytes.length) bytes[i + 1] = (r >>> 8) & 0xFF;
				if (i + 2 < bytes.length) bytes[i + 2] = (r >>> 16) & 0xFF;
				if (i + 3 < bytes.length) bytes[i + 3] = (r >>> 24) & 0xFF;
			}
			return arr;
		};
	})(),
	randomUUID: function() {
		return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, function(c) {
			var r = Math.random() * 16 | 0;
			return (c === "x" ? r : (r & 0x3 | 0x8)).toString(16);
		});
	}
};

// performance, Native C++ Performance created by engine.go's setupPerformance().
// Wire EventTarget prototype chain now that EventTarget is defined, and override
// getEntriesByType with the location-aware version that uses _ifrST.
(function() {
	var _perfTiming = _cfPt;

	// Wire Performance.prototype → EventTarget.prototype chain
	Object.setPrototypeOf(Performance.prototype, EventTarget.prototype);
	Object.defineProperty(Performance.prototype, 'constructor', { value: Performance, writable: true, configurable: true });

	// Override getEntriesByType with location-aware version (location not available in engine.go)
	Performance.prototype.getEntriesByType = function getEntriesByType(type) {
		if (type === 'navigation') {
			return [{
				name: location.href,
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
				serverTiming: _ifrST || [],
				toJSON: function() { return this; }
			}];
		}
		if (type === 'resource') {
			// Chrome's /fp page only has favicon.ico as a resource entry
			var s = 100 + Math.random() * 20;
			var d = 2 + Math.random() * 3;
			return [{
				name: location.protocol + '//' + location.host + '/favicon.ico',
				entryType: 'resource', startTime: s, duration: d,
				initiatorType: 'other', nextHopProtocol: 'h2',
				workerStart: 0, redirectStart: 0, redirectEnd: 0,
				fetchStart: s, domainLookupStart: s, domainLookupEnd: s,
				connectStart: s, connectEnd: s, secureConnectionStart: s,
				requestStart: s + 0.5, responseStart: s + d * 0.7, responseEnd: s + d,
				transferSize: 0, encodedBodySize: 0, decodedBodySize: 0,
				serverTiming: [], toJSON: function() { return this; }
			}];
		}
		if (type === 'visibility-state') {
			// Chrome /fp page has a visibility-state entry
			return [{name: 'visible', entryType: 'visibility-state', startTime: 0, duration: 0}];
		}
		if (type === 'paint') {
			// Chrome's /fp page has NO paint entries (it's a minimal challenge page)
			return [];
		}
		return [];
	};

	// Mark methods as native
	if (typeof _mkFnNat === 'function') {
		_mkFnNat(Performance.prototype.now, 'now');
		_mkFnNat(Performance.prototype.getEntries, 'getEntries');
		_mkFnNat(Performance.prototype.getEntriesByName, 'getEntriesByName');
		_mkFnNat(Performance.prototype.getEntriesByType, 'getEntriesByType');
		_mkFnNat(Performance.prototype.mark, 'mark');
		_mkFnNat(Performance.prototype.measure, 'measure');
		_mkFnNat(Performance.prototype.clearMarks, 'clearMarks');
		_mkFnNat(Performance.prototype.clearMeasures, 'clearMeasures');
		_mkFnNat(Performance.prototype.clearResourceTimings, 'clearResourceTimings');
		_mkFnNat(Performance.prototype.setResourceTimingBufferSize, 'setResourceTimingBufferSize');
		_mkFnNat(Performance.prototype.measureUserAgentSpecificMemory, 'measureUserAgentSpecificMemory');
		_mkFnNat(Performance.prototype.toJSON, 'toJSON');
	}
})();
var performance = window.performance;

// console, routes to Go for capture/debugging
var console = {
	log: function() { _goConsoleLog.apply(null, arguments); },
	warn: function() { _goConsoleLog.apply(null, ['[WARN]'].concat(Array.from(arguments))); },
	error: function() { _goConsoleLog.apply(null, ['[ERROR]'].concat(Array.from(arguments))); },
	info: function() { _goConsoleLog.apply(null, arguments); },
	debug: function() { _goConsoleLog.apply(null, arguments); },
	trace: function() {},
	dir: function() {},
	table: function() {},
	group: function() {},
	groupEnd: function() {},
	time: function() {},
	timeEnd: function() {},
	assert: function() {},
	count: function() {},
	clear: function() {}
};
Object.defineProperty(window, 'console', {value: console, writable: true, enumerable: false, configurable: true});

// history
window.history = {
	length: 1,
	state: null,
	pushState: function() {},
	replaceState: function() {},
	go: function() {},
	back: function() {},
	forward: function() {}
};

// localStorage / sessionStorage
let _str = {};
let _mkStr = function() {
	return {
		_data: {},
		getItem: function(key) { return this._data.hasOwnProperty(key) ? this._data[key] : null; },
		setItem: function(key, val) { this._data[key] = String(val); },
		removeItem: function(key) { delete this._data[key]; },
		clear: function() { this._data = {}; },
		get length() { return Object.keys(this._data).length; },
		key: function(i) { var keys = Object.keys(this._data); return keys[i] || null; }
	};
};
window.localStorage = _mkStr();
window.sessionStorage = _mkStr();

// CSS and style stubs
window.CSS = {
	supports: function(prop, val) {
		if (arguments.length === 1) {
			var s = String(prop).toLowerCase();
			if (s.indexOf('not ') === 0 || s.indexOf('selector(') !== -1) return true;
			return true;
		}
		var p = String(prop).toLowerCase().replace(/^-webkit-/, '');
		// Validate against known CSS properties (Chrome 144)
		var valid = {
			'display':1,'color':1,'background':1,'background-color':1,'background-image':1,
			'background-size':1,'background-position':1,'background-repeat':1,'background-attachment':1,
			'margin':1,'margin-top':1,'margin-right':1,'margin-bottom':1,'margin-left':1,
			'padding':1,'padding-top':1,'padding-right':1,'padding-bottom':1,'padding-left':1,
			'border':1,'border-top':1,'border-right':1,'border-bottom':1,'border-left':1,
			'border-color':1,'border-width':1,'border-style':1,'border-radius':1,
			'width':1,'height':1,'min-width':1,'min-height':1,'max-width':1,'max-height':1,
			'font':1,'font-family':1,'font-size':1,'font-weight':1,'font-style':1,'font-variant':1,
			'line-height':1,'letter-spacing':1,'word-spacing':1,'text-align':1,'text-decoration':1,
			'text-transform':1,'text-indent':1,'text-shadow':1,'white-space':1,'word-break':1,
			'word-wrap':1,'overflow-wrap':1,'text-overflow':1,
			'position':1,'top':1,'right':1,'bottom':1,'left':1,'z-index':1,
			'float':1,'clear':1,'overflow':1,'overflow-x':1,'overflow-y':1,
			'visibility':1,'opacity':1,'cursor':1,'pointer-events':1,
			'flex':1,'flex-direction':1,'flex-wrap':1,'flex-flow':1,'flex-grow':1,'flex-shrink':1,
			'flex-basis':1,'justify-content':1,'align-items':1,'align-self':1,'align-content':1,
			'order':1,'gap':1,'row-gap':1,'column-gap':1,
			'grid':1,'grid-template':1,'grid-template-columns':1,'grid-template-rows':1,
			'grid-template-areas':1,'grid-column':1,'grid-row':1,'grid-area':1,
			'grid-auto-columns':1,'grid-auto-rows':1,'grid-auto-flow':1,
			'transform':1,'transform-origin':1,'transition':1,'transition-property':1,
			'transition-duration':1,'transition-timing-function':1,'transition-delay':1,
			'animation':1,'animation-name':1,'animation-duration':1,'animation-timing-function':1,
			'animation-delay':1,'animation-iteration-count':1,'animation-direction':1,
			'animation-fill-mode':1,'animation-play-state':1,
			'box-shadow':1,'box-sizing':1,'outline':1,'outline-color':1,'outline-style':1,
			'outline-width':1,'outline-offset':1,'resize':1,'appearance':1,
			'list-style':1,'list-style-type':1,'list-style-position':1,'list-style-image':1,
			'table-layout':1,'border-collapse':1,'border-spacing':1,'vertical-align':1,
			'content':1,'counter-increment':1,'counter-reset':1,
			'filter':1,'backdrop-filter':1,'mix-blend-mode':1,'isolation':1,
			'object-fit':1,'object-position':1,'image-rendering':1,
			'clip-path':1,'mask':1,'mask-image':1,
			'will-change':1,'contain':1,'container-name':1,
			'aspect-ratio':1,'accent-color':1,'caret-color':1,'color-scheme':1,
			'scroll-behavior':1,'scroll-snap-type':1,'scroll-snap-align':1,
			'user-select':1,'touch-action':1,'overscroll-behavior':1,
			'writing-mode':1,'direction':1,'unicode-bidi':1,
			'columns':1,'column-count':1,'column-width':1,'column-rule':1,
			'break-before':1,'break-after':1,'break-inside':1,
			'all':1,'initial':1,'inherit':1,'unset':1,'revert':1
		};
		return p in valid;
	},
	escape: function(s) { return s; }
};
window.CSSStyleDeclaration = CSSStyleDeclaration;
window.PluginArray = PluginArray;
window.MimeTypeArray = MimeTypeArray;
window.Permissions = Permissions;
window.StyleSheet = function() {};

// Event constructors, isTrusted is non-configurable, non-writable (like real browsers)
window.Event = function(type, opts) {
	this.type = type;
	this.bubbles = (opts && opts.bubbles) || false;
	this.cancelable = (opts && opts.cancelable) || false;
	this.composed = (opts && opts.composed) || false;
	this.defaultPrevented = false;
	this.eventPhase = 0;
	this.target = null;
	this.currentTarget = null;
	this.srcElement = null;
	this.returnValue = true;
	this.cancelBubble = false;
	this.timeStamp = performance.now();
	Object.defineProperty(this, 'isTrusted', {value: false, writable: false, enumerable: true, configurable: false});
	this.preventDefault = function() { this.defaultPrevented = true; this.returnValue = false; };
	this.stopPropagation = function() { this.cancelBubble = true; };
	this.stopImmediatePropagation = function() { this.cancelBubble = true; };
	this.composedPath = function() { return this.target ? [this.target] : []; };
};
window.Event.prototype.AT_TARGET = 2;
window.Event.prototype.BUBBLING_PHASE = 3;
window.Event.prototype.CAPTURING_PHASE = 1;
window.Event.prototype.NONE = 0;
window.CustomEvent = function(type, opts) { window.Event.call(this, type, opts); this.detail = (opts && opts.detail) || null; };
window.MouseEvent = function(type, opts) { window.Event.call(this, type, opts); this.clientX = (opts && opts.clientX) || 0; this.clientY = (opts && opts.clientY) || 0; this.screenX = (opts && opts.screenX) || 0; this.screenY = (opts && opts.screenY) || 0; this.button = (opts && opts.button) || 0; this.buttons = (opts && opts.buttons) || 0; };
window.PointerEvent = function(type, opts) { window.MouseEvent.call(this, type, opts); this.pointerId = (opts && opts.pointerId) || 1; this.pointerType = (opts && opts.pointerType) || "mouse"; this.width = 1; this.height = 1; this.pressure = 0.5; this.isPrimary = true; };
window.KeyboardEvent = function(type, opts) { window.Event.call(this, type, opts); this.key = (opts && opts.key) || ""; this.code = (opts && opts.code) || ""; this.keyCode = (opts && opts.keyCode) || 0; };
window.MessageEvent = function(type, opts) { window.Event.call(this, type, opts); this.data = (opts && opts.data) || null; this.origin = (opts && opts.origin) || ""; this.source = (opts && opts.source) || null; this.ports = (opts && opts.ports) || []; this.lastEventId = ""; };

// Image constructor
window.Image = function() { return document.createElement("img"); };

// XMLHttpRequest, routes through Go's _goSyncFetch for real HTTP requests
window.XMLHttpRequest = function() {
	this.readyState = 0;
	this.status = 0;
	this.statusText = "";
	this.responseText = "";
	this.response = "";
	this.responseURL = "";
	this.responseType = "";
	this.timeout = 0;
	this.withCredentials = false;
	this._method = "GET";
	this._url = "";
	this._headers = {};
	this._respHeaders = {};
	this.onreadystatechange = null;
	this.onload = null;
	this.onerror = null;
	this.ontimeout = null;
	this.onprogress = null;
	this.upload = { addEventListener: function(){} };

	this.open = function(method, url, async) {
		this._method = method;
		// Resolve relative URLs against override origin (for iframe context).
		// In a real browser, an iframe on challenges.cloudflare.com resolves
		// relative URLs against its own origin, not the parent page's origin.
		if (url && url.charAt(0) === '/' && this._overrideOrigin) {
			this._url = this._overrideOrigin + url;
		} else {
			this._url = url;
		}
		this.readyState = 1;
		console.log('[DOM] XHR.open(' + method + ', ' + this._url + ')');
		if (url === undefined || url === null) {
			console.log('[DOM] XHR.open WARNING: URL is ' + url + ', stack: ' + new Error().stack.split('\n').slice(0,8).join(' <- '));
			console.log('[DOM] XHR.open: _overrideOrigin=' + this._overrideOrigin + ' window._cf_chl_opt=' + typeof window._cf_chl_opt);
			// Log all URL-relevant document/location properties
			try {
				console.log('[DOM] XHR.open diag: document.URL=' + document.URL);
				console.log('[DOM] XHR.open diag: document.location=' + (document.location ? document.location.href : 'NO-LOCATION'));
				console.log('[DOM] XHR.open diag: location.href=' + location.href);
				console.log('[DOM] XHR.open diag: window.location.href=' + window.location.href);
				console.log('[DOM] XHR.open diag: document.baseURI=' + document.baseURI);
				console.log('[DOM] XHR.open diag: document.documentURI=' + document.documentURI);
			} catch(diagErr) {
				console.log('[DOM] XHR.open diag error: ' + diagErr.message);
			}
		}
	};
	this.setRequestHeader = function(k, v) {
		this._headers[k] = v;
		console.log('[DOM] XHR.setRequestHeader(' + k + ': ' + v + ')');
	};
	this.getResponseHeader = function(k) {
		return this._respHeaders[k.toLowerCase()] || null;
	};
	this.getAllResponseHeaders = function() {
		var lines = [];
		for (var k in this._respHeaders) {
			lines.push(k + ': ' + this._respHeaders[k]);
		}
		return lines.join('\r\n');
	};
	this.send = function(body) {
		console.log('[DOM] XHR.send(' + this._method + ' ' + this._url + ', body=' + (body ? body.length : 0) + ' bytes, typeof=' + typeof body + ')');
		// Kasada error report debug, capture what Kasada detected wrong
		if (this._method === 'POST' && this._url.indexOf('cdndex.io') !== -1 && body) {
			console.log('[KASADA-ERROR-REPORT] len=' + body.length);
			if (typeof body === 'string') {
				_goWriteFile('/tmp/kasada_error_report.json', body);
				console.log('[KASADA-ERROR-REPORT] first500=' + body.substring(0, 500));
			}
		}
		// Kasada /tl POST payload debug
		if (this._method === 'POST' && this._url.indexOf('/tl') !== -1 && body) {
			var bodyBytes = [];
			if (typeof body === 'string') {
				for (var _bi = 0; _bi < body.length; _bi++) bodyBytes.push(body.charCodeAt(_bi));
			}
			console.log('[TL-BODY] len=' + body.length + ' charCodes=' + bodyBytes.slice(0, 50).join(','));
			console.log('[TL-BODY] type=' + typeof body + ' isUint8=' + (body instanceof Uint8Array) + ' isArrayBuf=' + (body instanceof ArrayBuffer));
			_goWriteFile('/tmp/kasada_tl_body.txt', body);
		}
		// Capture flow POST payload for debugging (minimal side effects: just file write)
		if (this._method === 'POST' && this._url.indexOf('/flow/') !== -1 && body) {
			console.log('[FLOW-BODY] len=' + body.length + ' first100=' + body.substring(0, 100));
			console.log('[FLOW-BODY] last100=' + body.substring(body.length - 100));
			console.log('[FLOW-BODY] isJSON=' + (body.charAt(0) === '{') + ' firstChar=' + body.charCodeAt(0));
			_goWriteFile('/tmp/turnstile_flow_body_original.txt', body);
		}
		// Auto-set Content-Type for string bodies (browser does this automatically)
		if (body && typeof body === 'string' && !this._headers['Content-Type'] && !this._headers['content-type']) {
			this._headers['Content-Type'] = 'text/plain;charset=UTF-8';
		}
		// Auto-add browser headers that real browsers set automatically on XHR
		if (!this._headers['Accept'] && !this._headers['accept']) {
			this._headers['Accept'] = '*/*';
		}
		if (!this._headers['Origin'] && !this._headers['origin']) {
			this._headers['Origin'] = this._overrideOrigin || location.origin;
		}
		if (!this._headers['Referer'] && !this._headers['referer']) {
			this._headers['Referer'] = this._overrideReferer || location.href;
		}
		// Sec-Fetch headers for XHR (not navigation)
		if (!this._headers['Sec-Fetch-Dest'] && !this._headers['sec-fetch-dest']) {
			this._headers['Sec-Fetch-Dest'] = 'empty';
		}
		if (!this._headers['Sec-Fetch-Mode'] && !this._headers['sec-fetch-mode']) {
			this._headers['Sec-Fetch-Mode'] = 'cors';
		}
		if (!this._headers['Sec-Fetch-Site'] && !this._headers['sec-fetch-site']) {
			// Compute sec-fetch-site based on request URL vs effective origin.
			// For iframe XHR, the effective origin is the iframe's origin.
			var _effOrigin = this._overrideOrigin || location.origin;
			var _reqOrigin = '';
			try { var _u = new URL(this._url); _reqOrigin = _u.origin; } catch(e) {}
			this._headers['Sec-Fetch-Site'] = (_reqOrigin === _effOrigin) ? 'same-origin' : 'cross-site';
		}
		if (!this._headers['Sec-Fetch-Storage-Access'] && !this._headers['sec-fetch-storage-access']) {
			this._headers['Sec-Fetch-Storage-Access'] = 'active';
		}
		// Convert Uint8Array/ArrayBuffer body to hex for lossless binary transfer to Go.
		// V8→Go string conversion uses UTF-8, which corrupts bytes >= 128 (single char → 2 bytes).
		// Hex encoding uses only ASCII chars [0-9a-f] which survive UTF-8 perfectly.
		var _sendBody = body || "";
		if (body && typeof body !== 'string') {
			var _bytes;
			if (body instanceof ArrayBuffer) { _bytes = new Uint8Array(body); }
			else if (body instanceof Uint8Array) { _bytes = body; }
			else if (body.buffer && body.buffer instanceof ArrayBuffer) { _bytes = new Uint8Array(body.buffer, body.byteOffset, body.byteLength); }
			if (_bytes) {
				var _hex = 'HEX:';
				var _lut = '0123456789abcdef';
				for (var _bi = 0; _bi < _bytes.length; _bi++) {
					_hex += _lut[_bytes[_bi] >> 4] + _lut[_bytes[_bi] & 0xf];
				}
				_sendBody = _hex;
			}
		}
		// Pass headers as 4th argument (JSON) so Go can forward them.
		// Use _safeStringify (original JSON.stringify) since challenge script may override it.
		var headersJSON = _safeStringify(this._headers);
		var resultJSON = _goSyncFetch(this._url, this._method, _sendBody, headersJSON);
		try {
			var result = JSON.parse(resultJSON);
			this.status = result.status || 0;
			this.statusText = this.status === 200 ? "OK" : "";
			this.responseText = result.body || "";
			this.responseURL = this._url;
			this._respHeaders = result.headers || {};
			this.readyState = 4;
			// Handle responseType: convert response to the requested type.
			if (this.responseType === 'arraybuffer') {
				// Convert the string body to an ArrayBuffer (each char → byte).
				var raw = this.responseText;
				var buf = new ArrayBuffer(raw.length);
				var view = new Uint8Array(buf);
				for (var _i = 0; _i < raw.length; _i++) {
					view[_i] = raw.charCodeAt(_i) & 0xFF;
				}
				this.response = buf;
				console.log('[DOM] XHR complete: status=' + this.status + ', body=' + raw.length + ' bytes (arraybuffer)');
			} else {
				this.response = this.responseText;
				console.log('[DOM] XHR complete: status=' + this.status + ', body=' + this.responseText.length + ' bytes');
			}
		} catch(e) {
			console.log('[DOM] XHR parse error: ' + e.message);
			this.status = 0;
			this.readyState = 4;
		}
		if (this.onreadystatechange) {
			// Don't catch errors here, let them propagate to the VM's own
			// try/catch so that error resilience patches can handle them.
			this.onreadystatechange();
		}
		if (this.status >= 200 && this.status < 300 && this.onload) {
			try { this.onload(); } catch(e) { console.log('[DOM] XHR onload error: ' + e.message); }
		}
		if (this.status === 0 && this.onerror) {
			try { this.onerror(); } catch(e) {}
		}
	};
	this.abort = function() { this.readyState = 0; };
	this.addEventListener = function(ev, fn) {
		if (ev === 'load') this.onload = fn;
		if (ev === 'error') this.onerror = fn;
		if (ev === 'readystatechange') this.onreadystatechange = fn;
	};
	this.removeEventListener = function() {};
	this.overrideMimeType = function() {};
};

// Blob, stores actual content for Worker blob URLs and other uses
let _bls = {};
let _blc = 0;
window.Blob = function(parts, opts) {
	this.type = (opts && opts.type) || "";
	// Store actual content so Worker can execute blob code
	var content = '';
	if (parts) {
		for (var i = 0; i < parts.length; i++) {
			var p = parts[i];
			if (typeof p === 'string') {
				content += p;
			} else if (p instanceof ArrayBuffer) {
				var bytes = new Uint8Array(p);
				for (var j = 0; j < bytes.length; j++) content += String.fromCharCode(bytes[j]);
			} else if (p && p.buffer instanceof ArrayBuffer) {
				// TypedArray or DataView
				var bytes = new Uint8Array(p.buffer, p.byteOffset || 0, p.byteLength || p.length);
				for (var j = 0; j < bytes.length; j++) content += String.fromCharCode(bytes[j]);
			} else if (p instanceof Blob) {
				content += p._content || '';
			}
		}
	}
	this._content = content;
	this.size = content.length;
	this.text = function() { return Promise.resolve(this._content); };
	this.arrayBuffer = function() {
		var enc = new TextEncoder();
		return Promise.resolve(enc.encode(this._content).buffer);
	};
	this.slice = function(start, end, type) {
		var sliced = this._content.slice(start || 0, end || this._content.length);
		return new Blob([sliced], {type: type || this.type});
	};
	this.stream = function() { return new ReadableStream(); };
};

// URLSearchParams, proper implementation for Turnstile URL parsing
window.URLSearchParams = function(init) {
	this._entries = [];
	if (typeof init === 'string') {
		var qs = init.charAt(0) === '?' ? init.substring(1) : init;
		var pairs = qs.split('&');
		for (var i = 0; i < pairs.length; i++) {
			if (!pairs[i]) continue;
			var eq = pairs[i].indexOf('=');
			if (eq === -1) { this._entries.push([decodeURIComponent(pairs[i]), '']); }
			else { this._entries.push([decodeURIComponent(pairs[i].substring(0, eq)), decodeURIComponent(pairs[i].substring(eq + 1))]); }
		}
	} else if (init && typeof init === 'object') {
		for (var k in init) { this._entries.push([k, String(init[k])]); }
	}
	this.get = function(k) { for (var i = 0; i < this._entries.length; i++) { if (this._entries[i][0] === k) return this._entries[i][1]; } return null; };
	this.getAll = function(k) { var r = []; for (var i = 0; i < this._entries.length; i++) { if (this._entries[i][0] === k) r.push(this._entries[i][1]); } return r; };
	this.set = function(k, v) { var found = false; for (var i = 0; i < this._entries.length; i++) { if (this._entries[i][0] === k) { if (!found) { this._entries[i][1] = String(v); found = true; } else { this._entries.splice(i, 1); i--; } } } if (!found) this._entries.push([k, String(v)]); };
	this.has = function(k) { for (var i = 0; i < this._entries.length; i++) { if (this._entries[i][0] === k) return true; } return false; };
	this.delete = function(k) { for (var i = 0; i < this._entries.length; i++) { if (this._entries[i][0] === k) { this._entries.splice(i, 1); i--; } } };
	this.append = function(k, v) { this._entries.push([k, String(v)]); };
	this.forEach = function(fn) { for (var i = 0; i < this._entries.length; i++) fn(this._entries[i][1], this._entries[i][0], this); };
	this.keys = function() { return this._entries.map(function(e) { return e[0]; }); };
	this.values = function() { return this._entries.map(function(e) { return e[1]; }); };
	this.entries = function() { return this._entries.slice(); };
	this.toString = function() { return this._entries.map(function(e) { return encodeURIComponent(e[0]) + '=' + encodeURIComponent(e[1]); }).join('&'); };
	Object.defineProperty(this, 'size', { get: function() { return this._entries.length; } });
};

// URL constructor, proper implementation for Turnstile and CF scripts
window.URL = function(url, base) {
	if (base && typeof url === 'string' && url.indexOf('://') === -1) {
		// Resolve relative URLs
		if (url.charAt(0) === '/') {
			var m = base.match(/^(https?:\/\/[^\/]+)/);
			url = (m ? m[1] : '') + url;
		} else {
			url = base.replace(/[^\/]*$/, '') + url;
		}
	}
	url = String(url);
	this.href = url;
	// Parse protocol
	var protoEnd = url.indexOf('://');
	this.protocol = protoEnd !== -1 ? url.substring(0, protoEnd + 1) : 'https:';
	// Parse host
	var rest = protoEnd !== -1 ? url.substring(protoEnd + 3) : url;
	var pathStart = rest.indexOf('/');
	var hostPart = pathStart !== -1 ? rest.substring(0, pathStart) : rest;
	this.host = hostPart;
	var colonIdx = hostPart.indexOf(':');
	if (colonIdx !== -1 && hostPart.indexOf(']') < colonIdx) {
		this.hostname = hostPart.substring(0, colonIdx);
		this.port = hostPart.substring(colonIdx + 1);
	} else {
		this.hostname = hostPart;
		this.port = '';
	}
	this.origin = this.protocol + '//' + this.host;
	// Parse path, search, hash
	var pathAndRest = pathStart !== -1 ? rest.substring(pathStart) : '/';
	var hashIdx = pathAndRest.indexOf('#');
	if (hashIdx !== -1) {
		this.hash = pathAndRest.substring(hashIdx);
		pathAndRest = pathAndRest.substring(0, hashIdx);
	} else {
		this.hash = '';
	}
	var searchIdx = pathAndRest.indexOf('?');
	if (searchIdx !== -1) {
		this.search = pathAndRest.substring(searchIdx);
		this.pathname = pathAndRest.substring(0, searchIdx);
	} else {
		this.search = '';
		this.pathname = pathAndRest;
	}
	this.searchParams = new URLSearchParams(this.search);
	this.username = '';
	this.password = '';
	this.toString = function() { return this.href; };
	this.toJSON = function() { return this.href; };
};
window.URL.createObjectURL = function(obj) {
	var url = "blob:null/" + (++_blc);
	if (obj && obj._content !== undefined) {
		_bls[url] = obj._content;
		console.log('[DOM] createObjectURL: stored ' + obj._content.length + ' chars as ' + url);
	}
	return url;
};
window.URL.revokeObjectURL = function() {};

// Worker, runs blob code in a SEPARATE V8 Isolate (real threading).
// In Chrome, Workers run on separate OS threads. The Turnstile VM detects
// synchronous (fake) workers by scheduling a timer before the Worker starts
// PoW and checking if it fires during Worker execution. With real threading,
// the main event loop continues (timers fire) while the Worker computes.
window.Worker = function(url) {
	var mainWorker = this;
	this._url = url;
	this.onmessage = null;
	this.onerror = null;

	// Get blob code from store (populated by URL.createObjectURL)
	var blobCode = _bls[url] || '';
	console.log('[WORKER] new Worker(' + url + ') blobCode=' + blobCode.length + ' chars');
	if (blobCode.length > 0) {
		console.log('[WORKER] code preview: ' + blobCode.substring(0, 300));
	}

	// Set up the callback that the Go event loop will invoke when the
	// worker thread posts a message back to the main thread.
	// This is called from engine.go's deliverWorkerMessages().
	window.__workerOnMessage = function(data) {
		console.log('[WORKER] worker→main message received (' + typeof data + ')');
		if (mainWorker.onmessage) {
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
			mainWorker.onmessage(evt);
		}
	};

	// Start the worker in a separate V8 Isolate via Go callback.
	// The blob code will be executed in a goroutine with its own Isolate+Context.
	if (blobCode) {
		_goCreateWorker(blobCode);
	}

	this.postMessage = function(data, transfer) {
		// Main → Worker: send data to the worker thread via Go channel.
		var dataStr = (typeof data === 'string') ? data : JSON.stringify(data);
		var dataPreview = dataStr.substring(0, 300);
		console.log('[WORKER] main→worker postMessage (' + dataStr.length + ' chars): ' + dataPreview);
		_goWorkerPostMessage(dataStr);
	};
	this.terminate = function() { console.log('[WORKER] terminated'); };
	this.addEventListener = function(ev, fn) {
		if (ev === 'message') this.onmessage = fn;
		if (ev === 'error') this.onerror = fn;
	};
	this.removeEventListener = function() {};
};

// ReadableStream stub, CF script checks ReadableStream.prototype.pipeTo existence
window.ReadableStream = function(source) {
	this.locked = false;
	this.cancel = function() { return Promise.resolve(); };
	this.getReader = function() {
		return {
			read: function() { return Promise.resolve({done: true, value: undefined}); },
			releaseLock: function() {},
			cancel: function() { return Promise.resolve(); }
		};
	};
	this.pipeThrough = function(transform) { return transform.readable || new ReadableStream(); };
	this.pipeTo = function(dest) { return Promise.resolve(); };
	this.tee = function() { return [new ReadableStream(), new ReadableStream()]; };
};
window.ReadableStream.prototype.pipeTo = function(dest) { return Promise.resolve(); };
window.WritableStream = function() {
	this.locked = false;
	this.getWriter = function() {
		return {
			write: function() { return Promise.resolve(); },
			close: function() { return Promise.resolve(); },
			abort: function() { return Promise.resolve(); },
			releaseLock: function() {}
		};
	};
};
window.TransformStream = function() {
	this.readable = new ReadableStream();
	this.writable = new WritableStream();
};

// Proxy for any missing globals
window.Reflect = typeof Reflect !== "undefined" ? Reflect : {};
window.Symbol = typeof Symbol !== "undefined" ? Symbol : function(desc) { return "@@" + desc; };
window.WeakRef = typeof WeakRef !== "undefined" ? WeakRef : function(obj) { this.deref = function() { return obj; }; };
window.FinalizationRegistry = typeof FinalizationRegistry !== "undefined" ? FinalizationRegistry : function() { this.register = function() {}; };
window.AbortController = function() { this.signal = {aborted: false, addEventListener: function(){}, removeEventListener: function(){}}; this.abort = function() { this.signal.aborted = true; }; };

// MutationObserver, real implementation for React 18 createRoot
window.MutationObserver = function(callback) {
	this._callback = callback;
	this._targets = [];
	this._records = [];
	this._scheduled = false;
	_domMutationObservers.push(this);
	this.observe = function(target, options) {
		this._targets.push({target: target, options: options || {}});
	};
	this.disconnect = function() {
		this._targets = [];
		var idx = _domMutationObservers.indexOf(this);
		if (idx !== -1) _domMutationObservers.splice(idx, 1);
	};
	this.takeRecords = function() {
		return this._records.splice(0);
	};
};
if (typeof _mkFnNat === 'function') _mkFnNat(MutationObserver, 'MutationObserver');
window.IntersectionObserver = function(callback, options) {
	this._callback = callback;
	this.observe = function(target) {
		// Fire callback immediately, element is "visible"
		var entry = {
			target: target,
			isIntersecting: true,
			intersectionRatio: 1.0,
			boundingClientRect: {x:0, y:0, width:300, height:65, top:0, right:300, bottom:65, left:0},
			intersectionRect: {x:0, y:0, width:300, height:65, top:0, right:300, bottom:65, left:0},
			rootBounds: {x:0, y:0, width:1920, height:1080, top:0, right:1920, bottom:1080, left:0},
			time: performance.now()
		};
		var cb = this._callback;
		setTimeout(function() { cb([entry]); }, 0);
	};
	this.unobserve = function() {};
	this.disconnect = function() {};
};
window.ResizeObserver = function(callback) {
	this._callback = callback;
	this.observe = function(target) {
		var cb = this._callback;
		var entry = {
			target: target,
			contentRect: {x:0, y:0, width:300, height:65, top:0, right:300, bottom:65, left:0},
			borderBoxSize: [{inlineSize: 300, blockSize: 65}],
			contentBoxSize: [{inlineSize: 300, blockSize: 65}],
			devicePixelContentBoxSize: [{inlineSize: 600, blockSize: 130}]
		};
		setTimeout(function() { cb([entry]); }, 0);
	};
	this.unobserve = function() {};
	this.disconnect = function() {};
};

// ============= WebGL Context =============
// Define proper WebGLRenderingContext/WebGL2RenderingContext classes
// so that instanceof checks work and 'class extends WebGLRenderingContext' succeeds.
(function() {
	function _wglCtor() { throw new TypeError("Illegal constructor"); }
	Object.defineProperty(_wglCtor, 'name', { value: 'WebGLRenderingContext', configurable: true });
	_wglCtor.prototype = {};
	_wglCtor.prototype.constructor = _wglCtor;
	Object.defineProperty(_wglCtor.prototype, Symbol.toStringTag, { value: 'WebGLRenderingContext', configurable: true });
	window.WebGLRenderingContext = _wglCtor;
	window._WebGL1Proto = _wglCtor.prototype;

	function _wgl2Ctor() { throw new TypeError("Illegal constructor"); }
	Object.defineProperty(_wgl2Ctor, 'name', { value: 'WebGL2RenderingContext', configurable: true });
	_wgl2Ctor.prototype = Object.create(_wglCtor.prototype);
	_wgl2Ctor.prototype.constructor = _wgl2Ctor;
	Object.defineProperty(_wgl2Ctor.prototype, Symbol.toStringTag, { value: 'WebGL2RenderingContext', configurable: true });
	window.WebGL2RenderingContext = _wgl2Ctor;
	window._WebGL2Proto = _wgl2Ctor.prototype;
})();

let _mkWGL = function(canvas, isV2) {
	var extensions = [
		"ANGLE_instanced_arrays", "EXT_blend_minmax", "EXT_clip_control",
		"EXT_color_buffer_half_float", "EXT_depth_clamp",
		"EXT_disjoint_timer_query", "EXT_float_blend", "EXT_frag_depth",
		"EXT_polygon_offset_clamp", "EXT_shader_texture_lod",
		"EXT_texture_compression_bptc", "EXT_texture_compression_rgtc",
		"EXT_texture_filter_anisotropic", "EXT_texture_mirror_clamp_to_edge",
		"EXT_sRGB", "KHR_parallel_shader_compile", "OES_element_index_uint",
		"OES_fbo_render_mipmap", "OES_standard_derivatives", "OES_texture_float",
		"OES_texture_float_linear", "OES_texture_half_float",
		"OES_texture_half_float_linear", "OES_vertex_array_object",
		"WEBGL_blend_func_extended", "WEBGL_color_buffer_float",
		"WEBGL_compressed_texture_astc", "WEBGL_compressed_texture_etc",
		"WEBGL_compressed_texture_etc1", "WEBGL_compressed_texture_pvrtc",
		"WEBGL_compressed_texture_s3tc", "WEBGL_compressed_texture_s3tc_srgb",
		"WEBGL_debug_renderer_info", "WEBGL_debug_shaders", "WEBGL_depth_texture",
		"WEBGL_draw_buffers", "WEBGL_lose_context", "WEBGL_multi_draw",
		"WEBGL_polygon_mode"
	];

	var _rawCtx = {
		getParameter: function(param) {
			if (param === undefined || param === null) return 0;
			var _glParamNames = {
				0x1F01: 'VENDOR', 0x1F00: 'RENDERER', 0x1F02: 'VERSION',
				0x8B8C: 'SHADING_LANGUAGE_VERSION', 0x9245: 'UNMASKED_VENDOR_WEBGL',
				0x9246: 'UNMASKED_RENDERER_WEBGL', 0x0D33: 'MAX_TEXTURE_SIZE',
				0x851C: 'MAX_CUBE_MAP_TEXTURE_SIZE', 0x8869: 'MAX_VERTEX_ATTRIBS',
				0x8DFB: 'MAX_VARYING_VECTORS', 0x8872: 'MAX_TEXTURE_IMAGE_UNITS',
				0x8B4C: 'MAX_VERTEX_UNIFORM_VECTORS', 0x8B49: 'MAX_FRAGMENT_UNIFORM_VECTORS',
				0x0B71: 'DEPTH_BITS', 0x0D55: 'STENCIL_BITS', 0x0BE2: 'BLEND',
				0x84E8: 'MAX_RENDERBUFFER_SIZE', 0x0BA2: 'VIEWPORT',
				0x0D3A: 'ALIASED_LINE_WIDTH_RANGE', 0x846D: 'ALIASED_POINT_SIZE_RANGE',
				0x8D57: 'MAX_COMBINED_TEXTURE_IMAGE_UNITS', 0x8B4A: 'MAX_VERTEX_TEXTURE_IMAGE_UNITS'
			};
			var result;
			switch (param) {
				// String parameters, Chrome 146 captured from real Turnstile iframe
				case 0x1F01: result = "WebKit"; break; // VENDOR
				case 0x1F00: result = "WebKit WebGL"; break; // RENDERER
				case 0x1F02: result = isV2 ? "WebGL 2.0 (OpenGL ES 3.0 Chromium)" : "WebGL 1.0 (OpenGL ES 2.0 Chromium)"; break; // VERSION
				case 0x8B8C: result = isV2 ? "WebGL GLSL ES 3.00 (OpenGL ES GLSL ES 3.0 Chromium)" : "WebGL GLSL ES 1.0 (OpenGL ES GLSL ES 1.0 Chromium)"; break; // SHADING_LANGUAGE_VERSION
				case 0x9245: result = "Google Inc. (Apple)"; break; // UNMASKED_VENDOR_WEBGL
				case 0x9246: result = "ANGLE (Apple, ANGLE Metal Renderer: Apple M2 Pro, Unspecified Version)"; break; // UNMASKED_RENDERER_WEBGL
				// Texture/buffer limits, Chrome 146 M2 Pro
				case 0x0D33: result = 16384; break; // MAX_TEXTURE_SIZE
				case 0x851C: result = 16384; break; // MAX_CUBE_MAP_TEXTURE_SIZE
				case 0x84E8: result = 16384; break; // MAX_RENDERBUFFER_SIZE
				case 0x8869: result = 16; break; // MAX_VERTEX_ATTRIBS
				case 0x8B4C: result = 1024; break; // MAX_VERTEX_UNIFORM_VECTORS
				case 0x8B49: result = 1024; break; // MAX_FRAGMENT_UNIFORM_VECTORS
				case 0x8DFB: result = 30; break; // MAX_VARYING_VECTORS
				case 0x8D57: result = 32; break; // MAX_COMBINED_TEXTURE_IMAGE_UNITS
				case 0x8872: result = 16; break; // MAX_TEXTURE_IMAGE_UNITS
				case 0x8B4A: result = 16; break; // MAX_VERTEX_TEXTURE_IMAGE_UNITS
				case 0x8824: result = 8; break; // MAX_DRAW_BUFFERS
				// Bit depths, Chrome 146
				case 0x0B71: result = 24; break; // DEPTH_BITS
				case 0x0D55: result = 0; break; // STENCIL_BITS
				case 0x0B72: result = 8; break; // RED_BITS
				case 0x0B73: result = 8; break; // GREEN_BITS
				case 0x0B74: result = 8; break; // BLUE_BITS
				case 0x0B75: result = 8; break; // ALPHA_BITS
				case 0x0D50: result = 4; break; // SUBPIXEL_BITS
				// Antialiasing, antialiasing=true in Chrome 146 capture
				case 0x0D56: result = 1; break; // SAMPLE_BUFFERS (1 when antialiasing enabled)
				case 0x0D57: result = 4; break; // SAMPLES (4 when antialiasing enabled)
				// State queries
				case 0x0BE2: result = false; break; // BLEND (default disabled)
				case 0x846E: result = 16; break; // MAX_TEXTURE_MAX_ANISOTROPY
				// Array/typed-array parameters
				case 0x0BA2: result = new Int32Array([0, 0, 300, 150]); break; // VIEWPORT
				case 0x0D3A: result = new Float32Array([1, 1]); break; // ALIASED_LINE_WIDTH_RANGE
				case 0x846D: result = new Float32Array([1, 511]); break; // ALIASED_POINT_SIZE_RANGE
				case 0x0BA6: result = new Int32Array([16384, 16384]); break; // MAX_VIEWPORT_DIMS
				case 0x0C10: result = new Float32Array([0, 0, 0, 0]); break; // SCISSOR_BOX
				// WebGL2 parameters (Kasada checks these for GPU fingerprinting)
				case 0x8A2F: result = 4096; break; // MAX_COMBINED_VERTEX_UNIFORM_COMPONENTS
				case 0x8A30: result = 4096; break; // MAX_COMBINED_FRAGMENT_UNIFORM_COMPONENTS
				case 0x8A2B: result = 16384; break; // MAX_UNIFORM_BUFFER_BINDINGS
				case 0x8A2C: result = 65536; break; // MAX_UNIFORM_BLOCK_SIZE
				case 0x8C8A: result = 256; break; // MAX_TRANSFORM_FEEDBACK_SEPARATE_ATTRIBS
				case 0x8C8B: result = 4; break; // MAX_TRANSFORM_FEEDBACK_INTERLEAVED_COMPONENTS
				case 0x8C8C: result = 4; break; // MAX_TRANSFORM_FEEDBACK_SEPARATE_COMPONENTS
				case 0x9122: result = 16; break; // MAX_SERVER_WAIT_TIMEOUT
				default: result = 0; break; // Return 0 instead of null - VM calls .toString() on result
			}
			return result;
		},
		getExtension: function(name) {
			if (name === "WEBGL_debug_renderer_info") {
				return {UNMASKED_VENDOR_WEBGL: 0x9245, UNMASKED_RENDERER_WEBGL: 0x9246};
			}
			if (extensions.indexOf(name) !== -1) {
				// Return simple extension stub (no Proxy, detectable as non-native)
				var ext = {
					drawArraysInstancedANGLE: function(){}, drawElementsInstancedANGLE: function(){},
					vertexAttribDivisorANGLE: function(){}, loseContext: function(){}, restoreContext: function(){}
				};
				// Add common GL constants for extension queries
				if (name === 'ANGLE_instanced_arrays') { ext.VERTEX_ATTRIB_ARRAY_DIVISOR_ANGLE = 0x88FE; }
				if (name === 'EXT_texture_filter_anisotropic') { ext.MAX_TEXTURE_MAX_ANISOTROPY_EXT = 0x84FF; ext.TEXTURE_MAX_ANISOTROPY_EXT = 0x84FE; }
				if (name === 'OES_element_index_uint') { /* no constants */ }
				if (name === 'OES_standard_derivatives') { ext.FRAGMENT_SHADER_DERIVATIVE_HINT_OES = 0x8B8B; }
				if (name === 'OES_vertex_array_object') { ext.VERTEX_ARRAY_BINDING_OES = 0x85B5; ext.createVertexArrayOES = function(){return {};}; ext.deleteVertexArrayOES = function(){}; ext.isVertexArrayOES = function(){return false;}; ext.bindVertexArrayOES = function(){}; }
				if (name === 'WEBGL_compressed_texture_s3tc') { ext.COMPRESSED_RGB_S3TC_DXT1_EXT = 0x83F0; ext.COMPRESSED_RGBA_S3TC_DXT1_EXT = 0x83F1; ext.COMPRESSED_RGBA_S3TC_DXT3_EXT = 0x83F2; ext.COMPRESSED_RGBA_S3TC_DXT5_EXT = 0x83F3; }
				return ext;
			}
			return null;
		},
		getSupportedExtensions: function() { return extensions; },
		createBuffer: function() { return {}; },
		createFramebuffer: function() { return {}; },
		createProgram: function() { return {}; },
		createRenderbuffer: function() { return {}; },
		createShader: function(type) { return {}; },
		createTexture: function() { return {}; },
		shaderSource: function() {},
		compileShader: function() {},
		getShaderParameter: function() { return true; },
		attachShader: function() {},
		linkProgram: function() {},
		getProgramParameter: function() { return true; },
		useProgram: function() {},
		bindBuffer: function() {},
		bufferData: function() {},
		enableVertexAttribArray: function() {},
		vertexAttribPointer: function() {},
		getAttribLocation: function() { return 0; },
		getUniformLocation: function() { return {}; },
		uniformMatrix4fv: function() {},
		uniform1f: function() {},
		uniform1i: function() {},
		uniform2f: function() {},
		uniform3f: function() {},
		uniform4f: function() {},
		drawArrays: function() {},
		drawElements: function() {},
		viewport: function() {},
		clear: function() {},
		clearColor: function() {},
		enable: function() {},
		disable: function() {},
		blendFunc: function() {},
		depthFunc: function() {},
		activeTexture: function() {},
		bindTexture: function() {},
		texImage2D: function() {},
		texParameteri: function() {},
		readPixels: function(x, y, w, h, format, type, pixels) {
			for (var i = 0; i < pixels.length; i++) pixels[i] = 0;
			console.log('[VMTRACE] webgl.readPixels(' + x + ',' + y + ',' + w + ',' + h + ') format=0x' + format.toString(16) + ' pixelCount=' + pixels.length);
		},
		getShaderInfoLog: function() { return ""; },
		getProgramInfoLog: function() { return ""; },
		deleteShader: function() {},
		deleteProgram: function() {},
		deleteBuffer: function() {},
		deleteTexture: function() {},
		deleteFramebuffer: function() {},
		deleteRenderbuffer: function() {},
		getContextAttributes: function() {
			return {alpha: true, antialias: true, depth: true, failIfMajorPerformanceCaveat: false,
				desynchronized: false, powerPreference: 'default', premultipliedAlpha: true,
				preserveDrawingBuffer: false, stencil: false, xrCompatible: false};
		},
		isContextLost: function() { return false; },
		getError: function() { return 0; },
		pixelStorei: function() {},
		scissor: function() {},
		colorMask: function() {},
		depthMask: function() {},
		stencilMask: function() {},
		stencilFunc: function() {},
		stencilOp: function() {},
		// Missing WebGL1 methods that Kasada may fingerprint
		hint: function() {},
		isBuffer: function() { return false; },
		isFramebuffer: function() { return false; },
		isProgram: function() { return false; },
		isRenderbuffer: function() { return false; },
		isShader: function() { return false; },
		isTexture: function() { return false; },
		blendColor: function() {},
		blendEquation: function() {},
		blendEquationSeparate: function() {},
		blendFuncSeparate: function() {},
		copyTexImage2D: function() {},
		copyTexSubImage2D: function() {},
		cullFace: function() {},
		frontFace: function() {},
		getUniform: function() { return 0; },
		stencilFuncSeparate: function() {},
		stencilMaskSeparate: function() {},
		stencilOpSeparate: function() {},
		texSubImage2D: function() {},
		uniform1fv: function() {}, uniform2fv: function() {}, uniform3fv: function() {}, uniform4fv: function() {},
		uniform1iv: function() {}, uniform2iv: function() {}, uniform3iv: function() {}, uniform4iv: function() {},
		vertexAttrib1f: function() {}, vertexAttrib2f: function() {}, vertexAttrib3f: function() {}, vertexAttrib4f: function() {},
		vertexAttrib1fv: function() {}, vertexAttrib2fv: function() {}, vertexAttrib3fv: function() {}, vertexAttrib4fv: function() {},
		drawingBufferWidth: 300,
		drawingBufferHeight: 150,
		drawingBufferColorSpace: 'srgb',
		lineWidth: function() {},
		polygonOffset: function() {},
		sampleCoverage: function() {},
		bindFramebuffer: function() {},
		bindRenderbuffer: function() {},
		renderbufferStorage: function() {},
		framebufferTexture2D: function() {},
		framebufferRenderbuffer: function() {},
		checkFramebufferStatus: function() { return 0x8CD5; },
		generateMipmap: function() {},
		flush: function() {},
		finish: function() {},
		getActiveAttrib: function() { return {name: 'a', size: 1, type: 0x1406}; },
		getActiveUniform: function() { return {name: 'u', size: 1, type: 0x1406}; },
		canvas: {width: 1, height: 1},
		getAttachedShaders: function() { return []; },
		getShaderPrecisionFormat: function() { return {rangeMin: 127, rangeMax: 127, precision: 23}; },
		getProgramParameter: function(p, pname) { return pname === 0x8B82 ? true : 0; },
		getShaderParameter: function(s, pname) { return pname === 0x8B81 ? true : ''; },
		getShaderInfoLog: function() { return ''; },
		getProgramInfoLog: function() { return ''; },
		getUniformLocation: function() { return {}; },
		getAttribLocation: function() { return 0; },
		uniform1f: function(){}, uniform2f: function(){}, uniform3f: function(){}, uniform4f: function(){},
		uniform1i: function(){}, uniform2i: function(){}, uniform3i: function(){}, uniform4i: function(){},
		uniformMatrix2fv: function(){}, uniformMatrix3fv: function(){}, uniformMatrix4fv: function(){},
		vertexAttribPointer: function(){}, enableVertexAttribArray: function(){},
		drawArrays: function(){}, drawElements: function(){},
		useProgram: function(){}, linkProgram: function(){},
		attachShader: function(){}, compileShader: function(){}, shaderSource: function(){},
		getBufferParameter: function() { return 0; },
		getRenderbufferParameter: function() { return 0; },
		getFramebufferAttachmentParameter: function() { return 0; },
		getTexParameter: function() { return 0; },
		getVertexAttrib: function() { return 0; },
		getVertexAttribOffset: function() { return 0; },
		isEnabled: function() { return false; },
		readPixels: function() {},
		// Missing WebGL2 methods
		getBufferSubData: function() {},
		texStorage2D: function() {},
		texStorage3D: function() {},
		texImage3D: function() {},
		texSubImage3D: function() {},
		copyTexSubImage3D: function() {},
		compressedTexImage3D: function() {},
		compressedTexSubImage3D: function() {},
		getQuery: function() { return null; },
		isQuery: function() { return false; },
		isSampler: function() { return false; },
		isSync: function() { return false; },
		isTransformFeedback: function() { return false; },
		isVertexArray: function() { return false; },
		createVertexArray: function() { return {}; },
		deleteVertexArray: function() {},
		bindVertexArray: function() {},
		uniform1ui: function(){}, uniform2ui: function(){}, uniform3ui: function(){}, uniform4ui: function(){},
		uniform1uiv: function(){}, uniform2uiv: function(){}, uniform3uiv: function(){}, uniform4uiv: function(){},
		uniform1fv: function(){}, uniform2fv: function(){}, uniform3fv: function(){}, uniform4fv: function(){},
		uniform1iv: function(){}, uniform2iv: function(){}, uniform3iv: function(){}, uniform4iv: function(){},
		uniformMatrix2x3fv: function(){}, uniformMatrix3x2fv: function(){},
		uniformMatrix2x4fv: function(){}, uniformMatrix4x2fv: function(){},
		uniformMatrix3x4fv: function(){}, uniformMatrix4x3fv: function(){},
		vertexAttribI4i: function(){}, vertexAttribI4iv: function(){},
		vertexAttribI4ui: function(){}, vertexAttribI4uiv: function(){},
		vertexAttribIPointer: function(){},
		drawArraysInstanced: function(){}, drawElementsInstanced: function(){},
		drawRangeElements: function(){},
		drawBuffers: function(){},
		clearBufferfv: function(){}, clearBufferiv: function(){}, clearBufferuiv: function(){}, clearBufferfi: function(){},
		blitFramebuffer: function(){},
		renderbufferStorageMultisample: function(){},
		framebufferTextureLayer: function(){},
		invalidateFramebuffer: function(){}, invalidateSubFramebuffer: function(){},
		readBuffer: function(){},
		getFragDataLocation: function() { return 0; },
		getUniformBlockIndex: function() { return 0; },
		uniformBlockBinding: function(){},
		getActiveUniformBlockParameter: function() { return 0; },
		getActiveUniformBlockName: function() { return ''; },
		getActiveUniforms: function() { return new Uint32Array(0); },
		transformFeedbackVaryings: function(){},
		getTransformFeedbackVarying: function() { return {name:'',size:0,type:0}; },
		pauseTransformFeedback: function(){},
		resumeTransformFeedback: function(){},
		bindBufferBase: function(){}, bindBufferRange: function(){},
		getUniformIndices: function() { return new Uint32Array(0); },
		drawingBufferWidth: 300,
		drawingBufferHeight: 150,
		drawingBufferColorSpace: 'srgb',
		// WebGL2-specific methods
		getIndexedParameter: function() { return null; },
		getInternalformatParameter: function() { return new Int32Array(0); },
		fenceSync: function() { return {}; },
		deleteSync: function() {},
		clientWaitSync: function() { return 0; },
		waitSync: function() {},
		getSyncParameter: function() { return 0; },
		createTransformFeedback: function() { return {}; },
		bindTransformFeedback: function() {},
		beginTransformFeedback: function() {},
		endTransformFeedback: function() {},
		createQuery: function() { return {}; },
		deleteQuery: function() {},
		beginQuery: function() {},
		endQuery: function() {},
		getQueryParameter: function() { return 0; },
		createSampler: function() { return {}; },
		deleteSampler: function() {},
		bindSampler: function() {},
		samplerParameteri: function() {},
		samplerParameterf: function() {},
		getSamplerParameter: function() { return 0; }
	};
	// Set proper prototype chain so instanceof checks work:
	// WebGL enum constants, Chrome exposes these on every context instance.
	// Without them, gl.RENDERER === undefined and getParameter(gl.RENDERER) returns 0.
	var _glConsts = {
		DEPTH_BUFFER_BIT:0x0100,STENCIL_BUFFER_BIT:0x0400,COLOR_BUFFER_BIT:0x4000,
		POINTS:0,LINES:1,LINE_LOOP:2,LINE_STRIP:3,TRIANGLES:4,TRIANGLE_STRIP:5,TRIANGLE_FAN:6,
		ZERO:0,ONE:1,SRC_COLOR:0x0300,ONE_MINUS_SRC_COLOR:0x0301,SRC_ALPHA:0x0302,ONE_MINUS_SRC_ALPHA:0x0303,DST_ALPHA:0x0304,ONE_MINUS_DST_ALPHA:0x0305,DST_COLOR:0x0306,ONE_MINUS_DST_COLOR:0x0307,SRC_ALPHA_SATURATE:0x0308,
		FUNC_ADD:0x8006,BLEND_EQUATION:0x8009,BLEND_EQUATION_RGB:0x8009,BLEND_EQUATION_ALPHA:0x883D,FUNC_SUBTRACT:0x800A,FUNC_REVERSE_SUBTRACT:0x800B,
		BLEND_DST_RGB:0x80C8,BLEND_SRC_RGB:0x80C9,BLEND_DST_ALPHA:0x80CA,BLEND_SRC_ALPHA:0x80CB,CONSTANT_COLOR:0x8001,ONE_MINUS_CONSTANT_COLOR:0x8002,CONSTANT_ALPHA:0x8003,ONE_MINUS_CONSTANT_ALPHA:0x8004,BLEND_COLOR:0x8005,
		ARRAY_BUFFER:0x8892,ELEMENT_ARRAY_BUFFER:0x8893,ARRAY_BUFFER_BINDING:0x8894,ELEMENT_ARRAY_BUFFER_BINDING:0x8895,
		STREAM_DRAW:0x88E0,STATIC_DRAW:0x88E4,DYNAMIC_DRAW:0x88E8,BUFFER_SIZE:0x8764,BUFFER_USAGE:0x8765,
		CURRENT_VERTEX_ATTRIB:0x8626,
		FRONT:0x0404,BACK:0x0405,FRONT_AND_BACK:0x0408,
		CULL_FACE:0x0B44,BLEND:0x0BE2,DITHER:0x0BD0,STENCIL_TEST:0x0B90,DEPTH_TEST:0x0B71,SCISSOR_TEST:0x0C11,POLYGON_OFFSET_FILL:0x8037,SAMPLE_ALPHA_TO_COVERAGE:0x809E,SAMPLE_COVERAGE:0x80A0,
		NO_ERROR:0,INVALID_ENUM:0x0500,INVALID_VALUE:0x0501,INVALID_OPERATION:0x0502,OUT_OF_MEMORY:0x0505,
		CW:0x0900,CCW:0x0901,LINE_WIDTH:0x0B21,ALIASED_POINT_SIZE_RANGE:0x846D,ALIASED_LINE_WIDTH_RANGE:0x0D3A,CULL_FACE_MODE:0x0B45,FRONT_FACE:0x0B46,
		DEPTH_RANGE:0x0B70,DEPTH_WRITEMASK:0x0B72,DEPTH_CLEAR_VALUE:0x0B73,DEPTH_FUNC:0x0B74,STENCIL_CLEAR_VALUE:0x0B91,
		DEPTH_BITS:0x0D56,STENCIL_BITS:0x0D57,
		KEEP:0x1E00,REPLACE:0x1E01,INCR:0x1E02,DECR:0x1E03,INVERT:0x150A,INCR_WRAP:0x8507,DECR_WRAP:0x8508,
		VENDOR:0x1F01,RENDERER:0x1F00,VERSION:0x1F02,
		NEAREST:0x2600,LINEAR:0x2601,
		NEAREST_MIPMAP_NEAREST:0x2700,LINEAR_MIPMAP_NEAREST:0x2701,NEAREST_MIPMAP_LINEAR:0x2702,LINEAR_MIPMAP_LINEAR:0x2703,
		TEXTURE_MAG_FILTER:0x2800,TEXTURE_MIN_FILTER:0x2801,TEXTURE_WRAP_S:0x2802,TEXTURE_WRAP_T:0x2803,
		TEXTURE_2D:0x0DE1,TEXTURE:0x1702,TEXTURE_CUBE_MAP:0x8513,
		TEXTURE0:0x84C0,ACTIVE_TEXTURE:0x84E0,
		REPEAT:0x2901,CLAMP_TO_EDGE:0x812F,MIRRORED_REPEAT:0x8370,
		FLOAT:0x1406,UNSIGNED_BYTE:0x1401,UNSIGNED_SHORT:0x1403,
		VERTEX_SHADER:0x8B31,FRAGMENT_SHADER:0x8B30,
		MAX_VERTEX_ATTRIBS:0x8869,MAX_VERTEX_UNIFORM_VECTORS:0x8B4B,MAX_VARYING_VECTORS:0x8DFC,MAX_COMBINED_TEXTURE_IMAGE_UNITS:0x8B4D,MAX_VERTEX_TEXTURE_IMAGE_UNITS:0x8B4C,MAX_TEXTURE_IMAGE_UNITS:0x8872,MAX_FRAGMENT_UNIFORM_VECTORS:0x8B49,
		SHADING_LANGUAGE_VERSION:0x8B8C,
		COMPILE_STATUS:0x8B81,LINK_STATUS:0x8B82,VALIDATE_STATUS:0x8B83,
		ATTACHED_SHADERS:0x8B85,ACTIVE_UNIFORMS:0x8B86,ACTIVE_ATTRIBUTES:0x8B89,
		RGBA:0x1908,RGB:0x1907,ALPHA:0x1906,LUMINANCE:0x1909,LUMINANCE_ALPHA:0x190A,
		UNSIGNED_SHORT_4_4_4_4:0x8033,UNSIGNED_SHORT_5_5_5_1:0x8034,UNSIGNED_SHORT_5_6_5:0x8363,
		FRAMEBUFFER:0x8D40,RENDERBUFFER:0x8D41,
		RGBA4:0x8056,RGB5_A1:0x8057,RGB565:0x8D62,DEPTH_COMPONENT16:0x81A5,STENCIL_INDEX8:0x8D48,
		COLOR_ATTACHMENT0:0x8CE0,DEPTH_ATTACHMENT:0x8D00,STENCIL_ATTACHMENT:0x8D20,DEPTH_STENCIL_ATTACHMENT:0x821A,
		FRAMEBUFFER_COMPLETE:0x8CD5,
		NONE:0,VIEWPORT:0x0BA2,SCISSOR_BOX:0x0C10,COLOR_CLEAR_VALUE:0x0C22,
		UNPACK_FLIP_Y_WEBGL:0x9240,UNPACK_PREMULTIPLY_ALPHA_WEBGL:0x9241,UNPACK_COLORSPACE_CONVERSION_WEBGL:0x9243,
		MAX_TEXTURE_SIZE:0x0D33,MAX_CUBE_MAP_TEXTURE_SIZE:0x851C,MAX_RENDERBUFFER_SIZE:0x84E8,
		HIGH_FLOAT:0x8DF2,MEDIUM_FLOAT:0x8DF1,LOW_FLOAT:0x8DF0,HIGH_INT:0x8DF5,MEDIUM_INT:0x8DF4,LOW_INT:0x8DF3,
		MAX_VIEWPORT_DIMS:0x0D3A
	};
	for (var k in _glConsts) _rawCtx[k] = _glConsts[k];

	// Move all methods and constants to the prototype (Chrome has them there, not on instance).
	// The VM checks Object.getPrototypeOf(gl) for methods.
	var _proto = isV2 ? window._WebGL2Proto : window._WebGL1Proto;
	if (_proto) {
		var _ownKeys = Object.keys(_rawCtx);
		for (var _ki = 0; _ki < _ownKeys.length; _ki++) {
			var _pk = _ownKeys[_ki];
			if (!_proto.hasOwnProperty(_pk)) {
				_proto[_pk] = _rawCtx[_pk];
			}
		}
		Object.setPrototypeOf(_rawCtx, _proto);
		// Clear own properties that are now on the prototype (Chrome has no own props on gl)
		for (var _ki2 = 0; _ki2 < _ownKeys.length; _ki2++) {
			try { delete _rawCtx[_ownKeys[_ki2]]; } catch(e) {}
		}
	} else {
		if (isV2 && typeof window._WebGL2Proto !== 'undefined') {
			Object.setPrototypeOf(_rawCtx, window._WebGL2Proto);
		} else if (!isV2 && typeof window._WebGL1Proto !== 'undefined') {
			Object.setPrototypeOf(_rawCtx, window._WebGL1Proto);
		}
	}
	return _rawCtx;
};

// fetch, backed by Go via _goFetch, returns a real Promise
// CRITICAL: Must serialize opts to JSON string so Go can parse method/body/headers.
// Passing a raw JS object to Go results in "[object Object]" which loses all data.
window.fetch = function(url, opts) {
	var serialized = '{}';
	if (opts) {
		var o = {};
		if (opts.method) o.method = opts.method;
		if (opts.body !== undefined && opts.body !== null) {
			o.body = typeof opts.body === 'string' ? opts.body : _safeStringify(opts.body);
		}
		// Collect headers from opts.
		o.headers = {};
		if (opts.headers) {
			if (typeof opts.headers.forEach === 'function') {
				opts.headers.forEach(function(v, k) { o.headers[k] = v; });
			} else if (typeof opts.headers === 'object') {
				for (var k in opts.headers) o.headers[k] = opts.headers[k];
			}
		}
		// Auto-add sec-fetch headers (browser adds these, JS can't set them).
		if (!o.headers['Sec-Fetch-Dest'] && !o.headers['sec-fetch-dest']) {
			o.headers['Sec-Fetch-Dest'] = 'empty';
		}
		if (!o.headers['Sec-Fetch-Mode'] && !o.headers['sec-fetch-mode']) {
			o.headers['Sec-Fetch-Mode'] = opts.mode || 'cors';
		}
		if (!o.headers['Sec-Fetch-Site'] && !o.headers['sec-fetch-site']) {
			// Determine same-origin vs cross-site based on URL.
			var urlOrigin = '';
			try { var u = new URL(typeof url === 'string' ? url : url.url || ''); urlOrigin = u.origin; } catch(e) {}
			o.headers['Sec-Fetch-Site'] = (urlOrigin === location.origin) ? 'same-origin' : 'cross-site';
		}
		if (opts.credentials) o.credentials = opts.credentials;
		if (opts.mode) o.mode = opts.mode;
		if (opts.redirect) o.redirect = opts.redirect;
		if (opts.signal) o.hasSignal = true;
		serialized = _safeStringify(o);
	}
	return _goFetch(url, serialized);
};

// TextEncoder / TextDecoder
if (typeof TextEncoder === "undefined") {
	window.TextEncoder = function() {};
	window.TextEncoder.prototype.encode = function(s) {
		var arr = new Uint8Array(s.length);
		for (var i = 0; i < s.length; i++) arr[i] = s.charCodeAt(i) & 0xff;
		return arr;
	};
}
if (typeof TextDecoder === "undefined") {
	window.TextDecoder = function() {};
	window.TextDecoder.prototype.decode = function(arr) {
		var s = "";
		for (var i = 0; i < arr.length; i++) s += String.fromCharCode(arr[i]);
		return s;
	};
}

// FormData stub
if (typeof FormData === "undefined") {
	window.FormData = function() { this._data = {}; };
	window.FormData.prototype.append = function(k, v) { this._data[k] = v; };
	window.FormData.prototype.get = function(k) { return this._data[k] || null; };
	window.FormData.prototype.has = function(k) { return k in this._data; };
}

// Headers constructor (for fetch Response)
if (typeof Headers === "undefined") {
	window.Headers = function(init) {
		this._h = {};
		if (init) for (var k in init) this._h[k.toLowerCase()] = init[k];
	};
	window.Headers.prototype.get = function(k) { return this._h[k.toLowerCase()] || null; };
	window.Headers.prototype.has = function(k) { return k.toLowerCase() in this._h; };
	window.Headers.prototype.set = function(k, v) { this._h[k.toLowerCase()] = v; };
	window.Headers.prototype.forEach = function(fn) { for (var k in this._h) fn(this._h[k], k, this); };
}

// NOTE: String.prototype.apply was removed. The VM's own try-catch in the dispatch
// loop (case 20) handles TypeError from calling methods on undefined receivers.
// Adding String.prototype.apply caused infinite loops by preventing error recovery.

// --- Object.prototype DOM fallbacks REMOVED (2026-03-16) ---
// Previously, Object.prototype had getters for querySelector, getAttribute, etc. that
// activated only for nodeType===1 objects. This was a workaround for CF VM objects that
// don't inherit from our Element.prototype.
//
// PROBLEM: In real Chrome, 'querySelector' in {} === false. Our Object.prototype getters
// made it true, which is a trivially detectable bot fingerprint.
//
// The proper prototype chain (Node -> Element -> HTMLElement) plus _mkEl's own properties
// now handle all DOM method resolution. If the CF VM creates bare {nodeType:1} objects,
// they should behave like plain objects (no DOM methods) -- which matches Chrome behavior.
//
// If this removal causes "Cannot create proxy with non-object" crashes, the fix is to
// ensure the CF VM's objects inherit from HTMLElement.prototype (via Object.setPrototypeOf)
// at creation time, NOT to pollute Object.prototype.
//
// REMOVED CODE: ~84 lines of Object.prototype getter definitions for domMethodFallbacks
// and domPropFallbacks (querySelector, getAttribute, appendChild, tagName, innerHTML, etc.)
// Previously located here as an IIFE.

// --- DOM constructor stubs (critical for CF VM environment probing) ---
// Add methods to the EXISTING prototypes from lines 231-259.
// Do NOT redefine the constructors (they're named functions with proper chains).
Node.ELEMENT_NODE = 1;
Node.TEXT_NODE = 3;
Node.COMMENT_NODE = 8;
Node.DOCUMENT_NODE = 9;
Node.DOCUMENT_FRAGMENT_NODE = 11;
Node.prototype.nodeType = 1;
Node.prototype.compareDocumentPosition = function(other) {
	if (this === other) return 0;
	return 4; // FOLLOWING
};
Node.prototype.isEqualNode = function(other) { return this === other; };
Node.prototype.isSameNode = function(other) { return this === other; };
Node.prototype.getRootNode = function() { return document; };
Node.prototype.hasChildNodes = function() { return this.childNodes && this.childNodes.length > 0; };
Node.prototype.normalize = function() {};
Node.DOCUMENT_POSITION_DISCONNECTED = 1;
Node.DOCUMENT_POSITION_PRECEDING = 2;
Node.DOCUMENT_POSITION_FOLLOWING = 4;
Node.DOCUMENT_POSITION_CONTAINS = 8;
Node.DOCUMENT_POSITION_CONTAINED_BY = 16;
Node.DOCUMENT_POSITION_IMPLEMENTATION_SPECIFIC = 32;

// --- Add missing Node.prototype properties to match Chrome (48 own properties) ---
// Constants on prototype (Chrome has these on both Node and Node.prototype)
Node.prototype.ELEMENT_NODE = 1;
Node.prototype.ATTRIBUTE_NODE = 2;
Node.prototype.TEXT_NODE = 3;
Node.prototype.CDATA_SECTION_NODE = 4;
Node.prototype.ENTITY_REFERENCE_NODE = 5;
Node.prototype.ENTITY_NODE = 6;
Node.prototype.PROCESSING_INSTRUCTION_NODE = 7;
Node.prototype.COMMENT_NODE = 8;
Node.prototype.DOCUMENT_NODE = 9;
Node.prototype.DOCUMENT_TYPE_NODE = 10;
Node.prototype.DOCUMENT_FRAGMENT_NODE = 11;
Node.prototype.NOTATION_NODE = 12;
Node.prototype.DOCUMENT_POSITION_DISCONNECTED = 1;
Node.prototype.DOCUMENT_POSITION_PRECEDING = 2;
Node.prototype.DOCUMENT_POSITION_FOLLOWING = 4;
Node.prototype.DOCUMENT_POSITION_CONTAINS = 8;
Node.prototype.DOCUMENT_POSITION_CONTAINED_BY = 16;
Node.prototype.DOCUMENT_POSITION_IMPLEMENTATION_SPECIFIC = 32;
// Methods that belong on Node.prototype (Chrome)
Node.prototype.appendChild = function(child) { return _domAppendChild(this, child); };
Node.prototype.removeChild = function(child) { return _domRemoveChild(this, child); };
Node.prototype.insertBefore = function(node, ref) { return _domInsertBefore(this, node, ref); };
Node.prototype.replaceChild = function(newChild, oldChild) { return _domReplaceChild(this, newChild, oldChild); };
Node.prototype.cloneNode = function(deep) {
	var clone = _mkEl(this.tagName || 'div', this.id);
	if (deep && this.childNodes) {
		for (var i = 0; i < this.childNodes.length; i++) {
			var child = this.childNodes[i];
			if (child && child.cloneNode) _domAppendChild(clone, child.cloneNode(true));
			else if (child && child.nodeType === 3) _domAppendChild(clone, {nodeType: 3, textContent: child.textContent, data: child.data, _parentNode: null, _parentElement: null});
		}
	}
	return clone;
};
Node.prototype.contains = function(node) {
	if (node === this) return true;
	if (!this.childNodes) return false;
	for (var i = 0; i < this.childNodes.length; i++) {
		if (this.childNodes[i] === node) return true;
		if (this.childNodes[i] && this.childNodes[i].contains && this.childNodes[i].contains(node)) return true;
	}
	return false;
};
Node.prototype.isDefaultNamespace = function() { return true; };
Node.prototype.lookupNamespaceURI = function() { return null; };
Node.prototype.lookupPrefix = function() { return null; };
// Getter-like properties on Node.prototype (configurable so _mkEl can override with getters)
Object.defineProperty(Node.prototype, 'baseURI', { value: '', writable: true, configurable: true, enumerable: true });
Object.defineProperty(Node.prototype, 'childNodes', { value: _mkNodeList(), writable: true, configurable: true, enumerable: true });
Object.defineProperty(Node.prototype, 'firstChild', { value: null, writable: true, configurable: true, enumerable: true });
Object.defineProperty(Node.prototype, 'lastChild', { value: null, writable: true, configurable: true, enumerable: true });
Object.defineProperty(Node.prototype, 'nextSibling', { value: null, writable: true, configurable: true, enumerable: true });
Object.defineProperty(Node.prototype, 'previousSibling', { value: null, writable: true, configurable: true, enumerable: true });
Object.defineProperty(Node.prototype, 'nodeName', { value: '', writable: true, configurable: true, enumerable: true });
Object.defineProperty(Node.prototype, 'nodeValue', { value: null, writable: true, configurable: true, enumerable: true });
Node.prototype.ownerDocument = null;
Node.prototype.parentElement = null;
Node.prototype.parentNode = null;
Node.prototype.textContent = '';
Node.prototype.isConnected = false;

// Prevent "Converting circular structure to JSON", strip _parentNode/_parentElement
Element.prototype.toJSON = function() {
	var r = {};
	var keys = Object.keys(this);
	for (var i = 0; i < keys.length; i++) {
		var k = keys[i];
		if (k === '_parentNode' || k === '_parentElement' || k === 'ownerDocument' || k === 'parentNode' || k === 'parentElement') continue;
		var v = this[k];
		if (typeof v !== 'function' && typeof v !== 'object') r[k] = v;
	}
	r.tagName = this.tagName || this.nodeName;
	return r;
};
Node.prototype.toJSON = Element.prototype.toJSON;

// Add DOM methods to Element.prototype (using the original Element from line 241)
Element.prototype.querySelector = function(sel) { return _domQuerySelector(this, sel); };
Element.prototype.querySelectorAll = function(sel) { return _domQuerySelectorAll(this, sel); };
Element.prototype.getElementsByTagName = function(tag) {
	var r = _domQuerySelectorAll(this, tag.toLowerCase() === '*' ? '*' : tag.toLowerCase());
	Object.setPrototypeOf(r, HTMLCollection.prototype);
	return r;
};
Element.prototype.getElementsByClassName = function(cls) {
	var r = _domQuerySelectorAll(this, '.' + cls.split(/\s+/).join('.'));
	Object.setPrototypeOf(r, HTMLCollection.prototype);
	return r;
};
Element.prototype.getAttribute = function(k) { return null; };
Element.prototype.setAttribute = function(k, v) {};
Element.prototype.removeAttribute = function(k) {};
Element.prototype.hasAttribute = function(k) { return false; };
Element.prototype.matches = function(sel) {
	try { return _matchesSingle(this, _parseSingleSelector(sel)); } catch(e) { return false; }
};
Element.prototype.closest = function(sel) {
	var el = this;
	while (el && el.nodeType === 1) {
		try { if (_matchesSingle(el, _parseSingleSelector(sel))) return el; } catch(e) {}
		el = el._parentNode;
	}
	return null;
};
Element.prototype.getBoundingClientRect = function() { return {top:0,left:0,bottom:0,right:0,width:0,height:0,x:0,y:0}; };
Element.prototype.getClientRects = function() { return []; };
Element.prototype.attachShadow = function(opts) {
	var host = this;
	var root = {nodeType: 11, mode: (opts && opts.mode) || "open", host: host,
		childNodes: _mkNodeList(), children: _mkHTMLCollection(), innerHTML: "", textContent: "",
		appendChild: function(c) { return _domAppendChild(this, c); },
		removeChild: function(c) { return _domRemoveChild(this, c); },
		insertBefore: function(n, ref) { return _domInsertBefore(this, n, ref); },
		append: function() { for (var i = 0; i < arguments.length; i++) _domAppendChild(this, arguments[i]); },
		querySelector: function(sel) { return _domQuerySelector(this, sel); },
		querySelectorAll: function(sel) { return _domQuerySelectorAll(this, sel); },
		getElementById: function(id) { return _domQuerySelector(this, '#' + id); },
		addEventListener: function() {}, removeEventListener: function() {}, dispatchEvent: function() { return true; },
		getRootNode: function() { return this; }
	};
	host.shadowRoot = (root.mode === "open") ? root : null;
	return root;
};
Element.prototype.tagName = "DIV";
Element.prototype.innerHTML = "";
Element.prototype.outerHTML = "";
Element.prototype.id = "";
Element.prototype.className = "";
Element.prototype.children = [];
Element.prototype.attributes = {};
Element.prototype.clientWidth = 0;
Element.prototype.clientHeight = 0;

// --- Add missing Element.prototype properties to match Chrome (149 own properties) ---
// Methods
['after','animate','append','before','checkVisibility','computedStyleMap',
'getAnimations','getAttributeNS','getAttributeNames','getAttributeNode',
'getAttributeNodeNS','getElementsByTagNameNS','getHTML',
'hasAttributeNS','hasAttributes','hasPointerCapture',
'insertAdjacentElement','insertAdjacentHTML','insertAdjacentText',
'moveBefore','prepend','releasePointerCapture','remove',
'removeAttributeNS','removeAttributeNode','replaceChildren','replaceWith',
'requestFullscreen','requestPointerLock','scroll','scrollBy',
'scrollIntoView','scrollIntoViewIfNeeded','scrollTo',
'setAttributeNS','setAttributeNode','setAttributeNodeNS',
'setHTML','setHTMLUnsafe','setPointerCapture','toggleAttribute',
'webkitMatchesSelector','webkitRequestFullScreen','webkitRequestFullscreen',
'ariaNotify'].forEach(function(name) {
	if (!(name in Element.prototype)) {
		Element.prototype[name] = function() {};
	}
});
// Override Element.prototype.animate to return an Animation-like object
// Kasada accesses .effect, .playbackRate, .finished, .cancel on the result
Element.prototype.animate = function(keyframes, options) {
	var anim = {
		effect: {
			target: this,
			getKeyframes: function() { return keyframes || []; },
			getComputedTiming: function() { return {duration: 0, fill: "none", iterations: 1}; },
			getTiming: function() { return {duration: 0, fill: "none", iterations: 1}; }
		},
		playbackRate: 1,
		playState: "finished",
		replaceState: "active",
		startTime: 0,
		currentTime: 0,
		timeline: { currentTime: performance.now() },
		pending: false,
		id: "",
		onfinish: null,
		oncancel: null,
		onremove: null,
		finished: Promise.resolve(),
		ready: Promise.resolve(),
		play: function() {},
		pause: function() {},
		cancel: function() { this.playState = "idle"; },
		finish: function() { this.playState = "finished"; },
		reverse: function() {},
		updatePlaybackRate: function(r) { this.playbackRate = r; },
		persist: function() {},
		commitStyles: function() {},
		addEventListener: function() {},
		removeEventListener: function() {},
		dispatchEvent: function() { return true; }
	};
	// Make finished/ready resolve to the animation itself
	anim.finished = Promise.resolve(anim);
	anim.ready = Promise.resolve(anim);
	return anim;
};
// Getter-like properties (null/empty/0 defaults)
['assignedSlot','firstElementChild','lastElementChild',
'nextElementSibling','previousElementSibling','shadowRoot',
'part','prefix','slot','localName','namespaceURI',
'elementTiming','currentCSSZoom','customElementRegistry','role'].forEach(function(name) {
	if (!(name in Element.prototype)) {
		Element.prototype[name] = null;
	}
});
// Numeric properties
['childElementCount','clientLeft','clientTop',
'scrollHeight','scrollLeft','scrollTop','scrollWidth'].forEach(function(name) {
	if (!(name in Element.prototype)) {
		Element.prototype[name] = 0;
	}
});
// classList as DOMTokenList stub
if (!('classList' in Element.prototype)) {
	Element.prototype.classList = { add: function(){}, remove: function(){}, contains: function(){ return false; }, toggle: function(){ return false; }, length: 0 };
}
// ARIA properties (all null by default in Chrome)
['ariaActiveDescendantElement','ariaAtomic','ariaAutoComplete',
'ariaBrailleLabel','ariaBrailleRoleDescription','ariaBusy','ariaChecked',
'ariaColCount','ariaColIndex','ariaColIndexText','ariaColSpan',
'ariaControlsElements','ariaCurrent','ariaDescribedByElements',
'ariaDescription','ariaDetailsElements','ariaDisabled',
'ariaErrorMessageElements','ariaExpanded','ariaFlowToElements',
'ariaHasPopup','ariaHidden','ariaInvalid','ariaKeyShortcuts',
'ariaLabel','ariaLabelledByElements','ariaLevel','ariaLive',
'ariaModal','ariaMultiLine','ariaMultiSelectable','ariaOrientation',
'ariaPlaceholder','ariaPosInSet','ariaPressed','ariaReadOnly',
'ariaRelevant','ariaRequired','ariaRoleDescription','ariaRowCount',
'ariaRowIndex','ariaRowIndexText','ariaRowSpan','ariaSelected',
'ariaSetSize','ariaSort','ariaValueMax','ariaValueMin',
'ariaValueNow','ariaValueText'].forEach(function(name) {
	if (!(name in Element.prototype)) {
		Element.prototype[name] = null;
	}
});
// Event handler properties on Element.prototype
['onbeforecopy','onbeforecut','onbeforepaste',
'onfullscreenchange','onfullscreenerror','onsearch',
'onwebkitfullscreenchange','onwebkitfullscreenerror'].forEach(function(name) {
	if (!(name in Element.prototype)) {
		Element.prototype[name] = null;
	}
});

// HTMLElement already defined at line 246 with proper name and prototype chain.
// Move focus/blur/style/offset to HTMLElement.prototype (they belong there, not on Element)
HTMLElement.prototype.focus = function() {};
HTMLElement.prototype.blur = function() {};
Object.defineProperty(HTMLElement.prototype, 'style', {
	get: function() { if (!this._style) { this._style = _mkStyle(); } return this._style; },
	set: function(v) { this._style = v; },
	configurable: true
});
HTMLElement.prototype.childNodes = _mkNodeList();  // inherited from Node but override needed

// HTMLElement.prototype: Chrome 146 has 141 properties.
// Add getter properties (31 total)
Object.defineProperty(HTMLElement.prototype, 'title', { get: function(){ return this._title || ''; }, set: function(v){ this._title = v; }, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'lang', { get: function(){ return this._lang || ''; }, set: function(v){ this._lang = v; }, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'translate', { get: function(){ return true; }, set: function(v){}, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'dir', { get: function(){ return this._dir || ''; }, set: function(v){ this._dir = v; }, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'hidden', { get: function(){ return this._hidden || false; }, set: function(v){ this._hidden = v; }, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'inert', { get: function(){ return false; }, set: function(v){}, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'accessKey', { get: function(){ return ''; }, set: function(v){}, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'draggable', { get: function(){ return false; }, set: function(v){}, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'spellcheck', { get: function(){ return true; }, set: function(v){}, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'autocapitalize', { get: function(){ return ''; }, set: function(v){}, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'editContext', { get: function(){ return null; }, set: function(v){}, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'contentEditable', { get: function(){ return 'inherit'; }, set: function(v){}, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'enterKeyHint', { get: function(){ return ''; }, set: function(v){}, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'isContentEditable', { get: function(){ return false; }, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'inputMode', { get: function(){ return ''; }, set: function(v){}, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'virtualKeyboardPolicy', { get: function(){ return ''; }, set: function(v){}, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'offsetParent', { get: function(){ return null; }, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'offsetTop', { get: function(){ return this._offsetTop || 0; }, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'offsetLeft', { get: function(){ return this._offsetLeft || 0; }, enumerable: true, configurable: true });
// offsetWidth/offsetHeight already defined above, convert to getters
Object.defineProperty(HTMLElement.prototype, 'offsetWidth', { get: function(){ return this._offsetWidth || 0; }, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'offsetHeight', { get: function(){ return this._offsetHeight || 0; }, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'popover', { get: function(){ return null; }, set: function(v){}, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'innerText', { get: function(){ return this.textContent || ''; }, set: function(v){ this.textContent = v; }, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'outerText', { get: function(){ return this.textContent || ''; }, set: function(v){}, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'writingSuggestions', { get: function(){ return ''; }, set: function(v){}, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'dataset', { get: function(){ if(!this._dataset)this._dataset={};return this._dataset; }, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'nonce', { get: function(){ return ''; }, set: function(v){}, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'autofocus', { get: function(){ return false; }, set: function(v){}, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'tabIndex', { get: function(){ return this._tabIndex || -1; }, set: function(v){ this._tabIndex = v; }, enumerable: true, configurable: true });
Object.defineProperty(HTMLElement.prototype, 'attributeStyleMap', { get: function(){ return {size:0, get:function(){return null;}, has:function(){return false;}}; }, enumerable: true, configurable: true });
// Value methods
HTMLElement.prototype.attachInternals = function attachInternals(){ return {}; };
HTMLElement.prototype.click = function click(){};
HTMLElement.prototype.hidePopover = function hidePopover(){};
HTMLElement.prototype.showPopover = function showPopover(){};
HTMLElement.prototype.togglePopover = function togglePopover(){ return false; };
// Event handler properties (102 total), all null getter/setter pairs
(function(){
	var evts = ['onabort','onbeforeinput','onbeforematch','onbeforetoggle','onblur','oncancel','oncanplay','oncanplaythrough','onchange','onclick','onclose','oncommand','oncontentvisibilityautostatechange','oncontextlost','oncontextmenu','oncontextrestored','oncuechange','ondblclick','ondrag','ondragend','ondragenter','ondragleave','ondragover','ondragstart','ondrop','ondurationchange','onemptied','onended','onerror','onfocus','onformdata','oninput','oninvalid','onkeydown','onkeypress','onkeyup','onload','onloadeddata','onloadedmetadata','onloadstart','onmousedown','onmouseenter','onmouseleave','onmousemove','onmouseout','onmouseover','onmouseup','onmousewheel','onpause','onplay','onplaying','onprogress','onratechange','onreset','onresize','onscroll','onscrollend','onsecuritypolicyviolation','onseeked','onseeking','onselect','onslotchange','onstalled','onsubmit','onsuspend','ontimeupdate','ontoggle','onvolumechange','onwaiting','onwebkitanimationend','onwebkitanimationiteration','onwebkitanimationstart','onwebkittransitionend','onwheel','onauxclick','ongotpointercapture','onlostpointercapture','onpointerdown','onpointermove','onpointerup','onpointercancel','onpointerover','onpointerout','onpointerenter','onpointerleave','onselectstart','onselectionchange','onanimationcancel','onanimationend','onanimationiteration','onanimationstart','ontransitionrun','ontransitionstart','ontransitionend','ontransitioncancel','onbeforexrselect','oncopy','oncut','onpaste','onscrollsnapchange','onscrollsnapchanging','onpointerrawupdate'];
	for (var i = 0; i < evts.length; i++) {
		(function(name) {
			var key = '_' + name;
			Object.defineProperty(HTMLElement.prototype, name, {
				get: function(){ return this[key] || null; },
				set: function(v){ this[key] = v; },
				enumerable: true, configurable: true
			});
		})(evts[i]);
	}
})();

// Specific HTML element constructors that CF might check
let htmlTags = ['Div','Span','Script','Style','Link','Meta','Input','Form','Button',
	'Anchor','Image','Canvas','IFrame','Table','TableRow','TableCell',
	'Paragraph','Heading','Select','Option','TextArea',
	'Label','Video','Audio','Source','Body','Head','Html','Pre'];
for (let i = 0; i < htmlTags.length; i++) {
	let name = 'HTML' + htmlTags[i] + 'Element';
	if (!window[name]) {
		let ctor = function() { throw new TypeError("Illegal constructor"); };
		Object.defineProperty(ctor, 'name', { value: name, configurable: true });
		ctor.prototype = Object.create(HTMLElement.prototype);
		ctor.prototype.constructor = ctor;
		Object.defineProperty(ctor.prototype, Symbol.toStringTag, { value: name, configurable: true });
		window[name] = ctor;
	}
}
// Common aliases
window.HTMLAnchorElement = window.HTMLAnchorElement || window.HTMLElement;
window.HTMLImageElement = window.HTMLImageElement || window.HTMLElement;

// Symbol.hasInstance for element type checks (e.g. Turnstile uses instanceof HTMLScriptElement)
// Our elements are plain objects from _mkEl, not created via new HTMLScriptElement(),
// so standard prototype-chain instanceof would fail. This custom check fixes that.
Object.defineProperty(window.HTMLScriptElement, Symbol.hasInstance, {
	value: function(obj) { return obj && obj.nodeType === 1 && (obj.tagName === 'SCRIPT' || obj.nodeName === 'SCRIPT'); }
});
Object.defineProperty(window.HTMLIFrameElement, Symbol.hasInstance, {
	value: function(obj) { return obj && obj.nodeType === 1 && (obj.tagName === 'IFRAME' || obj.nodeName === 'IFRAME'); }
});
// HTMLIFrameElement.prototype, Chrome 146 has 28 properties. All must be getters.
if (window.HTMLIFrameElement && window.HTMLIFrameElement.prototype) {
	var _ifProto = window.HTMLIFrameElement.prototype;
	var _ifStrProps = {src:'',srcdoc:'',name:'',allow:'',referrerPolicy:'',csp:'',loading:'lazy',align:'',scrolling:'',frameBorder:'',longDesc:'',marginHeight:'',marginWidth:''};
	for (var _ik in _ifStrProps) {
		(function(name, def) {
			Object.defineProperty(_ifProto, name, {
				get: function() { return this['_if_'+name] !== undefined ? this['_if_'+name] : def; },
				set: function(v) { this['_if_'+name] = String(v); },
				enumerable: true, configurable: true
			});
		})(_ik, _ifStrProps[_ik]);
	}
	var _ifBoolProps = {allowFullscreen:false, credentialless:false, allowPaymentRequest:false, browsingTopics:false, adAuctionHeaders:false, sharedStorageWritable:false};
	for (var _ib in _ifBoolProps) {
		(function(name, def) {
			Object.defineProperty(_ifProto, name, {
				get: function() { return this['_if_'+name] !== undefined ? this['_if_'+name] : def; },
				set: function(v) { this['_if_'+name] = !!v; },
				enumerable: true, configurable: true
			});
		})(_ib, _ifBoolProps[_ib]);
	}
	Object.defineProperty(_ifProto, 'width', { get: function(){ return this._if_width || ''; }, set: function(v){ this._if_width = String(v); }, enumerable: true, configurable: true });
	Object.defineProperty(_ifProto, 'height', { get: function(){ return this._if_height || ''; }, set: function(v){ this._if_height = String(v); }, enumerable: true, configurable: true });
	Object.defineProperty(_ifProto, 'sandbox', {
		get: function() { return this._if_sandbox || {length:0, value:'', add:function(){}, remove:function(){}, contains:function(){return false}, toggle:function(){return false}, toString:function(){return ''},item:function(){return null},[Symbol.iterator]:function(){return{next:function(){return{done:true}}}}}; },
		set: function(v) { this._if_sandbox = v; },
		enumerable: true, configurable: true
	});
	Object.defineProperty(_ifProto, 'contentWindow', {
		get: function() {
			if (!this._if_cw) {
				var _cw = {};
				_cw.document = null; _cw.location = {href:'about:blank',toString:function(){return 'about:blank'}};
				_cw.navigator = window.navigator; _cw.self = _cw; _cw.window = _cw;
				_cw.postMessage = function(){}; _cw.close = function(){}; _cw.focus = function(){}; _cw.blur = function(){};
				_cw.closed = true; _cw.frames = {length:0}; _cw.length = 0; _cw.parent = window; _cw.top = window; _cw.opener = null;
				_cw.toJSON = function(){ return '[object Window]'; };
				this._if_cw = _cw;
			}
			return this._if_cw;
		},
		enumerable: true, configurable: true
	});
	Object.defineProperty(_ifProto, 'contentDocument', {
		get: function() { return null; },
		enumerable: true, configurable: true
	});
	Object.defineProperty(_ifProto, 'featurePolicy', {
		get: function() { return {allowsFeature:function(){return false}, features:function(){return []}, allowedFeatures:function(){return []}, getAllowlistForFeature:function(){return []}}; },
		enumerable: true, configurable: true
	});
	Object.defineProperty(_ifProto, 'privateToken', { get: function(){ return ''; }, set: function(v){}, enumerable: true, configurable: true });
	_ifProto.getSVGDocument = function getSVGDocument() { return null; };
}
Object.defineProperty(window.HTMLElement, Symbol.hasInstance, {
	value: function(obj) { return obj && obj.nodeType === 1; }
});
Object.defineProperty(window.Element, Symbol.hasInstance, {
	value: function(obj) { return obj && typeof obj.nodeType === 'number' && obj.nodeType === 1; }
});
Object.defineProperty(window.Node, Symbol.hasInstance, {
	value: function(obj) { return obj && typeof obj.nodeType === 'number'; }
});
// instanceof checks for Document, Window, Navigator, Screen, EventTarget
Object.defineProperty(HTMLDocument, Symbol.hasInstance, {
	value: function(obj) { return obj && obj.nodeType === 9; }
});
Object.defineProperty(Document, Symbol.hasInstance, {
	value: function(obj) { return obj && obj.nodeType === 9; }
});
Object.defineProperty(Window, Symbol.hasInstance, {
	value: function(obj) { return obj && (obj === window || (typeof obj.document !== 'undefined' && typeof obj.location !== 'undefined')); }
});
Object.defineProperty(Navigator, Symbol.hasInstance, {
	value: function(obj) { return obj && typeof obj.userAgent === 'string' && typeof obj.platform === 'string'; }
});
Object.defineProperty(Screen, Symbol.hasInstance, {
	value: function(obj) { return obj && typeof obj.width === 'number' && typeof obj.colorDepth === 'number'; }
});
Object.defineProperty(EventTarget, Symbol.hasInstance, {
	value: function(obj) { return obj && typeof obj.addEventListener === 'function'; }
});
Object.defineProperty(ShadowRoot, Symbol.hasInstance, {
	value: function(obj) { return obj && obj._isShadowRoot === true; }
});

window.SVGElement = function() {};
window.SVGElement.prototype = Object.create(Element.prototype);
window.SVGElement.prototype.constructor = window.SVGElement;

// HTMLMediaElement, base class for HTMLVideoElement and HTMLAudioElement.
// Kasada VM calls canPlayType() via the prototype, not instances.
window.HTMLMediaElement = function() { throw new TypeError("Illegal constructor"); };
window.HTMLMediaElement.prototype = Object.create(HTMLElement.prototype);
window.HTMLMediaElement.prototype.constructor = window.HTMLMediaElement;
Object.defineProperty(window.HTMLMediaElement.prototype, Symbol.toStringTag, { value: 'HTMLMediaElement', configurable: true });
window.HTMLMediaElement.prototype.canPlayType = function canPlayType(mime) {
	if (!mime || typeof mime !== 'string') return '';
	mime = mime.toLowerCase();
	if (mime.indexOf('video/mp4')>=0 || mime.indexOf('audio/mpeg')>=0 ||
		mime.indexOf('audio/mp4')>=0 || mime.indexOf('video/webm')>=0 ||
		mime.indexOf('audio/webm')>=0 || mime.indexOf('audio/ogg')>=0 ||
		mime.indexOf('video/ogg')>=0 || mime.indexOf('audio/wav')>=0 ||
		mime.indexOf('audio/x-wav')>=0 || mime.indexOf('audio/flac')>=0 ||
		mime.indexOf('audio/aac')>=0) {
		return mime.indexOf('codecs')>=0 ? 'probably' : 'maybe';
	}
	return '';
};
window.HTMLMediaElement.prototype.play = function play(){ return Promise.resolve(); };
window.HTMLMediaElement.prototype.pause = function pause(){};
window.HTMLMediaElement.prototype.load = function load(){};
window.HTMLMediaElement.prototype.addTextTrack = function addTextTrack(){ return {}; };
window.HTMLMediaElement.prototype.getStartDate = function getStartDate(){ return new Date(0); };
window.HTMLMediaElement.prototype.fastSeek = function fastSeek(){};
// Media properties as getters
(function(){
	var mediaGetters = {
		src:'',currentSrc:'',crossOrigin:null,networkState:0,preload:'auto',
		buffered:{length:0,start:function(){return 0},end:function(){return 0}},
		readyState:0,seeking:false,currentTime:0,duration:NaN,paused:true,
		defaultPlaybackRate:1,playbackRate:1,played:{length:0,start:function(){return 0},end:function(){return 0}},
		seekable:{length:0,start:function(){return 0},end:function(){return 0}},
		ended:false,autoplay:false,loop:false,controls:false,volume:1,muted:false,
		defaultMuted:false,textTracks:{length:0},sinkId:'',preservesPitch:true,
		remote:null,disableRemotePlayback:false,srcObject:null,mediaKeys:null
	};
	for (var k in mediaGetters) {
		(function(name, val) {
			Object.defineProperty(window.HTMLMediaElement.prototype, name, {
				get: function(){ return typeof val==='object'&&val!==null?val:(this['_media_'+name]!==undefined?this['_media_'+name]:val); },
				set: function(v){ this['_media_'+name]=v; },
				enumerable: true, configurable: true
			});
		})(k, mediaGetters[k]);
	}
})();
// Make HTMLVideoElement/HTMLAudioElement inherit from HTMLMediaElement
if (window.HTMLVideoElement) {
	window.HTMLVideoElement.prototype = Object.create(window.HTMLMediaElement.prototype);
	window.HTMLVideoElement.prototype.constructor = window.HTMLVideoElement;
	Object.defineProperty(window.HTMLVideoElement.prototype, Symbol.toStringTag, { value: 'HTMLVideoElement', configurable: true });
	window.HTMLVideoElement.prototype.getVideoPlaybackQuality = function(){ return {totalVideoFrames:0,droppedVideoFrames:0,corruptedVideoFrames:0,creationTime:performance.now()}; };
	window.HTMLVideoElement.prototype.requestPictureInPicture = function(){ return Promise.reject(new DOMException('Not supported')); };
}
if (window.HTMLAudioElement) {
	window.HTMLAudioElement.prototype = Object.create(window.HTMLMediaElement.prototype);
	window.HTMLAudioElement.prototype.constructor = window.HTMLAudioElement;
	Object.defineProperty(window.HTMLAudioElement.prototype, Symbol.toStringTag, { value: 'HTMLAudioElement', configurable: true });
}
// MediaSource, Chrome has this for MSE (Media Source Extensions)
window.MediaSource = function MediaSource() {};
window.MediaSource.isTypeSupported = function isTypeSupported(mimeType) {
	if (!mimeType) return false;
	mimeType = mimeType.toLowerCase();
	return mimeType.indexOf('video/mp4')>=0 || mimeType.indexOf('audio/mp4')>=0 ||
		mimeType.indexOf('video/webm')>=0 || mimeType.indexOf('audio/webm')>=0;
};
window.MediaSource.prototype.addEventListener = function(){};
window.MediaSource.prototype.removeEventListener = function(){};
Object.defineProperty(window.MediaSource.prototype, Symbol.toStringTag, { value: 'MediaSource', configurable: true });

// Document and HTMLDocument are already defined above (lines 246+) with proper
// prototype chains and constructor names. Don't redefine them here.

window.Text = function(data) { this.nodeType = 3; this.data = data || ''; this.textContent = this.data; };
window.Text.prototype = Object.create(Node.prototype);
window.Comment = function(data) { this.nodeType = 8; this.data = data || ''; };
window.Comment.prototype = Object.create(Node.prototype);
function DocumentFragment() { this.nodeType = 11; this.childNodes = []; this.children = []; }
DocumentFragment.prototype = Object.create(Node.prototype);
DocumentFragment.prototype.constructor = DocumentFragment;
Object.defineProperty(DocumentFragment.prototype, Symbol.toStringTag, { value: 'DocumentFragment', configurable: true });
window.DocumentFragment = DocumentFragment;

window.ShadowRoot = function() { this.nodeType = 11; this.childNodes = []; this.children = []; this.mode = "open"; this.host = null; };
window.ShadowRoot.prototype = Object.create(DocumentFragment.prototype);
window.ShadowRoot.prototype.constructor = window.ShadowRoot;
window.ShadowRoot.prototype.appendChild = function(c) { return _domAppendChild(this, c); };
window.ShadowRoot.prototype.removeChild = function(c) { return _domRemoveChild(this, c); };
window.ShadowRoot.prototype.querySelector = function(sel) { return _domQuerySelector(this, sel); };
window.ShadowRoot.prototype.querySelectorAll = function(sel) { return _domQuerySelectorAll(this, sel); };
window.ShadowRoot.prototype.getElementById = function(id) { return _domQuerySelector(this, '#' + id); };
window.ShadowRoot.prototype.addEventListener = function(ev, fn, opts) {
	if (!this[_sEL]) this[_sEL] = {};
	if (!this[_sEL][ev]) this[_sEL][ev] = [];
	this[_sEL][ev].push(fn);
};
window.ShadowRoot.prototype.removeEventListener = function(ev, fn) {
	if (this[_sEL] && this[_sEL][ev]) {
		this[_sEL][ev] = this[_sEL][ev].filter(function(f) { return f !== fn; });
	}
};
window.ShadowRoot.prototype.getRootNode = function() { return this; };

// Add methods to EventTarget.prototype (constructor already defined at line 231)
EventTarget.prototype.addEventListener = function(ev, fn, opts) {
	if (!this[_sEL]) this[_sEL] = {};
	if (!this[_sEL][ev]) this[_sEL][ev] = [];
	var entry = {fn: fn, capture: false, once: false, passive: false};
	if (opts === true) entry.capture = true;
	else if (opts && typeof opts === 'object') {
		entry.capture = !!opts.capture;
		entry.once = !!opts.once;
		entry.passive = !!opts.passive;
	}
	this[_sEL][ev].push(entry);
};
EventTarget.prototype.removeEventListener = function(ev, fn, opts) {
	if (this[_sEL] && this[_sEL][ev]) {
		var capture = (opts === true) || (opts && typeof opts === 'object' && opts.capture);
		this[_sEL][ev] = this[_sEL][ev].filter(function(e) {
			if (typeof e === 'function') return e !== fn;
			return !(e.fn === fn && e.capture === !!capture);
		});
	}
};
EventTarget.prototype.dispatchEvent = function(evt) {
	var type = evt.type || evt;
	if (typeof evt === 'string') evt = {type: type, bubbles: false, cancelable: false};
	// Build propagation path (target -> ancestors)
	var path = [];
	var node = this;
	while (node) {
		path.push(node);
		var next = node._parentNode;
		if (!next || next === node) break;
		node = next;
	}
	evt.target = this;
	var stopped = false, prevented = false;
	evt.stopPropagation = function() { stopped = true; };
	evt.stopImmediatePropagation = function() { stopped = true; evt._immediate = true; };
	evt.preventDefault = function() { if (evt.cancelable !== false) prevented = true; };
	Object.defineProperty(evt, 'defaultPrevented', { get: function() { return prevented; }, configurable: true });
	// Phase 1: Capture (top -> target)
	for (var i = path.length - 1; i > 0 && !stopped; i--) {
		evt.currentTarget = path[i]; evt.eventPhase = 1;
		_fireHandlers(path[i], type, evt, true);
	}
	// Phase 2: Target
	if (!stopped) { evt.currentTarget = this; evt.eventPhase = 2; _fireHandlers(this, type, evt, false); _fireHandlers(this, type, evt, true); }
	// Phase 3: Bubble
	if (!stopped && evt.bubbles !== false) {
		for (var i = 1; i < path.length && !stopped; i++) {
			evt.currentTarget = path[i]; evt.eventPhase = 3;
			_fireHandlers(path[i], type, evt, false);
		}
	}
	evt.eventPhase = 0; evt.currentTarget = null;
	return !prevented;
};
function _fireHandlers(target, type, evt, capturePhase) {
	if (!target[_sEL] || !target[_sEL][type]) return;
	var handlers = target[_sEL][type].slice();
	for (var i = 0; i < handlers.length; i++) {
		if (evt._immediate) return;
		var entry = handlers[i];
		var fn = (typeof entry === 'function') ? entry : entry.fn;
		var isCapture = (typeof entry === 'object' && entry !== null && entry.capture);
		if (evt.eventPhase !== 2 && isCapture !== capturePhase) continue;
		try { fn.call(target, evt); } catch(e) {}
		if (typeof entry === 'object' && entry !== null && entry.once) {
			var idx = target[_sEL][type].indexOf(entry);
			if (idx !== -1) target[_sEL][type].splice(idx, 1);
		}
	}
}
EventTarget.prototype.when = function(type) {
	return new Promise(function(resolve) {
		this.addEventListener(type, resolve, {once: true});
	}.bind(this));
};

// CharacterData, base for Text/Comment nodes (Turnstile checks this)
window.CharacterData = function() {};
window.CharacterData.prototype = Object.create(Node.prototype);
window.CharacterData.prototype.constructor = window.CharacterData;
window.CharacterData.prototype.data = '';
window.CharacterData.prototype.length = 0;
window.CharacterData.prototype.substringData = function(offset, count) { return this.data.substring(offset, offset + count); };

// AbortController / AbortSignal, Turnstile checks these exist
window.AbortSignal = function() { this.aborted = false; this.reason = undefined; };
window.AbortSignal.prototype.addEventListener = function() {};
window.AbortSignal.prototype.removeEventListener = function() {};
window.AbortSignal.abort = function() { var s = new AbortSignal(); s.aborted = true; return s; };
window.AbortSignal.timeout = function() { return new AbortSignal(); };
window.AbortController = function() { this.signal = new AbortSignal(); };
window.AbortController.prototype.abort = function() { this.signal.aborted = true; };

// Request / Response, Fetch API constructors (Turnstile fingerprints these)
window.Request = function(input, init) {
	this.url = typeof input === 'string' ? input : (input && input.url) || '';
	this.method = (init && init.method) || 'GET';
	this.headers = (init && init.headers) || new Headers();
	this.body = (init && init.body) || null;
	this.mode = (init && init.mode) || 'cors';
	this.credentials = (init && init.credentials) || 'same-origin';
	this.redirect = (init && init.redirect) || 'follow';
	this.signal = (init && init.signal) || new AbortSignal();
};
window.Request.prototype.clone = function() { return new Request(this.url, this); };
window.Request.prototype.text = function() { return Promise.resolve(''); };
window.Request.prototype.json = function() { return Promise.resolve({}); };

window.Response = function(body, init) {
	this.ok = true;
	this.status = (init && init.status) || 200;
	this.statusText = (init && init.statusText) || '';
	this.headers = (init && init.headers) || new Headers();
	this.url = '';
	this.type = 'basic';
	this.bodyUsed = false;
	this._body = body || '';
};
window.Response.prototype.clone = function() { return new Response(this._body, {status: this.status}); };
window.Response.prototype.text = function() { return Promise.resolve(typeof this._body === 'string' ? this._body : ''); };
window.Response.prototype.json = function() { var b = this._body; return Promise.resolve(typeof b === 'string' ? JSON.parse(b) : b); };
window.Response.prototype.arrayBuffer = function() { return Promise.resolve(new ArrayBuffer(0)); };

// Window/Navigator/Screen already defined above with proper names.
// Add missing global constructors that aren't defined yet.
(function() {
	var missing = ['Storage','Location','History','Performance','CSSStyleSheet','CSSRule'];
	for (var i = 0; i < missing.length; i++) {
		if (!window[missing[i]]) {
			var f = function() { throw new TypeError("Illegal constructor"); };
			Object.defineProperty(f, 'name', { value: missing[i], configurable: true });
			f.prototype = Object.create(null);
			f.prototype.constructor = f;
			Object.defineProperty(f.prototype, Symbol.toStringTag, { value: missing[i], configurable: true });
			window[missing[i]] = f;
		}
	}
})();

// Range and Selection
window.Range = function() { this.collapsed = true; this.startOffset = 0; this.endOffset = 0; };
window.Range.prototype.setStart = function() {};
window.Range.prototype.setEnd = function() {};
window.Range.prototype.collapse = function() {};
window.Range.prototype.getBoundingClientRect = function() { return {top: 0, left: 0, bottom: 0, right: 0, width: 0, height: 0}; };
window.Selection = function() {};
window.Selection.prototype.getRangeAt = function() { return new Range(); };
window.Selection.prototype.rangeCount = 0;
window.getSelection = function() { return new Selection(); };

// NodeList / HTMLCollection / DOMTokenList / NamedNodeMap, expose on window
// (constructors already defined early, before _mkEl)
window.NodeList = NodeList;
window.HTMLCollection = HTMLCollection;
window.DOMTokenList = DOMTokenList;
window.NamedNodeMap = NamedNodeMap;

// --- Missing browser APIs (comprehensive stubs) ---

// DOMException constructor
window.DOMException = function(message, name) {
	this.message = message || "";
	this.name = name || "Error";
	this.code = 0;
};
window.DOMException.prototype = Object.create(Error.prototype);
window.DOMException.prototype.constructor = window.DOMException;

// DOMParser
window.DOMParser = function() {};
window.DOMParser.prototype.parseFromString = function(str, type) {
	return {
		documentElement: _mkEl("html"),
		body: _mkEl("body"),
		head: _mkEl("head"),
		querySelector: function() { return null; },
		querySelectorAll: function() { return []; },
		getElementById: function() { return null; },
		getElementsByTagName: function() { var r = []; Object.setPrototypeOf(r, HTMLCollection.prototype); return r; }
	};
};

// document.fonts (FontFaceSet), must match Chrome's FontFaceSet interface
document.fonts = (function() {
	var _fonts = {
		ready: Promise.resolve(),
		check: function() { return true; },
		load: function() { return Promise.resolve([]); },
		has: function() { return false; },
		add: function() { return this; },
		delete: function() { return false; },
		clear: function() {},
		forEach: function() {},
		entries: function() { return [][Symbol.iterator](); },
		keys: function() { return [][Symbol.iterator](); },
		values: function() { return [][Symbol.iterator](); },
		size: 0,
		status: "loaded",
		onloading: null,
		onloadingdone: null,
		onloadingerror: null,
		addEventListener: function() {},
		removeEventListener: function() {},
		dispatchEvent: function() { return true; }
	};
	_fonts[Symbol.iterator] = function() { return [][Symbol.iterator](); };
	Object.setPrototypeOf(_fonts, EventTarget.prototype);
	Object.defineProperty(_fonts, Symbol.toStringTag, { value: 'FontFaceSet', configurable: true });
	return _fonts;
})();

// document.currentScript
document.currentScript = null;

// document.adoptedStyleSheets
document.adoptedStyleSheets = [];

// document.timeline
document.timeline = { currentTime: performance.now() };

// document.elementsFromPoint
document.elementsFromPoint = function() { return []; };
document.elementFromPoint = function() { return document.body; };
document.caretRangeFromPoint = function() { return null; };

// document.featurePolicy / permissionsPolicy, Turnstile checks these
document.featurePolicy = {
	allowsFeature: function() { return true; },
	allowedFeatures: function() { return ["accelerometer","autoplay","camera","cross-origin-isolated","display-capture","encrypted-media","fullscreen","geolocation","gyroscope","magnetometer","microphone","midi","payment","picture-in-picture","publickey-credentials-get","screen-wake-lock","sync-xhr","usb","web-share","xr-spatial-tracking"]; },
	features: function() { return this.allowedFeatures(); },
	getAllowlistForFeature: function() { return ["*"]; }
};
document.permissionsPolicy = document.featurePolicy;

// NodeFilter constants (used by createNodeIterator/createTreeWalker)
window.NodeFilter = {
	FILTER_ACCEPT: 1, FILTER_REJECT: 2, FILTER_SKIP: 3,
	SHOW_ALL: 0xFFFFFFFF, SHOW_ELEMENT: 1, SHOW_ATTRIBUTE: 2,
	SHOW_TEXT: 4, SHOW_CDATA_SECTION: 8, SHOW_PROCESSING_INSTRUCTION: 64,
	SHOW_COMMENT: 128, SHOW_DOCUMENT: 256, SHOW_DOCUMENT_TYPE: 512,
	SHOW_DOCUMENT_FRAGMENT: 1024
};

// PerformanceObserver
window.PerformanceObserver = function(callback) {
	this.observe = function() {};
	this.disconnect = function() {};
	this.takeRecords = function() { return []; };
};
window.PerformanceObserver.supportedEntryTypes = ["mark", "measure", "navigation", "resource", "paint", "longtask"];

// MessageChannel / MessagePort, real implementation for React scheduler
window.MessagePort = function() {
	this.onmessage = null;
	this.onmessageerror = null;
	this._partner = null;
	this._listeners = {};
	this._started = false;
	this._queue = [];
	this.start = function() { this._started = true; this._flush(); };
	this.close = function() { this._started = false; };
	this.postMessage = function(data) {
		var target = this._partner;
		if (!target) return;
		var msg = {data: data, ports: [], origin: ''};
		if (target._started || target.onmessage) {
			// Deliver async via microtask (matches browser behavior)
			var t = target;
			Promise.resolve().then(function() {
				var evt = {data: msg.data, ports: [], origin: '', type: 'message'};
				if (t.onmessage) t.onmessage(evt);
				if (t._listeners['message']) {
					for (var i = 0; i < t._listeners['message'].length; i++) {
						t._listeners['message'][i](evt);
					}
				}
			});
		} else {
			target._queue.push(msg);
		}
	};
	this._flush = function() {
		while (this._queue.length > 0) {
			var msg = this._queue.shift();
			var evt = {data: msg.data, ports: [], origin: '', type: 'message'};
			if (this.onmessage) this.onmessage(evt);
			if (this._listeners['message']) {
				for (var i = 0; i < this._listeners['message'].length; i++) {
					this._listeners['message'][i](evt);
				}
			}
		}
	};
	this.addEventListener = function(type, fn) {
		if (!this._listeners[type]) this._listeners[type] = [];
		this._listeners[type].push(fn);
	};
	this.removeEventListener = function(type, fn) {
		if (this._listeners[type]) {
			var idx = this._listeners[type].indexOf(fn);
			if (idx !== -1) this._listeners[type].splice(idx, 1);
		}
	};
};
window.MessageChannel = function() {
	this.port1 = new MessagePort();
	this.port2 = new MessagePort();
	this.port1._partner = this.port2;
	this.port2._partner = this.port1;
	// Auto-start (React expects ports to be active without explicit start())
	this.port1._started = true;
	this.port2._started = true;
};

// BroadcastChannel
window.BroadcastChannel = function(name) {
	this.name = name;
	this.onmessage = null;
	this.postMessage = function() {};
	this.close = function() {};
	this.addEventListener = function() {};
	this.removeEventListener = function() {};
};

// process.env, React checks process.env.NODE_ENV for production/development mode
if (typeof process === 'undefined') {
	window.process = { env: { NODE_ENV: 'production' }, version: 'v18.0.0', versions: {}, platform: 'linux', nextTick: function(fn) { Promise.resolve().then(fn); } };
}

// setImmediate, React scheduler fallback
if (typeof setImmediate === 'undefined') {
	window.setImmediate = function(fn) { return setTimeout(fn, 0); };
	window.clearImmediate = function(id) { clearTimeout(id); };
}

// structuredClone
window.structuredClone = function(value) {
	try { return JSON.parse(_safeStringify(value)); } catch(e) { return value; }
};

// caches (CacheStorage)
window.caches = {
	open: function() { return Promise.resolve({ match: function() { return Promise.resolve(undefined); }, put: function() { return Promise.resolve(); }, add: function() { return Promise.resolve(); }, delete: function() { return Promise.resolve(false); }, keys: function() { return Promise.resolve([]); } }); },
	match: function() { return Promise.resolve(undefined); },
	has: function() { return Promise.resolve(false); },
	delete: function() { return Promise.resolve(false); },
	keys: function() { return Promise.resolve([]); }
};

// indexedDB
window.indexedDB = {
	open: function(name, version) {
		return {
			result: null, error: null, readyState: "done",
			onsuccess: null, onerror: null, onupgradeneeded: null,
			addEventListener: function() {}, removeEventListener: function() {}
		};
	},
	deleteDatabase: function() { return { onsuccess: null, onerror: null }; }
};

// visualViewport, must match iframe dimensions (innerWidth/innerHeight)
window.visualViewport = {
	width: 300, height: 65, offsetLeft: 0, offsetTop: 0,
	pageLeft: 0, pageTop: 0, scale: 1,
	onresize: null, onscroll: null, onscrollend: null,
	addEventListener: function() {}, removeEventListener: function() {},
	dispatchEvent: function() { return true; }
};
Object.setPrototypeOf(window.visualViewport, EventTarget.prototype);
Object.defineProperty(window.visualViewport, Symbol.toStringTag, { value: 'VisualViewport', configurable: true });

// AudioContext / OfflineAudioContext, critical for audio fingerprinting
// Cloudflare Turnstile checks for their existence and may use audio fingerprinting
window.AudioContext = window.AudioContext || (function() {
	var AC = function() {
		this.sampleRate = 44100;
		this.state = 'running';
		this.currentTime = 0;
		this.baseLatency = 0.005333333333333333;
		this.outputLatency = 0;
		this.destination = {
			maxChannelCount: 2, numberOfInputs: 1, numberOfOutputs: 0,
			channelCount: 2, channelCountMode: 'explicit',
			channelInterpretation: 'speakers', context: this
		};
		this.listener = {
			positionX: {value: 0}, positionY: {value: 0}, positionZ: {value: 0},
			forwardX: {value: 0}, forwardY: {value: 0}, forwardZ: {value: -1},
			upX: {value: 0}, upY: {value: 1}, upZ: {value: 0},
			setPosition: function() {}, setOrientation: function() {}
		};
	};
	AC.prototype.createOscillator = function() {
		return {
			type: 'sine', frequency: {value: 440, setValueAtTime: function(){}},
			detune: {value: 0}, connect: function() { return this; },
			disconnect: function() {}, start: function() {}, stop: function() {},
			addEventListener: function() {}, removeEventListener: function() {}
		};
	};
	AC.prototype.createDynamicsCompressor = function() {
		return {
			threshold: {value: -24}, knee: {value: 30}, ratio: {value: 12},
			attack: {value: 0.003}, release: {value: 0.25},
			reduction: 0, connect: function() { return this; }, disconnect: function() {},
			addEventListener: function() {}, removeEventListener: function() {}
		};
	};
	AC.prototype.createGain = function() {
		return {
			gain: {value: 1, setValueAtTime: function(){}},
			connect: function() { return this; }, disconnect: function() {},
			addEventListener: function() {}, removeEventListener: function() {}
		};
	};
	AC.prototype.createAnalyser = function() {
		return {
			fftSize: 2048, frequencyBinCount: 1024, minDecibels: -100, maxDecibels: -30,
			smoothingTimeConstant: 0.8,
			getFloatFrequencyData: function(arr) { for (var i = 0; i < arr.length; i++) arr[i] = -100; },
			getByteFrequencyData: function(arr) { for (var i = 0; i < arr.length; i++) arr[i] = 0; },
			getFloatTimeDomainData: function(arr) { for (var i = 0; i < arr.length; i++) arr[i] = 0; },
			getByteTimeDomainData: function(arr) { for (var i = 0; i < arr.length; i++) arr[i] = 128; },
			connect: function() { return this; }, disconnect: function() {},
			addEventListener: function() {}, removeEventListener: function() {}
		};
	};
	AC.prototype.createBiquadFilter = function() {
		return {
			type: 'lowpass', frequency: {value: 350}, Q: {value: 1},
			gain: {value: 0}, detune: {value: 0},
			connect: function() { return this; }, disconnect: function() {},
			addEventListener: function() {}, removeEventListener: function() {}
		};
	};
	AC.prototype.createBufferSource = function() {
		return {
			buffer: null, loop: false, loopStart: 0, loopEnd: 0,
			playbackRate: {value: 1}, detune: {value: 0},
			connect: function() { return this; }, disconnect: function() {},
			start: function() {}, stop: function() {},
			addEventListener: function() {}, removeEventListener: function() {}
		};
	};
	AC.prototype.createBuffer = function(channels, length, sampleRate) {
		var data = [];
		for (var c = 0; c < channels; c++) data.push(new Float32Array(length));
		return {
			numberOfChannels: channels, length: length, sampleRate: sampleRate,
			duration: length / sampleRate,
			getChannelData: function(ch) { return data[ch] || new Float32Array(0); },
			copyFromChannel: function() {}, copyToChannel: function() {}
		};
	};
	AC.prototype.createScriptProcessor = function(bufferSize, inputChannels, outputChannels) {
		return {
			bufferSize: bufferSize || 4096,
			onaudioprocess: null,
			connect: function() { return this; }, disconnect: function() {},
			addEventListener: function() {}, removeEventListener: function() {}
		};
	};
	AC.prototype.createChannelSplitter = function() {
		return { connect: function() { return this; }, disconnect: function() {} };
	};
	AC.prototype.createChannelMerger = function() {
		return { connect: function() { return this; }, disconnect: function() {} };
	};
	AC.prototype.decodeAudioData = function() { return Promise.resolve(this.createBuffer(2, 44100, 44100)); };
	AC.prototype.close = function() { this.state = 'closed'; return Promise.resolve(); };
	AC.prototype.resume = function() { this.state = 'running'; return Promise.resolve(); };
	AC.prototype.suspend = function() { this.state = 'suspended'; return Promise.resolve(); };
	AC.prototype.addEventListener = function() {};
	AC.prototype.removeEventListener = function() {};
	return AC;
})();
window.OfflineAudioContext = window.OfflineAudioContext || (function() {
	var OAC = function(channels, length, sampleRate) {
		this.sampleRate = sampleRate || 44100;
		this.length = length || 44100;
		this.numberOfChannels = channels || 1;
		this.state = 'suspended';
		this.currentTime = 0;
		this.destination = {
			maxChannelCount: channels || 1, numberOfInputs: 1, numberOfOutputs: 0,
			channelCount: channels || 1, channelCountMode: 'explicit',
			channelInterpretation: 'speakers', context: this
		};
		this.oncomplete = null;
	};
	OAC.prototype = Object.create(window.AudioContext.prototype);
	OAC.prototype.constructor = OAC;
	OAC.prototype.startRendering = function() {
		var self = this;
		console.log('[VMTRACE] OfflineAudioContext.startRendering() called: channels=' + self.numberOfChannels + ' length=' + self.length + ' sampleRate=' + self.sampleRate);
		// Return a buffer with real Chrome audio fingerprint data
		var buf = {
			numberOfChannels: self.numberOfChannels,
			length: self.length,
			sampleRate: self.sampleRate,
			duration: self.length / self.sampleRate,
			getChannelData: function(ch) {
				// Use real Chrome audio samples from embedded binary
				if (typeof _chromeAudioSamples !== 'undefined' && _chromeAudioSamples.length > 0) {
					var src = _chromeAudioSamples;
					var data = new Float32Array(self.length);
					var copyLen = Math.min(src.length, self.length);
					for (var i = 0; i < copyLen; i++) {
						data[i] = src[i];
					}
					console.log('[VMTRACE] audioContext.getChannelData(' + ch + '): length=' + data.length + ' first5=[' + data[0] + ',' + data[1] + ',' + data[2] + ',' + data[3] + ',' + data[4] + '] absSum=' + data.reduce(function(a,b){return a+Math.abs(b);},0));
					return data;
				}
				// Fallback: should not happen if samples are embedded
				console.log('[VMTRACE] WARNING: _chromeAudioSamples not available, returning zeros');
				return new Float32Array(self.length);
			},
			copyFromChannel: function() {},
			copyToChannel: function() {}
		};
		self.state = 'closed';
		if (self.oncomplete) {
			setTimeout(function() { self.oncomplete({renderedBuffer: buf}); }, 1);
		}
		return Promise.resolve(buf);
	};
	return OAC;
})();
// webkitAudioContext, Chrome alias for AudioContext
// openDatabase, deprecated Web SQL but Chrome still has it
window.openDatabase = function(name, version, desc, size) { return null; };
// FontFaceSet constructor
if (typeof window.FontFaceSet === 'undefined') {
	window.FontFaceSet = function() { throw new TypeError('Illegal constructor'); };
	window.FontFaceSet.prototype = document.fonts ? Object.getPrototypeOf(document.fonts) : Object.create(EventTarget.prototype);
}
// webkitAudioContext, Chrome alias for AudioContext (BM script checks for it)
window.webkitAudioContext = window.AudioContext;
window.webkitOfflineAudioContext = window.OfflineAudioContext;

// speechSynthesis, Chrome on macOS has these voices
window.speechSynthesis = {
	speaking: false, pending: false, paused: false, runningState: 'stopped',
	getVoices: function() {
		var V = function(name, lang, def, local) { return {name: name, lang: lang, default: def, localService: local, voiceURI: name}; };
		return [
			V('Alex', 'en-US', true, true),
			V('Daniel', 'en-GB', false, true),
			V('Karen', 'en-AU', false, true),
			V('Moira', 'en-IE', false, true),
			V('Samantha', 'en-US', false, true),
			V('Tessa', 'en-ZA', false, true),
			V('Google US English', 'en-US', false, false),
			V('Google UK English Female', 'en-GB', false, false),
			V('Google UK English Male', 'en-GB', false, false)
		];
	},
	speak: function() {}, cancel: function() {}, pause: function() {}, resume: function() {},
	addEventListener: function() {}, removeEventListener: function() {},
	onvoiceschanged: null
};

// SpeechSynthesisUtterance, Kasada calls new SpeechSynthesisUtterance()
window.SpeechSynthesisUtterance = function(text) {
	this.text = text || "";
	this.lang = "";
	this.voice = null;
	this.volume = 1;
	this.rate = 1;
	this.pitch = 1;
	this.onstart = null;
	this.onend = null;
	this.onerror = null;
	this.onpause = null;
	this.onresume = null;
	this.onmark = null;
	this.onboundary = null;
};
Object.defineProperty(window.SpeechSynthesisUtterance, 'name', { value: 'SpeechSynthesisUtterance', configurable: true });
window.SpeechSynthesisUtterance.prototype = Object.create(EventTarget.prototype);
window.SpeechSynthesisUtterance.prototype.constructor = window.SpeechSynthesisUtterance;
Object.defineProperty(window.SpeechSynthesisUtterance.prototype, Symbol.toStringTag, { value: 'SpeechSynthesisUtterance', configurable: true });

// Notification
window.Notification = function() {};
window.Notification.permission = "granted";
window.Notification.requestPermission = function() { return Promise.resolve("granted"); };

// --- performance.memory (Chrome-specific, V8 heap stats) ---
if (!performance.memory) {
	Object.defineProperty(performance, 'memory', {
		get: function() {
			return {
				jsHeapSizeLimit: 4294705152,
				totalJSHeapSize: 39000000 + Math.floor(Math.random() * 5000000),
				usedJSHeapSize: 24000000 + Math.floor(Math.random() * 5000000)
			};
		},
		configurable: true, enumerable: true
	});
}

// --- Intl.DateTimeFormat timezone override ---
// V8 defaults to UTC. Override resolvedOptions() to return a real timezone.
(function() {
	if (typeof Intl !== 'undefined' && Intl.DateTimeFormat) {
		var _origDTF = Intl.DateTimeFormat;
		var _tzones = ['America/New_York','America/Chicago','America/Denver','America/Los_Angeles',
			'America/Phoenix','America/Anchorage','Pacific/Honolulu','America/Detroit',
			'America/Indiana/Indianapolis','America/Boise'];
		var _tz = _tzones[Math.floor(Math.random() * _tzones.length)];
		var _origResolvedOptions = _origDTF.prototype.resolvedOptions;
		_origDTF.prototype.resolvedOptions = function() {
			var opts = _origResolvedOptions.call(this);
			if (opts.timeZone === 'UTC' || !opts.timeZone) opts.timeZone = _tz;
			return opts;
		};
	}
})();

// --- SharedWorker: throw TypeError("Illegal constructor") ---
// Chrome throws this when you try new SharedWorker(blobURL). The BM script
// catches it and records the error message in the s151 signal field.
window.SharedWorker = function SharedWorker() {
	throw new TypeError("Failed to construct 'SharedWorker': 1 argument required, but only 0 present.");
};
Object.defineProperty(window.SharedWorker, 'name', { value: 'SharedWorker', configurable: true });
window.SharedWorker.prototype = {};
Object.defineProperty(window.SharedWorker.prototype, Symbol.toStringTag, { value: 'SharedWorker', configurable: true });

// --- CSS.supports() ---
if (typeof CSS === 'undefined') {
	window.CSS = {};
}
if (!CSS.supports) {
	CSS.supports = function(prop, val) {
		if (arguments.length === 1) return true; // CSS.supports("display: flex") form
		// Check common CSS properties
		var supported = ['display','flex','grid','position','transform','opacity','color',
			'background','border','margin','padding','font','width','height','overflow',
			'visibility','cursor','z-index','animation','transition','filter'];
		return supported.indexOf(prop) >= 0 || prop.startsWith('-webkit-');
	};
}
if (!CSS.escape) {
	CSS.escape = function(s) { return s.replace(/([^\w-])/g, '\\$1'); };
}

// Additional event constructors
window.PointerEvent = function(type, opts) { window.Event.call(this, type, opts); this.pointerId = (opts && opts.pointerId) || 0; this.pointerType = (opts && opts.pointerType) || "mouse"; this.width = 1; this.height = 1; this.pressure = 0; this.tiltX = 0; this.tiltY = 0; this.isPrimary = true; };
window.WheelEvent = function(type, opts) { window.Event.call(this, type, opts); this.deltaX = 0; this.deltaY = 0; this.deltaZ = 0; this.deltaMode = 0; };
window.TouchEvent = function(type, opts) { window.Event.call(this, type, opts); this.touches = []; this.targetTouches = []; this.changedTouches = []; };
window.FocusEvent = function(type, opts) { window.Event.call(this, type, opts); this.relatedTarget = null; };
window.InputEvent = function(type, opts) { window.Event.call(this, type, opts); this.data = (opts && opts.data) || null; this.inputType = (opts && opts.inputType) || ""; };
window.AnimationEvent = function(type, opts) { window.Event.call(this, type, opts); };
window.TransitionEvent = function(type, opts) { window.Event.call(this, type, opts); };
window.ErrorEvent = function(type, opts) { window.Event.call(this, type, opts); this.message = (opts && opts.message) || ""; this.filename = ""; this.lineno = 0; this.colno = 0; this.error = null; };
window.PromiseRejectionEvent = function(type, opts) { window.Event.call(this, type, opts); this.promise = (opts && opts.promise) || null; this.reason = (opts && opts.reason) || null; };

// DOMRect / DOMRectReadOnly
window.DOMRect = function(x, y, w, h) { this.x = x||0; this.y = y||0; this.width = w||0; this.height = h||0; this.top = this.y; this.left = this.x; this.bottom = this.y + this.height; this.right = this.x + this.width; };
window.DOMRectReadOnly = window.DOMRect;

// --- Add Document.prototype properties to match Chrome (251 own properties) ---
// In Chrome, Document.prototype has ~251 properties. Most are methods or event handlers.
// The _m2p call below will migrate document's own properties to Document.prototype,
// but we need to pre-populate Document.prototype with stubs for properties that
// don't exist on the document object itself.

// Document.prototype methods (no-op stubs for missing ones)
['adoptNode','append','captureEvents','caretPositionFromPoint',
'clear','close','createAttribute','createAttributeNS',
'createCDATASection','createComment','createDocumentFragment',
'createElementNS','createEvent','createExpression',
'createNSResolver','createNodeIterator','createProcessingInstruction',
'createRange','createTextNode','createTreeWalker',
'evaluate','execCommand','exitFullscreen','exitPictureInPicture',
'exitPointerLock','getAnimations','getElementsByName',
'getElementsByTagNameNS','getSelection',
'hasPrivateToken','hasRedemptionRecord','hasStorageAccess',
'hasUnpartitionedCookieAccess','importNode','moveBefore',
'open','prepend','queryCommandEnabled','queryCommandIndeterm',
'queryCommandState','queryCommandSupported','queryCommandValue',
'releaseEvents','replaceChildren','requestStorageAccess',
'requestStorageAccessFor','startViewTransition',
'webkitCancelFullScreen','webkitExitFullscreen',
'write','writeln','browsingTopics','ariaNotify'].forEach(function(name) {
	if (!(name in Document.prototype)) {
		Document.prototype[name] = function() {};
	}
});
// Document.prototype methods that return specific types
if (!('hasFocus' in Document.prototype)) Document.prototype.hasFocus = function() { return false; };
if (!('querySelector' in Document.prototype)) Document.prototype.querySelector = function(sel) { return document.querySelector(sel); };
if (!('querySelectorAll' in Document.prototype)) Document.prototype.querySelectorAll = function(sel) { return document.querySelectorAll(sel); };
if (!('getElementById' in Document.prototype)) Document.prototype.getElementById = function(id) { return document.getElementById(id); };
if (!('getElementsByClassName' in Document.prototype)) Document.prototype.getElementsByClassName = function(cls) { return document.getElementsByClassName(cls); };
if (!('getElementsByTagName' in Document.prototype)) Document.prototype.getElementsByTagName = function(tag) { return document.getElementsByTagName(tag); };
if (!('createElement' in Document.prototype)) Document.prototype.createElement = function(tag) { return document.createElement(tag); };
if (!('createElementNS' in Document.prototype)) Document.prototype.createElementNS = function(ns, tag) { return document.createElement(tag); };
if (!('caretRangeFromPoint' in Document.prototype)) Document.prototype.caretRangeFromPoint = function() { return null; };
if (!('elementFromPoint' in Document.prototype)) Document.prototype.elementFromPoint = function() { return document.body || null; };
if (!('elementsFromPoint' in Document.prototype)) Document.prototype.elementsFromPoint = function() { return document.body ? [document.body] : []; };

// Document.prototype getter-like properties (null/empty defaults)
// Properties that must be arrays (not null) when accessed via prototype
var _docArrayProps = {'adoptedStyleSheets':1,'styleSheets':1,'anchors':1,'applets':1,'children':1,'embeds':1,'forms':1,'images':1,'links':1,'plugins':1,'scripts':1};
['activeElement','activeViewTransition','adoptedStyleSheets','all','anchors',
'applets','body','characterSet','charset','childElementCount','children',
'compatMode','contentType','currentScript','customElementRegistry',
'defaultView','designMode','dir','doctype','documentElement',
'documentURI','domain','embeds','featurePolicy','firstElementChild',
'fonts','forms','fragmentDirective','fullscreenElement',
'head','images','implementation','inputEncoding',
'lastElementChild','lastModified','links',
'pictureInPictureElement','plugins','pointerLockElement',
'readyState','referrer','rootElement','scripts',
'scrollingElement','styleSheets','timeline','title',
'webkitCurrentFullScreenElement','webkitFullscreenElement',
'xmlEncoding','xmlVersion'].forEach(function(name) {
	if (!(name in Document.prototype)) {
		Document.prototype[name] = _docArrayProps[name] ? [] : null;
	}
});
// String properties with specific defaults
if (!('URL' in Document.prototype)) Document.prototype.URL = '';
if (!('cookie' in Document.prototype)) Document.prototype.cookie = '';
if (!('visibilityState' in Document.prototype)) Document.prototype.visibilityState = 'visible';
if (!('alinkColor' in Document.prototype)) Document.prototype.alinkColor = '';
if (!('bgColor' in Document.prototype)) Document.prototype.bgColor = '';
if (!('fgColor' in Document.prototype)) Document.prototype.fgColor = '';
if (!('linkColor' in Document.prototype)) Document.prototype.linkColor = '';
if (!('vlinkColor' in Document.prototype)) Document.prototype.vlinkColor = '';
if (!('xmlStandalone' in Document.prototype)) Document.prototype.xmlStandalone = false;
// Boolean properties
if (!('fullscreen' in Document.prototype)) Document.prototype.fullscreen = false;
if (!('fullscreenEnabled' in Document.prototype)) Document.prototype.fullscreenEnabled = true;
if (!('hidden' in Document.prototype)) Document.prototype.hidden = false;
if (!('pictureInPictureEnabled' in Document.prototype)) Document.prototype.pictureInPictureEnabled = true;
if (!('prerendering' in Document.prototype)) Document.prototype.prerendering = false;
if (!('wasDiscarded' in Document.prototype)) Document.prototype.wasDiscarded = false;
if (!('webkitFullscreenEnabled' in Document.prototype)) Document.prototype.webkitFullscreenEnabled = true;
if (!('webkitHidden' in Document.prototype)) Document.prototype.webkitHidden = false;
if (!('webkitIsFullScreen' in Document.prototype)) Document.prototype.webkitIsFullScreen = false;
if (!('webkitVisibilityState' in Document.prototype)) Document.prototype.webkitVisibilityState = 'visible';

// Document.prototype event handler properties (all null by default in Chrome)
['onabort','onanimationcancel','onanimationend','onanimationiteration',
'onanimationstart','onauxclick','onbeforecopy','onbeforecut','onbeforeinput',
'onbeforematch','onbeforepaste','onbeforetoggle','onbeforexrselect',
'onblur','oncancel','oncanplay','oncanplaythrough','onchange','onclick',
'onclose','oncommand','oncontentvisibilityautostatechange','oncontextlost',
'oncontextmenu','oncontextrestored','oncopy','oncuechange','oncut',
'ondblclick','ondrag','ondragend','ondragenter','ondragleave','ondragover',
'ondragstart','ondrop','ondurationchange','onemptied','onended','onerror',
'onfocus','onformdata','onfreeze','onfullscreenchange','onfullscreenerror',
'ongotpointercapture','oninput','oninvalid','onkeydown','onkeypress',
'onkeyup','onload','onloadeddata','onloadedmetadata','onloadstart',
'onlostpointercapture','onmousedown','onmouseenter','onmouseleave',
'onmousemove','onmouseout','onmouseover','onmouseup','onmousewheel',
'onpaste','onpause','onplay','onplaying','onpointercancel','onpointerdown',
'onpointerenter','onpointerleave','onpointerlockchange','onpointerlockerror',
'onpointermove','onpointerout','onpointerover','onpointerrawupdate',
'onpointerup','onprerenderingchange','onprogress','onratechange',
'onreadystatechange','onreset','onresize','onresume','onscroll',
'onscrollend','onscrollsnapchange','onscrollsnapchanging','onsearch',
'onsecuritypolicyviolation','onseeked','onseeking','onselect',
'onselectionchange','onselectstart','onslotchange','onstalled','onsubmit',
'onsuspend','ontimeupdate','ontoggle','ontransitioncancel','ontransitionend',
'ontransitionrun','ontransitionstart','onvisibilitychange','onvolumechange',
'onwaiting','onwebkitanimationend','onwebkitanimationiteration',
'onwebkitanimationstart','onwebkitfullscreenchange','onwebkitfullscreenerror',
'onwebkittransitionend','onwheel'].forEach(function(name) {
	if (!(name in Document.prototype)) {
		Document.prototype[name] = null;
	}
});

// --- Set prototype chains on core objects ---
// This makes Object.prototype.toString.call(window) return "[object Window]" etc.
// Without this, all our objects show "[object Object]", a trivial bot detection signal.
Object.setPrototypeOf(navigator, Navigator.prototype);
Object.defineProperty(navigator, Symbol.toStringTag, { value: 'Navigator', configurable: true });
// document prototype already set early (after DOM tree creation) for native getter access.
// This is a no-op but kept for clarity.
Object.setPrototypeOf(document, HTMLDocument.prototype);
Object.defineProperty(document, Symbol.toStringTag, { value: 'HTMLDocument', configurable: true });
// screen prototype is set natively by engine.go setupScreen()
Object.setPrototypeOf(window, Window.prototype);
Object.defineProperty(window, Symbol.toStringTag, { value: 'Window', configurable: true });
try { Object.defineProperty(location, Symbol.toStringTag, { value: 'Location', configurable: true }); } catch(e) { console.log('[FIX] location toStringTag failed: ' + e); }
try { Object.defineProperty(window.performance, Symbol.toStringTag, { value: 'Performance', configurable: true }); } catch(e) { console.log('[FIX] performance toStringTag failed: ' + e); }
try {
	if (navigator.connection) {
		Object.setPrototypeOf(navigator.connection, EventTarget.prototype);
		Object.defineProperty(navigator.connection, Symbol.toStringTag, { value: 'NetworkInformation', configurable: true });
	}
} catch(e) { console.log('[FIX] connection setup failed: ' + e); }

// --- Migrate own properties to prototypes (Chrome-accurate) ---
// In real Chrome, navigator/screen/document have NO own properties, everything is
// on Navigator.prototype/Screen.prototype/Document.prototype as getters.
// NOTE: document's own properties go to Document.prototype (NOT HTMLDocument.prototype).
// Chrome's HTMLDocument.prototype only has constructor.
let _m2p = function(obj, proto) {
	var names = Object.getOwnPropertyNames(obj);
	for (var i = 0; i < names.length; i++) {
		var key = names[i];
		if (key === 'constructor' || typeof key === 'symbol') continue;
		var desc = Object.getOwnPropertyDescriptor(obj, key);
		if (!desc) continue;
		if (typeof desc.value === 'function') {
			Object.defineProperty(proto, key, {
				value: desc.value, writable: true, configurable: true, enumerable: true
			});
		} else if (desc.get || desc.set) {
			// Ensure getter/setter names match Chrome's format: "get propName" / "set propName"
			if (desc.get && typeof desc.get === 'function') {
				try { Object.defineProperty(desc.get, 'name', { value: 'get ' + key, configurable: true }); } catch(e) {}
			}
			if (desc.set && typeof desc.set === 'function') {
				try { Object.defineProperty(desc.set, 'name', { value: 'set ' + key, configurable: true }); } catch(e) {}
			}
			Object.defineProperty(proto, key, desc);
		} else {
			// Chrome Web API properties are getter-only (no setter) on prototypes.
			// Use a closure to store the value, but ONLY expose a getter.
			// Internal code can still override via Object.defineProperty (configurable: true).
			(function(k, v) {
				var getter = function() { return v; };
				// Chrome native getters toString as "function get propName() { [native code] }"
				try { Object.defineProperty(getter, 'name', { value: 'get ' + k, configurable: true }); } catch(e) {}
				Object.defineProperty(proto, k, {
					get: getter,
					set: undefined,
					configurable: true,
					enumerable: true
				});
			})(key, desc.value);
		}
		// Only delete from instance if it's a function/getter that we migrated to prototype.
		// Complex object properties (plugins, mimeTypes, etc.) must stay on the instance
		// because native V8 objects (navigator, document) have a V8-native prototype chain
		// that doesn't include the JS-defined prototype we're writing to.
		if (typeof desc.value === 'function' || desc.get || desc.set) {
			try { delete obj[key]; } catch(e) {}
		}
	}
};
_m2p(navigator, Navigator.prototype);
// CRITICAL FIX: Set complex navigator properties on Navigator.prototype (NOT as own props).
// Chrome: GOPN(navigator).length === 1. Own props on navigator are detected instantly.
// The native V8 navigator inherits from a V8-internal prototype, not our JS Navigator.prototype.
// Solution: use Object.setPrototypeOf to re-chain the native navigator to inherit from
// our JS Navigator.prototype which has the complex properties as getters.
(function() {
	var nav = window.navigator;
	if (!nav) return;
	// Re-chain: native_nav → Navigator.prototype (JS) → original_V8_proto
	var origProto = Object.getPrototypeOf(nav);
	// Only re-chain if Navigator.prototype isn't already in the chain
	if (origProto !== Navigator.prototype) {
		// First, make Navigator.prototype inherit from the V8-native proto (preserves scalar accessors)
		Object.setPrototypeOf(Navigator.prototype, origProto);
		// Then set the navigator instance's prototype to our JS Navigator.prototype
		Object.setPrototypeOf(nav, Navigator.prototype);
	}
	// Now properties set on Navigator.prototype are accessible via navigator.xxx
	// and GOPN(navigator) returns only own properties (should be ~1 or 0)
	var proto = Navigator.prototype;
	// plugins
	if (typeof proto.plugins === 'undefined' && typeof nav.plugins === 'undefined') {
		// Each plugin has indexed MimeType sub-items. Chrome has 5 plugins × 2 mimes.
		var _mkP = function(name) {
			var m0 = {type:'application/pdf', description:'Portable Document Format', suffixes:'pdf', enabledPlugin:null};
			var m1 = {type:'text/pdf', description:'Portable Document Format', suffixes:'pdf', enabledPlugin:null};
			var pl = {name:name, filename:'internal-pdf-viewer', description:'Portable Document Format', length:2, 0:m0, 1:m1};
			pl['application/pdf'] = m0; pl['text/pdf'] = m1;
			pl.item = function(i){return this[i]||null;}; pl.namedItem = function(n){for(var j=0;j<this.length;j++)if(this[j].type===n)return this[j];return null;};
			pl[Symbol.iterator] = function(){var idx=0,self=this;return{next:function(){return idx<self.length?{value:self[idx++],done:false}:{done:true}}};};
			m0.enabledPlugin = pl; m1.enabledPlugin = pl;
			return pl;
		};
		var items = [_mkP("PDF Viewer"),_mkP("Chrome PDF Plugin"),_mkP("Chrome PDF Viewer"),_mkP("Microsoft Edge PDF Viewer"),_mkP("WebKit built-in PDF")];
		var p = {};
		for (var i = 0; i < items.length; i++) p[i] = items[i];
		p.length = items.length;
		p.namedItem = function(name) { for (var j = 0; j < p.length; j++) { if (p[j].name === name) return p[j]; } return null; };
		p.item = function(i) { return p[i] || null; };
		p.refresh = function() {};
		p[Symbol.iterator] = function() { var idx = 0; return { next: function() { return idx < p.length ? { value: p[idx++], done: false } : { done: true }; } }; };
		Object.setPrototypeOf(p, PluginArray.prototype);
		proto.plugins = p;
	}
	// mimeTypes, Chrome has 2 MimeType entries for PDF
	if (typeof proto.mimeTypes === 'undefined') {
		var mt0 = {type:'application/pdf', description:'Portable Document Format', suffixes:'pdf', enabledPlugin: proto.plugins ? proto.plugins[0] : null};
		var mt1 = {type:'text/pdf', description:'Portable Document Format', suffixes:'pdf', enabledPlugin: proto.plugins ? proto.plugins[0] : null};
		var m = {0: mt0, 1: mt1, length: 2, 'application/pdf': mt0, 'text/pdf': mt1};
		m.item = function(i) { return m[i] || null; };
		m.namedItem = function(n) { for (var i = 0; i < m.length; i++) { if (m[i] && m[i].type === n) return m[i]; } return null; };
		m[Symbol.iterator] = function() { var idx = 0; return { next: function() { return idx < m.length ? { value: m[idx++], done: false } : { done: true }; } }; };
		Object.setPrototypeOf(m, MimeTypeArray.prototype);
		proto.mimeTypes = m;
	}
	// permissions
	if (typeof proto.permissions === 'undefined') {
		var _perms = { query: function(desc) { return Promise.resolve({state: 'prompt', name: desc ? desc.name : '', onchange: null, addEventListener: function(){}, removeEventListener: function(){}}); } };
		Object.setPrototypeOf(_perms, Permissions.prototype);
		proto.permissions = _perms;
	}
	// clipboard
	if (typeof proto.clipboard === 'undefined') {
		proto.clipboard = {
			readText: function() { return Promise.reject(new DOMException('NotAllowedError')); },
			read: function() { return Promise.reject(new DOMException('NotAllowedError')); },
			writeText: function(text) { return Promise.resolve(); },
			write: function(data) { return Promise.resolve(); }
		};
	}
	// keyboard
	if (typeof proto.keyboard === 'undefined') {
		proto.keyboard = {
			getLayoutMap: function() {
				var map = new Map();
				var keys = {'KeyA':'a','KeyB':'b','KeyC':'c','KeyD':'d','KeyE':'e','KeyF':'f','KeyG':'g','KeyH':'h',
					'KeyI':'i','KeyJ':'j','KeyK':'k','KeyL':'l','KeyM':'m','KeyN':'n','KeyO':'o','KeyP':'p',
					'KeyQ':'q','KeyR':'r','KeyS':'s','KeyT':'t','KeyU':'u','KeyV':'v','KeyW':'w','KeyX':'x',
					'KeyY':'y','KeyZ':'z','Digit0':'0','Digit1':'1','Digit2':'2','Digit3':'3','Digit4':'4',
					'Digit5':'5','Digit6':'6','Digit7':'7','Digit8':'8','Digit9':'9'};
				for (var k in keys) map.set(k, keys[k]);
				return Promise.resolve(map);
			},
			lock: function() { return Promise.resolve(); },
			unlock: function() { return Promise.resolve(); },
			addEventListener: function() {},
			removeEventListener: function() {}
		};
	}
	// connection, credentials, storage, locks, etc.
	if (typeof proto.connection === 'undefined') proto.connection = {effectiveType:'4g',type:'wifi',rtt:50,downlink:10,saveData:false,onchange:null,addEventListener:function(){},removeEventListener:function(){}};
	if (typeof proto.credentials === 'undefined') proto.credentials = {get:function(){return Promise.resolve(null);},create:function(){return Promise.resolve(null);},store:function(){return Promise.resolve();}};
	if (typeof proto.storage === 'undefined') proto.storage = {estimate:function(){return Promise.resolve({quota:1073741824,usage:0});},getDirectory:function(){return Promise.resolve({});},persist:function(){return Promise.resolve(false);}};
	if (typeof proto.locks === 'undefined') proto.locks = {request:function(){return Promise.resolve();},query:function(){return Promise.resolve({held:[],pending:[]});}};
	if (typeof proto.mediaDevices === 'undefined') proto.mediaDevices = {enumerateDevices:function(){return Promise.resolve([{deviceId:'',groupId:'default',kind:'audioinput',label:''},{deviceId:'',groupId:'default',kind:'audiooutput',label:''},{deviceId:'',groupId:'',kind:'videoinput',label:''}]);},getUserMedia:function(){return Promise.reject(new DOMException('NotAllowedError'));}};
	if (typeof proto.mediaCapabilities === 'undefined') proto.mediaCapabilities = {decodingInfo:function(){return Promise.resolve({supported:true,smooth:true,powerEfficient:true});}};
	if (typeof proto.serviceWorker === 'undefined') proto.serviceWorker = {controller:null,ready:Promise.resolve(null),register:function(){return Promise.reject(new DOMException('SecurityError'));},getRegistrations:function(){return Promise.resolve([]);}};
	if (typeof proto.geolocation === 'undefined') proto.geolocation = {getCurrentPosition:function(s,e){if(e)e({code:1,message:'denied'});},watchPosition:function(){return 0;},clearWatch:function(){}};
	if (typeof proto.gpu === 'undefined') proto.gpu = {requestAdapter:function(){return Promise.resolve(null);}};
	if (typeof proto.userActivation === 'undefined') proto.userActivation = {hasBeenActive:true,isActive:false};
	if (typeof proto.mediaSession === 'undefined') proto.mediaSession = {metadata:null,playbackState:'none',setActionHandler:function(){},setPositionState:function(){}};
	if (typeof proto.scheduling === 'undefined') proto.scheduling = {isInputPending:function(){return false;}};
	// Methods
	if (typeof proto.sendBeacon === 'undefined') proto.sendBeacon = function(){return true;};
	if (typeof proto.javaEnabled === 'undefined') proto.javaEnabled = function(){return false;};
	if (typeof proto.getGamepads === 'undefined') proto.getGamepads = function(){return [null,null,null,null];};
	if (typeof proto.vibrate === 'undefined') proto.vibrate = function(){return true;};
	if (typeof proto.getInstalledRelatedApps === 'undefined') proto.getInstalledRelatedApps = function(){return Promise.resolve([]);};
	if (typeof proto.getBattery === 'undefined') proto.getBattery = function(){return Promise.resolve({charging:true,chargingTime:0,dischargingTime:Infinity,level:1});};
})();
// screen properties are native accessors from engine.go setupScreen()
_m2p(document, Document.prototype);
// Re-chain document's prototype: native_doc → Document.prototype (JS) → original_V8_proto
// This ensures GOPN(document).length matches Chrome (1 own prop: 'location')
(function() {
	var doc = document;
	var origProto = Object.getPrototypeOf(doc);
	if (origProto !== Document.prototype && origProto !== HTMLDocument.prototype) {
		// Check for cycles: don't set if Document.prototype already has origProto in chain
		var p = origProto;
		var hasCycle = false;
		for (var _ci = 0; _ci < 20 && p; _ci++) {
			if (p === Document.prototype || p === HTMLDocument.prototype) { hasCycle = true; break; }
			p = Object.getPrototypeOf(p);
		}
		if (!hasCycle) {
			Object.setPrototypeOf(Document.prototype, origProto);
			Object.setPrototypeOf(doc, Document.prototype);
		} else {
			// Just set doc's prototype directly without touching Document.prototype
			try { Object.setPrototypeOf(doc, Document.prototype); } catch(e) {}
		}
	}
	// Move remaining own object properties to prototype (Chrome has them there)
	var ownNames = Object.getOwnPropertyNames(doc);
	for (var i = 0; i < ownNames.length; i++) {
		var k = ownNames[i];
		if (k === 'location') continue; // location stays as own prop (Chrome has it as own)
		var desc = Object.getOwnPropertyDescriptor(doc, k);
		if (!desc || !desc.configurable) continue;
		if (desc.get || desc.set) continue; // getters already migrated by _m2p
		// Move to Document.prototype as a getter
		(function(key, val) {
			Object.defineProperty(Document.prototype, key, {
				get: function() { return val; },
				set: function(v) { val = v; },
				configurable: true, enumerable: true
			});
		})(k, desc.value);
		try { delete doc[k]; } catch(e) {}
	}
})();

// Export browser constructors on window
window.EventTarget = EventTarget;
window.Node = Node;
window.Element = Element;
window.HTMLElement = HTMLElement;
window.Document = Document;
window.HTMLDocument = HTMLDocument;
window.Window = Window;
window.Navigator = Navigator;
// window.Screen is set natively by engine.go setupScreen()
window.ShadowRoot = ShadowRoot;

// --- Constructable Web APIs (defined BEFORE stubs so they aren't overwritten) ---
// These ARE constructable in Chrome. The VM tests constructability as a fingerprint signal.
window.MutationObserver = window.MutationObserver || function(cb) { this._cb = cb; this.observe = function(){}; this.disconnect = function(){}; this.takeRecords = function(){ return []; }; };
window.ResizeObserver = window.ResizeObserver || function(cb) { this._cb = cb; this.observe = function(){}; this.unobserve = function(){}; this.disconnect = function(){}; };
window.IntersectionObserver = window.IntersectionObserver || function(cb, opts) { this._cb = cb; this.root = (opts&&opts.root)||null; this.rootMargin = '0px'; this.thresholds = [0]; this.observe = function(){}; this.unobserve = function(){}; this.disconnect = function(){}; this.takeRecords = function(){ return []; }; };
window.PerformanceObserver = window.PerformanceObserver || function(cb) { this._cb = cb; this.observe = function(){}; this.disconnect = function(){}; this.takeRecords = function(){ return []; }; };
window.AbortController = window.AbortController || function() { var _aborted = false; this.signal = {aborted: false, reason: undefined, addEventListener: function(){}, removeEventListener: function(){}, throwIfAborted: function(){}}; this.abort = function(r) { _aborted = true; this.signal.aborted = true; this.signal.reason = r; }; };
window.AbortSignal = window.AbortSignal || function() {};
if (!window.AbortSignal.abort) window.AbortSignal.abort = function(r) { return {aborted: true, reason: r, addEventListener: function(){}, removeEventListener: function(){}}; };
if (!window.AbortSignal.timeout) window.AbortSignal.timeout = function() { return {aborted: false, addEventListener: function(){}, removeEventListener: function(){}}; };
window.MessageChannel = window.MessageChannel || function() { this.port1 = {postMessage: function(){}, close: function(){}, onmessage: null, addEventListener: function(){}, removeEventListener: function(){}}; this.port2 = {postMessage: function(){}, close: function(){}, onmessage: null, addEventListener: function(){}, removeEventListener: function(){}}; };
window.BroadcastChannel = window.BroadcastChannel || function(name) { this.name = name; this.postMessage = function(){}; this.close = function(){}; this.onmessage = null; this.addEventListener = function(){}; this.removeEventListener = function(){}; };
window.DOMParser = window.DOMParser || function() { this.parseFromString = function(str, type) { return document; }; };
window.XMLSerializer = window.XMLSerializer || function() { this.serializeToString = function(node) { return ''; }; };
window.TextDecoder = window.TextDecoder || function(encoding) { this.encoding = encoding || 'utf-8'; this.decode = function(buf) { if (!buf) return ''; var a = new Uint8Array(buf.buffer || buf); var s = ''; for (var i = 0; i < a.length; i++) s += String.fromCharCode(a[i]); return s; }; };
window.TextEncoder = window.TextEncoder || function() { this.encoding = 'utf-8'; this.encode = function(str) { var a = []; for (var i = 0; i < str.length; i++) a.push(str.charCodeAt(i) & 0xFF); return new Uint8Array(a); }; };
window.ReadableStream = window.ReadableStream || function() { this.locked = false; this.cancel = function(){ return Promise.resolve(); }; this.getReader = function(){ return {read: function(){ return Promise.resolve({done: true}); }, releaseLock: function(){}, cancel: function(){ return Promise.resolve(); }}; }; };
window.WritableStream = window.WritableStream || function() { this.locked = false; this.abort = function(){ return Promise.resolve(); }; this.close = function(){ return Promise.resolve(); }; this.getWriter = function(){ return {write: function(){ return Promise.resolve(); }, close: function(){ return Promise.resolve(); }, releaseLock: function(){}}; }; };
window.TransformStream = window.TransformStream || function() { this.readable = new ReadableStream(); this.writable = new WritableStream(); };
window.DOMRect = window.DOMRect || function(x,y,w,h) { this.x = x||0; this.y = y||0; this.width = w||0; this.height = h||0; this.top = this.y; this.right = this.x+this.width; this.bottom = this.y+this.height; this.left = this.x; };
window.DOMRectReadOnly = window.DOMRectReadOnly || window.DOMRect;
window.DOMPoint = window.DOMPoint || function(x,y,z,w) { this.x = x||0; this.y = y||0; this.z = z||0; this.w = w||1; };
window.DOMPointReadOnly = window.DOMPointReadOnly || window.DOMPoint;
window.DOMMatrix = window.DOMMatrix || function() { this.a=1; this.b=0; this.c=0; this.d=1; this.e=0; this.f=0; this.is2D = true; this.isIdentity = true; };
window.DOMMatrixReadOnly = window.DOMMatrixReadOnly || window.DOMMatrix;
window.Range = window.Range || function() { this.collapsed = true; this.commonAncestorContainer = document; this.startContainer = document; this.endContainer = document; this.startOffset = 0; this.endOffset = 0; this.cloneContents = function(){ return document.createDocumentFragment(); }; this.cloneRange = function(){ return new Range(); }; this.collapse = function(){}; this.setStart = function(){}; this.setEnd = function(){}; this.getBoundingClientRect = function(){ return new DOMRect(); }; this.getClientRects = function(){ return []; }; };
window.Selection = window.Selection || function() { this.anchorNode = null; this.anchorOffset = 0; this.focusNode = null; this.focusOffset = 0; this.isCollapsed = true; this.rangeCount = 0; this.type = 'None'; this.addRange = function(){}; this.removeAllRanges = function(){}; this.getRangeAt = function(){ return new Range(); }; this.toString = function(){ return ''; }; };
window.FontFace = window.FontFace || function(family, source) { this.family = family; this.status = 'loaded'; this.loaded = Promise.resolve(this); this.load = function(){ return this.loaded; }; };

// --- Chrome Web API constructor stubs ---
// Real Chrome 146 has ~1246 globals. Add the missing Web API constructors as
// "Illegal constructor" stubs so getOwnPropertyNames(window).length is realistic.
(function() {
	function illegalCtor(name) {
		var f = function() { throw new TypeError("Illegal constructor"); };
		Object.defineProperty(f, 'name', { value: name, configurable: true });
		// HTML element constructors inherit from HTMLElement.prototype
		// so that instanceof checks work: div instanceof HTMLDivElement
		if (name.indexOf('HTML') === 0 && name !== 'HTMLElement') {
			f.prototype = Object.create(HTMLElement.prototype);
		} else {
			f.prototype = Object.create(null);
		}
		f.prototype.constructor = f;
		Object.defineProperty(f.prototype, Symbol.toStringTag, { value: name, configurable: true });
		return f;
	}
	// All Chrome 146 window globals that we don't already define
	var stubs = [
		// DOM
		'Attr','CDATASection','CharacterData','Comment','DocumentType','DOMImplementation',
		'DOMStringList','DOMStringMap','HTMLAllCollection',
		'MutationRecord','ProcessingInstruction','Range','StaticRange',
		'TreeWalker','NodeIterator','XPathExpression','XPathEvaluator','XPathResult',
		'XMLDocument','XMLSerializer','XSLTProcessor',
		// HTML elements
		'HTMLBRElement','HTMLBaseElement','HTMLDListElement','HTMLDataElement',
		'HTMLDataListElement','HTMLDetailsElement','HTMLDialogElement',
		'HTMLDirectoryElement','HTMLEmbedElement','HTMLFieldSetElement',
		'HTMLFontElement','HTMLFrameElement','HTMLFrameSetElement','HTMLHRElement',
		'HTMLLIElement','HTMLLegendElement','HTMLMapElement','HTMLMarqueeElement',
		'HTMLMediaElement','HTMLMenuElement','HTMLMeterElement',
		'HTMLModElement','HTMLOListElement','HTMLObjectElement','HTMLOptGroupElement',
		'HTMLOutputElement','HTMLPictureElement','HTMLProgressElement',
		'HTMLQuoteElement','HTMLSlotElement','HTMLTableCaptionElement',
		'HTMLTableColElement','HTMLTableSectionElement','HTMLTemplateElement',
		'HTMLTimeElement','HTMLTitleElement','HTMLTrackElement',
		'HTMLUListElement','HTMLUnknownElement',
		// SVG
		'SVGSVGElement','SVGCircleElement','SVGClipPathElement','SVGDefsElement',
		'SVGDescElement','SVGEllipseElement','SVGFEBlendElement','SVGFEColorMatrixElement',
		'SVGFEComponentTransferElement','SVGFECompositeElement','SVGFEConvolveMatrixElement',
		'SVGFEDiffuseLightingElement','SVGFEDisplacementMapElement','SVGFEDistantLightElement',
		'SVGFEDropShadowElement','SVGFEFloodElement','SVGFEFuncAElement','SVGFEFuncBElement',
		'SVGFEFuncGElement','SVGFEFuncRElement','SVGFEGaussianBlurElement','SVGFEImageElement',
		'SVGFEMergeElement','SVGFEMergeNodeElement','SVGFEMorphologyElement',
		'SVGFEOffsetElement','SVGFEPointLightElement','SVGFESpecularLightingElement',
		'SVGFESpotLightElement','SVGFETileElement','SVGFETurbulenceElement',
		'SVGFilterElement','SVGForeignObjectElement','SVGGElement','SVGGeometryElement',
		'SVGGradientElement','SVGGraphicsElement','SVGImageElement','SVGLineElement',
		'SVGLinearGradientElement','SVGMPathElement','SVGMarkerElement','SVGMaskElement',
		'SVGMetadataElement','SVGPathElement','SVGPatternElement','SVGPolygonElement',
		'SVGPolylineElement','SVGRadialGradientElement','SVGRectElement',
		'SVGSetElement','SVGStopElement','SVGStyleElement','SVGSwitchElement',
		'SVGSymbolElement','SVGTSpanElement','SVGTextContentElement','SVGTextElement',
		'SVGTextPathElement','SVGTextPositioningElement','SVGTitleElement','SVGUseElement',
		'SVGViewElement','SVGAnimatedAngle','SVGAnimatedBoolean','SVGAnimatedEnumeration',
		'SVGAnimatedInteger','SVGAnimatedLength','SVGAnimatedLengthList','SVGAnimatedNumber',
		'SVGAnimatedNumberList','SVGAnimatedPreserveAspectRatio','SVGAnimatedRect',
		'SVGAnimatedString','SVGAnimatedTransformList','SVGAngle','SVGLength','SVGLengthList',
		'SVGMatrix','SVGNumber','SVGNumberList','SVGPoint','SVGPointList',
		'SVGPreserveAspectRatio','SVGRect','SVGStringList','SVGTransform','SVGTransformList',
		'SVGUnitTypes','SVGAnimateElement','SVGAnimateMotionElement','SVGAnimateTransformElement',
		'SVGAnimationElement','SVGComponentTransferFunctionElement','SVGAElement',
		// Events
		'AnimationPlaybackEvent','BeforeUnloadEvent','ClipboardEvent','CompositionEvent',
		'DragEvent','GamepadEvent','HashChangeEvent','MediaQueryListEvent',
		'PageTransitionEvent','PopStateEvent','ProgressEvent','SecurityPolicyViolationEvent',
		'SubmitEvent','UIEvent',
		// Canvas / WebGL
		'CanvasGradient','CanvasPattern','CanvasRenderingContext2D','ImageBitmap',
		'ImageBitmapRenderingContext','ImageData','OffscreenCanvas','OffscreenCanvasRenderingContext2D',
		'Path2D','TextMetrics','WebGLRenderingContext','WebGL2RenderingContext',
		'WebGLActiveInfo','WebGLBuffer','WebGLFramebuffer','WebGLProgram','WebGLQuery',
		'WebGLRenderbuffer','WebGLSampler','WebGLShader','WebGLShaderPrecisionFormat',
		'WebGLSync','WebGLTexture','WebGLTransformFeedback','WebGLUniformLocation',
		'WebGLVertexArrayObject',
		// Media
		'MediaStream','MediaStreamTrack','MediaStreamEvent','MediaRecorder',
		'MediaSource','SourceBuffer','SourceBufferList','MediaCapabilities',
		'MediaDeviceInfo','MediaDevices','MediaEncryptedEvent','MediaError',
		'MediaKeyMessageEvent','MediaKeySession','MediaKeyStatusMap','MediaKeySystemAccess',
		'MediaKeys','MediaList','MediaQueryList','MediaSession','MediaMetadata',
		// Audio
		'AudioBuffer','AudioBufferSourceNode','AudioDestinationNode','AudioListener',
		'AudioNode','AudioParam','AudioParamMap','AudioProcessingEvent',
		'AudioScheduledSourceNode','AudioWorklet','AudioWorkletNode',
		'BaseAudioContext','BiquadFilterNode','ChannelMergerNode','ChannelSplitterNode',
		'ConstantSourceNode','ConvolverNode','DelayNode','DynamicsCompressorNode',
		'GainNode','IIRFilterNode','MediaElementAudioSourceNode','MediaStreamAudioDestinationNode',
		'MediaStreamAudioSourceNode','OscillatorNode','PannerNode','PeriodicWave',
		'ScriptProcessorNode','StereoPannerNode','WaveShaperNode','AnalyserNode',
		// Fetch / Streams
		'ReadableStreamDefaultReader','ReadableStreamBYOBReader',
		'WritableStreamDefaultWriter','ByteLengthQueuingStrategy','CountQueuingStrategy',
		// Crypto
		'CryptoKey','SubtleCrypto',
		// Workers
		'SharedWorker','ServiceWorker','ServiceWorkerContainer','ServiceWorkerRegistration',
		// Storage
		'StorageManager','CacheStorage','Cache','IDBDatabase','IDBFactory','IDBIndex',
		'IDBKeyRange','IDBObjectStore','IDBOpenDBRequest','IDBRequest','IDBTransaction',
		'IDBVersionChangeEvent','IDBCursor','IDBCursorWithValue',
		// Network
		'WebSocket','EventSource','XMLHttpRequestEventTarget','XMLHttpRequestUpload',
		// CSS
		'CSSAnimation','CSSConditionRule','CSSContainerRule','CSSCounterStyleRule',
		'CSSFontFaceRule','CSSFontPaletteValuesRule','CSSGroupingRule',
		'CSSImageValue','CSSImportRule','CSSKeyframeRule','CSSKeyframesRule',
		'CSSLayerBlockRule','CSSLayerStatementRule','CSSMathClamp','CSSMathInvert',
		'CSSMathMax','CSSMathMin','CSSMathNegate','CSSMathProduct','CSSMathSum',
		'CSSMathValue','CSSMatrixComponent','CSSMediaRule','CSSNamespaceRule',
		'CSSNumericArray','CSSNumericValue','CSSPageRule','CSSPerspective',
		'CSSPositionValue','CSSPropertyRule','CSSRotate','CSSScale',
		'CSSSkew','CSSSkewX','CSSSkewY','CSSStartingStyleRule','CSSStyleValue',
		'CSSSupportsRule','CSSTransformComponent','CSSTransformValue','CSSTranslate',
		'CSSUnparsedValue','CSSUnitValue','CSSVariableReferenceValue',
		'StylePropertyMap','StylePropertyMapReadOnly','StyleSheetList',
		// Geometry
		'DOMMatrix','DOMMatrixReadOnly','DOMPoint','DOMPointReadOnly',
		'DOMQuad','DOMRectList',
		// Permissions / Notifications
		'Permissions','PermissionStatus','PushManager','PushSubscription',
		'PushSubscriptionOptions',
		// File
		'File','FileList','FileReader','FileSystemDirectoryHandle',
		'FileSystemFileHandle','FileSystemHandle','FileSystemWritableFileStream',
		// Misc Web APIs
		'AbortSignal','AbortController','CloseEvent','Credential','CredentialsContainer',
		'CustomElementRegistry','DataTransfer','DataTransferItem','DataTransferItemList',
		'DeviceMotionEvent','DeviceOrientationEvent','FontFace',
		'Gamepad','GamepadButton','GamepadHapticActuator',
		'Geolocation','GeolocationCoordinates','GeolocationPosition',
		'GeolocationPositionError','IdleDeadline','IntersectionObserverEntry',
		'NavigationPreloadManager','Navigator',
		'PerformanceEntry','PerformanceMark','PerformanceMeasure','PerformanceNavigation',
		'PerformanceNavigationTiming','PerformanceObserverEntryList',
		'PerformancePaintTiming','PerformanceResourceTiming','PerformanceServerTiming',
		'PerformanceTiming','PerformanceEventTiming','PerformanceLongTaskTiming',
		'Plugin','PluginArray','MimeType','MimeTypeArray',
		'RadioNodeList','ReportBody','ReportingObserver',
		'ResizeObserverEntry','ResizeObserverSize',
		'Screen','ScreenOrientation','ScrollTimeline',
		'Sanitizer','TaskController','TaskPriorityChangeEvent','TaskSignal',
		'TextTrack','TextTrackCue','TextTrackCueList','TextTrackList',
		'TimeRanges','Touch','TouchList',
		'ValidityState','VideoColorSpace','VideoFrame','VideoPlaybackQuality',
		'VisualViewport','WakeLock','WakeLockSentinel',
		'WebTransport','WebTransportBidirectionalStream','WebTransportDatagramDuplexStream',
		'Window',
		// Animation
		'Animation','AnimationEffect','AnimationTimeline','DocumentTimeline',
		'KeyframeEffect',
		// Encoding
		'TextDecoderStream','TextEncoderStream',
		// Clipboard
		'Clipboard','ClipboardItem',
		// Resize
		'ResizeObserver',
		// Broadcast / Message
		'BroadcastChannel','MessageChannel','MessagePort',
		// Observers
		'PerformanceObserver','MutationObserver','IntersectionObserver',
		// Other
		'BarProp','BeforeInstallPromptEvent','HTMLFormControlsCollection',
		'Location','History','External','Selection',
		'CSSStyleSheet','StyleSheet',
		// --- Chrome 146 additions (417 new stubs) ---
		// HTML Elements
		'HTMLAreaElement','HTMLAudioElement','HTMLBodyElement','HTMLButtonElement','HTMLCanvasElement',
		'HTMLDivElement','HTMLFencedFrameElement','HTMLFormElement','HTMLGeolocationElement','HTMLHeadElement',
		'HTMLHeadingElement','HTMLHtmlElement','HTMLIFrameElement','HTMLInputElement','HTMLLabelElement',
		'HTMLLinkElement','HTMLMetaElement','HTMLOptionElement','HTMLOptionsCollection','HTMLParagraphElement',
		'HTMLParamElement','HTMLPreElement','HTMLScriptElement','HTMLSelectElement','HTMLSelectedContentElement',
		'HTMLSourceElement','HTMLSpanElement','HTMLStyleElement','HTMLTableCellElement','HTMLTableElement',
		'HTMLTableRowElement','HTMLTextAreaElement','HTMLVideoElement',
		// SVG
		'SVGScriptElement',
		// CSS
		'CSSFontFeatureValuesRule','CSSFunctionDeclarations','CSSFunctionDescriptors','CSSFunctionRule','CSSKeywordValue',
		'CSSMarginRule','CSSNestedDeclarations','CSSPositionTryDescriptors','CSSPositionTryRule','CSSRule',
		'CSSRuleList','CSSScopeRule','CSSStyleRule','CSSTransition','CSSViewTransitionRule',
		// WebGL / GPU
		'GPU','GPUAdapter','GPUAdapterInfo','GPUBindGroup','GPUBindGroupLayout',
		'GPUBuffer','GPUBufferUsage','GPUCanvasContext','GPUColorWrite','GPUCommandBuffer',
		'GPUCommandEncoder','GPUCompilationInfo','GPUCompilationMessage','GPUComputePassEncoder','GPUComputePipeline',
		'GPUDevice','GPUDeviceLostInfo','GPUError','GPUExternalTexture','GPUInternalError',
		'GPUMapMode','GPUOutOfMemoryError','GPUPipelineError','GPUPipelineLayout','GPUQuerySet',
		'GPUQueue','GPURenderBundle','GPURenderBundleEncoder','GPURenderPassEncoder','GPURenderPipeline',
		'GPUSampler','GPUShaderModule','GPUShaderStage','GPUSupportedFeatures','GPUSupportedLimits',
		'GPUTexture','GPUTextureUsage','GPUTextureView','GPUUncapturedErrorEvent','GPUValidationError',
		'WGSLLanguageFeatures','WebGLContextEvent','WebGLObject',
		// Audio / Video
		'Audio','AudioData','AudioDecoder','AudioEncoder','AudioPlaybackStats',
		'AudioSinkInfo','VTTCue','VideoDecoder','VideoEncoder',
		// Media
		'MediaSourceHandle','MediaStreamTrackAudioStats','MediaStreamTrackEvent',
		'MediaStreamTrackGenerator','MediaStreamTrackProcessor','MediaStreamTrackVideoStats',
		// WebRTC
		'RTCCertificate','RTCDTMFSender','RTCDTMFToneChangeEvent','RTCDataChannel','RTCDataChannelEvent',
		'RTCDtlsTransport','RTCEncodedAudioFrame','RTCEncodedVideoFrame','RTCError','RTCErrorEvent',
		'RTCIceCandidate','RTCIceTransport','RTCPeerConnection','RTCPeerConnectionIceErrorEvent','RTCPeerConnectionIceEvent',
		'RTCRtpReceiver','RTCRtpScriptTransform','RTCRtpSender','RTCRtpTransceiver','RTCSctpTransport',
		'RTCSessionDescription','RTCStatsReport','RTCTrackEvent',
		// Streams
		'BrowserCaptureMediaStreamTrack','CanvasCaptureMediaStreamTrack','CompressionStream','DecompressionStream',
		'ReadableByteStreamController','ReadableStreamBYOBRequest','ReadableStreamDefaultController',
		'TransformStreamDefaultController','WebSocketStream','WritableStreamDefaultController',
		// XR (WebXR)
		'XRAnchor','XRAnchorSet','XRBoundedReferenceSpace','XRCPUDepthInformation','XRCamera',
		'XRDOMOverlayState','XRDepthInformation','XRFrame','XRHand','XRHitTestResult',
		'XRHitTestSource','XRInputSource','XRInputSourceArray','XRInputSourceEvent','XRInputSourcesChangeEvent',
		'XRJointPose','XRJointSpace','XRLayer','XRLightEstimate','XRLightProbe',
		'XRPose','XRRay','XRReferenceSpace','XRReferenceSpaceEvent','XRRenderState',
		'XRRigidTransform','XRSession','XRSessionEvent','XRSpace','XRSystem',
		'XRTransientInputHitTestResult','XRTransientInputHitTestSource','XRView','XRViewerPose','XRViewport',
		'XRVisibilityMaskChangeEvent','XRWebGLBinding','XRWebGLDepthInformation','XRWebGLLayer',
		// USB / HID / Serial / Bluetooth
		'Bluetooth','BluetoothCharacteristicProperties','BluetoothDevice','BluetoothRemoteGATTCharacteristic',
		'BluetoothRemoteGATTDescriptor','BluetoothRemoteGATTServer','BluetoothRemoteGATTService','BluetoothUUID',
		'HID','HIDConnectionEvent','HIDDevice','HIDInputReportEvent',
		'Serial','SerialPort',
		'USB','USBAlternateInterface','USBConfiguration','USBConnectionEvent','USBDevice',
		'USBEndpoint','USBInTransferResult','USBInterface','USBIsochronousInTransferPacket',
		'USBIsochronousInTransferResult','USBIsochronousOutTransferPacket','USBIsochronousOutTransferResult','USBOutTransferResult',
		// Crypto / Auth
		'AuthenticatorAssertionResponse','AuthenticatorAttestationResponse','AuthenticatorResponse',
		'Crypto','DigitalCredential','FederatedCredential',
		'IdentityCredential','IdentityCredentialError','IdentityProvider',
		'OTPCredential','PasswordCredential','PublicKeyCredential',
		// Storage / IDB
		'IDBRecord','Storage','StorageBucket','StorageBucketManager','StorageEvent',
		// Speech
		'SpeechGrammar','SpeechGrammarList','SpeechRecognition','SpeechRecognitionErrorEvent','SpeechRecognitionEvent',
		'SpeechRecognitionPhrase','SpeechSynthesis','SpeechSynthesisErrorEvent','SpeechSynthesisEvent',
		'SpeechSynthesisUtterance','SpeechSynthesisVoice',
		// Payments
		'PaymentAddress','PaymentManager','PaymentMethodChangeEvent','PaymentRequest',
		'PaymentRequestUpdateEvent','PaymentResponse',
		// Presentation
		'Presentation','PresentationAvailability','PresentationConnection',
		'PresentationConnectionAvailableEvent','PresentationConnectionCloseEvent',
		'PresentationConnectionList','PresentationReceiver','PresentationRequest',
		// Sensors
		'AbsoluteOrientationSensor','Accelerometer','DevicePosture','GravitySensor','Gyroscope',
		'LinearAccelerationSensor','OrientationSensor','PressureObserver','PressureRecord',
		'RelativeOrientationSensor','Sensor','SensorErrorEvent',
		// SharedStorage
		'SharedStorage','SharedStorageAppendMethod','SharedStorageClearMethod','SharedStorageDeleteMethod',
		'SharedStorageModifierMethod','SharedStorageSetMethod','SharedStorageWorklet',
		// Trusted Types
		'TrustedHTML','TrustedScript','TrustedScriptURL','TrustedTypePolicy','TrustedTypePolicyFactory',
		// Navigation
		'NavigateEvent','Navigation','NavigationActivation','NavigationCurrentEntryChangeEvent',
		'NavigationDestination','NavigationHistoryEntry','NavigationPrecommitController',
		'NavigationTransition','NavigatorLogin','NavigatorManagedData','NavigatorUAData',
		// Performance
		'Performance','PerformanceElementTiming','PerformanceLongAnimationFrameTiming',
		'PerformanceScriptTiming','PerformanceTimingConfidence',
		// Events
		'BlobEvent','CharacterBoundsUpdateEvent','ClipboardChangeEvent','CommandEvent',
		'ContentVisibilityAutoStateChangeEvent','CookieChangeEvent',
		'DeviceMotionEventAcceleration','DeviceMotionEventRotationRate',
		'DocumentPictureInPictureEvent','EventCounts','FontFaceSetLoadEvent','FormDataEvent',
		'InterestEvent','MIDIConnectionEvent','MIDIMessageEvent','OfflineAudioCompletionEvent',
		'PageRevealEvent','PageSwapEvent','PictureInPictureEvent','SnapEvent',
		'TextEvent','TextFormatUpdateEvent','TextUpdateEvent','ToggleEvent','TrackEvent',
		'VirtualKeyboardGeometryChangeEvent','WindowControlsOverlayGeometryChangeEvent',
		// Web Transport
		'WebTransportError',
		// Misc Web APIs
		'AbstractRange','AnimationTrigger','BackgroundFetchManager','BackgroundFetchRecord',
		'BackgroundFetchRegistration','BarcodeDetector','BatteryManager','CSPViolationReportBody',
		'CaptureController','CaretPosition','ChapterInformation','CloseWatcher',
		'CookieStore','CookieStoreManager','CrashReportContext','CreateMonitor',
		'CropTarget','CustomStateSet','DOMError','DelegatedInkTrailPresenter',
		'DocumentPictureInPicture','EditContext','ElementInternals','EncodedAudioChunk','EncodedVideoChunk',
		'EyeDropper','FeaturePolicy','Fence','FencedFrameConfig','FetchLaterResult',
		'FileSystemObserver','FontData','FragmentDirective','Highlight','HighlightRegistry',
		'IdleDetector','ImageCapture','ImageDecoder','ImageTrack','ImageTrackList',
		'Ink','InputDeviceCapabilities','InputDeviceInfo','IntegrityViolationReportBody',
		'Keyboard','KeyboardLayoutMap','LanguageDetector','LargestContentfulPaint',
		'LaunchParams','LaunchQueue','LayoutShift','LayoutShiftAttribution',
		'Lock','LockManager','MIDIAccess','MIDIInput','MIDIInputMap',
		'MIDIOutput','MIDIOutputMap','MIDIPort','MathMLElement','NetworkInformation',
		'NotRestoredReasonDetails','NotRestoredReasons','Observable','Option','Origin',
		'OverconstrainedError','PeriodicSyncManager','PictureInPictureWindow',
		'Profiler','ProtectedAudience','QuotaExceededError','RemotePlayback','RestrictionTarget',
		'Scheduler','Scheduling','ScreenDetailed','ScreenDetails','Subscriber',
		'Summarizer','SyncManager','TaskAttributionTiming','TextFormat','TimelineTrigger',
		'TimelineTriggerRange','TimelineTriggerRangeList','Translator','URLPattern','UserActivation',
		'ViewTimeline','ViewTransition','ViewTransitionTypeSet','Viewport','VirtualKeyboard',
		'VisibilityStateEntry','WebKitCSSMatrix','WebKitMutationObserver','WebSocketError','WindowControlsOverlay',
		'Worklet'
	];
	for (var i = 0; i < stubs.length; i++) {
		if (typeof window[stubs[i]] === 'undefined') {
			Object.defineProperty(window, stubs[i], {
				value: illegalCtor(stubs[i]),
				writable: true,
				enumerable: false,
				configurable: true
			});
		}
	}
})();

// === PROTOTYPE METHODS FOR STUB CONSTRUCTORS ===
// BM scripts access window.Constructor.prototype.method for feature detection.
// Stubs above only have constructor + Symbol.toStringTag on prototype.
// Add essential prototype methods so feature checks pass.
(function() {
	var _noop = function() {};
	var _noopStr = function() { return ''; };
	var _noopArr = function() { return []; };
	var _noopNull = function() { return null; };
	var _noopFalse = function() { return false; };
	var _noopTrue = function() { return true; };
	var _noopZero = function() { return 0; };
	var _noopObj = function() { return {}; };
	var _noopPromise = function() { return Promise.resolve(); };

	// HTMLCanvasElement.prototype
	if (window.HTMLCanvasElement && window.HTMLCanvasElement.prototype) {
		var hcp = window.HTMLCanvasElement.prototype;
		if (!hcp.getContext) {
			hcp.getContext = function() { return null; };
			hcp.toDataURL = function() { return 'data:image/png;base64,'; };
			hcp.toBlob = function(cb) { if (cb) cb(new Blob([], {type:'image/png'})); };
			hcp.transferControlToOffscreen = function() { return {}; };
			hcp.captureStream = function() { return {}; };
			Object.defineProperty(hcp, 'width', { get: _noopZero, set: _noop, configurable: true });
			Object.defineProperty(hcp, 'height', { get: _noopZero, set: _noop, configurable: true });
		}
	}

	// CanvasRenderingContext2D.prototype, Chrome has 74 proto keys
	if (window.CanvasRenderingContext2D && window.CanvasRenderingContext2D.prototype) {
		var crp = window.CanvasRenderingContext2D.prototype;
		if (!crp.fillRect) {
			var ctx2dMethods = ['fillRect','strokeRect','clearRect','fillText','strokeText','beginPath',
				'closePath','moveTo','lineTo','arc','arcTo','quadraticCurveTo','bezierCurveTo',
				'rect','ellipse','fill','stroke','clip','save','restore','scale','rotate','translate',
				'transform','setTransform','resetTransform','drawImage','putImageData',
				'createLinearGradient','createRadialGradient','createConicGradient','setLineDash',
				'drawFocusIfNeeded','scrollPathIntoView','roundRect','reset'];
			for (var i = 0; i < ctx2dMethods.length; i++) crp[ctx2dMethods[i]] = _noop;
			crp.getImageData = function(x,y,w,h) { return {data:new Uint8ClampedArray(w*h*4),width:w,height:h}; };
			crp.createImageData = function(w,h) { return {data:new Uint8ClampedArray(w*h*4),width:w,height:h}; };
			crp.measureText = function() { return {width:0,actualBoundingBoxAscent:0,actualBoundingBoxDescent:0,fontBoundingBoxAscent:0,fontBoundingBoxDescent:0,actualBoundingBoxLeft:0,actualBoundingBoxRight:0}; };
			crp.getTransform = function() { return {a:1,b:0,c:0,d:1,e:0,f:0}; };
			crp.getLineDash = _noopArr;
			crp.getContextAttributes = _noopObj;
			crp.isPointInPath = _noopFalse;
			crp.isPointInStroke = _noopFalse;
			crp.isContextLost = _noopFalse;
			crp.createPattern = _noopNull;
			Object.defineProperty(crp, 'canvas', { get: _noopNull, configurable: true });
		}
	}

	// WebGLRenderingContext.prototype, Chrome has 443 proto keys (mostly GL constants)
	if (window.WebGLRenderingContext && window.WebGLRenderingContext.prototype) {
		var wglp = window.WebGLRenderingContext.prototype;
		if (!wglp.getParameter) {
			wglp.getParameter = function() { return null; };
			wglp.getExtension = _noopNull;
			wglp.getSupportedExtensions = _noopArr;
			wglp.getShaderPrecisionFormat = function() { return {rangeMin:127,rangeMax:127,precision:23}; };
			wglp.getContextAttributes = _noopObj;
			wglp.isContextLost = _noopFalse;
			// ALL WebGL methods from Chrome's WebGLRenderingContext.prototype
			var wglNoops = ['createBuffer','bindBuffer','bufferData','bufferSubData',
				'createShader','shaderSource','compileShader','getShaderParameter','getShaderInfoLog',
				'createProgram','attachShader','linkProgram','getProgramParameter','getProgramInfoLog',
				'useProgram','deleteBuffer','deleteProgram','deleteShader','deleteTexture',
				'enableVertexAttribArray','disableVertexAttribArray','vertexAttribPointer',
				'vertexAttrib1f','vertexAttrib2f','vertexAttrib3f','vertexAttrib4f',
				'vertexAttrib1fv','vertexAttrib2fv','vertexAttrib3fv','vertexAttrib4fv',
				'drawArrays','drawElements','viewport','enable','disable',
				'blendFunc','blendFuncSeparate','blendEquation','blendEquationSeparate','blendColor',
				'depthFunc','depthMask','depthRange','clearColor','clearDepth','clearStencil','clear',
				'colorMask','stencilFunc','stencilFuncSeparate','stencilMask','stencilMaskSeparate',
				'stencilOp','stencilOpSeparate','cullFace','frontFace','lineWidth','polygonOffset',
				'scissor','sampleCoverage','pixelStorei','hint',
				'bindTexture','activeTexture','texParameteri','texParameterf',
				'texImage2D','texSubImage2D','generateMipmap','compressedTexImage2D','compressedTexSubImage2D',
				'copyTexImage2D','copyTexSubImage2D',
				'bindFramebuffer','framebufferTexture2D','framebufferRenderbuffer',
				'checkFramebufferStatus','deleteFramebuffer',
				'bindRenderbuffer','renderbufferStorage','deleteRenderbuffer',
				'uniform1i','uniform2i','uniform3i','uniform4i',
				'uniform1f','uniform2f','uniform3f','uniform4f',
				'uniform1iv','uniform2iv','uniform3iv','uniform4iv',
				'uniform1fv','uniform2fv','uniform3fv','uniform4fv',
				'uniformMatrix2fv','uniformMatrix3fv','uniformMatrix4fv',
				'readPixels','finish','flush','validateProgram',
				'getActiveAttrib','getActiveUniform','getAttachedShaders',
				'getBufferParameter','getError','getFramebufferAttachmentParameter',
				'getRenderbufferParameter','getTexParameter','getVertexAttrib','getVertexAttribOffset',
				'isBuffer','isEnabled','isFramebuffer','isProgram','isRenderbuffer','isShader','isTexture'];
			for (var j = 0; j < wglNoops.length; j++) { if (!wglp[wglNoops[j]]) wglp[wglNoops[j]] = _noop; }
			wglp.getShaderSource = _noopStr;
			wglp.getUniform = _noopNull;
			wglp.getAttribLocation = _noopZero;
			wglp.getUniformLocation = _noopNull;
			wglp.createTexture = _noopObj;
			wglp.createFramebuffer = _noopObj;
			wglp.createRenderbuffer = _noopObj;
			Object.defineProperty(wglp, 'canvas', { get: _noopNull, configurable: true });
			Object.defineProperty(wglp, 'drawingBufferWidth', { get: function(){return 300;}, configurable: true });
			Object.defineProperty(wglp, 'drawingBufferHeight', { get: function(){return 150;}, configurable: true });
		}
	}

	// AudioContext.prototype
	if (window.AudioContext && window.AudioContext.prototype) {
		var acp = window.AudioContext.prototype;
		if (!acp.createOscillator) {
			var audioNode = function() { return {connect:_noop,disconnect:_noop,start:_noop,stop:_noop,frequency:{value:440},gain:{value:1},type:'sine',channelCount:2}; };
			acp.createOscillator = audioNode;
			acp.createGain = audioNode;
			acp.createDynamicsCompressor = function() { return {connect:_noop,disconnect:_noop,threshold:{value:-24},knee:{value:30},ratio:{value:12},attack:{value:0.003},release:{value:0.25},reduction:0,channelCount:2}; };
			acp.createAnalyser = function() { return {connect:_noop,disconnect:_noop,fftSize:2048,frequencyBinCount:1024,getFloatFrequencyData:_noop,getByteFrequencyData:_noop,getFloatTimeDomainData:_noop,getByteTimeDomainData:_noop,channelCount:2}; };
			acp.createBiquadFilter = audioNode;
			acp.createBuffer = function(ch,len,rate) { return {getChannelData:function(){return new Float32Array(len||1);},numberOfChannels:ch||1,length:len||1,sampleRate:rate||44100,duration:(len||1)/(rate||44100)}; };
			acp.createBufferSource = function() { return {connect:_noop,disconnect:_noop,start:_noop,stop:_noop,buffer:null,loop:false,playbackRate:{value:1},channelCount:2}; };
			acp.createScriptProcessor = function() { return {connect:_noop,disconnect:_noop,onaudioprocess:null,channelCount:2}; };
			acp.createMediaElementSource = audioNode;
			acp.close = _noopPromise;
			acp.resume = _noopPromise;
			acp.suspend = _noopPromise;
			acp.decodeAudioData = function(buf,ok,err) { if(ok)ok(acp.createBuffer(1,1,44100)); return Promise.resolve(acp.createBuffer(1,1,44100)); };
		}
	}

	// RTCPeerConnection.prototype
	if (window.RTCPeerConnection && window.RTCPeerConnection.prototype) {
		var rtcp = window.RTCPeerConnection.prototype;
		if (!rtcp.createDataChannel) {
			rtcp.createDataChannel = function(label) { return {label:label,readyState:'connecting',send:_noop,close:_noop,onopen:null,onclose:null,onmessage:null,onerror:null}; };
			rtcp.createOffer = function() { return Promise.resolve({type:'offer',sdp:'v=0\r\n'}); };
			rtcp.createAnswer = function() { return Promise.resolve({type:'answer',sdp:'v=0\r\n'}); };
			rtcp.setLocalDescription = _noopPromise;
			rtcp.setRemoteDescription = _noopPromise;
			rtcp.addIceCandidate = _noopPromise;
			rtcp.getStats = function() { return Promise.resolve(new Map()); };
			rtcp.close = _noop;
			Object.defineProperty(rtcp, 'localDescription', { get: _noopNull, configurable: true });
			Object.defineProperty(rtcp, 'remoteDescription', { get: _noopNull, configurable: true });
			Object.defineProperty(rtcp, 'signalingState', { get: function(){return 'stable';}, configurable: true });
			Object.defineProperty(rtcp, 'iceGatheringState', { get: function(){return 'new';}, configurable: true });
			Object.defineProperty(rtcp, 'iceConnectionState', { get: function(){return 'new';}, configurable: true });
			Object.defineProperty(rtcp, 'connectionState', { get: function(){return 'new';}, configurable: true });
		}
	}

	// SpeechSynthesis.prototype
	if (window.SpeechSynthesis && window.SpeechSynthesis.prototype) {
		var ssp = window.SpeechSynthesis.prototype;
		if (!ssp.getVoices) {
			ssp.getVoices = _noopArr;
			ssp.speak = _noop;
			ssp.cancel = _noop;
			ssp.pause = _noop;
			ssp.resume = _noop;
			Object.defineProperty(ssp, 'pending', { get: _noopFalse, configurable: true });
			Object.defineProperty(ssp, 'speaking', { get: _noopFalse, configurable: true });
			Object.defineProperty(ssp, 'paused', { get: _noopFalse, configurable: true });
		}
	}
	// XMLHttpRequest.prototype, Chrome has 27 proto keys
	if (window.XMLHttpRequest && window.XMLHttpRequest.prototype) {
		var xhrp = window.XMLHttpRequest.prototype;
		if (!xhrp.open || Object.getOwnPropertyNames(xhrp).length < 10) {
			xhrp.open = _noop; xhrp.send = _noop; xhrp.abort = _noop;
			xhrp.setRequestHeader = _noop; xhrp.getResponseHeader = _noopNull;
			xhrp.getAllResponseHeaders = _noopStr; xhrp.overrideMimeType = _noop;
			Object.defineProperty(xhrp, 'readyState', { get: function(){return 0;}, configurable: true });
			Object.defineProperty(xhrp, 'status', { get: _noopZero, configurable: true });
			Object.defineProperty(xhrp, 'statusText', { get: _noopStr, configurable: true });
			Object.defineProperty(xhrp, 'response', { get: _noopStr, configurable: true });
			Object.defineProperty(xhrp, 'responseText', { get: _noopStr, configurable: true });
			Object.defineProperty(xhrp, 'responseURL', { get: _noopStr, configurable: true });
			Object.defineProperty(xhrp, 'responseType', { get: _noopStr, set: _noop, configurable: true });
			Object.defineProperty(xhrp, 'timeout', { get: _noopZero, set: _noop, configurable: true });
			Object.defineProperty(xhrp, 'withCredentials', { get: _noopFalse, set: _noop, configurable: true });
			Object.defineProperty(xhrp, 'upload', { get: function(){return {};}, configurable: true });
			xhrp.UNSENT = 0; xhrp.OPENED = 1; xhrp.HEADERS_RECEIVED = 2;
			xhrp.LOADING = 3; xhrp.DONE = 4;
		}
	}

	// WebSocket.prototype, Chrome has 17 proto keys
	if (window.WebSocket && window.WebSocket.prototype) {
		var wsp = window.WebSocket.prototype;
		if (Object.getOwnPropertyNames(wsp).length < 5) {
			wsp.send = _noop; wsp.close = _noop;
			Object.defineProperty(wsp, 'readyState', { get: function(){return 3;}, configurable: true });
			Object.defineProperty(wsp, 'bufferedAmount', { get: _noopZero, configurable: true });
			Object.defineProperty(wsp, 'url', { get: _noopStr, configurable: true });
			Object.defineProperty(wsp, 'protocol', { get: _noopStr, configurable: true });
			Object.defineProperty(wsp, 'extensions', { get: _noopStr, configurable: true });
			Object.defineProperty(wsp, 'binaryType', { get: function(){return 'blob';}, set: _noop, configurable: true });
			wsp.CONNECTING = 0; wsp.OPEN = 1; wsp.CLOSING = 2; wsp.CLOSED = 3;
		}
	}

	// DocumentFragment.prototype, Chrome has 12 proto keys
	if (window.DocumentFragment && window.DocumentFragment.prototype) {
		var dfp = window.DocumentFragment.prototype;
		if (Object.getOwnPropertyNames(dfp).length < 5) {
			dfp.getElementById = _noopNull;
			dfp.querySelector = _noopNull;
			dfp.querySelectorAll = function() { return []; };
			dfp.append = _noop; dfp.prepend = _noop;
			dfp.replaceChildren = _noop;
			Object.defineProperty(dfp, 'childElementCount', { get: _noopZero, configurable: true });
			Object.defineProperty(dfp, 'children', { get: _noopArr, configurable: true });
			Object.defineProperty(dfp, 'firstElementChild', { get: _noopNull, configurable: true });
			Object.defineProperty(dfp, 'lastElementChild', { get: _noopNull, configurable: true });
		}
	}

	// ShadowRoot.prototype, Chrome has 23 proto keys
	if (window.ShadowRoot && window.ShadowRoot.prototype) {
		var srp = window.ShadowRoot.prototype;
		if (Object.getOwnPropertyNames(srp).length < 10) {
			srp.getElementById = _noopNull;
			srp.querySelector = _noopNull;
			srp.querySelectorAll = function() { return []; };
			srp.getAnimations = _noopArr;
			srp.elementFromPoint = _noopNull;
			srp.elementsFromPoint = _noopArr;
			srp.getSelection = _noopNull;
			Object.defineProperty(srp, 'mode', { get: function(){return 'open';}, configurable: true });
			Object.defineProperty(srp, 'host', { get: _noopNull, configurable: true });
			Object.defineProperty(srp, 'innerHTML', { get: _noopStr, set: _noop, configurable: true });
			Object.defineProperty(srp, 'delegatesFocus', { get: _noopFalse, configurable: true });
			Object.defineProperty(srp, 'adoptedStyleSheets', { get: _noopArr, set: _noop, configurable: true });
			Object.defineProperty(srp, 'fullscreenElement', { get: _noopNull, configurable: true });
			Object.defineProperty(srp, 'pictureInPictureElement', { get: _noopNull, configurable: true });
			Object.defineProperty(srp, 'activeElement', { get: _noopNull, configurable: true });
			Object.defineProperty(srp, 'pointerLockElement', { get: _noopNull, configurable: true });
			Object.defineProperty(srp, 'styleSheets', { get: _noopArr, configurable: true });
		}
	}

	// MutationObserver.prototype, Chrome has 4 proto keys
	if (window.MutationObserver && window.MutationObserver.prototype) {
		var mop = window.MutationObserver.prototype;
		if (Object.getOwnPropertyNames(mop).length < 3) {
			mop.observe = _noop; mop.disconnect = _noop;
			mop.takeRecords = _noopArr;
		}
	}

	// IntersectionObserver.prototype, Chrome has 11 proto keys
	if (window.IntersectionObserver && window.IntersectionObserver.prototype) {
		var iop = window.IntersectionObserver.prototype;
		if (Object.getOwnPropertyNames(iop).length < 5) {
			iop.observe = _noop; iop.unobserve = _noop; iop.disconnect = _noop;
			iop.takeRecords = _noopArr;
			Object.defineProperty(iop, 'root', { get: _noopNull, configurable: true });
			Object.defineProperty(iop, 'rootMargin', { get: function(){return '0px 0px 0px 0px';}, configurable: true });
			Object.defineProperty(iop, 'thresholds', { get: function(){return [0];}, configurable: true });
		}
	}

	// ResizeObserver.prototype, Chrome has 4 proto keys
	if (window.ResizeObserver && window.ResizeObserver.prototype) {
		var rop = window.ResizeObserver.prototype;
		if (Object.getOwnPropertyNames(rop).length < 3) {
			rop.observe = _noop; rop.unobserve = _noop; rop.disconnect = _noop;
		}
	}

	// PerformanceObserver.prototype, Chrome has 4 proto keys
	if (window.PerformanceObserver && window.PerformanceObserver.prototype) {
		var pop = window.PerformanceObserver.prototype;
		if (Object.getOwnPropertyNames(pop).length < 3) {
			pop.observe = _noop; pop.disconnect = _noop;
			pop.takeRecords = _noopArr;
		}
	}

	// StyleSheet.prototype, Chrome has 8 proto keys
	if (window.StyleSheet && window.StyleSheet.prototype) {
		var sshp = window.StyleSheet.prototype;
		if (Object.getOwnPropertyNames(sshp).length < 4) {
			Object.defineProperty(sshp, 'type', { get: function(){return 'text/css';}, configurable: true });
			Object.defineProperty(sshp, 'href', { get: _noopNull, configurable: true });
			Object.defineProperty(sshp, 'ownerNode', { get: _noopNull, configurable: true });
			Object.defineProperty(sshp, 'parentStyleSheet', { get: _noopNull, configurable: true });
			Object.defineProperty(sshp, 'title', { get: _noopNull, configurable: true });
			Object.defineProperty(sshp, 'media', { get: function(){return {length:0};}, configurable: true });
			Object.defineProperty(sshp, 'disabled', { get: _noopFalse, set: _noop, configurable: true });
		}
	}

	// CSSStyleSheet.prototype, Chrome has 10 proto keys
	if (window.CSSStyleSheet && window.CSSStyleSheet.prototype) {
		var csshp = window.CSSStyleSheet.prototype;
		if (Object.getOwnPropertyNames(csshp).length < 5) {
			csshp.insertRule = _noopZero; csshp.deleteRule = _noop;
			csshp.addRule = _noopZero; csshp.removeRule = _noop;
			csshp.replace = function() { return Promise.resolve(this); };
			csshp.replaceSync = _noop;
			Object.defineProperty(csshp, 'cssRules', { get: function(){return [];}, configurable: true });
			Object.defineProperty(csshp, 'rules', { get: function(){return [];}, configurable: true });
			Object.defineProperty(csshp, 'ownerRule', { get: _noopNull, configurable: true });
		}
	}

	// DOMRect.prototype, Chrome has 5 proto keys
	if (window.DOMRect && window.DOMRect.prototype) {
		var drp = window.DOMRect.prototype;
		if (Object.getOwnPropertyNames(drp).length < 3) {
			Object.defineProperty(drp, 'x', { get: _noopZero, set: _noop, configurable: true });
			Object.defineProperty(drp, 'y', { get: _noopZero, set: _noop, configurable: true });
			Object.defineProperty(drp, 'width', { get: _noopZero, set: _noop, configurable: true });
			Object.defineProperty(drp, 'height', { get: _noopZero, set: _noop, configurable: true });
		}
	}

	// DOMRectReadOnly.prototype, Chrome has 10 proto keys
	if (window.DOMRectReadOnly && window.DOMRectReadOnly.prototype) {
		var drrp = window.DOMRectReadOnly.prototype;
		if (Object.getOwnPropertyNames(drrp).length < 5) {
			drrp.toJSON = function(){return{x:0,y:0,width:0,height:0,top:0,right:0,bottom:0,left:0};};
			Object.defineProperty(drrp, 'x', { get: _noopZero, configurable: true });
			Object.defineProperty(drrp, 'y', { get: _noopZero, configurable: true });
			Object.defineProperty(drrp, 'width', { get: _noopZero, configurable: true });
			Object.defineProperty(drrp, 'height', { get: _noopZero, configurable: true });
			Object.defineProperty(drrp, 'top', { get: _noopZero, configurable: true });
			Object.defineProperty(drrp, 'right', { get: _noopZero, configurable: true });
			Object.defineProperty(drrp, 'bottom', { get: _noopZero, configurable: true });
			Object.defineProperty(drrp, 'left', { get: _noopZero, configurable: true });
		}
	}

	// DOMMatrix.prototype, Chrome has 35 proto keys
	if (window.DOMMatrix && window.DOMMatrix.prototype) {
		var dmp = window.DOMMatrix.prototype;
		if (Object.getOwnPropertyNames(dmp).length < 10) {
			var matNoops = ['multiplySelf','preMultiplySelf','translateSelf','scaleSelf','scale3dSelf',
				'rotateSelf','rotateFromVectorSelf','rotateAxisAngleSelf','skewXSelf','skewYSelf',
				'invertSelf','setMatrixValue'];
			for (var mi = 0; mi < matNoops.length; mi++) dmp[matNoops[mi]] = function(){return this;};
		}
	}

	// DOMMatrixReadOnly.prototype, Chrome has 43 proto keys
	if (window.DOMMatrixReadOnly && window.DOMMatrixReadOnly.prototype) {
		var dmrp = window.DOMMatrixReadOnly.prototype;
		if (Object.getOwnPropertyNames(dmrp).length < 10) {
			var matRONoops = ['translate','scale','scale3d','scaleNonUniform','rotate',
				'rotateFromVector','rotateAxisAngle','skewX','skewY','multiply',
				'flipX','flipY','inverse','transformPoint','toFloat32Array','toFloat64Array','toJSON'];
			for (var mri = 0; mri < matRONoops.length; mri++) dmrp[matRONoops[mri]] = function(){return {};};
			dmrp.toString = function(){return 'matrix(1, 0, 0, 1, 0, 0)';};
			var matProps = ['a','b','c','d','e','f','m11','m12','m13','m14','m21','m22','m23','m24',
				'm31','m32','m33','m34','m41','m42','m43','m44','is2D','isIdentity'];
			for (var mpi = 0; mpi < matProps.length; mpi++) {
				if (!(matProps[mpi] in dmrp)) {
					Object.defineProperty(dmrp, matProps[mpi], {
						get: matProps[mpi] === 'is2D' ? _noopTrue : matProps[mpi] === 'isIdentity' ? _noopTrue :
							(matProps[mpi]==='a'||matProps[mpi]==='d'||matProps[mpi]==='m11'||matProps[mpi]==='m22'||matProps[mpi]==='m33'||matProps[mpi]==='m44') ? function(){return 1;} : _noopZero,
						configurable: true
					});
				}
			}
		}
	}

	// DOMPoint.prototype, Chrome has 5 proto keys
	if (window.DOMPoint && window.DOMPoint.prototype) {
		var dpp = window.DOMPoint.prototype;
		if (Object.getOwnPropertyNames(dpp).length < 3) {
			Object.defineProperty(dpp, 'x', { get: _noopZero, set: _noop, configurable: true });
			Object.defineProperty(dpp, 'y', { get: _noopZero, set: _noop, configurable: true });
			Object.defineProperty(dpp, 'z', { get: _noopZero, set: _noop, configurable: true });
			Object.defineProperty(dpp, 'w', { get: function(){return 1;}, set: _noop, configurable: true });
		}
	}

	// MediaStream.prototype, Chrome has 14 proto keys
	if (window.MediaStream && window.MediaStream.prototype) {
		var msp = window.MediaStream.prototype;
		if (Object.getOwnPropertyNames(msp).length < 5) {
			msp.getAudioTracks = _noopArr; msp.getVideoTracks = _noopArr;
			msp.getTracks = _noopArr; msp.getTrackById = _noopNull;
			msp.addTrack = _noop; msp.removeTrack = _noop; msp.clone = function(){return {};};
			Object.defineProperty(msp, 'id', { get: _noopStr, configurable: true });
			Object.defineProperty(msp, 'active', { get: _noopFalse, configurable: true });
		}
	}

	// Notification.prototype, Chrome has 20 proto keys
	if (window.Notification && window.Notification.prototype) {
		var notp = window.Notification.prototype;
		if (Object.getOwnPropertyNames(notp).length < 5) {
			notp.close = _noop;
			var notProps = ['title','dir','lang','body','tag','icon','badge','image',
				'data','vibrate','renotify','requireInteraction','silent','timestamp','actions'];
			for (var npi = 0; npi < notProps.length; npi++) {
				Object.defineProperty(notp, notProps[npi], { get: _noopNull, configurable: true });
			}
		}
	}

	// BroadcastChannel.prototype, Chrome has 6 proto keys
	if (window.BroadcastChannel && window.BroadcastChannel.prototype) {
		var bcp = window.BroadcastChannel.prototype;
		if (Object.getOwnPropertyNames(bcp).length < 3) {
			bcp.postMessage = _noop; bcp.close = _noop;
			Object.defineProperty(bcp, 'name', { get: _noopStr, configurable: true });
		}
	}

	// MessageChannel.prototype, Chrome has 3 proto keys
	if (window.MessageChannel && window.MessageChannel.prototype) {
		var mcp2 = window.MessageChannel.prototype;
		if (Object.getOwnPropertyNames(mcp2).length < 2) {
			Object.defineProperty(mcp2, 'port1', { get: function(){return {};}, configurable: true });
			Object.defineProperty(mcp2, 'port2', { get: function(){return {};}, configurable: true });
		}
	}

	// TreeWalker.prototype, Chrome has 12 proto keys
	if (window.TreeWalker && window.TreeWalker.prototype) {
		var twp = window.TreeWalker.prototype;
		if (Object.getOwnPropertyNames(twp).length < 5) {
			twp.parentNode = _noopNull; twp.firstChild = _noopNull;
			twp.lastChild = _noopNull; twp.previousSibling = _noopNull;
			twp.nextSibling = _noopNull; twp.previousNode = _noopNull;
			twp.nextNode = _noopNull;
			Object.defineProperty(twp, 'root', { get: _noopNull, configurable: true });
			Object.defineProperty(twp, 'whatToShow', { get: function(){return 0xFFFFFFFF;}, configurable: true });
			Object.defineProperty(twp, 'filter', { get: _noopNull, configurable: true });
			Object.defineProperty(twp, 'currentNode', { get: _noopNull, set: _noop, configurable: true });
		}
	}

	// NodeIterator.prototype, Chrome has 9 proto keys
	if (window.NodeIterator && window.NodeIterator.prototype) {
		var nip = window.NodeIterator.prototype;
		if (Object.getOwnPropertyNames(nip).length < 5) {
			nip.nextNode = _noopNull; nip.previousNode = _noopNull;
			nip.detach = _noop;
			Object.defineProperty(nip, 'root', { get: _noopNull, configurable: true });
			Object.defineProperty(nip, 'whatToShow', { get: function(){return 0xFFFFFFFF;}, configurable: true });
			Object.defineProperty(nip, 'filter', { get: _noopNull, configurable: true });
			Object.defineProperty(nip, 'referenceNode', { get: _noopNull, configurable: true });
			Object.defineProperty(nip, 'pointerBeforeReferenceNode', { get: _noopTrue, configurable: true });
		}
	}

	// FontFace.prototype, Chrome has 17 proto keys
	if (window.FontFace && window.FontFace.prototype) {
		var ffp = window.FontFace.prototype;
		if (Object.getOwnPropertyNames(ffp).length < 5) {
			ffp.load = _noopPromise;
			var ffProps = ['family','style','weight','stretch','unicodeRange','variant',
				'featureSettings','variationSettings','display','ascentOverride','descentOverride',
				'lineGapOverride','sizeAdjust','status','loaded'];
			for (var ffi = 0; ffi < ffProps.length; ffi++) {
				Object.defineProperty(ffp, ffProps[ffi], {
					get: ffProps[ffi] === 'status' ? function(){return 'unloaded';} :
						ffProps[ffi] === 'loaded' ? function(){return Promise.resolve(this);} : _noopStr,
					set: _noop, configurable: true
				});
			}
		}
	}

	// Range.prototype, Chrome has 31 proto keys
	if (window.Range && window.Range.prototype) {
		var rgp = window.Range.prototype;
		if (Object.getOwnPropertyNames(rgp).length < 10) {
			var rgNoops = ['setStart','setEnd','setStartBefore','setStartAfter',
				'setEndBefore','setEndAfter','collapse','selectNode','selectNodeContents',
				'deleteContents','insertNode','surroundContents','detach'];
			for (var rgi = 0; rgi < rgNoops.length; rgi++) rgp[rgNoops[rgi]] = _noop;
			rgp.compareBoundaryPoints = _noopZero;
			rgp.comparePoint = _noopZero;
			rgp.cloneRange = function(){return {};};
			rgp.cloneContents = function(){return {};};
			rgp.extractContents = function(){return {};};
			rgp.createContextualFragment = function(){return {};};
			rgp.getBoundingClientRect = function(){return{x:0,y:0,width:0,height:0,top:0,right:0,bottom:0,left:0};};
			rgp.getClientRects = _noopArr;
			rgp.intersectsNode = _noopFalse;
			rgp.isPointInRange = _noopFalse;
			rgp.toString = _noopStr;
		}
	}

	// Selection.prototype, Chrome has 30 proto keys
	if (window.Selection && window.Selection.prototype) {
		var slp = window.Selection.prototype;
		if (Object.getOwnPropertyNames(slp).length < 10) {
			var slNoops = ['addRange','removeRange','removeAllRanges','collapse','collapseToStart',
				'collapseToEnd','extend','setBaseAndExtent','selectAllChildren',
				'deleteFromDocument','empty','modify','setPosition'];
			for (var sli = 0; sli < slNoops.length; sli++) slp[slNoops[sli]] = _noop;
			slp.getRangeAt = function(){return {};};
			slp.containsNode = _noopFalse;
			slp.toString = _noopStr;
			Object.defineProperty(slp, 'anchorNode', { get: _noopNull, configurable: true });
			Object.defineProperty(slp, 'anchorOffset', { get: _noopZero, configurable: true });
			Object.defineProperty(slp, 'focusNode', { get: _noopNull, configurable: true });
			Object.defineProperty(slp, 'focusOffset', { get: _noopZero, configurable: true });
			Object.defineProperty(slp, 'isCollapsed', { get: _noopTrue, configurable: true });
			Object.defineProperty(slp, 'rangeCount', { get: _noopZero, configurable: true });
			Object.defineProperty(slp, 'type', { get: function(){return 'None';}, configurable: true });
			Object.defineProperty(slp, 'direction', { get: function(){return 'none';}, configurable: true });
		}
	}
})();

// Window event handler properties (null by default in real Chrome)
(function() {
	var _evtHandlers = [
		'onabort','onafterprint','onanimationend','onanimationiteration','onanimationstart',
		'onauxclick','onbeforeinput','onbeforeprint','onbeforetoggle','onbeforeunload',
		'onblur','oncancel','oncanplay','oncanplaythrough','onchange','onclick','onclose',
		'oncontextlost','oncontextmenu','oncontextrestored','oncuechange','ondblclick',
		'ondevicemotion','ondeviceorientation','ondeviceorientationabsolute',
		'ondrag','ondragend','ondragenter','ondragleave','ondragover','ondragstart','ondrop',
		'ondurationchange','onemptied','onended','onerror','onfocus','onformdata',
		'ongamepadconnected','ongamepaddisconnected','ongotpointercapture',
		'onhashchange','oninput','oninvalid','onkeydown','onkeypress','onkeyup',
		'onlanguagechange','onload','onloadeddata','onloadedmetadata','onloadstart',
		'onlostpointercapture','onmessage','onmessageerror',
		'onmousedown','onmouseenter','onmouseleave','onmousemove','onmouseout',
		'onmouseover','onmouseup','onmousewheel','onoffline','ononline',
		'onpagehide','onpagereveal','onpageshow','onpageswap',
		'onpause','onplay','onplaying','onpointercancel','onpointerdown',
		'onpointerenter','onpointerleave','onpointermove','onpointerout',
		'onpointerover','onpointerrawupdate','onpointerup','onpopstate',
		'onprogress','onratechange','onrejectionhandled','onreset','onresize',
		'onscroll','onscrollend','onsearch','onsecuritypolicyviolation',
		'onseeked','onseeking','onselect','onselectionchange','onselectstart',
		'onslotchange','onstalled','onstorage','onsubmit','onsuspend',
		'ontimeupdate','ontoggle','ontransitioncancel','ontransitionend',
		'ontransitionrun','ontransitionstart','onunhandledrejection','onunload',
		'onvolumechange','onwaiting','onwebkitanimationend','onwebkitanimationiteration',
		'onwebkitanimationstart','onwebkittransitionend','onwheel',
		'onanimationcancel','onappinstalled','onbeforeinstallprompt','onbeforematch',
		'onbeforexrselect','oncommand','oncontentvisibilityautostatechange',
		'onscrollsnapchange','onscrollsnapchanging'
	];
	for (var i = 0; i < _evtHandlers.length; i++) {
		if (!((_evtHandlers[i]) in window)) {
			window[_evtHandlers[i]] = null;
		}
	}
})();

// Turnstile API stub (no Proxy wrapper)
window.turnstile = {
	render: function(container, options) {
		console.log('[TURNSTILE] render(' + String(container) + ', ' + JSON.stringify(options) + ')');
		var widgetId = 'widget-' + Math.random().toString(36).substring(2, 10);
		if (options && options.callback) {
			setTimeout(function() {
				var token = '0.' + Math.random().toString(36).substring(2) + '.' + Date.now();
				options.callback(token);
			}, 100);
		}
		return widgetId;
	},
	getResponse: function(widgetId) { return '0.' + Math.random().toString(36).substring(2) + '.' + Date.now(); },
	reset: function() {},
	remove: function() {},
	isExpired: function() { return false; },
	execute: function(container, options) { return this.render(container, options); },
	ready: function(fn) { if (fn) fn(); }
};

// String.prototype.apply/call left as default (removing custom getters
// which are detectable as non-standard browser behavior).

// Error.stack: In real Chrome, native DOM functions (createElement etc.) are
// C++ and don't appear in JS stack traces. Our JS-based DOM stubs (_mkEl
// etc.) DO appear. We filter internal frames via string post-processing on
// V8's default stack format (NOT Error.prepareStackTrace, since Chrome's
// iframe has 'prepareStackTrace' in Error === false).

// --- Make key window properties non-configurable getters (matching Chrome) ---
// In Chrome, document/window/location/top are non-configurable accessor properties.
// Our var declarations make them configurable. The VM checks this.
(function() {
	var _doc = document, _win = window, _loc = location, _top = top;
	try { Object.defineProperty(window, 'document', { get: function() { return _doc; }, set: undefined, enumerable: true, configurable: false }); } catch(e) {}
	try { Object.defineProperty(window, 'location', { get: function() { return _loc; }, set: function(v) { _loc = v; }, enumerable: true, configurable: false }); } catch(e) {}
	try { Object.defineProperty(window, 'top', { get: function() { return _top; }, set: undefined, enumerable: true, configurable: false }); } catch(e) {}
})();

// Simulate user interaction after a delay so CF script's event listeners are registered.
// The script polls for Xx > 0 (any user event) before proceeding to the challenge XHR.
// We use Object.defineProperty to set isTrusted as non-configurable (like browser-dispatched events).
setTimeout(function() {
	console.log('[DOM] Simulating user interaction events');
	var cx = 500 + Math.floor(Math.random() * 200);
	var cy = 400 + Math.floor(Math.random() * 100);
	var events = [
		new PointerEvent('pointerover', {bubbles: true, cancelable: true, clientX: cx, clientY: cy, pointerId: 1, pointerType: 'mouse'}),
		new PointerEvent('pointermove', {bubbles: true, cancelable: true, clientX: cx + 3, clientY: cy + 2, pointerId: 1, pointerType: 'mouse'}),
		new MouseEvent('mousemove', {bubbles: true, cancelable: true, clientX: cx + 5, clientY: cy + 1}),
		new KeyboardEvent('keydown', {bubbles: true, cancelable: true, key: 'a', code: 'KeyA', keyCode: 65})
	];
	for (var i = 0; i < events.length; i++) {
		var ev = events[i];
		ev.pageX = ev.clientX || 0;
		ev.pageY = ev.clientY || 0;
		ev.screenX = ev.clientX || 0;
		ev.screenY = ev.clientY || 0;
		// isTrusted is set to false by V8 event constructors and is non-configurable.
		// Instead of using Proxy (detectable), override isTrusted on the prototype
		// chain by creating a wrapper object that inherits from the event.
		var wrapper = Object.create(ev, {
			isTrusted: { value: true, enumerable: true, configurable: false, writable: false },
			target: { value: document, enumerable: true, configurable: true, writable: true },
			currentTarget: { value: document, enumerable: true, configurable: true, writable: true }
		});
		document.dispatchEvent(wrapper);
	}
}, 500);

// --- window.chrome (Chrome browser API surface) ---
// Real Chrome exposes window.chrome but in cross-origin iframes (like Turnstile),
// chrome.runtime is undefined. Only the top-level page has chrome.runtime.
window.chrome = {
	loadTimes: function() {
		var navStart = performance.timing.navigationStart / 1000;
		return {
			commitLoadTime: navStart + 0.1,
			connectionInfo: "h2",
			finishDocumentLoadTime: navStart + 0.5,
			finishLoadTime: navStart + 0.8,
			firstPaintAfterLoadTime: 0,
			firstPaintTime: navStart + 0.3,
			navigationType: "Other",
			npnNegotiatedProtocol: "h2",
			requestTime: navStart,
			startLoadTime: navStart + 0.05,
			wasAlternateProtocolAvailable: false,
			wasFetchedViaSpdy: true,
			wasNpnNegotiated: true
		};
	},
	csi: function() { return {startE: Date.now() - 3000, onloadT: Date.now() - 500, pageT: 3000, tran: 15, getDetails: function() { return {timings: {}, isFromMemoryCache: false}; }}; },
	app: { isInstalled: false, getDetails: function() { return null; }, getIsInstalled: function() { return false; }, installState: function() { return 'disabled'; }, runningState: function() { return 'cannot_run'; }, InstallState: {DISABLED:'disabled',INSTALLED:'installed',NOT_INSTALLED:'not_installed'}, RunningState: {CANNOT_RUN:'cannot_run',READY_TO_RUN:'ready_to_run',RUNNING:'running'} }
};

// --- Function.prototype.toString masking (WeakSet-based, zero-trace) ---
// Uses WeakSet so no properties are left on function objects. This prevents
// detection via Object.getOwnPropertySymbols, Reflect.ownKeys, etc.
(function() {
	var __nativeFns = new WeakSet();
	var __fnNames = new WeakMap(); // store custom names without touching the function
	var __origToStr = Function.prototype.toString;

	var __maskedToString = function toString() {
		if (__nativeFns.has(this)) {
			var name = __fnNames.get(this) || this.name || '';
			return 'function ' + name + '() { [native code] }';
		}
		// Guard: if 'this' is not a function, return a safe representation.
		// The VM calls Function.prototype.toString on non-function values (e.g. Numbers)
		// to detect if toString has been overridden.
		if (typeof this !== 'function') {
			return typeof this === 'object' && this !== null ? '[object Object]' : String(this);
		}
		return __origToStr.call(this);
	};
	__nativeFns.add(__maskedToString);
	__fnNames.set(__maskedToString, 'toString');
	Function.prototype.toString = __maskedToString;

	// Mark all function properties on an object as "native"
	function __mark(obj) {
		if (!obj || typeof obj !== 'object') return;
		try {
			var names = Object.getOwnPropertyNames(obj);
			for (var i = 0; i < names.length; i++) {
				try {
					var desc = Object.getOwnPropertyDescriptor(obj, names[i]);
					if (desc && typeof desc.value === 'function') {
						__nativeFns.add(desc.value);
						if (!desc.value.name && names[i]) __fnNames.set(desc.value, names[i]);
					}
					// Also mark getter/setter functions with proper "get prop"/"set prop" names
					if (desc && typeof desc.get === 'function') {
						__nativeFns.add(desc.get);
						__fnNames.set(desc.get, 'get ' + names[i]);
					}
					if (desc && typeof desc.set === 'function') {
						__nativeFns.add(desc.set);
						__fnNames.set(desc.set, 'set ' + names[i]);
					}
				} catch(e) {}
			}
		} catch(e) {}
	}

	// Mark browser-like API surfaces
	__mark(window); __mark(document); __mark(navigator); __mark(screen);
	__mark(console); __mark(performance); __mark(crypto); __mark(location);
	if (crypto && crypto.subtle) __mark(crypto.subtle);
	if (navigator.userAgentData) __mark(navigator.userAgentData);
	if (navigator.connection) __mark(navigator.connection);
	if (navigator.mediaDevices) __mark(navigator.mediaDevices);
	if (navigator.permissions) __mark(navigator.permissions);
	if (navigator.storage) __mark(navigator.storage);
	if (navigator.locks) __mark(navigator.locks);
	if (navigator.mediaCapabilities) __mark(navigator.mediaCapabilities);
	if (navigator.credentials) __mark(navigator.credentials);
	if (navigator.gpu) __mark(navigator.gpu);
	if (window.chrome) { __mark(window.chrome); __mark(window.chrome.runtime); }
	// Mark prototypes
	var protos = [Element, HTMLElement, Document, HTMLDocument, Node, Event, EventTarget,
		Window, Navigator, Screen, ShadowRoot,
		CharacterData, AbortSignal, AbortController, XMLHttpRequest,
		window.Request, window.Response, window.Headers,
		window.MessageChannel, window.MessagePort, window.BroadcastChannel,
		window.PerformanceObserver, window.MutationObserver, window.IntersectionObserver,
		window.Range, window.Selection, window.DOMParser, window.TreeWalker,
		window.Worker, window.Blob, window.File, window.FileReader,
		window.AudioContext, window.OfflineAudioContext];
	for (var i = 0; i < protos.length; i++) {
		try {
			if (typeof protos[i] === 'function') {
				__nativeFns.add(protos[i]);
				if (protos[i].prototype) __mark(protos[i].prototype);
			}
		} catch(e) {}
	}
	// Mark common global functions (eval included, VM checks its toString)
	var globals = ['eval','setTimeout','setInterval','clearTimeout','clearInterval',
		'fetch','atob','btoa','requestAnimationFrame','cancelAnimationFrame',
		'queueMicrotask','structuredClone','postMessage','alert','confirm','prompt',
		'getComputedStyle','matchMedia','open','close','focus','blur','scroll','scrollTo',
		'scrollBy','print','stop','getSelection','createImageBitmap',
		'addEventListener','removeEventListener','dispatchEvent'];
	// Also mark the EventTarget.prototype methods (inherited, not own) as native
	var _etpMethods = ['addEventListener','removeEventListener','dispatchEvent'];
	for (var j = 0; j < _etpMethods.length; j++) {
		try {
			var fn = EventTarget.prototype[_etpMethods[j]];
			if (typeof fn === 'function') {
				__nativeFns.add(fn);
				__fnNames.set(fn, _etpMethods[j]);
			}
		} catch(e) {}
	}
	for (var i = 0; i < globals.length; i++) {
		try {
			if (typeof window[globals[i]] === 'function') {
				__nativeFns.add(window[globals[i]]);
				__fnNames.set(window[globals[i]], globals[i]);
			}
		} catch(e) {}
	}

	// Expose via script-scoped vars (not on window) to avoid detection
	_mkNat = function(obj) { __mark(obj); };
	_mkFnNat = function(fn, name) {
		if (typeof fn === 'function') {
			__nativeFns.add(fn);
			if (name) __fnNames.set(fn, name);
		}
	};
})();

// --- Hide any remaining __ prefixed names from the global ---
// Catch-all: make any leftover __* names non-enumerable so they're invisible to
// for-in loops and Object.keys. Most internal state has been moved to const
// (script-scoped) or Symbol keys, but this catches any stragglers.
(function() {
	var _g = (0, eval)('this');
	var _internalSet = {};
	var _allNames = Object.getOwnPropertyNames(_g);
	for (var i = 0; i < _allNames.length; i++) {
		var k = _allNames[i];
		if (k.indexOf('__') === 0 || _internalSet[k]) {
			try { Object.defineProperty(_g, k, { enumerable: false }); } catch(e) {}
		}
	}
})();

// --- Make all constructor/API properties non-enumerable (matching Chrome) ---
// In Chrome, virtually ALL window constructors (Event, HTMLDivElement, AudioContext, etc.)
// are non-enumerable. Our assignments (window.X = ...) create enumerable properties.
// The VM fingerprints the enumerable/non-enumerable status of each property.
(function() {
	var names = Object.getOwnPropertyNames(window);
	for (var i = 0; i < names.length; i++) {
		var n = names[i];
		// Skip lowercase properties (these are instance properties, not constructors)
		if (n.charAt(0) >= 'a' && n.charAt(0) <= 'z') continue;
		// Skip already non-enumerable
		var desc = Object.getOwnPropertyDescriptor(window, n);
		if (!desc || !desc.enumerable || !desc.configurable) continue;
		// Make non-enumerable (constructor/API properties)
		try {
			Object.defineProperty(window, n, {
				value: desc.value,
				writable: desc.writable !== false,
				enumerable: false,
				configurable: desc.configurable
			});
		} catch(e) {}
	}
})();

// --- Error.stack filtering (string post-processing, NO prepareStackTrace) ---
// In real Chrome, native DOM functions (createElement, querySelector etc.) are C++
// and do NOT appear in JS stack traces. Our JS stubs (_mkEl, createElement etc.)
// DO appear. We filter them from the default V8 stack string.
// IMPORTANT: Chrome's iframe does NOT have Error.prepareStackTrace as an own property.
// 'prepareStackTrace' in Error must return false. So we avoid setting it entirely.
// Instead, we post-process the V8 default stack string which already produces
// Chrome-compatible eval frames: "at eval (eval at evaluate (:290:30), <anonymous>:98:17)"
(function() {
	// Internal frame patterns to filter from stack traces
	var _skipFns = {
		'_mkEl':1, '_mk2DC':1, '_mkWGL':1, '_m2p':1,
		'createElement':1, 'createElementNS':1, 'getElementById':1,
		'getElementsByClassName':1, 'getElementsByTagName':1,
		'querySelector':1, 'querySelectorAll':1, 'getAttribute':1,
		'setAttribute':1, 'removeAttribute':1, 'insertBefore':1,
		'appendChild':1, 'removeChild':1, 'replaceChild':1,
		'cloneNode':1, 'contains':1, 'getComputedStyle':1,
		'matchMedia':1, 'getSelection':1, 'createRange':1,
		'focus':1, 'blur':1, 'click':1,
		'dispatchEvent':1, 'addEventListener':1, 'removeEventListener':1
	};

	// Filter internal frames from a V8 default stack string.
	// V8 default format: "Error: msg\n    at funcName (file:line:col)\n    at eval (eval at ..., <anonymous>:line:col)"
	function _filterStack(stack) {
		if (typeof stack !== 'string') return stack;
		var lines = stack.split('\n');
		var out = [];
		for (var i = 0; i < lines.length; i++) {
			var line = lines[i];
			// Always keep the first line (error message)
			if (i === 0) { out.push(line); continue; }
			// Extract function name from "    at funcName (...)" or "    at funcName (eval at ...)"
			var m = line.match(/^\s+at\s+([^\s(]+)/);
			if (m) {
				var fn = m[1];
				// Skip internal __ prefixed frames
				if (fn.indexOf('__') === 0) continue;
				// Skip known DOM stub function names
				if (_skipFns[fn]) continue;
			}
			out.push(line);
		}
		return out.join('\n');
	}

	// Wrap Error.captureStackTrace to post-filter the generated stack
	var _origCapture = Error.captureStackTrace;
	Error.captureStackTrace = function(obj, constructorOpt) {
		_origCapture.call(Error, obj, constructorOpt);
		// V8 sets .stack as a lazy accessor; reading it triggers formatting.
		// Replace with filtered version.
		var rawStack = obj.stack;
		if (rawStack) {
			Object.defineProperty(obj, 'stack', {
				value: _filterStack(rawStack),
				writable: true,
				configurable: true,
				enumerable: false
			});
		}
	};
	if (typeof _mkFnNat === 'function') _mkFnNat(Error.captureStackTrace, 'captureStackTrace');

	// Also patch Error constructor to filter stacks created via "new Error()" / "throw new Error()"
	// V8 auto-captures stack on new Error() even without explicit captureStackTrace call.
	var _OrigError = Error;
	var _PatchedError = function Error(message) {
		var err;
		if (new.target) {
			err = new _OrigError(message);
			Object.setPrototypeOf(err, new.target.prototype);
		} else {
			err = _OrigError(message);
		}
		// Re-capture stack excluding our wrapper frame from the trace
		_origCapture.call(_OrigError, err, _PatchedError);
		// Filter the auto-captured stack (removes internal DOM stub frames)
		var rawStack = err.stack;
		if (rawStack) {
			Object.defineProperty(err, 'stack', {
				value: _filterStack(rawStack),
				writable: true,
				configurable: true,
				enumerable: false
			});
		}
		return err;
	};
	_PatchedError.prototype = _OrigError.prototype;
	_PatchedError.prototype.constructor = _PatchedError;
	_PatchedError.captureStackTrace = Error.captureStackTrace;
	_PatchedError.stackTraceLimit = _OrigError.stackTraceLimit;
	// Copy all own properties from original Error (except prepareStackTrace which we never set)
	var _errProps = Object.getOwnPropertyNames(_OrigError);
	for (var i = 0; i < _errProps.length; i++) {
		var p = _errProps[i];
		if (p === 'prototype' || p === 'captureStackTrace' || p === 'stackTraceLimit') continue;
		try {
			var d = Object.getOwnPropertyDescriptor(_OrigError, p);
			if (d) Object.defineProperty(_PatchedError, p, d);
		} catch(e) {}
	}
	// stackTraceLimit as a forwarding accessor so changes propagate
	Object.defineProperty(_PatchedError, 'stackTraceLimit', {
		get: function() { return _OrigError.stackTraceLimit; },
		set: function(v) { _OrigError.stackTraceLimit = v; },
		configurable: true,
		enumerable: false
	});
	window.Error = _PatchedError;
	if (typeof _mkFnNat === 'function') _mkFnNat(_PatchedError, 'Error');
})();

// --- Filter internal names from all property enumeration methods ---
// var-declared internal names (e.g. _cfDb, _cfDh, _cfDe, _cfDc) can't be deleted
// from V8 global, so filter them out from Object.getOwnPropertyNames, Reflect.ownKeys,
// Object.keys, Object.getOwnPropertyDescriptor, and hasOwnProperty.
(function() {
	var _g = (0, eval)('this');
	var _internalPrefixes = {'htmlTags':1,'i':1,
		'_cfDb':1,'_cfDh':1,'_cfDe':1,'_cfDc':1,'_cfPt':1,'_cfPto':1,
		'WindowProperties':1,'SharedArrayBuffer':1,'turnstile':1};
	function _isInternal(n) {
		if (typeof n !== 'string') return false;
		if (n.indexOf('__') === 0) return true;
		if (n.charAt(0) === '_' && n.length > 1 && n.charAt(1) !== '_') return true; // _mkStyle, _wDoc, etc.
		if (_internalPrefixes[n]) return true;
		if (n === 'print') return true; // V8 built-in, not in Chrome
		return false;
	}
	function _isGlobal(obj) { return obj === window || obj === _g; }

	// Helper: should this property be filtered from the given object?
	// (window.__DISABLE_GOPN_FILTER checked for detection binary search)
	function _shouldFilter(obj, n) {
		if (window.__DISABLE_GOPN_FILTER) return false;
		if (_isGlobal(obj)) return _isInternal(n);
		return false;
	}

	// 1. Object.getOwnPropertyNames, cached for window
	var _origGOPN = Object.getOwnPropertyNames;
	var _cachedWindowGOPN = null;
	var _filteredGOPN = function getOwnPropertyNames(obj) {
		var result = _origGOPN.call(Object, obj);
		if (_isGlobal(obj)) {
			if (!_cachedWindowGOPN) {
				_cachedWindowGOPN = result.filter(function(n) { return !_shouldFilter(obj, n); });
			}
			return _cachedWindowGOPN.slice();
		}
		return result;
	};
	Object.getOwnPropertyNames = _filteredGOPN;

	// 2. Reflect.ownKeys, cached for window
	var _origROK = Reflect.ownKeys;
	var _cachedWindowROK = null;
	var _filteredROK = function ownKeys(obj) {
		var result = _origROK.call(Reflect, obj);
		if (_isGlobal(obj)) {
			if (!_cachedWindowROK) {
				_cachedWindowROK = result.filter(function(n) { return !_shouldFilter(obj, n); });
			}
			return _cachedWindowROK.slice();
		}
		return result;
	};
	Reflect.ownKeys = _filteredROK;

	// 3. Object.keys
	var _origKeys = Object.keys;
	var _filteredKeys = function keys(obj) {
		var result = _origKeys.call(Object, obj);
		if (_isGlobal(obj)) result = result.filter(function(n) { return !_shouldFilter(obj, n); });
		return result;
	};
	Object.keys = _filteredKeys;

	// 4. Object.getOwnPropertyDescriptor, return undefined for hidden names
	var _origGOPD = Object.getOwnPropertyDescriptor;
	var _filteredGOPD = function getOwnPropertyDescriptor(obj, prop) {
		if (_shouldFilter(obj, prop)) return undefined;
		return _origGOPD.call(Object, obj, prop);
	};
	Object.getOwnPropertyDescriptor = _filteredGOPD;

	// 5. Object.getOwnPropertyDescriptors
	var _origGOPDs = Object.getOwnPropertyDescriptors;
	if (_origGOPDs) {
		var _filteredGOPDs = function getOwnPropertyDescriptors(obj) {
			var result = _origGOPDs.call(Object, obj);
			if (_isGlobal(obj)) {
				var keys = _origGOPN.call(Object, result);
				for (var i = 0; i < keys.length; i++) {
					if (_shouldFilter(obj, keys[i])) delete result[keys[i]];
				}
			}
			return result;
		};
		Object.getOwnPropertyDescriptors = _filteredGOPDs;
	}

	// 6. Patch hasOwnProperty on window to deny hidden names
	var _origHOP = Object.prototype.hasOwnProperty;
	var _filteredHOP = function hasOwnProperty(prop) {
		if (_shouldFilter(this, prop)) return false;
		return _origHOP.call(this, prop);
	};
	Object.prototype.hasOwnProperty = _filteredHOP;

	// 7. Patch 'in' operator indirectly by making internal vars non-enumerable
	// (can't intercept 'in' directly, but we can try to hide with defineProperty)
	var _allOwn = _origGOPN.call(Object, _g);
	for (var i = 0; i < _allOwn.length; i++) {
		if (_isInternal(_allOwn[i])) {
			try { Object.defineProperty(_g, _allOwn[i], { enumerable: false }); } catch(e) {}
		}
	}

	// Mark all patched functions as native
	if (typeof _mkFnNat === 'function') {
		_mkFnNat(_filteredGOPN, 'getOwnPropertyNames');
		_mkFnNat(_filteredROK, 'ownKeys');
		_mkFnNat(_filteredKeys, 'keys');
		_mkFnNat(_filteredGOPD, 'getOwnPropertyDescriptor');
		if (_origGOPDs) _mkFnNat(_filteredGOPDs, 'getOwnPropertyDescriptors');
		_mkFnNat(_filteredHOP, 'hasOwnProperty');
	}
})();

// Prevent "Converting circular structure to JSON" for window (window.self = window)
window.toJSON = function() { return '[object Window]'; };

// Ensure globals are accessible at top level
var self = window;

`,
		cfg.TimeDilation,
		cfg.URL,
		hostname,
		origin,
		pathname,
		protocol,
		hostname,
		parsedURL.RawQuery,
		cfg.CanvasFingerprint,
		cfg.UserAgent,
		cfg.UserAgent,
		// document.referrer, domain, URL, documentURI removed, now native getters
		// on Document.prototype set by engine.go setupDocument()
		origin,
	)

	// --- Detection binary-search toggles (env vars) ---
	// When set, these env vars disable specific DOM features to isolate which
	// one contributes to the AfhcW1 detection counter.
	if os.Getenv("DISABLE_CANVAS") == "1" {
		script += `
// [DETECTION-TEST] DISABLE_CANVAS: canvas.toDataURL returns blank 1x1 PNG
(function() {
	var _blankPNG = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==';
	var _origCreateEl = document.createElement;
	document.createElement = function(tag) {
		var el = _origCreateEl.call(document, tag);
		if (tag === 'canvas') {
			el.toDataURL = function() { return _blankPNG; };
			el.toBlob = function(cb) { if (cb) setTimeout(function(){ cb(new Blob([''], {type:'image/png'})); },0); };
			var _origGetCtx = el.getContext;
			el.getContext = function(type) {
				var ctx = _origGetCtx ? _origGetCtx.call(this, type) : null;
				if (ctx && ctx.getImageData) {
					ctx.getImageData = function(x,y,w,h) {
						return { data: new Uint8ClampedArray(w*h*4), width: w, height: h, colorSpace: 'srgb' };
					};
				}
				return ctx;
			};
		}
		return el;
	};
	console.log('[DETECTION-TEST] DISABLE_CANVAS active');
})();
`
	}
	if os.Getenv("DISABLE_AUDIO") == "1" {
		script += `
// [DETECTION-TEST] DISABLE_AUDIO: OfflineAudioContext.startRendering returns silence
(function() {
	var _origOAC = window.OfflineAudioContext;
	if (_origOAC) {
		var _origStartRendering = _origOAC.prototype.startRendering;
		_origOAC.prototype.startRendering = function() {
			var self = this;
			var buf = {
				numberOfChannels: self.numberOfChannels || 1,
				length: self.length || 44100,
				sampleRate: self.sampleRate || 44100,
				duration: (self.length || 44100) / (self.sampleRate || 44100),
				getChannelData: function() { return new Float32Array(self.length || 44100); },
				copyFromChannel: function() {}, copyToChannel: function() {}
			};
			if (self.oncomplete) setTimeout(function(){ self.oncomplete({renderedBuffer: buf}); }, 1);
			return Promise.resolve(buf);
		};
	}
	console.log('[DETECTION-TEST] DISABLE_AUDIO active');
})();
`
	}
	if os.Getenv("DISABLE_GETCOMPUTEDSTYLE") == "1" {
		script += `
// [DETECTION-TEST] DISABLE_GETCOMPUTEDSTYLE: return minimal object
(function() {
	window.getComputedStyle = function(el, pseudo) {
		var empty = {};
		empty.getPropertyValue = function() { return ''; };
		Object.defineProperty(empty, 'length', { value: 0, writable: false });
		empty.item = function() { return ''; };
		empty.setProperty = function() {};
		empty.removeProperty = function() {};
		return empty;
	};
	console.log('[DETECTION-TEST] DISABLE_GETCOMPUTEDSTYLE active');
})();
`
	}
	if os.Getenv("DISABLE_GOPN_FILTER") == "1" {
		script += `
// [DETECTION-TEST] DISABLE_GOPN_FILTER: bypass GOPN filtering, expose internal vars
(function() {
	// Undo the GOPN filter by restoring originals.
	// This is tricky because the filter runs later. Instead, set a flag.
	window.__DISABLE_GOPN_FILTER = true;
	console.log('[DETECTION-TEST] DISABLE_GOPN_FILTER active');
})();
`
	}
	if os.Getenv("DISABLE_SETHANDLER") == "1" {
		script += `
// [DETECTION-TEST] DISABLE_SETHANDLER: make _goCreateElement return undefined
// This forces all elements to use the non-native JS fallback path.
(function() {
	// Override the script-scope const won't work, but we can neuter the native callback.
	globalThis.__goCreateElement = function() { return undefined; };
	console.log('[DETECTION-TEST] DISABLE_SETHANDLER active');
})();
`
	}
	if os.Getenv("DISABLE_MATCHMEDIA") == "1" {
		script += `
// [DETECTION-TEST] DISABLE_MATCHMEDIA: always return matches=false
(function() {
	window.matchMedia = function(q) {
		return { matches: false, media: q, onchange: null,
			addListener: function(){}, removeListener: function(){},
			addEventListener: function(){}, removeEventListener: function(){},
			dispatchEvent: function(){ return true; } };
	};
	console.log('[DETECTION-TEST] DISABLE_MATCHMEDIA active');
})();
`
	}
	if os.Getenv("DISABLE_PERFORMANCE") == "1" {
		script += `
// [DETECTION-TEST] DISABLE_PERFORMANCE: neuter performance.now and timing
(function() {
	if (window.performance) {
		window.performance.now = function() { return 0; };
		if (window.performance.timing) {
			var t = {};
			var keys = Object.getOwnPropertyNames(window.performance.timing);
			for (var i = 0; i < keys.length; i++) t[keys[i]] = 0;
			window.performance.timing = t;
		}
	}
	console.log('[DETECTION-TEST] DISABLE_PERFORMANCE active');
})();
`
	}
	if os.Getenv("DISABLE_SPEECHSYNTH") == "1" {
		script += `
// [DETECTION-TEST] DISABLE_SPEECHSYNTH: speechSynthesis.getVoices returns []
(function() {
	if (window.speechSynthesis) {
		window.speechSynthesis.getVoices = function() { return []; };
	}
	console.log('[DETECTION-TEST] DISABLE_SPEECHSYNTH active');
})();
`
	}

	// Inject real Chrome audio samples from embedded chrome_audio_44100.bin.
	// These are used by OfflineAudioContext.startRendering().getChannelData().
	// const at script top level is script-scoped, not a global property, invisible
	// to 'in' operator, Object.keys, and property enumeration on window.
	script += "\nconst _chromeAudioSamples = new Float32Array(" + chromeAudioSamplesJS() + ");\n"

	// Populate document.cookie with initial cookies from HTTP responses.
	if len(cfg.Cookies) > 0 {
		var cookieInit strings.Builder
		cookieInit.WriteString("\n// --- Initial cookies from HTTP responses ---\n")
		for name, value := range cfg.Cookies {
			// Escape quotes in cookie values for safe JS string embedding.
			safeName := strings.ReplaceAll(name, `"`, `\"`)
			safeValue := strings.ReplaceAll(value, `"`, `\"`)
			cookieInit.WriteString(fmt.Sprintf("document.cookie = \"%s=%s\";\n", safeName, safeValue))
		}
		script += cookieInit.String()
	}

	// --- TEMPORARY DIAGNOSTIC: Environment comparison checks ---
	// Same checks as capture_env_comprehensive.js to compare against Chrome.
	// Results logged via console.log so they appear in solver debug output.
	if os.Getenv("ENV_DIAG") == "1" {
		script += `
// ===== ENVIRONMENT DIAGNOSTIC (temporary) =====
(function() {
	var r = {};

	// A. IFRAME-SPECIFIC WINDOW PROPERTIES
	r['A01_window.parent===window'] = window.parent === window;
	r['A02_window.top===window'] = window.top === window;
	r['A03_window.self===window'] = window.self === window;
	r['A04_typeof_window.frameElement'] = typeof window.frameElement;
	r['A05_window.frameElement_is_null'] = window.frameElement === null;
	r['A06_window.frameElement_tagName'] = window.frameElement ? window.frameElement.tagName : 'N/A';
	r['A07_window.length'] = window.length;
	r['A08_window.name'] = window.name;
	r['A09_window.opener'] = window.opener;
	r['A10_window.closed'] = window.closed;
	r['A11_typeof_window.parent'] = typeof window.parent;
	r['A12_typeof_window.top'] = typeof window.top;
	r['A13_window.parent===window.top'] = window.parent === window.top;
	try { r['A14_Object.prototype.toString.call(parent)'] = Object.prototype.toString.call(window.parent); } catch(e) { r['A14_Object.prototype.toString.call(parent)'] = 'ERROR:' + e.message; }
	try { r['A15_Object.prototype.toString.call(top)'] = Object.prototype.toString.call(window.top); } catch(e) { r['A15_Object.prototype.toString.call(top)'] = 'ERROR:' + e.message; }
	try { r['A16_parent_instanceof_Window'] = window.parent instanceof Window; } catch(e) { r['A16_parent_instanceof_Window'] = 'ERROR:' + e.message; }

	// B. CROSS-ORIGIN BEHAVIOR
	try { r['B01_parent.location'] = String(window.parent.location); r['B01_threw'] = false; } catch(e) { r['B01_parent.location'] = e.message.substring(0, 120); r['B01_threw'] = true; r['B01_errorName'] = e.name; }
	try { r['B02_parent.document'] = typeof window.parent.document; r['B02_threw'] = false; } catch(e) { r['B02_parent.document'] = e.message.substring(0, 120); r['B02_threw'] = true; r['B02_errorName'] = e.name; }
	try { r['B03_top.location'] = String(window.top.location); r['B03_threw'] = false; } catch(e) { r['B03_top.location'] = e.message.substring(0, 120); r['B03_threw'] = true; r['B03_errorName'] = e.name; }
	r['B04_typeof_parent.postMessage'] = typeof window.parent.postMessage;
	try { r['B05_parent.closed'] = window.parent.closed; } catch(e) { r['B05_parent.closed'] = 'ERROR:' + e.message; }
	try { r['B06_parent.frames'] = typeof window.parent.frames; } catch(e) { r['B06_parent.frames'] = 'ERROR:' + e.message; }
	try { r['B07_parent.length'] = window.parent.length; } catch(e) { r['B07_parent.length'] = 'ERROR:' + e.message; }
	var parentCheckProps = ['location','document','navigator','console','alert','setTimeout','eval','origin','name','chrome','performance','screen','innerWidth'];
	for (var pi = 0; pi < parentCheckProps.length; pi++) {
		var prop = parentCheckProps[pi];
		try { var v = window.parent[prop]; r['B_parent.' + prop + '_typeof'] = typeof v; r['B_parent.' + prop + '_threw'] = false; } catch(e) { r['B_parent.' + prop + '_typeof'] = 'ERROR'; r['B_parent.' + prop + '_threw'] = true; r['B_parent.' + prop + '_errName'] = e.name; }
	}

	// C. SCRIPT EXECUTION CONTEXT
	r['C01_document.currentScript'] = document.currentScript;
	try { r['C02_error_stack_line1'] = new Error('test').stack.split('\\\n')[1].trim().substring(0, 120); } catch(e) { r['C02_error_stack_line1'] = 'ERROR'; }
	try { r['C03_error_stack_has_eval'] = new Error('test').stack.includes('eval'); } catch(e) { r['C03_error_stack_has_eval'] = 'ERROR'; }

	// D. TIMING PRECISION
	r['D01_performance.now_type'] = typeof performance.now;
	r['D02_performance.now()'] = performance.now();
	r['D03_performance.timeOrigin'] = performance.timeOrigin;
	r['D04_Date.now()'] = Date.now();
	r['D05_perf_now_monotonic'] = (function() {
		var a = performance.now(), b = performance.now(), c = performance.now();
		return b >= a && c >= b;
	})();

	// E. WEB API BEHAVIORS
	r['E01_typeof_MessageChannel'] = typeof MessageChannel;
	try {
		var mc = new MessageChannel();
		r['E02_MessageChannel_port1_type'] = typeof mc.port1;
		r['E03_MessageChannel_port1.postMessage_type'] = typeof mc.port1.postMessage;
	} catch(e) { r['E02_MessageChannel_error'] = e.message; }
	r['E04_typeof_queueMicrotask'] = typeof queueMicrotask;
	r['E05_typeof_structuredClone'] = typeof structuredClone;
	r['E06_typeof_BroadcastChannel'] = typeof BroadcastChannel;
	r['E07_typeof_crypto'] = typeof crypto;
	r['E08_typeof_crypto.subtle'] = typeof (crypto && crypto.subtle);
	r['E09_typeof_crypto.getRandomValues'] = typeof (crypto && crypto.getRandomValues);
	r['E10_typeof_Intl'] = typeof Intl;
	r['E11_typeof_WebAssembly'] = typeof WebAssembly;
	r['E12_typeof_SharedArrayBuffer'] = typeof SharedArrayBuffer;
	r['E13_typeof_Atomics'] = typeof Atomics;

	// F. V8-SPECIFIC
	try { r['F02_eval_this_is_window'] = (0, eval)('this') === window; } catch(e) { r['F02_eval_this_is_window'] = 'ERROR:' + e.message; }
	r['F03_typeof_Proxy'] = typeof Proxy;
	r['F04_typeof_Reflect'] = typeof Reflect;
	r['F07_typeof_WeakRef'] = typeof WeakRef;
	r['F08_typeof_FinalizationRegistry'] = typeof FinalizationRegistry;
	r['F10_globalThis===window'] = globalThis === window;

	// H. OBJECT IDENTITY & PROTOTYPE CHECKS
	r['H01_window_constructor_name'] = window.constructor ? window.constructor.name : 'null';
	r['H02_window_toString'] = Object.prototype.toString.call(window);
	r['H04_window_instanceof_Window'] = window instanceof Window;
	r['H05_window_instanceof_EventTarget'] = window instanceof EventTarget;
	r['H06_document_constructor_name'] = document.constructor ? document.constructor.name : 'null';
	r['H07_document_toString'] = Object.prototype.toString.call(document);
	r['H08_document_instanceof_Document'] = document instanceof Document;
	r['H09_document_instanceof_HTMLDocument'] = document instanceof HTMLDocument;
	r['H10_navigator_constructor_name'] = navigator.constructor ? navigator.constructor.name : 'null';
	r['H11_navigator_toString'] = Object.prototype.toString.call(navigator);
	r['H12_screen_constructor_name'] = screen.constructor ? screen.constructor.name : 'null';
	r['H13_screen_toString'] = Object.prototype.toString.call(screen);

	// I. FUNCTION TOSTRING CHECKS
	r['I01_setTimeout_toString'] = Function.prototype.toString.call(setTimeout).substring(0, 80);
	r['I02_setInterval_toString'] = Function.prototype.toString.call(setInterval).substring(0, 80);
	r['I03_eval_toString'] = Function.prototype.toString.call(eval).substring(0, 80);
	r['I04_fetch_toString'] = Function.prototype.toString.call(fetch).substring(0, 80);
	r['I07_addEventListener_toString'] = Function.prototype.toString.call(window.addEventListener).substring(0, 80);
	r['I08_document.createElement_toString'] = Function.prototype.toString.call(document.createElement).substring(0, 80);

	// J. PROPERTY DESCRIPTOR CHECKS
	var windowProtoDescs = ['document','navigator','screen','location','parent','top','self','frameElement','innerWidth','innerHeight','outerWidth','outerHeight','devicePixelRatio','name','closed','opener','length','frames','origin','isSecureContext','crossOriginIsolated'];
	for (var ji = 0; ji < windowProtoDescs.length; ji++) {
		var jprop = windowProtoDescs[ji];
		var d = Object.getOwnPropertyDescriptor(Window.prototype, jprop);
		if (d) {
			r['J_WinProto_' + jprop] = _safeStringify({ get: !!d.get, set: !!d.set, enum: d.enumerable, conf: d.configurable });
		} else {
			var d2 = Object.getOwnPropertyDescriptor(window, jprop);
			r['J_WinInst_' + jprop] = d2 ? _safeStringify({ val: typeof d2.value, get: !!d2.get, enum: d2.enumerable, conf: d2.configurable }) : 'NOT_FOUND';
		}
	}

	// K. MISSING/EXTRA PROPERTIES
	var checkProps = ['chrome','speechSynthesis','customElements','trustedTypes','navigation','scheduler','cookieStore','caches','indexedDB','localStorage','sessionStorage','visualViewport','screenX','screenY','scrollX','scrollY','pageXOffset','pageYOffset','originAgentCluster','credentialless','crossOriginIsolated','isSecureContext','external','offscreenBuffering','event','status','styleMedia','fence','launchQueue','documentPictureInPicture','sharedStorage','onbeforeunload','onload','onerror','onmessage'];
	for (var ki = 0; ki < checkProps.length; ki++) {
		r['K_typeof_' + checkProps[ki]] = typeof window[checkProps[ki]];
	}

	// L. DOCUMENT CHECKS
	r['L01_document.readyState'] = document.readyState;
	r['L02_document.visibilityState'] = document.visibilityState;
	r['L03_document.hidden'] = document.hidden;
	r['L04_document.compatMode'] = document.compatMode;
	r['L05_document.characterSet'] = document.characterSet;
	r['L06_document.contentType'] = document.contentType;
	r['L07_document.doctype'] = document.doctype ? document.doctype.name : null;
	r['L08_typeof_document.all'] = typeof document.all;
	r['L09_document.all==null'] = document.all == null;
	r['L10_document.cookie_type'] = typeof document.cookie;
	r['L11_document.domain'] = document.domain;
	r['L12_document.title'] = document.title;
	r['L13_document.dir'] = document.dir;
	r['L14_document.designMode'] = document.designMode;
	r['L15_document.activeElement_tag'] = document.activeElement ? document.activeElement.tagName : null;

	// M. CRITICAL: WINDOW OWN PROPERTIES
	var ownNames = Object.getOwnPropertyNames(window);
	r['M01_GOPN_window_count'] = ownNames.length;
	r['M02_parent_is_own'] = ownNames.indexOf('parent') !== -1;
	r['M03_top_is_own'] = ownNames.indexOf('top') !== -1;
	r['M04_self_is_own'] = ownNames.indexOf('self') !== -1;
	r['M05_document_is_own'] = ownNames.indexOf('document') !== -1;
	r['M06_navigator_is_own'] = ownNames.indexOf('navigator') !== -1;
	r['M07_location_is_own'] = ownNames.indexOf('location') !== -1;
	r['M08_frames_is_own'] = ownNames.indexOf('frames') !== -1;
	r['M09_length_is_own'] = ownNames.indexOf('length') !== -1;
	r['M10_name_is_own'] = ownNames.indexOf('name') !== -1;
	r['M11_closed_is_own'] = ownNames.indexOf('closed') !== -1;
	r['M12_opener_is_own'] = ownNames.indexOf('opener') !== -1;
	r['M13_frameElement_is_own'] = ownNames.indexOf('frameElement') !== -1;

	// N. PARENT OBJECT SHAPE
	try {
		var parentOwnNames = Object.getOwnPropertyNames(window.parent);
		r['N01_parent_GOPN_count'] = parentOwnNames.length;
		r['N02_parent_GOPN_first10'] = _safeStringify(parentOwnNames.slice(0, 10));
	} catch(e) {
		r['N01_parent_GOPN_error'] = e.name + ':' + e.message.substring(0, 80);
	}
	try {
		var topOwnNames = Object.getOwnPropertyNames(window.top);
		r['N03_top_GOPN_count'] = topOwnNames.length;
	} catch(e) {
		r['N03_top_GOPN_error'] = e.name + ':' + e.message.substring(0, 80);
	}

	// O. CROSS-ORIGIN GETOWNPROPERTYNAMES ON PARENT
	try { r['O01_parent_hasOwnProperty_postMessage'] = Object.prototype.hasOwnProperty.call(window.parent, 'postMessage'); } catch(e) { r['O01_error'] = e.message.substring(0, 80); }
	try { r['O02_parent_in_postMessage'] = 'postMessage' in window.parent; } catch(e) { r['O02_error'] = e.message.substring(0, 80); }
	try { r['O03_parent_in_location'] = 'location' in window.parent; } catch(e) { r['O03_error'] = e.message.substring(0, 80); }

	// P. CONSTRUCTOR / PROTOTYPE IDENTITY
	r['P01_Window.prototype_toString'] = Object.prototype.toString.call(Window.prototype);
	r['P02_Window.length'] = Window.length;
	r['P03_Window.name'] = Window.name;
	r['P05_navigator_proto_is_Navigator.prototype'] = Object.getPrototypeOf(navigator) === Navigator.prototype;
	r['P06_screen_proto_is_Screen.prototype'] = Object.getPrototypeOf(screen) === Screen.prototype;
	r['P07_document_proto_is_HTMLDocument.prototype'] = Object.getPrototypeOf(document) === HTMLDocument.prototype;
	r['P08_performance_proto_is_Performance.prototype'] = Object.getPrototypeOf(performance) === Performance.prototype;

	// R. SPECIFIC DETECTION VECTORS
	r['R01_navigator.webdriver'] = navigator.webdriver;
	r['R02_Error.prepareStackTrace_in_Error'] = 'prepareStackTrace' in Error;
	r['R03_Object.prototype.querySelector'] = 'querySelector' in {};
	r['R04_Array.isArray_plugins'] = Array.isArray(navigator.plugins);

	// S. WINDOW IDENTITY
	r['S01_window===self'] = window === self;
	r['S02_window===globalThis'] = window === globalThis;

	console.log('[ENV-DIAG] ' + _safeStringify(r));
})();
`
	}

	os.WriteFile("/tmp/kasada_dom.js", []byte(script), 0644)
	return script
}
