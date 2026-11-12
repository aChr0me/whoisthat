package crypt5

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestParseMarker(t *testing.T) {
	ok := map[string]string{
		"happ://crypt5/AbCd-_09":         "AbCd-_09",
		"  happ://crypt5/AbCd\n":         "AbCd", // providers append newlines
		"happ://crypt5/a/b+c=":           "a/b+c=",
		"HAPP://CRYPT5/Zm9v":             "Zm9v", // scheme and host are case-insensitive
		"happ://crypt5/Zm9v?utm=ignored": "Zm9v",
	}
	for in, want := range ok {
		got, err := ParseMarker(in)
		if err != nil || got != want {
			t.Errorf("ParseMarker(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"", "happ://crypt5/", "happ://crypt5", "happ://routing/add/xyz",
		"https://crypt5/abc", "crypt5/abc", "happ://crypt4/abc", "happ://crypt5/ab cd",
	} {
		if _, err := ParseMarker(bad); !errors.Is(err, ErrNotMarker) {
			t.Errorf("ParseMarker(%q) err = %v, want ErrNotMarker", bad, err)
		}
	}
}

// scripted is a Doer answering from a fixed URL -> response table, no network.
type scripted struct {
	routes map[string]*http.Response
	calls  []string
}

func (s *scripted) Do(req *http.Request) (*http.Response, error) {
	s.calls = append(s.calls, req.URL.String())
	if r, ok := s.routes[req.URL.String()]; ok {
		return r, nil
	}
	return resp(404, "", ""), nil
}

func resp(code int, location, body string) *http.Response {
	h := http.Header{}
	if location != "" {
		h.Set("Location", location)
	}
	return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

func TestResolveMarkerFollowsRedirectChain(t *testing.T) {
	d := &scripted{routes: map[string]*http.Response{
		"https://panel.example/happy-link/abc": resp(302, "/hop/2", ""),
		"https://panel.example/hop/2":          resp(301, "https://cdn.example/hop/3", ""),
		"https://cdn.example/hop/3":            resp(302, "happ://crypt5/Zm9vYmFy", ""),
	}}
	got, err := ResolveMarker(context.Background(), d, "https://panel.example/happy-link/abc", 5)
	if err != nil || got != "Zm9vYmFy" {
		t.Fatalf("ResolveMarker = %q, %v", got, err)
	}
	if len(d.calls) != 3 {
		t.Fatalf("calls = %v, want 3 hops (relative Location resolved against the hop URL)", d.calls)
	}
}

func TestResolveMarkerRefusesCleartextHop(t *testing.T) {
	d := &scripted{routes: map[string]*http.Response{
		"https://panel.example/happy-link/abc": resp(302, "http://panel.example/plain", ""),
	}}
	if _, err := ResolveMarker(context.Background(), d, "https://panel.example/happy-link/abc", 5); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("http hop must be refused, got %v", err)
	}
	if _, err := ResolveMarker(context.Background(), d, "http://panel.example/happy-link/abc", 5); err == nil {
		t.Fatal("http start URL must be refused")
	}
}

func TestResolveMarkerBoundsChainAndRejectsLoops(t *testing.T) {
	d := &scripted{routes: map[string]*http.Response{
		"https://a.example/x": resp(302, "https://a.example/x", ""),
	}}
	if _, err := ResolveMarker(context.Background(), d, "https://a.example/x", 3); !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("loop must hit the hop limit, got %v", err)
	}
	if len(d.calls) > 3 {
		t.Fatalf("made %d calls with maxHops=3", len(d.calls))
	}
}

func TestResolveMarkerErrorsWithoutMarker(t *testing.T) {
	cases := map[string]*http.Response{
		"final 200 JSON, no marker": resp(200, "", `{"outbounds":[]}`),
		"redirect to other scheme":  resp(302, "ftp://x/y", ""),
		"redirect w/o Location":     resp(302, "", ""),
		"server error":              resp(500, "", "boom"),
		"junk happ link":            resp(302, "happ://routing/add/zzz", ""),
	}
	for name, r := range cases {
		d := &scripted{routes: map[string]*http.Response{"https://p.example/l": r}}
		if _, err := ResolveMarker(context.Background(), d, "https://p.example/l", 5); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestCheckBodyRejectsNonConfig(t *testing.T) {
	good := `{"outbounds":[{"protocol":"vless","tag":"proxy","settings":{"vnext":[{"address":"1.2.3.4","port":443,"users":[{"id":"00000000-0000-0000-0000-000000000000"}]}]}}]}`
	if err := CheckBody([]byte(good)); err != nil {
		t.Fatalf("valid config body rejected: %v", err)
	}
	for name, body := range map[string]string{
		"html error page": "<html><body>Forbidden</body></html>",
		"plain text":      "device limit reached",
		"empty":           "",
		"json non-config": `{"error":"hwid"}`,
		"truncated json":  `{"outbounds":[`,
	} {
		if err := CheckBody([]byte(body)); !errors.Is(err, ErrNotConfig) {
			t.Errorf("%s: err = %v, want ErrNotConfig", name, err)
		}
	}
}
