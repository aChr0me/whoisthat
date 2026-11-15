package mainproxy

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"whoisthat-core/structs"
)

type armRecorder struct {
	mu    sync.Mutex
	calls [][]string
	err   error
}

func (a *armRecorder) handler(ips []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, append([]string(nil), ips...))
	return a.err
}

func (a *armRecorder) snapshot() [][]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([][]string(nil), a.calls...)
}

func connectedManager(ips ...string) *ProxyManager {
	p := newTestProxyManager()
	p.status = structs.ProxyStatus{
		Connection: "connected",
		Profile:    structs.Profile{Address: "vpn.example.org"},
	}
	p.proxyIPs = ips
	return p
}

// K-5: a rotated server IP must reach the kill-switch allowlist without a
// reconnect, and the old address must stop being the tracked one.
func TestRotateProxyIPsRearmsWithNewAddress(t *testing.T) {
	p := connectedManager("203.0.113.1")
	rec := &armRecorder{}
	p.SetPreStartHandler(rec.handler)

	changed, err := p.RotateProxyIPs([]string{"203.0.113.2"})
	if err != nil || !changed {
		t.Fatalf("RotateProxyIPs = %v, %v; want true, nil", changed, err)
	}
	if got := rec.snapshot(); !reflect.DeepEqual(got, [][]string{{"203.0.113.2"}}) {
		t.Fatalf("re-arm calls = %v", got)
	}
	if got := p.GetProxyIPs(); !reflect.DeepEqual(got, []string{"203.0.113.2"}) {
		t.Fatalf("GetProxyIPs = %v, old address must be gone", got)
	}
}

func TestRotateProxyIPsUnchangedSetIsNoop(t *testing.T) {
	p := connectedManager("203.0.113.1", "203.0.113.9")
	rec := &armRecorder{}
	p.SetPreStartHandler(rec.handler)

	changed, err := p.RotateProxyIPs([]string{"203.0.113.9", "203.0.113.1"})
	if err != nil || changed {
		t.Fatalf("RotateProxyIPs = %v, %v; want false, nil", changed, err)
	}
	if n := len(rec.snapshot()); n != 0 {
		t.Fatalf("re-armed %d times for an unchanged set", n)
	}
}

// An empty resolution (transient DNS failure) must never replace a working
// allowlist with nothing.
func TestRotateProxyIPsIgnoresEmptyResolution(t *testing.T) {
	p := connectedManager("203.0.113.1")
	rec := &armRecorder{}
	p.SetPreStartHandler(rec.handler)

	if changed, err := p.RotateProxyIPs(nil); changed || err != nil {
		t.Fatalf("RotateProxyIPs(nil) = %v, %v", changed, err)
	}
	if n := len(rec.snapshot()); n != 0 {
		t.Fatalf("armed with empty allowlist")
	}
	if got := p.GetProxyIPs(); !reflect.DeepEqual(got, []string{"203.0.113.1"}) {
		t.Fatalf("GetProxyIPs = %v", got)
	}
}

// A failed re-arm keeps the previous tracked IPs so the next tick retries.
func TestRotateProxyIPsFailedRearmKeepsOldIPs(t *testing.T) {
	p := connectedManager("203.0.113.1")
	rec := &armRecorder{err: errors.New("nft failed")}
	p.SetPreStartHandler(rec.handler)

	changed, err := p.RotateProxyIPs([]string{"203.0.113.2"})
	if err == nil || changed {
		t.Fatalf("RotateProxyIPs = %v, %v; want false, error", changed, err)
	}
	if got := p.GetProxyIPs(); !reflect.DeepEqual(got, []string{"203.0.113.1"}) {
		t.Fatalf("GetProxyIPs = %v, must keep old IPs", got)
	}
}

func TestRotateProxyIPsIgnoredWhenDisconnected(t *testing.T) {
	p := newTestProxyManager()
	rec := &armRecorder{}
	p.SetPreStartHandler(rec.handler)
	if changed, _ := p.RotateProxyIPs([]string{"203.0.113.2"}); changed {
		t.Fatal("rotated while disconnected")
	}
	if len(rec.snapshot()) != 0 {
		t.Fatal("armed while disconnected")
	}
}

// The watcher re-resolves the connected profile and feeds rotations through
// RotateProxyIPs until retired.
func TestRotationWatcherRearmsOnResolvedChange(t *testing.T) {
	p := connectedManager("203.0.113.1")
	rec := &armRecorder{}
	p.SetPreStartHandler(rec.handler)

	oldResolve, oldEvery := resolveProxyIPs, proxyIPRefreshInterval
	t.Cleanup(func() { resolveProxyIPs, proxyIPRefreshInterval = oldResolve, oldEvery })
	resolveProxyIPs = func(structs.Profile, *ProxyManager) []string { return []string{"203.0.113.77"} }
	proxyIPRefreshInterval = 5 * time.Millisecond

	done := make(chan struct{})
	defer close(done)
	go p.watchRotation(done)

	deadline := time.After(2 * time.Second)
	for len(rec.snapshot()) == 0 {
		select {
		case <-deadline:
			t.Fatal("watcher never re-armed")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if got := rec.snapshot()[0]; !reflect.DeepEqual(got, []string{"203.0.113.77"}) {
		t.Fatalf("armed with %v", got)
	}
}
