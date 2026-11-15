package tunmode

import (
	"strings"
	"testing"
)

// K-7: cleanup must find the conntrack-bypass artifacts by name, never by the
// interface that happens to be the default right now (it may have changed, or
// the state may come from a crashed session on another interface).
func TestConntrackCleanupScriptIsInterfaceIndependent(t *testing.T) {
	script := buildConntrackCleanupScript()
	for _, want := range []string{
		"nft delete table ip whoisthat_mangle",
		"nft delete table ip6 whoisthat_mangle",
		"nft delete table inet whoisthat_mangle",
		"WHOISTHAT_MANGLE_PRE",
		"WHOISTHAT_MANGLE_OUT",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("cleanup script missing %q:\n%s", want, script)
		}
	}
	for _, bad := range []string{"DEF_IFACE", " -i ", "eth0"} {
		if strings.Contains(script, bad) {
			t.Errorf("cleanup script must not depend on an interface (%q):\n%s", bad, script)
		}
	}
}

// The iptables backend must put its rules in dedicated chains so they can be
// removed by name.
func TestConntrackIptablesSetupUsesNamedChains(t *testing.T) {
	script := buildConntrackIptablesScript("wittun", "eth0")
	for _, want := range []string{
		`-N WHOISTHAT_MANGLE_PRE`,
		`-N WHOISTHAT_MANGLE_OUT`,
		`-I PREROUTING 1 -j WHOISTHAT_MANGLE_PRE`,
		`-I OUTPUT 1 -j WHOISTHAT_MANGLE_OUT`,
		`-A WHOISTHAT_MANGLE_PRE -i "$DEF_IFACE"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("iptables setup missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "-A PREROUTING") || strings.Contains(script, "-A OUTPUT") {
		t.Errorf("rules must not be appended to the shared builtin chains:\n%s", script)
	}
}

func TestReconcileScriptClearsConntrackChains(t *testing.T) {
	script := buildReconcileScript("whoisthattun")
	for _, want := range []string{"WHOISTHAT_MANGLE_PRE", "WHOISTHAT_MANGLE_OUT"} {
		if !strings.Contains(script, want) {
			t.Errorf("reconcile script missing %q", want)
		}
	}
}
