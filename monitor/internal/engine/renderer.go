//go:build solver

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// RenderRequest holds the parameters for a V8 content render.
type RenderRequest struct {
	URL     string            `json:"url"`
	HTML    string            `json:"html"`               // pre-fetched HTML (skip initial fetch)
	Cookies map[string]string `json:"cookies,omitempty"`  // cookies for script fetches
	Proxy   string            `json:"proxy,omitempty"`    // proxy for fetches
	Timeout int               `json:"timeout,omitempty"`  // max render time in ms
	WaitFor string            `json:"wait_for,omitempty"` // CSS selector to wait for
}

// RenderResult holds the output of a V8 content render.
type RenderResult struct {
	Success       bool     `json:"success"`
	HTML          string   `json:"html"`
	Text          string   `json:"text"`
	DurationMs    int64    `json:"duration_ms"`
	ScriptsLoaded int      `json:"scripts_loaded"`
	Errors        []string `json:"errors"`
}

// scriptInfo represents a parsed <script> element from HTML.
type scriptInfo struct {
	Src    string
	Code   string // inline script content
	Defer  bool
	Async  bool
	Module bool
	Type   string
}

// pageContent holds the parsed parts of an HTML page.
type pageContent struct {
	BodyHTML   string       // body innerHTML (with scripts stripped)
	BodyAttrs  string       // body tag attributes
	HeadHTML   string       // head innerHTML
	Scripts    []scriptInfo // all executable scripts in order
	InlineJSON []string     // JSON-LD, __NEXT_DATA__, etc.
}

// parsePageContent uses golang.org/x/net/html to properly split the page
// into head, body (without scripts), and script list.
func parsePageContent(htmlStr string) *pageContent {
	pc := &pageContent{}

	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		// Fallback: treat entire HTML as body
		pc.BodyHTML = htmlStr
		return pc
	}

	// Find <head> and <body> nodes
	var headNode, bodyNode *html.Node
	var findNodes func(*html.Node)
	findNodes = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "head" && headNode == nil {
				headNode = n
			}
			if n.Data == "body" && bodyNode == nil {
				bodyNode = n
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findNodes(c)
		}
	}
	findNodes(doc)

	// Render head content (excluding scripts, they're handled separately)
	if headNode != nil {
		var headBuf bytes.Buffer
		for c := headNode.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "script" {
				// Extract script info
				s := extractScriptInfo(c)
				if s != nil {
					pc.Scripts = append(pc.Scripts, *s)
				}
				// Also extract JSON-LD
				if isJSONScript(c) {
					pc.InlineJSON = append(pc.InlineJSON, getScriptText(c))
				}
			} else {
				html.Render(&headBuf, c)
			}
		}
		pc.HeadHTML = headBuf.String()
	}

	// Render body content (scripts extracted separately)
	if bodyNode != nil {
		// Capture body attributes
		var attrParts []string
		for _, a := range bodyNode.Attr {
			attrParts = append(attrParts, fmt.Sprintf(`%s="%s"`, a.Key, html.EscapeString(a.Val)))
		}
		pc.BodyAttrs = strings.Join(attrParts, " ")

		var bodyBuf bytes.Buffer
		for c := bodyNode.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "script" {
				s := extractScriptInfo(c)
				if s != nil {
					pc.Scripts = append(pc.Scripts, *s)
				}
				if isJSONScript(c) {
					pc.InlineJSON = append(pc.InlineJSON, getScriptText(c))
				}
			} else {
				html.Render(&bodyBuf, c)
			}
		}
		pc.BodyHTML = bodyBuf.String()
	}

	return pc
}

func extractScriptInfo(n *html.Node) *scriptInfo {
	s := &scriptInfo{}
	for _, a := range n.Attr {
		switch a.Key {
		case "src":
			s.Src = a.Val
		case "defer":
			s.Defer = true
		case "async":
			s.Async = true
		case "type":
			s.Type = a.Val
			if a.Val == "module" {
				s.Module = true
			}
		}
	}
	s.Code = getScriptText(n)
	// Only return executable scripts
	if s.Type == "" || s.Type == "text/javascript" || s.Type == "application/javascript" || s.Type == "module" {
		return s
	}
	return nil
}

func isJSONScript(n *html.Node) bool {
	for _, a := range n.Attr {
		if a.Key == "type" && (a.Val == "application/ld+json" || a.Val == "application/json") {
			return true
		}
		if a.Key == "id" && (a.Val == "__NEXT_DATA__" || a.Val == "__NUXT_DATA__") {
			return true
		}
	}
	return false
}

