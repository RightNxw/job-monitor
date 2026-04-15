//go:build solver

package cloudflare

// httpcloak TLS client wrapper for the Cloudflare solver. Uses a patched
// httpcloak fork for Chrome-identical TLS fingerprints. The public build's HTTP
// layer lives in internal/httpclient instead.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sardanioss/httpcloak"
)

// Navigation header order (Chrome 146 page load -- from real browser capture).
var navigationHeaderOrder = []string{
	"sec-ch-ua",
	"sec-ch-ua-mobile",
	"sec-ch-ua-platform",
	"upgrade-insecure-requests",
	"user-agent",
	"accept",
	"sec-fetch-site",
	"sec-fetch-mode",
	"sec-fetch-user",
	"sec-fetch-dest",
	"sec-fetch-storage-access",
	"accept-encoding",
	"accept-language",
	"cookie",
	"priority",
}

// XHR/CORS header order (Chrome 146 fetch/XHR -- from real browser Cloudflare capture).
// Includes cf-chl and cf-chl-ra headers used by managed challenge flow.
var xhrHeaderOrder = []string{
	"content-length",
	"sec-ch-ua-platform",
	"user-agent",
	"sec-ch-ua",
	"content-type",
	"cf-chl",
	"cf-chl-ra",
	"sec-ch-ua-mobile",
	"accept",
	"origin",
	"sec-fetch-site",
	"sec-fetch-mode",
	"sec-fetch-dest",
	"sec-fetch-storage-access",
	"referer",
	"accept-encoding",
	"accept-language",
	"cookie",
	"priority",
}

// httpClient is a browser-identical HTTP client using httpcloak for TLS/HTTP2 fingerprinting.
type httpClient struct {
	session   *httpcloak.Session
	preset    string
	userAgent string
	proxyURL  string
}

// DefaultPreset is the default browser preset.
const DefaultPreset = "chrome-146-macos"

// presetMap maps old profile names to httpcloak presets.
var presetMap = map[string]string{
	"chrome-133-macos":   "chrome-133",
	"chrome-133-windows": "chrome-133",
	"chrome-131-macos":   "chrome-133",
	"chrome-143":         "chrome-143-macos",
	"chrome-144":         "chrome-144-macos",
	"chrome-145":         "chrome-146-macos",
	"chrome-145-macos":   "chrome-146-macos",
	"chrome-146":         "chrome-146-macos",
	"safari-16":          "safari-18",
	"firefox-120":        "firefox-133",
}

// defaultUAs maps presets to user agent strings.
var defaultUAs = map[string]string{
	"chrome-133":       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36",
	"chrome-141":       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36",
	"chrome-143-macos": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36",
	"chrome-144-macos": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36",
	"chrome-146-macos": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
}

// newHTTPClient creates a new TLS client with the given profile, proxy, and user agent.
func newHTTPClient(profileName, proxyURL, userAgent string) (*httpClient, error) {
	// Map old profile names to httpcloak presets.
	preset := DefaultPreset
	if mapped, ok := presetMap[profileName]; ok {
		preset = mapped
	} else if profileName != "" {
		// Try using the name directly as a preset.
		preset = profileName
	}

	var opts []httpcloak.SessionOption
	opts = append(opts, httpcloak.WithSessionTimeout(60*time.Second))
	if proxyURL != "" {
		opts = append(opts, httpcloak.WithSessionProxy(proxyURL))
	}

	session := httpcloak.NewSession(preset, opts...)

	if userAgent == "" {
		if ua, ok := defaultUAs[preset]; ok {
			userAgent = ua
		} else {
			userAgent = defaultUAs[DefaultPreset]
		}
	}

	return &httpClient{
		session:   session,
		preset:    preset,
		userAgent: userAgent,
		proxyURL:  proxyURL,
	}, nil
}

// wrapHTTPClient wraps an existing httpcloak session into an httpClient.
// Used when the router's detection session is reused by the solver.
func wrapHTTPClient(session *httpcloak.Session, proxyURL, userAgent string) *httpClient {
	if userAgent == "" {
		userAgent = defaultUAs[DefaultPreset]
	}
	return &httpClient{
		session:   session,
		preset:    DefaultPreset,
		userAgent: userAgent,
		proxyURL:  proxyURL,
	}
}

// ProfileName returns the name used for the TLS profile.
func (c *httpClient) ProfileName() string {
	return c.preset
}

// UserAgent returns the client's user agent string.
func (c *httpClient) UserAgent() string {
	return c.userAgent
}

// ProxyURL returns the proxy URL used by this client.
func (c *httpClient) ProxyURL() string {
	return c.proxyURL
}

// Get performs an HTTP GET request with navigation header ordering.
func (c *httpClient) Get(ctx context.Context, rawURL string) (*http.Response, error) {
	c.session.SetHeaderOrder(navigationHeaderOrder)

	// Wrap in goroutine to enforce context deadline.
	// httpcloak's transport may not respect context cancellation on some systems.
	type result struct {
		resp *httpcloak.Response
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		r, e := c.session.Get(ctx, rawURL)
		ch <- result{r, e}
	}()

	select {
	case out := <-ch:
		if out.err != nil {
			return nil, out.err
		}
		return convertResponse(out.resp)
	case <-ctx.Done():
		return nil, fmt.Errorf("request cancelled: %w", ctx.Err())
	}
}

