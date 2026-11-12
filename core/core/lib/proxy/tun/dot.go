package tunmode

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// DNS-over-TLS (K-2): with systemd-resolved in DoT mode the stub sends port 853
// to the upstream, which the kill-switch drop chain blackholes. On arm we force
// `dnsovertls no` on the TUN link, remembering the prior value; on disarm we
// restore it. If the setting cannot be neutralised we refuse to arm, because an
// armed switch over a DoT-only resolver means no DNS at all.

var (
	resolvectlLookPath = exec.LookPath
	resolvectlRun      = func(args ...string) (string, error) {
		out, err := exec.Command("resolvectl", args...).CombinedOutput()
		return string(out), err
	}

	dotMu    sync.Mutex
	dotLink  string
	dotPrior string
	dotSaved bool
)

func resetDoTState() {
	dotMu.Lock()
	defer dotMu.Unlock()
	dotLink, dotPrior, dotSaved = "", "", false
}

// readDoT returns the link's current dnsovertls value (`Link 7 (x): yes`).
func readDoT(link string) (string, error) {
	out, err := resolvectlRun("dnsovertls", link)
	if err != nil {
		return "", fmt.Errorf("resolvectl dnsovertls %s: %w: %s", link, err, strings.TrimSpace(out))
	}
	line := strings.TrimSpace(out)
	i := strings.LastIndex(line, ": ")
	if i < 0 {
		return "", fmt.Errorf("unparseable resolvectl dnsovertls output %q", line)
	}
	return strings.TrimSpace(line[i+2:]), nil
}

// neutraliseDoT disables DoT on the TUN link and records the prior value.
func neutraliseDoT(tunName string) error {
	if tunName == "" {
		return nil
	}
	if _, err := resolvectlLookPath("resolvectl"); err != nil {
		return nil // no systemd-resolved: nothing to neutralise
	}
	dotMu.Lock()
	defer dotMu.Unlock()

	prior, err := readDoT(tunName)
	if err != nil {
		return fmt.Errorf("kill-switch not armed: cannot read DNS-over-TLS state: %w", err)
	}
	if !dotSaved { // keep the first-seen value across repeated arms
		dotLink, dotPrior, dotSaved = tunName, prior, true
	}
	if _, err := resolvectlRun("dnsovertls", tunName, "no"); err != nil {
		return fmt.Errorf("kill-switch not armed: cannot disable DNS-over-TLS on %s: %w", tunName, err)
	}
	now, err := readDoT(tunName)
	if err != nil || now != "no" {
		return fmt.Errorf("kill-switch not armed: DNS-over-TLS is still %q on %s and would be dropped; disable DNSOverTLS in resolved.conf or use a plain-DNS resolver", now, tunName)
	}
	return nil
}

// restoreDoT puts the recorded prior value back. No-op when nothing was saved.
func restoreDoT() error {
	dotMu.Lock()
	defer dotMu.Unlock()
	if !dotSaved {
		return nil
	}
	if _, err := resolvectlRun("dnsovertls", dotLink, dotPrior); err != nil {
		return fmt.Errorf("restore dnsovertls=%s on %s: %w", dotPrior, dotLink, err)
	}
	dotLink, dotPrior, dotSaved = "", "", false
	return nil
}