func getScriptText(n *html.Node) string {
	var buf bytes.Buffer
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			buf.WriteString(c.Data)
		}
	}
	return buf.String()
}

// Render executes a V8 content render: parses HTML, loads scripts, runs them,
// waits for content to stabilize, and returns the rendered HTML/text.
func Render(ctx context.Context, req *RenderRequest, fetchFn FetchFunc) (*RenderResult, error) {
	start := time.Now()
	result := &RenderResult{Errors: []string{}}

	// Determine timeout
	timeout := 15 * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Fetch HTML if not provided
	htmlStr := req.HTML
	if htmlStr == "" && req.URL != "" {
		body, status, _, err := fetchFn(req.URL, nil)
		if err != nil {
			return nil, fmt.Errorf("fetch HTML: %w", err)
		}
		if status < 200 || status >= 400 {
			return nil, fmt.Errorf("fetch HTML: status %d", status)
		}
		htmlStr = body
	}
	if htmlStr == "" {
		return nil, fmt.Errorf("no HTML content")
	}

	// Parse the page into head/body/scripts
	pc := parsePageContent(htmlStr)

	// Create V8 engine
	userAgent := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"
	domCfg := DefaultDOMConfig(userAgent, req.URL)
	if req.Cookies != nil {
		domCfg.Cookies = req.Cookies
	}

	eng, err := NewEngine(domCfg, false)
	if err != nil {
		return nil, fmt.Errorf("create V8 engine: %w", err)
	}
	defer eng.Close()

	// Set up fetch handler for script loading
	eng.SetFetchHandler(fetchFn)

	// Set up global error handler
	eng.ExecuteScript(ctx, `
		var __renderErrors = [];
		window.onerror = function(msg, src, line, col, err) {
			__renderErrors.push(String(msg) + (src ? ' at ' + src + ':' + line : ''));
			return true;
		};
		window.addEventListener('unhandledrejection', function(e) {
			__renderErrors.push('Unhandled promise: ' + String(e.reason || e));
			e.preventDefault();
		});
	`, "error-handler.js")

	// Set desktop viewport dimensions (sites use these for responsive layouts)
	eng.ExecuteScript(ctx, `
		try {
			window.innerWidth = 1920;
			window.innerHeight = 1080;
			window.outerWidth = 1920;
			window.outerHeight = 1080;
			if (window.screen) {
				window.screen.width = 1920;
				window.screen.height = 1080;
				window.screen.availWidth = 1920;
				window.screen.availHeight = 1080;
			}
			if (window.visualViewport) {
				window.visualViewport.width = 1920;
				window.visualViewport.height = 1080;
			}
		} catch(e) {}
	`, "viewport.js")

	// Set document.readyState to 'loading' (scripts check this)
	eng.ExecuteScript(ctx, `
		try {
			Object.defineProperty(document, 'readyState', {
				value: 'loading', writable: true, configurable: true
			});
		} catch(e) {}
	`, "readystate-loading.js")

	// Inject head content (meta tags, title, links - no scripts)
	if pc.HeadHTML != "" {
		headScript := fmt.Sprintf(`
			(function() {
				try {
					var headHTML = %s;
					document.head.innerHTML = headHTML;
				} catch(e) {
					console.log('[RENDER] head innerHTML error: ' + e.message);
				}
			})();
		`, jsStringLiteral(pc.HeadHTML))
		eng.ExecuteScript(ctx, headScript, "render-head.js")
	}

	// Inject body content (with scripts stripped out)
	if pc.BodyHTML != "" {
		// Also set body attributes
		bodyScript := fmt.Sprintf(`
			(function() {
				try {
					var bodyHTML = %s;
					document.body.innerHTML = bodyHTML;
					// Set body attributes
					var bodyAttrs = %s;
					if (bodyAttrs) {
						var pairs = bodyAttrs.match(/(\w[\w-]*)="([^"]*)"/g) || [];
						for (var i = 0; i < pairs.length; i++) {
							var eq = pairs[i].indexOf('=');
							if (eq > 0) {
								var k = pairs[i].substring(0, eq);
								var v = pairs[i].substring(eq+2, pairs[i].length-1);
								document.body.setAttribute(k, v);
								if (k === 'id') document.body.id = v;
								if (k === 'class') document.body.className = v;
							}
						}
					}
				} catch(e) {
					console.log('[RENDER] body innerHTML error: ' + e.message);
				}
			})();
		`, jsStringLiteral(pc.BodyHTML), jsStringLiteral(pc.BodyAttrs))
		eng.ExecuteScript(ctx, bodyScript, "render-body.js")
	}

	// Classify scripts by timing
	var syncScripts, deferScripts, asyncScripts []scriptInfo
	for _, s := range pc.Scripts {
		if s.Async {
			asyncScripts = append(asyncScripts, s)
		} else if s.Defer {
			deferScripts = append(deferScripts, s)
		} else {
			syncScripts = append(syncScripts, s)
		}
	}

	scriptsLoaded := 0

	// Execute sync scripts (blocking, in order)
	for _, s := range syncScripts {
		select {
		case <-ctx.Done():
			result.Errors = append(result.Errors, "timeout during sync scripts")
			goto done
		default:
		}
		code := s.Code
		if s.Src != "" {
			body, status, _, err := fetchFn(resolveURL(req.URL, s.Src), nil)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("fetch script %s: %v", s.Src, err))
				continue
			}
			if status < 200 || status >= 400 {
				continue
			}
			code = body
		}
		if code != "" {
			_, err := eng.ExecuteScript(ctx, code, s.Src)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("script error (%s): %v", s.Src, err))
			}
			scriptsLoaded++
		}
	}

	// Set readyState to 'interactive' before defer scripts
	eng.ExecuteScript(ctx, `
		try { document.readyState = 'interactive'; } catch(e) {}
	`, "readystate-interactive.js")

	// Execute defer scripts
	for _, s := range deferScripts {
		select {
		case <-ctx.Done():
			result.Errors = append(result.Errors, "timeout during defer scripts")
			goto done
		default:
		}
		code := s.Code
		if s.Src != "" {
			body, status, _, err := fetchFn(resolveURL(req.URL, s.Src), nil)
			if err != nil {
				continue
			}
			if status < 200 || status >= 400 {
				continue
			}
			code = body
		}
		if code != "" {
			_, err := eng.ExecuteScript(ctx, code, s.Src)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("script error (%s): %v", s.Src, err))
			}
			scriptsLoaded++
		}
	}

	// Fire DOMContentLoaded
	eng.ExecuteScript(ctx, `
		try {
			document.readyState = 'interactive';
			var evt = new Event('DOMContentLoaded', {bubbles: true, cancelable: false});
			document.dispatchEvent(evt);
		} catch(e) {}
	`, "dom-content-loaded.js")

	// Execute async scripts
	for _, s := range asyncScripts {
		select {
		case <-ctx.Done():
			break
		default:
		}
		code := s.Code
		if s.Src != "" {
			body, status, _, err := fetchFn(resolveURL(req.URL, s.Src), nil)
			if err != nil || status < 200 || status >= 400 {
				continue
			}
			code = body
		}
		if code != "" {
			eng.ExecuteScript(ctx, code, s.Src)
			scriptsLoaded++
		}
	}

	// Set readyState to 'complete' and fire load
	eng.ExecuteScript(ctx, `
		try {
			document.readyState = 'complete';
			window.dispatchEvent(new Event('load', {bubbles: false}));
		} catch(e) {}
	`, "window-load.js")

	// Simulate scroll to trigger lazy-loaded content (IntersectionObserver, scroll handlers)
	eng.ExecuteScript(ctx, `
		try {
			// Fire scroll events at different positions to trigger lazy loading
			for (var scrollY = 0; scrollY <= 3000; scrollY += 500) {
				window.scrollY = scrollY;
				window.pageYOffset = scrollY;
				document.documentElement.scrollTop = scrollY;
				document.body.scrollTop = scrollY;
				window.dispatchEvent(new Event('scroll', {bubbles: true}));
				document.dispatchEvent(new Event('scroll', {bubbles: true}));
			}
			// Reset to top
			window.scrollY = 0;
			window.pageYOffset = 0;
		} catch(e) {}
	`, "scroll-simulate.js")

	// Small delay for scroll-triggered content to render
	time.Sleep(200 * time.Millisecond)

	// Content stabilization: poll text length until stable
	{
		// textContent that excludes script elements
		stabilizationJS := `
			(function() {
				if (!document.body) return 0;
				var text = '';
				function walk(n) {
					if (n.tagName === 'SCRIPT' || n.tagName === 'STYLE') return;
					if (n.nodeType === 3) text += n.textContent || n.data || '';
					var kids = n.childNodes || [];
					for (var i = 0; i < kids.length; i++) walk(kids[i]);
				}
				walk(document.body);
				return text.length;
			})();
		`
		lastLen := 0
		stableCount := 0
		maxPolls := 30
		for i := 0; i < maxPolls; i++ {
			select {
			case <-ctx.Done():
				goto done
			default:
			}
			val, err := eng.ExecuteScript(ctx, stabilizationJS, "stabilize.js")
			if err != nil {
				break
			}
			currentLen := int(val.Int32())
			if currentLen == lastLen {
				stableCount++
				if stableCount >= 3 {
					break
				}
			} else {
				stableCount = 0
				lastLen = currentLen
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Wait for specific selector if requested
	if req.WaitFor != "" {
		waitJS := fmt.Sprintf(`document.querySelector(%s) !== null`, jsStringLiteral(req.WaitFor))
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				goto done
			default:
			}
			val, err := eng.ExecuteScript(ctx, waitJS, "wait-for.js")
			if err == nil && val.Boolean() {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

done:
	// Extract rendered content, filter out script/style text
	htmlVal, err := eng.ExecuteScript(ctx, `document.body ? document.body.innerHTML : ''`, "get-html.js")
	if err == nil {
		result.HTML = htmlVal.String()
	}

	// Get visible text (excluding script/style elements) PLUS embedded structured data
	textVal, err := eng.ExecuteScript(ctx, `
		(function() {
			if (!document.body) return '';
			var text = '';
			function walk(n) {
				if (n.tagName === 'SCRIPT' || n.tagName === 'STYLE' || n.tagName === 'NOSCRIPT') return;
				if (n.nodeType === 3) {
					var t = n.textContent || n.data || '';
					if (t.trim()) text += t;
				}
				if (n.nodeType === 1 && (n.tagName === 'BR' || n.tagName === 'P' || n.tagName === 'DIV' || n.tagName === 'H1' || n.tagName === 'H2' || n.tagName === 'H3' || n.tagName === 'H4' || n.tagName === 'H5' || n.tagName === 'H6' || n.tagName === 'LI' || n.tagName === 'TR')) text += '\n';
				var kids = n.childNodes || [];
				for (var i = 0; i < kids.length; i++) walk(kids[i]);
			}
			walk(document.body);

			// Extract structured data from embedded JSON sources
			text += '\n';
			try {
				// 1. adobeDataLayer (AEM sites, Dollar General, etc.)
				if (typeof adobeDataLayer !== 'undefined' && Array.isArray(adobeDataLayer)) {
					for (var i = 0; i < adobeDataLayer.length; i++) {
						var entry = adobeDataLayer[i];
						if (!entry || typeof entry !== 'object') continue;
						var flat = JSON.stringify(entry);
						if (flat.indexOf('finalPrice') !== -1 || flat.indexOf('productName') !== -1 || flat.indexOf('price') !== -1) {
							text += '\n[Product Data] ' + flat.substring(0, 5000);
						}
					}
				}
				// 2. Next.js __NEXT_DATA__
				if (typeof __NEXT_DATA__ !== 'undefined' && __NEXT_DATA__ && __NEXT_DATA__.props) {
					var nd = JSON.stringify(__NEXT_DATA__.props);
					if (nd.length > 100) {
						text += '\n[Next.js Data] ' + nd.substring(0, 10000);
					}
				}
				// 3. data-cmp-data-layer attributes (AEM components embed JSON here)
				var layerEls = document.querySelectorAll('[data-cmp-data-layer]');
				for (var i = 0; i < layerEls.length; i++) {
					try {
						var attr = layerEls[i].getAttribute('data-cmp-data-layer');
						if (attr && (attr.indexOf('price') !== -1 || attr.indexOf('Price') !== -1 || attr.indexOf('product') !== -1)) {
							var decoded = attr.replace(/&quot;/g, '"').replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&#34;/g, '"').replace(/&#39;/g, "'");
							text += '\n[Component Data] ' + decoded.substring(0, 5000);
						}
					} catch(e2) {}
				}
				// 4. Extract from <script type="application/ld+json"> (JSON-LD)
				var ldScripts = document.querySelectorAll('script[type="application/ld+json"]');
				for (var i = 0; i < ldScripts.length; i++) {
					try {
						var ldText = ldScripts[i].textContent || '';
						if (ldText.trim()) text += '\n[JSON-LD] ' + ldText.trim().substring(0, 3000);
					} catch(e3) {}
				}
				// 5. Scan all elements for data attributes containing product/price info
				var allEls = document.querySelectorAll('*');
				for (var ei = 0; ei < allEls.length; ei++) {
					var el = allEls[ei];
					if (!el || !el.attributes) continue;
					var akeys = Object.keys(el.attributes);
					for (var ak = 0; ak < akeys.length; ak++) {
						var aname = akeys[ak];
						if (typeof aname !== 'string') continue;
						if (typeof el.attributes[aname] !== 'string') continue;
						var aval = el.attributes[aname];
						// Only look at data-* attributes with substantial content
						if (aname.indexOf('data-') !== 0 || aval.length < 50) continue;
						// Decode HTML entities
						var decoded = aval.replace(/&#34;/g, '"').replace(/&#39;/g, "'").replace(/&quot;/g, '"').replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>');
						// Check if it contains product/price info
						if (decoded.indexOf('price') !== -1 || decoded.indexOf('Price') !== -1 || decoded.indexOf('product') !== -1) {
							try {
								var parsed = JSON.parse(decoded);
								// Format key product fields as readable text
								function extractProduct(obj) {
									if (!obj || typeof obj !== 'object') return '';
									var lines = [];
									var pd = obj.productDetails || obj;
									if (pd.description) lines.push('Product: ' + pd.description);
									if (pd.brand) lines.push('Brand: ' + pd.brand);
									if (pd.longDescription) lines.push('Description: ' + pd.longDescription);
									if (pd.finalPrice !== undefined) lines.push('Price: $' + Number(pd.finalPrice).toFixed(2));
									if (pd.originalPrice !== undefined && pd.originalPrice !== pd.finalPrice) lines.push('Original Price: $' + Number(pd.originalPrice).toFixed(2));
									if (pd.rating !== undefined) lines.push('Rating: ' + pd.rating + '/5');
									if (pd.ratingReviewCount !== undefined) lines.push('Reviews: ' + pd.ratingReviewCount);
									if (pd.upc) lines.push('UPC: ' + pd.upc);
									if (pd.sku) lines.push('SKU: ' + pd.sku);
									if (pd.unitSize) lines.push('Size: ' + pd.unitSize + (pd.unitOfMeasure ? ' ' + pd.unitOfMeasure : ''));
									if (pd.categoryHierarchies) lines.push('Category: ' + (Array.isArray(pd.categoryHierarchies) ? pd.categoryHierarchies.join(' > ') : pd.categoryHierarchies));
									return lines.join('\n');
								}
								var readable = extractProduct(parsed);
								if (readable) text += '\n' + readable;
								text += '\n[Product Data] ' + JSON.stringify(parsed).substring(0, 8000);
							} catch(pe) {
								text += '\n[Product Attr] ' + aname + ': ' + decoded.substring(0, 5000);
							}
						}
					}
				}
			} catch(e) {}
			return text;
		})();
	`, "get-text.js")
	if err == nil {
		result.Text = textVal.String()
	}

	// Collect JS-side errors
	errVal, err := eng.ExecuteScript(ctx, `JSON.stringify(typeof __renderErrors !== 'undefined' ? __renderErrors : [])`, "get-errors.js")
	if err == nil {
		var jsErrors []string
		if json.Unmarshal([]byte(errVal.String()), &jsErrors) == nil {
			result.Errors = append(result.Errors, jsErrors...)
		}
	}

	result.Success = len(result.HTML) > 0
	result.ScriptsLoaded = scriptsLoaded
	result.DurationMs = time.Since(start).Milliseconds()

	return result, nil
}

// resolveURL resolves a relative URL against a base URL.
func resolveURL(base, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "//") {
		if strings.HasPrefix(ref, "//") {
			if strings.HasPrefix(base, "https://") {
				return "https:" + ref
			}
			return "http:" + ref
		}
		return ref
	}
	if base == "" {
		return ref
	}
	idx := strings.Index(base, "://")
	if idx == -1 {
		return ref
	}
	afterScheme := base[idx+3:]
	pathStart := strings.Index(afterScheme, "/")
	if pathStart == -1 {
		return base + "/" + ref
	}
	origin := base[:idx+3+pathStart]
	if strings.HasPrefix(ref, "/") {
		return origin + ref
	}
	lastSlash := strings.LastIndex(base, "/")
	if lastSlash > idx+3 {
		return base[:lastSlash+1] + ref
	}
	return origin + "/" + ref
}

// jsStringLiteral wraps a string as a JS template literal (with proper escaping).
func jsStringLiteral(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "$", "\\$")
	return "`" + s + "`"
}