// Do executes an HTTP request with browser-identical TLS and header ordering.
func (c *httpClient) Do(req *http.Request) (*http.Response, error) {
	// Determine header order based on request type.
	mode := req.Header.Get("Sec-Fetch-Mode")
	if mode == "cors" || mode == "no-cors" || mode == "same-origin" {
		c.session.SetHeaderOrder(xhrHeaderOrder)
	} else {
		c.session.SetHeaderOrder(navigationHeaderOrder)
	}

	// Convert net/http headers to httpcloak format (lowercase keys).
	headers := make(map[string][]string)
	for k, v := range req.Header {
		headers[strings.ToLower(k)] = v
	}

	// Ensure Content-Length is in the headers map (Go stores it in
	// req.ContentLength, not req.Header, but browsers send it as a header).
	if _, ok := headers["content-length"]; !ok && req.ContentLength > 0 {
		headers["content-length"] = []string{fmt.Sprintf("%d", req.ContentLength)}
	}

	// Inject browser-default headers that a real browser always sends
	// but JS XHR/fetch don't explicitly set.
	c.ensureBrowserHeaders(headers)

	// Attach cookies from the session jar.
	if _, ok := headers["cookie"]; !ok {
		if cookies := c.session.GetCookies(); len(cookies) > 0 {
			var parts []string
			for name, val := range cookies {
				parts = append(parts, name+"="+val)
			}
			headers["cookie"] = []string{strings.Join(parts, "; ")}
		}
	}

	cloakReq := &httpcloak.Request{
		Method:  req.Method,
		URL:     req.URL.String(),
		Headers: headers,
		Body:    req.Body,
	}

	resp, err := c.session.Do(req.Context(), cloakReq)
	if err != nil {
		return nil, err
	}
	return convertResponse(resp)
}

// ensureBrowserHeaders adds headers that a real browser always includes
// but that JS code (XHR/fetch) doesn't explicitly set.
func (c *httpClient) ensureBrowserHeaders(headers map[string][]string) {
	if _, ok := headers["user-agent"]; !ok {
		headers["user-agent"] = []string{c.userAgent}
	}
	if _, ok := headers["sec-ch-ua"]; !ok {
		if v := c.secChUA(); v != "" {
			headers["sec-ch-ua"] = []string{v}
		}
	}
	if _, ok := headers["sec-ch-ua-mobile"]; !ok {
		headers["sec-ch-ua-mobile"] = []string{"?0"}
	}
	if _, ok := headers["sec-ch-ua-platform"]; !ok {
		headers["sec-ch-ua-platform"] = []string{`"macOS"`}
	}
	if _, ok := headers["accept-language"]; !ok {
		headers["accept-language"] = []string{"en-US,en;q=0.9"}
	}
	if _, ok := headers["accept-encoding"]; !ok {
		headers["accept-encoding"] = []string{"gzip, deflate, br, zstd"}
	}
	// Chrome sends Priority header on XHR/fetch requests.
	if _, ok := headers["priority"]; !ok {
		headers["priority"] = []string{"u=1, i"}
	}
}

// secChUA returns the sec-ch-ua header value for the current preset.
func (c *httpClient) secChUA() string {
	secChUAs := map[string]string{
		"chrome-133":       `"Google Chrome";v="133", "Chromium";v="133", "Not_A Brand";v="24"`,
		"chrome-141":       `"Google Chrome";v="141", "Chromium";v="141", "Not:A-Brand";v="24"`,
		"chrome-143-macos": `"Google Chrome";v="143", "Chromium";v="143", "Not A(Brand";v="24"`,
		"chrome-144-macos": `"Not(A:Brand";v="8", "Chromium";v="144", "Google Chrome";v="144"`,
		"chrome-146-macos": `"Chromium";v="146", "Not-A.Brand";v="24", "Google Chrome";v="146"`,
	}
	if v, ok := secChUAs[c.preset]; ok {
		return v
	}
	return ""
}

// GetCookieValue returns the value of a named cookie.
func (c *httpClient) GetCookieValue(rawURL, name string) (string, bool) {
	cookies := c.session.GetCookies()
	if val, ok := cookies[name]; ok {
		return val, true
	}
	return "", false
}

// AllCookies returns all cookies as a map.
func (c *httpClient) AllCookies(rawURL string) map[string]string {
	return c.session.GetCookies()
}

// Close releases resources held by the client.
func (c *httpClient) Close() {
	c.session.Close()
}

// convertResponse wraps an httpcloak.Response into a standard *http.Response
// so the solver doesn't need to change its response handling code.
func convertResponse(resp *httpcloak.Response) (*http.Response, error) {
	// Convert lowercase httpcloak headers to canonical http.Header.
	httpHeader := make(http.Header)
	for k, v := range resp.Headers {
		httpHeader[http.CanonicalHeaderKey(k)] = v
	}

	// Read body into memory so we can create a proper ReadCloser.
	bodyBytes, err := resp.Bytes()
	if err != nil {
		return nil, err
	}

	return &http.Response{
		StatusCode: resp.StatusCode,
		Header:     httpHeader,
		Body:       io.NopCloser(strings.NewReader(string(bodyBytes))),
	}, nil
}
