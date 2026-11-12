// Package crypt5 holds the format-independent half of the crypt5 subscription
// fetcher: recognising the happ://crypt5/<payload> marker, following the
// happy-link redirect chain to it over https, and refusing non-config bodies.
//
// Deliberately NOT here: the AEAD unwrap and its key table. The byte layout of
// the payload and the keys are not in this repository and must come from a
// confirmed capture, not from guesswork.
package crypt5

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"whoisthat-core/lib/xrayjson"
)

var (
	ErrNotMarker        = errors.New("crypt5: not a happ://crypt5/ marker")
	ErrTooManyRedirects = errors.New("crypt5: too many redirects")
	ErrNotConfig        = errors.New("crypt5: body is not a JSON config")
)

// ParseMarker extracts the payload from happ://crypt5/<payload>. Scheme and
// host are case-insensitive; surrounding whitespace and a query string are
// dropped; the payload must be a single non-empty token.
func ParseMarker(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	const prefix = "happ://crypt5/"
	if len(s) <= len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return "", ErrNotMarker
	}
	payload := s[len(prefix):]
	if i := strings.IndexAny(payload, "?#"); i >= 0 {
		payload = payload[:i]
	}
	if payload == "" || strings.ContainsAny(payload, " \t\r\n") {
		return "", ErrNotMarker
	}
	return payload, nil
}

// Doer is the slice of http.Client the resolver needs. Real callers must use a
// client that does not follow redirects itself (CheckRedirect returning
// http.ErrUseLastResponse): the chain ends in a non-http(s) Location that
// net/http cannot follow, and every hop has to be vetted here.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// ResolveMarker GETs start and follows 3xx hops (at most maxHops requests)
// until a Location is a crypt5 marker, returning its payload. Every requested
// URL must be https.
func ResolveMarker(ctx context.Context, d Doer, start string, maxHops int) (string, error) {
	cur, err := url.Parse(start)
	if err != nil {
		return "", fmt.Errorf("crypt5: bad start URL: %w", err)
	}
	for hop := 0; hop < maxHops; hop++ {
		if cur.Scheme != "https" || cur.Host == "" {
			return "", fmt.Errorf("crypt5: refusing non-https hop %q (https required)", cur.Redacted())
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cur.String(), nil)
		if err != nil {
			return "", err
		}
		resp, err := d.Do(req)
		if err != nil {
			return "", err
		}
		loc := resp.Header.Get("Location")
		code := resp.StatusCode
		resp.Body.Close()

		if code < 300 || code > 399 {
			return "", fmt.Errorf("crypt5: HTTP %d at hop %d, no marker in the redirect chain", code, hop+1)
		}
		if loc == "" {
			return "", fmt.Errorf("crypt5: HTTP %d without Location at hop %d", code, hop+1)
		}
		if payload, err := ParseMarker(loc); err == nil {
			return payload, nil
		}
		next, err := cur.Parse(loc) // resolves relative Locations against this hop
		if err != nil {
			return "", fmt.Errorf("crypt5: bad Location at hop %d: %w", hop+1, err)
		}
		if next.Scheme != "https" && next.Scheme != "http" {
			return "", fmt.Errorf("crypt5: redirect to unsupported scheme %q", next.Scheme)
		}
		cur = next
	}
	return "", ErrTooManyRedirects
}

// CheckBody accepts only a body the existing import path can use. HTML error
// pages, plain-text refusals, truncated JSON and unrelated JSON are rejected.
func CheckBody(body []byte) error {
	if !xrayjson.IsConfigBody(body) {
		return ErrNotConfig
	}
	return nil
}
