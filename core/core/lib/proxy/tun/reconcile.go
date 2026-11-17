package tunmode

import (
	"fmt"
	"strings"
)

// buildReconcileScript returns an idempotent teardown script that removes any
// leftover whoisthat networking state from a previous core that exited without
// cleaning up — a hard `Q`, a crash, an OOM kill, or a power loss mid-TUN.
//
// It covers every artifact the core can create:
//   - nftables tables: whoisthat (DNS hijack v4/v6), whoisthat_ks (kill-switch),
//     whoisthat_mangle (conntrack bypass), whoisthat_split (split tunnel)
//   - iptables kill-switch chains (legacy backend)
//   - policy-routing rules pointing at table 100 (fwmark + uidrange) and
//     table 200 (include-mode split tunnel), plus their routes
//
// Every command is guarded so the script is a clean no-op when nothing exists.
// The `ip rule del table N` loops delete one matching rule per iteration and
// stop as soon as none remain (bounded to avoid an unbounded loop). This
// deletes fwmark AND uidrange rules regardless of their specific values, so it
// stays correct even if the dedicated UID differs from the crashed session's.
//
// The iptables DNS hijack lives in dedicated nat chains, so a crash cannot
// leave address-specific rules behind: removing the chain jump and flushing the
// chain is enough regardless of which sink or interface was in use.
func buildReconcileScript(tunName string) string {
	if tunName == "" {
		tunName = "whoisthattun"
	}
	return fmt.Sprintf(`
# --- nftables tables (modern default backend) ---
nft delete table ip whoisthat 2>/dev/null || true
nft delete table ip6 whoisthat 2>/dev/null || true
nft delete table inet whoisthat_ks 2>/dev/null || true
nft delete table ip whoisthat_mangle 2>/dev/null || true
nft delete table ip6 whoisthat_mangle 2>/dev/null || true
nft delete table inet whoisthat_mangle 2>/dev/null || true
nft delete table inet whoisthat_split 2>/dev/null || true

# --- iptables kill-switch chains (legacy backend) ---
for IPT in iptables ip6tables; do
    while "$IPT" -C OUTPUT -j WHOISTHAT_KS 2>/dev/null; do
        "$IPT" -D OUTPUT -j WHOISTHAT_KS
    done
    "$IPT" -F WHOISTHAT_KS 2>/dev/null || true
    "$IPT" -X WHOISTHAT_KS 2>/dev/null || true
done

# --- iptables conntrack-bypass chains (legacy backend) ---
for IPT in iptables ip6tables; do
    while "$IPT" -t mangle -D PREROUTING -j WHOISTHAT_MANGLE_PRE 2>/dev/null; do :; done
    while "$IPT" -t mangle -D OUTPUT -j WHOISTHAT_MANGLE_OUT 2>/dev/null; do :; done
    "$IPT" -t mangle -F WHOISTHAT_MANGLE_PRE 2>/dev/null || true
    "$IPT" -t mangle -F WHOISTHAT_MANGLE_OUT 2>/dev/null || true
    "$IPT" -t mangle -X WHOISTHAT_MANGLE_PRE 2>/dev/null || true
    "$IPT" -t mangle -X WHOISTHAT_MANGLE_OUT 2>/dev/null || true
done

# --- iptables DNS hijack chains (legacy backend) ---
for IPT in iptables ip6tables; do
    "$IPT" -t nat -D OUTPUT -j WHOISTHAT_DNS_OUT 2>/dev/null || true
    "$IPT" -t nat -D PREROUTING -j WHOISTHAT_DNS_PRE 2>/dev/null || true
    "$IPT" -t nat -D POSTROUTING -j WHOISTHAT_DNS_POST 2>/dev/null || true
    "$IPT" -t nat -F WHOISTHAT_DNS_OUT 2>/dev/null || true
    "$IPT" -t nat -F WHOISTHAT_DNS_PRE 2>/dev/null || true
    "$IPT" -t nat -F WHOISTHAT_DNS_POST 2>/dev/null || true
    "$IPT" -t nat -X WHOISTHAT_DNS_OUT 2>/dev/null || true
    "$IPT" -t nat -X WHOISTHAT_DNS_PRE 2>/dev/null || true
    "$IPT" -t nat -X WHOISTHAT_DNS_POST 2>/dev/null || true
done

# --- policy routing: drop every rule pointing at table 100/200, then flush ---
for TABLE in 100 200; do
    i=0
    while [ $i -lt 32 ]; do
        ip rule del table $TABLE 2>/dev/null || break
        i=$((i + 1))
    done
    i=0
    while [ $i -lt 32 ]; do
        ip -6 rule del table $TABLE 2>/dev/null || break
        i=$((i + 1))
    done
    ip route flush table $TABLE 2>/dev/null || true
    ip -6 route flush table $TABLE 2>/dev/null || true
done

resolvectl revert %[1]q 2>/dev/null || true
ip link set %[1]q down 2>/dev/null || true
ip tuntap del mode tun dev %[1]q 2>/dev/null || true
ip link del dev %[1]q 2>/dev/null || true
`, tunName)
}

