package utils

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

const capsHelperEnv = "WHOISTHAT_CAPS_HELPER"

// The helper runs in a re-executed copy of the test binary so capset/prctl
// never touch the real test process. It mirrors the daemon's startup order.
func TestMain(m *testing.M) {
	if os.Getenv(capsHelperEnv) == "1" {
		RaiseAmbientCaps()
		DropSetpcap()
		for _, k := range []string{"CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"} {
			fmt.Println(k, procCap(k))
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func procCap(key string) string {
	data, _ := os.ReadFile("/proc/self/status")
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, key+":") {
			return strings.TrimSpace(strings.TrimPrefix(line, key+":"))
		}
	}
	return ""
}

func hasBit(hexv string, bit uintptr) bool {
	v, err := strconv.ParseUint(hexv, 16, 64)
	return err == nil && v&(1<<bit) != 0
}

// F-01: CAP_SETPCAP lets a compromised core rewrite its own capability sets
// (and grant them to children). It is only needed to bootstrap the ambient
// raise, so it must be gone from every set right after.
func TestDropSetpcapAfterAmbientRaise(t *testing.T) {
	if !hasBit(procCap("CapEff"), unix.CAP_SETPCAP) || !hasBit(procCap("CapEff"), unix.CAP_NET_ADMIN) {
		t.Skip("test process lacks CAP_SETPCAP/CAP_NET_ADMIN to exercise the drop")
	}
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), capsHelperEnv+"=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("helper: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			got[f[0]] = f[1]
		}
	}
	for _, set := range []string{"CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"} {
		if hasBit(got[set], unix.CAP_SETPCAP) {
			t.Errorf("%s still contains CAP_SETPCAP (%s)", set, got[set])
		}
	}
	for _, set := range []string{"CapPrm", "CapEff", "CapAmb"} {
		for name, bit := range map[string]uintptr{"NET_ADMIN": unix.CAP_NET_ADMIN, "NET_RAW": unix.CAP_NET_RAW} {
			if !hasBit(got[set], bit) {
				t.Errorf("%s lost CAP_%s (%s): the drop must not break TUN/nft", set, name, got[set])
			}
		}
	}
}
