package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	appconfig "whoisthat-core/lib/AppConfig"
	"whoisthat-core/lib/crypt5"
	"whoisthat-core/lib/netmark"
)

// Happ clients identify themselves with this User-Agent; panels answer any other
// agent (or a missing HWID) with placeholder cards.
const (
	happUserAgent = "Happ/4.7.2/Desktop/341"
	happMaxHops   = 5
)

// ErrNoHardwareID means no usable HWID exists, so the panel would return
// placeholder cards. It is raised before any network request.
var ErrNoHardwareID = errors.New("happ: no hardware id (HWID disabled, unset or invalid)")

var hwidRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// happResolver holds everything the crypt5 chain needs, so tests can point it
// at an httptest server and a throwaway key table. Nothing here is cached
// between calls: crypt5 is re-minted per refresh, so every refresh walks the
// whole chain and decrypts again.
type happResolver struct {
	chain *http.Client // must not follow redirects: the chain ends in happ://
	fetch *http.Client
	keys  crypt5.KeyLookup
	hwid  func() string
}

func newHappResolver() *happResolver {
	return &happResolver{
		chain: &http.Client{
			Timeout:       subClient.Timeout,
			Transport:     netmark.Transport(),
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		fetch: subClient,
		keys:  crypt5.DefaultKeys(),
		hwid:  configuredHwid,
	}
}

// configuredHwid returns the persisted per-machine id (generated once by the
// config loader; it only changes on an explicit reset), or "" when HWID is off.
func configuredHwid() string {
	cfg := appconfig.GetConfig()
	if !cfg.HwidEnabled {
		return ""
	}
	return cfg.Hwid
}

// isHappLink reports whether raw must go through the crypt5 resolver: a
// happ://crypt5/ marker, or a panel …/happy-link/<id> URL.
func isHappLink(raw string) bool {
	if _, err := crypt5.ParseMarker(raw); err == nil {
		return true
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && strings.Contains(u.Path, "/happy-link/")
}

// headerDoer stamps the Happ identity on every hop.
type headerDoer struct {
	c    *http.Client
	hwid string
}

func (d headerDoer) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("X-HWID", d.hwid)
	req.Header.Set("User-Agent", happUserAgent)
	return d.c.Do(req)
}

// requireStrictHTTPS has no loopback exemption: these URLs come from a remote
// panel and carry credentials.
func requireStrictHTTPS(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Scheme != "https" {
		return fmt.Errorf("happ: %q is not an https URL (https required)", redactSecret(raw))
	}
	return nil
}

// resolve returns the config body behind a Happ link. Every failure is a
// distinct error (ErrNoHardwareID, crypt5.ErrNotMarker, crypt5.ErrUnknownMarker,
// crypt5.ErrMalformed, crypt5.ErrDecrypt, crypt5.ErrNotConfig, …) and none of
// them falls back to fetching the link as an ordinary subscription.
func (r *happResolver) resolve(ctx context.Context, raw string) (string, *http.Response, error) {
	hwid := r.hwid()
	if !hwidRe.MatchString(hwid) || hwid == "0000000000000000" {
		return "", nil, ErrNoHardwareID
	}
	doer := headerDoer{c: r.chain, hwid: hwid}

	payload, err := crypt5.ParseMarker(raw)
	if err != nil {
		if err := requireStrictHTTPS(raw); err != nil {
			return "", nil, err
		}
		payload, err = crypt5.ResolveMarker(ctx, doer, strings.TrimSpace(raw), happMaxHops)
		if err != nil {
			return "", nil, err
		}
	}

	target, err := crypt5.Decrypt(payload, r.keys)
	if err != nil {
		return "", nil, err
	}
	if err := requireStrictHTTPS(target); err != nil {
		return "", nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("X-HWID", hwid)
	req.Header.Set("User-Agent", happUserAgent)
	resp, err := r.fetch.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSubBytes))
	if err != nil {
		return "", nil, err
	}
	if err := crypt5.CheckBody(body); err != nil {
		return "", nil, err
	}
	return string(body), resp, nil
}
