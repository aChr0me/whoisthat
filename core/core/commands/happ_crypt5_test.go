package cmd

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"whoisthat-core/lib/crypt5"
	"whoisthat-core/lib/crypt5/crypt5test"
	"whoisthat-core/structs"
)

const testHwid = "0123456789abcdef"

// happPanel is a hermetic stand-in for a crypt5 panel: /happy-link/<id> answers
// 302 -> happ://crypt5/<payload>, /happy-accy/<id> serves the config. The mint
// func builds the payload for the n-th happy-link hit.
type happPanel struct {
	srv  *httptest.Server
	mint func(p *happPanel, n int) (string, error)

	mu        sync.Mutex
	mints     int
	accyHdrs  []http.Header
	otherHits []string // any request that is neither a chain hop nor the decrypted URL
}

func newHappPanel(t *testing.T, mint func(p *happPanel, n int) (string, error)) *happPanel {
	t.Helper()
	p := &happPanel{mint: mint}
	p.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/happy-link/"):
			p.mints++
			payload, err := p.mint(p, p.mints)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.Header().Set("Location", "happ://crypt5/"+payload)
			w.WriteHeader(http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/happy-accy/"):
			p.accyHdrs = append(p.accyHdrs, r.Header.Clone())
			w.Header().Set("profile-update-interval", "2")
			fmt.Fprintf(w, `[{"remarks":"rev","outbounds":[{"protocol":"vless","tag":"proxy","settings":{"vnext":[{"address":"1.2.3.%s","port":443,"users":[{"id":"00000000-0000-0000-0000-000000000000"}]}]}}]}]`, r.URL.Query().Get("rev"))
		default:
			p.otherHits = append(p.otherHits, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *happPanel) link() string { return p.srv.URL + "/happy-link/abc" }

// vendoredMint encrypts https://<panel>/happy-accy/abc?rev=<n> under the vendored table.
func vendoredMint(p *happPanel, n int) (string, error) {
	return crypt5test.EncryptVendored(fmt.Sprintf("%s/happy-accy/abc?rev=%d", p.srv.URL, n))
}

// useResolver points the package resolver at the panel for one test.
func useResolver(t *testing.T, p *happPanel, hwid string) {
	t.Helper()
	old := happFetcher
	t.Cleanup(func() { happFetcher = old })
	c := p.srv.Client()
	chain := *c
	chain.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	happFetcher = &happResolver{chain: &chain, fetch: c, keys: crypt5.DefaultKeys(), hwid: func() string { return hwid }}
}

func TestHappLinkDetection(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://panel.example/happy-link/abc": true,
		"happ://crypt5/abcdefgh":               true,
		"https://panel.example/api/sub/abc":    false,
		"https://panel.example/happy/abc":      false,
	} {
		if got := isHappLink(raw); got != want {
			t.Errorf("isHappLink(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestHappResolveHappyPath(t *testing.T) {
	p := newHappPanel(t, vendoredMint)
	useResolver(t, p, testHwid)

	body, resp, isJSON, err := fetchSubscription(structs.Group{SubscriptionUrl: p.link()})
	if err != nil {
		t.Fatal(err)
	}
	if !isJSON || !strings.Contains(body, "1.2.3.1") {
		t.Fatalf("want JSON config for mint 1, got isJSON=%v body=%q", isJSON, body)
	}
	if len(p.accyHdrs) != 1 {
		t.Fatalf("decrypted URL fetched %d times, want 1", len(p.accyHdrs))
	}
	h := p.accyHdrs[0]
	if h.Get("X-HWID") != testHwid {
		t.Errorf("X-HWID = %q, want %q", h.Get("X-HWID"), testHwid)
	}
	if h.Get("User-Agent") != "Happ/4.7.2/Desktop/341" {
		t.Errorf("User-Agent = %q", h.Get("User-Agent"))
	}
	if len(p.otherHits) != 0 {
		t.Errorf("unexpected requests: %v", p.otherHits)
	}
	if secs, action := parseProfileUpdateInterval(resp, body); action != updateIntervalSet || secs != 2*3600 {
		t.Errorf("profile-update-interval not honoured: %d, %v", secs, action)
	}
}

// crypt5 is re-minted per refresh: every refresh walks the chain and decrypts
// the new payload; nothing is cached from add time.
func TestHappRefreshRedecryptsChangedPayload(t *testing.T) {
	var payloads []string
	p := newHappPanel(t, func(p *happPanel, n int) (string, error) {
		s, err := vendoredMint(p, n)
		payloads = append(payloads, s)
		return s, err
	})
	useResolver(t, p, testHwid)
	group := structs.Group{SubscriptionUrl: p.link()}

	for rev := 1; rev <= 2; rev++ {
		body, _, _, err := fetchSubscription(group)
		if err != nil {
			t.Fatalf("refresh %d: %v", rev, err)
		}
		if want := fmt.Sprintf("1.2.3.%d", rev); !strings.Contains(body, want) {
			t.Fatalf("refresh %d: body %q lacks %s (stale decrypt?)", rev, body, want)
		}
	}
	if len(payloads) != 2 || payloads[0] == payloads[1] {
		t.Fatalf("test premise: want two distinct payloads, got %d", len(payloads))
	}
	if p.mints != 2 || len(p.accyHdrs) != 2 {
		t.Fatalf("mints=%d accy fetches=%d, want 2 and 2", p.mints, len(p.accyHdrs))
	}
}

func TestHappErrorsAreDistinctAndNeverFallThrough(t *testing.T) {
	stranger, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		mint func(p *happPanel, n int) (string, error)
		hwid string
		want error
		sub  string // substring when the sentinel is a plain error
	}{
		{"unknown marker", func(*happPanel, int) (string, error) {
			return crypt5test.Encrypt(&stranger.PublicKey, "zzzzzzzz", "https://x.example/")
		}, testHwid, crypt5.ErrUnknownMarker, ""},
		{"malformed payload", func(*happPanel, int) (string, error) { return "abc", nil }, testHwid, crypt5.ErrMalformed, ""},
		{"AEAD failure", func(p *happPanel, n int) (string, error) {
			return crypt5test.EncryptVendoredCorruptAEAD(p.srv.URL + "/happy-accy/abc")
		}, testHwid, crypt5.ErrDecrypt, "AEAD"},
		{"decrypted URL is http", func(p *happPanel, n int) (string, error) {
			return crypt5test.EncryptVendored(strings.Replace(p.srv.URL, "https://", "http://", 1) + "/happy-accy/abc")
		}, testHwid, nil, "https"},
		{"missing hardware id", vendoredMint, "", ErrNoHardwareID, ""},
		{"malformed hardware id", vendoredMint, "NOT-HEX", ErrNoHardwareID, ""},
		{"zero fallback hardware id", vendoredMint, "0000000000000000", ErrNoHardwareID, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newHappPanel(t, tc.mint)
			useResolver(t, p, tc.hwid)
			_, _, _, err := fetchSubscription(structs.Group{SubscriptionUrl: p.link()})
			if err == nil {
				t.Fatal("want an error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("error %v is not %v", err, tc.want)
			}
			if tc.sub != "" && !strings.Contains(err.Error(), tc.sub) {
				t.Errorf("error %q lacks %q", err, tc.sub)
			}
			// No fall-through: the happy-link is never fetched as a plain
			// subscription, nor its /json twin, nor any other path.
			if len(p.otherHits) != 0 {
				t.Errorf("fell through to a plain fetch: %v", p.otherHits)
			}
			if len(p.accyHdrs) != 0 {
				t.Errorf("decrypted URL fetched despite %q", tc.name)
			}
			if tc.hwid != testHwid && p.mints != 0 {
				t.Errorf("network touched before the hardware-id check")
			}
		})
	}
}

// The distinct sentinels must stay distinct so callers can tell them apart.
func TestHappErrorSentinelsDiffer(t *testing.T) {
	errs := []error{ErrNoHardwareID, crypt5.ErrUnknownMarker, crypt5.ErrMalformed, crypt5.ErrDecrypt}
	for i := range errs {
		for j := range errs {
			if i != j && errors.Is(errs[i], errs[j]) {
				t.Errorf("%v matches %v", errs[i], errs[j])
			}
		}
	}
}

// http (not https) is rejected at the link itself, before any request, and the
// link is not retried as a plain subscription.
func TestHappRejectsHTTPLink(t *testing.T) {
	p := newHappPanel(t, vendoredMint)
	useResolver(t, p, testHwid)
	httpLink := strings.Replace(p.link(), "https://", "http://", 1)

	_, _, _, err := fetchSubscription(structs.Group{SubscriptionUrl: httpLink})
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("http happy-link must be rejected with an https error, got %v", err)
	}
	if p.mints != 0 || len(p.accyHdrs) != 0 || len(p.otherHits) != 0 {
		t.Fatalf("http link produced requests: mints=%d accy=%d other=%v", p.mints, len(p.accyHdrs), p.otherHits)
	}
}

// A marker pasted directly resolves without the chain, and still decrypts on
// every refresh.
func TestHappDirectMarkerLink(t *testing.T) {
	p := newHappPanel(t, vendoredMint)
	useResolver(t, p, testHwid)
	payload, err := vendoredMint(p, 7)
	if err != nil {
		t.Fatal(err)
	}
	body, _, _, err := fetchSubscription(structs.Group{SubscriptionUrl: "happ://crypt5/" + payload})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "1.2.3.7") || p.mints != 0 {
		t.Fatalf("direct marker: body=%q mints=%d", body, p.mints)
	}
}

func TestVendoredTestMarkerIsNotOrphan(t *testing.T) {
	if crypt5test.VendoredMarker == crypt5test.OrphanMarker {
		t.Fatal("encryption must skip the orphan marker")
	}
	if _, ok := crypt5.DefaultKeys()(crypt5test.VendoredMarker); !ok {
		t.Fatal("test marker missing from the vendored table")
	}
}
