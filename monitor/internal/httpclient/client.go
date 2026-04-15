// Package httpclient wraps every outbound request the scrapers make.
//
// It uses httpcloak for TLS fingerprint control, which most job boards check
// before they'll serve a real response. This builds against the public
// httpcloak.
//
// If you'd rather not depend on httpcloak at all, try swapping this package
// for github.com/bogdanfinn/tls-client. It covers the same ground (JA3/JA4
// profiles, HTTP/2 frame ordering, header order) and is the more common public
// choice. The surface this package exposes is small: Get, GetWithHeaders, and
// the Response alias. Point those at tls-client and see if the scrapers still
// come back with real data; if a source starts returning blocks, the preset
// below is the first thing to tune.
package httpclient

import (
	"context"
	"time"

	"github.com/sardanioss/httpcloak"
)

const defaultPreset = "chrome-143"

// Response is an alias for httpcloak.Response so callers don't need to import httpcloak.
type Response = httpcloak.Response

// Get performs an HTTP GET with httpcloak and an optional proxy.
func Get(ctx context.Context, url, proxyURL string) (*httpcloak.Response, error) {
	var opts []httpcloak.Option
	opts = append(opts, httpcloak.WithTimeout(30*time.Second))
	if proxyURL != "" {
		opts = append(opts, httpcloak.WithProxy(proxyURL))
	}

	client := httpcloak.New(defaultPreset, opts...)
	defer client.Close()

	return client.Get(ctx, url)
}

// GetJSON performs an HTTP GET with Accept: application/json header.
// Needed for APIs (like Lever public) that return HTML to browser-like Accept headers.
func GetJSON(ctx context.Context, url, proxyURL string) (*httpcloak.Response, error) {
	var opts []httpcloak.Option
	opts = append(opts, httpcloak.WithTimeout(30*time.Second))
	if proxyURL != "" {
		opts = append(opts, httpcloak.WithProxy(proxyURL))
	}

	client := httpcloak.New(defaultPreset, opts...)
	defer client.Close()

	headers := map[string][]string{
		"accept": {"application/json"},
	}
	return client.GetWithHeaders(ctx, url, headers)
}

// GetWithHeaders performs an HTTP GET with custom headers and an optional proxy.
func GetWithHeaders(ctx context.Context, url, proxyURL string, headers map[string][]string) (*httpcloak.Response, error) {
	var opts []httpcloak.Option
	opts = append(opts, httpcloak.WithTimeout(30*time.Second))
	if proxyURL != "" {
		opts = append(opts, httpcloak.WithProxy(proxyURL))
	}

	client := httpcloak.New(defaultPreset, opts...)
	defer client.Close()

	return client.GetWithHeaders(ctx, url, headers)
}
