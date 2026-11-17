package tunmode

import (
	"errors"
	"strings"
	"testing"
)

// fakeProbe stubs orphanProbeRun. present lists the "nft table <fam> <name>"
// and "ip link <name>" keys that exist; failOn makes one key return a
// non-"absent" error.
func fakeProbe(t *testing.T, present map[string]bool, failOn string) {
	t.Helper()
	orig := orphanProbeRun
	t.Cleanup(func() { orphanProbeRun = orig })
	orphanProbeRun = func(name string, args ...string) (string, error) {
		key := name + " " + strings.Join(args, " ")
		if failOn != "" && strings.Contains(key, failOn) {
			return "Operation not permitted", errors.New("exit status 1")
		}
		if present[key] {
			return "", nil
		}
		return "Error: No such file or directory", errors.New("exit status 1")
	}
}

func TestDetectOrphanedStateCleanSystem(t *testing.T) {
	fakeProbe(t, nil, "")
	found, err := DetectOrphanedState("whoisthattun")
	if err != nil || len(found) != 0 {
		t.Fatalf("clean system: found=%v err=%v", found, err)
	}
}

func TestDetectOrphanedStateReportsTablesAndInterface(t *testing.T) {
	fakeProbe(t, map[string]bool{
		"nft list table inet whoisthat_ks": true,
		"ip link show mytun":               true,
	}, "")
	found, err := DetectOrphanedState("mytun")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"nft table inet whoisthat_ks", "interface mytun"}
	if strings.Join(found, "|") != strings.Join(want, "|") {
		t.Fatalf("found = %v, want %v", found, want)
	}
}

func TestDetectOrphanedStateDefaultsTunName(t *testing.T) {
	fakeProbe(t, map[string]bool{"ip link show whoisthattun": true}, "")
	found, err := DetectOrphanedState("")
	if err != nil || len(found) != 1 || found[0] != "interface whoisthattun" {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

func TestDetectOrphanedStateUnexpectedNftErrorIsNotClean(t *testing.T) {
	// A probe that cannot tell must fail loudly rather than report "clean".
	fakeProbe(t, nil, "whoisthat_mangle")
	_, err := DetectOrphanedState("whoisthattun")
	if err == nil || !strings.Contains(err.Error(), "cannot inspect nft table") {
		t.Fatalf("err = %v, want a cannot-inspect error", err)
	}
}

func TestDetectOrphanedStateNeverMutates(t *testing.T) {
	orig := orphanProbeRun
	t.Cleanup(func() { orphanProbeRun = orig })
	orphanProbeRun = func(name string, args ...string) (string, error) {
		if name == "nft" && args[0] != "list" || name == "ip" && args[0] != "link" {
			t.Errorf("probe ran a mutating command: %s %v", name, args)
		}
		return "No such file or directory", errors.New("absent")
	}
	if _, err := DetectOrphanedState("whoisthattun"); err != nil {
		t.Fatal(err)
	}
}
