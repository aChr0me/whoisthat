package mainproxy

import (
	"sort"
	"time"

	appconfig "whoisthat-core/lib/AppConfig"
	"whoisthat-core/lib/logger"
	"whoisthat-core/structs"
)

// proxyIPRefreshInterval is how often a connected profile's server address is
// re-resolved. A var so tests can shorten it.
var proxyIPRefreshInterval = 2 * time.Minute

// resolveProxyIPs re-resolves the server IPs of a profile. A var for tests.
var resolveProxyIPs = func(profile structs.Profile, p *ProxyManager) []string {
	dns := appconfig.GetConfig().DnsServers
	if profile.Managed && p.DB != nil {
		ips, _, err := p.managedSource(profile)
		if err != nil {
			return nil
		}
		return ips
	}
	return resolveProfileIPs(profile.Address, dns)
}

func sameIPSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// RotateProxyIPs re-arms the kill-switch allowlist when the server's IPs
// changed while connected (K-5), without reconnecting. The arm is one atomic
// nft batch, so the old address is replaced, not accumulated. An empty
// resolution is ignored and a failed re-arm keeps the previous IPs so the
// next refresh retries.
func (p *ProxyManager) RotateProxyIPs(newIPs []string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.status.Connection != "connected" || len(newIPs) == 0 || sameIPSet(p.proxyIPs, newIPs) {
		return false, nil
	}
	if err := p.runPreStartHandler(newIPs); err != nil {
		return false, err
	}
	logger.Infof("proxy IPs rotated %v -> %v, allowlist re-armed", p.proxyIPs, newIPs)
	p.proxyIPs = newIPs
	p.profileIPs = newIPs
	return true, nil
}

// watchRotation periodically re-resolves the connected profile until done is
// closed (the same channel that retires the exit watcher).
func (p *ProxyManager) watchRotation(done <-chan struct{}) {
	ticker := time.NewTicker(proxyIPRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
		}
		profile := p.GetStatus().Profile
		ips := resolveProxyIPs(profile, p)
		if _, err := p.RotateProxyIPs(ips); err != nil {
			logger.Warnf("proxy IP rotation re-arm failed: %v", err)
		}
	}
}
