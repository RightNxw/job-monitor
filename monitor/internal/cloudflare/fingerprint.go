//go:build solver

package cloudflare

// Static Chrome 144/macOS fingerprint for JSD beacon payload.
//
// Format: OrderedMap where keys are values and values are arrays of property names
// that have that value. This is how Cloudflare's JSD encodes the fingerprint.

import (
	"fmt"
	"time"

	"github.com/iancoleman/orderedmap"
)

// GenerateFingerprint builds a JSD fingerprint payload for Chrome 144/macOS.
// host: target hostname (e.g. "tix.axs.com")
// uri: full page URI (e.g. "https://tix.axs.com/shop/search")
// cookie: current cookie string (can be "s" for minimal)
func GenerateFingerprint(host, uri, cookie string) *orderedmap.OrderedMap {
	now := time.Now()
	lastModified := now.Format("01/02/2006 15:04:05")

	if cookie == "" {
		cookie = "s"
	}

	o := orderedmap.New()

	// Numeric property values -- keys are the value, values are property name arrays.
	o.Set("0", []string{"length", "innerWidth", "innerHeight", "scrollX", "pageXOffset", "scrollY", "pageYOffset", "screenX", "screenY", "screenLeft", "screenTop", "TEMPORARY", "n.maxTouchPoints"})
	o.Set("1", []string{"devicePixelRatio", "PERSISTENT", "d.childElementCount", "d.ELEMENT_NODE", "d.DOCUMENT_POSITION_DISCONNECTED"})
	o.Set("2", []string{"d.ATTRIBUTE_NODE", "d.DOCUMENT_POSITION_PRECEDING"})
	o.Set("3", []string{"d.TEXT_NODE"})
	o.Set("4", []string{"d.CDATA_SECTION_NODE", "d.DOCUMENT_POSITION_FOLLOWING"})
	o.Set("5", []string{"d.ENTITY_REFERENCE_NODE"})
	o.Set("6", []string{"d.ENTITY_NODE"})
	o.Set("7", []string{"d.PROCESSING_INSTRUCTION_NODE"})
	o.Set("8", []string{"n.deviceMemory", "d.COMMENT_NODE", "d.DOCUMENT_POSITION_CONTAINS"})
	o.Set("9", []string{"d.nodeType", "d.DOCUMENT_NODE"})
	o.Set("10", []string{"d.DOCUMENT_TYPE_NODE"})
	o.Set("11", []string{"d.DOCUMENT_FRAGMENT_NODE"})
	o.Set("12", []string{"d.NOTATION_NODE"})
	o.Set("16", []string{"n.hardwareConcurrency", "d.DOCUMENT_POSITION_CONTAINED_BY"})
	o.Set("32", []string{"d.DOCUMENT_POSITION_IMPLEMENTATION_SPECIFIC"})
	// macOS screen dimensions (Retina 1440x900 logical)
	o.Set("900", []string{"outerHeight"})
	o.Set("1440", []string{"outerWidth"})

	// Object-type properties
	o.Set("o", []string{
		"window", "self", "document", "location", "customElements", "history", "navigation",
		"locationbar", "menubar", "personalbar", "scrollbars", "statusbar", "toolbar",
		"frames", "top", "parent", "frameElement", "navigator", "external", "screen",
		"visualViewport", "clientInformation", "styleMedia", "crypto", "scheduler",
		"performance", "trustedTypes", "indexedDB", "localStorage", "sessionStorage",
		"chrome", "cookieStore", "caches", "documentPictureInPicture", "sharedStorage",
		"viewport", "launchQueue", "speechSynthesis", "globalThis", "JSON", "Math",
		"Intl", "Atomics", "Reflect", "console", "CSS", "WebAssembly",
		"GPUBufferUsage", "GPUColorWrite", "GPUMapMode", "GPUShaderStage", "GPUTextureUsage",
		"n.scheduling", "n.userActivation", "n.geolocation", "n.plugins", "n.mimeTypes",
		"n.webkitTemporaryStorage", "n.webkitPersistentStorage", "n.connection",
		"n.windowControlsOverlay", "n.protectedAudience", "n.bluetooth", "n.clipboard",
		"n.credentials", "n.keyboard", "n.managed", "n.mediaDevices", "n.storage",
		"n.serviceWorker", "n.virtualKeyboard", "n.wakeLock", "n.userAgentData",
		"n.locks", "n.login", "n.ink", "n.mediaCapabilities", "n.devicePosture",
		"n.hid", "n.mediaSession", "n.permissions", "n.presentation", "n.serial",
		"n.gpu", "n.usb", "n.xr", "n.storageBuckets",
		"d.location", "d.implementation", "d.documentElement", "d.body", "d.head",
		"d.images", "d.embeds", "d.plugins", "d.links", "d.forms", "d.scripts",
		"d.defaultView", "d.anchors", "d.applets", "d.scrollingElement",
		"d.featurePolicy", "d.timeline", "d.children", "d.firstElementChild",
		"d.lastElementChild", "d.activeElement", "d.styleSheets", "d.fonts",
		"d.fragmentDirective", "d.childNodes", "d.firstChild", "d.lastChild",
	})

	// Boolean false properties
	o.Set("F", []string{
		"closed", "crossOriginIsolated", "credentialless", "n.webdriver",
		"n.deprecatedRunAdAuctionEnforcesKAnonymity", "d.xmlStandalone", "d.hidden",
		"d.wasDiscarded", "d.prerendering", "d.webkitHidden", "d.fullscreen",
		"d.webkitIsFullScreen",
	})

	// Null/undefined properties
	o.Set("x", []string{
		"opener", "onsearch", "onappinstalled", "onbeforeinstallprompt", "onbeforexrselect",
		"onabort", "onbeforeinput", "onbeforematch", "onbeforetoggle", "onblur", "oncancel",
		"oncanplay", "oncanplaythrough", "onchange", "onclick", "onclose", "oncommand",
		"oncontentvisibilityautostatechange", "oncontextlost", "oncontextmenu",
		"oncontextrestored", "oncuechange", "ondblclick", "ondrag", "ondragend",
		"ondragenter", "ondragleave", "ondragover", "ondragstart", "ondrop",
		"ondurationchange", "onemptied", "onended", "onerror", "onfocus", "onformdata",
		"oninput", "oninvalid", "onkeydown", "onkeypress", "onkeyup", "onload",
		"onloadeddata", "onloadedmetadata", "onloadstart", "onmousedown", "onmouseenter",
		"onmouseleave", "onmousemove", "onmouseout", "onmouseover", "onmouseup",
		"onmousewheel", "onpause", "onplay", "onplaying", "onprogress", "onratechange",
		"onreset", "onresize", "onscroll", "onscrollend", "onsecuritypolicyviolation",
		"onseeked", "onseeking", "onselect", "onslotchange", "onstalled", "onsubmit",
		"onsuspend", "ontimeupdate", "ontoggle", "onvolumechange", "onwaiting",
		"fence", "n.doNotTrack", "d.doctype", "d.xmlEncoding", "d.xmlVersion",
		"d.currentScript", "d.onreadystatechange", "d.all",
	})

	// Origin
	o.Set(fmt.Sprintf("https://%s", host), []string{"origin"})

	// Undefined properties
	o.Set("u", []string{"event", "undefined"})

	// Boolean true properties
	o.Set("T", []string{
		"isSecureContext", "originAgentCluster", "offscreenBuffering",
		"n.pdfViewerEnabled", "n.cookieEnabled", "n.onLine",
		"d.fullscreenEnabled", "d.webkitFullscreenEnabled",
		"d.pictureInPictureEnabled", "d.isConnected",
	})

	// Function-type properties
	o.Set("N", []string{
		"alert", "atob", "blur", "btoa", "cancelAnimationFrame", "cancelIdleCallback",
		"captureEvents", "clearInterval", "clearTimeout", "close", "confirm",
		"createImageBitmap", "fetch", "find", "focus", "getComputedStyle", "getSelection",
		"matchMedia", "moveBy", "moveTo", "open", "postMessage", "print", "prompt",
		"queueMicrotask", "releaseEvents", "reportError", "requestAnimationFrame",
		"requestIdleCallback", "resizeBy", "resizeTo", "scroll", "scrollBy", "scrollTo",
		"setInterval", "setTimeout", "stop", "structuredClone",
		"addEventListener", "dispatchEvent", "removeEventListener",
		"Object", "Function", "Number", "parseFloat", "parseInt", "Boolean", "String",
		"Symbol", "Date", "Promise", "RegExp", "Error", "AggregateError", "EvalError",
		"RangeError", "ReferenceError", "SyntaxError", "TypeError", "URIError",
		"ArrayBuffer", "Uint8Array", "Int8Array", "Uint16Array", "Int16Array",
		"Uint32Array", "Int32Array", "BigUint64Array", "BigInt64Array",
		"Uint8ClampedArray", "Float32Array", "Float64Array", "DataView",
		"Map", "BigInt", "Set", "WeakMap", "WeakSet", "Proxy", "WeakRef",
		"decodeURI", "decodeURIComponent", "encodeURI", "encodeURIComponent",
		"escape", "unescape", "eval", "isFinite", "isNaN",
		"XMLHttpRequest", "Request", "Response", "Headers", "URL", "URLSearchParams",
		"Blob", "File", "FileReader", "FormData", "WebSocket", "Worker",
		"Event", "CustomEvent", "EventTarget", "Node", "Element", "Document",
		"HTMLElement", "HTMLDivElement", "HTMLSpanElement", "HTMLInputElement",
		"d.getElementById", "d.getElementsByClassName", "d.getElementsByTagName",
		"d.querySelector", "d.querySelectorAll", "d.createElement", "d.createTextNode",
		"d.createDocumentFragment", "d.appendChild", "d.removeChild", "d.insertBefore",
		"d.addEventListener", "d.removeEventListener", "d.dispatchEvent",
	})

	// Array constructor
	o.Set("E", []string{"Array"})

	// Special values
	o.Set("Infinity", []string{"Infinity"})
	o.Set("NaN", []string{"NaN"})

	// Navigator strings -- Chrome 144 macOS
	o.Set("Google Inc.", []string{"n.vendor"})
	o.Set("Mozilla", []string{"n.appCodeName"})
	o.Set("Netscape", []string{"n.appName"})
	o.Set("5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36", []string{"n.appVersion"})
	o.Set("MacIntel", []string{"n.platform"})
	o.Set("Gecko", []string{"n.product"})
	o.Set("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36", []string{"n.userAgent"})
	o.Set("en-US", []string{"n.language"})
	o.Set("en-US,en", []string{"n.languages"})

	// Document properties
	o.Set("about:blank", []string{"d.URL", "d.documentURI"})
	o.Set("BackCompat", []string{"d.compatMode"})
	o.Set("UTF-8", []string{"d.characterSet", "d.charset", "d.inputEncoding"})
	o.Set("text/html", []string{"d.contentType"})
	o.Set(host, []string{"d.domain"})
	o.Set(uri, []string{"d.referrer", "d.baseURI"})
	o.Set(cookie, []string{"d.cookie"})
	o.Set(lastModified, []string{"d.lastModified"})
	o.Set("complete", []string{"d.readyState"})
	o.Set("off", []string{"d.designMode"})
	o.Set("visible", []string{"d.visibilityState", "d.webkitVisibilityState"})
	o.Set("", []string{"d.adoptedStyleSheets"})
	o.Set("#document", []string{"d.nodeName"})

	return o
}