// ReconcileOrphanedRules tears down any leftover whoisthat networking state from
// a previous core that did not clean up after itself. It is safe to call at
// startup only once the caller has confirmed no other core instance is running
// (otherwise it would nuke the live core's rules); see main.go's port probe.
func ReconcileOrphanedRules(tunName string) error {
	// Restore rp_filter before deleting the interfaces: once the TUN device is
	// gone its sysctl node disappears and the saved value can no longer be
	// applied. The state file is written by loosenRpFilter before it changes
	// anything.
	if err := restoreRpFilter(); err != nil {
		return err
	}
	_, err := runScriptWithSh(buildReconcileScript(tunName))
	return err
}

// orphanedStateTables lists the nft tables ReconcileOrphanedRules tears down
// that --check-clean treats as a "stale state" signal. whoisthat_split is
// deliberately excluded: it is only ever present together with whoisthat_ks
// or whoisthat_mangle, so checking those three is sufficient and keeps the
// message focused on what a user would actually need to act on.
var orphanedStateTables = []struct {
	family string
	name   string
}{
	{"ip", "whoisthat"},
	{"ip6", "whoisthat"},
	{"ip", "whoisthat_mangle"},
	{"ip6", "whoisthat_mangle"},
	{"inet", "whoisthat_mangle"},
	{"inet", "whoisthat_ks"},
}

// orphanProbeRun is the command seam for DetectOrphanedState so the probe logic
// can be tested without nft/ip or a live system.
var orphanProbeRun = runCommandWithCaps

// DetectOrphanedState reports whether any nftables table or TUN interface the
// core creates is still present on the system, which --check-clean treats as
// evidence of a previous core that exited without cleaning up after itself.
// It is a read-only probe: unlike ReconcileOrphanedRules it never deletes
// anything, so it is safe to call regardless of whether a core is running.
// The caller (main.go's --check-clean) is responsible for first confirming no
// core is currently running — a live core legitimately owns this state.
func DetectOrphanedState(tunName string) (found []string, err error) {
	if tunName == "" {
		tunName = "whoisthattun"
	}
	seen := map[string]bool{}
	for _, t := range orphanedStateTables {
		key := t.family + " " + t.name
		if seen[key] {
			continue
		}
		seen[key] = true
		// Mirrors the detection pattern nftKillSwitchState already uses:
		// CombinedOutput + LC_ALL=C so "No such file or directory" is a
		// stable, locale-independent signal for "table absent".
		out, cmdErr := orphanProbeRun("nft", "list", "table", t.family, t.name)
		if cmdErr == nil {
			found = append(found, fmt.Sprintf("nft table %s %s", t.family, t.name))
			continue
		}
		if !strings.Contains(out, "No such file or directory") {
			return found, fmt.Errorf("cannot inspect nft table %s %s: %w: %s", t.family, t.name, cmdErr, strings.TrimSpace(out))
		}
	}
	if _, cmdErr := orphanProbeRun("ip", "link", "show", tunName); cmdErr == nil {
		found = append(found, fmt.Sprintf("interface %s", tunName))
	}
	return found, nil
}
