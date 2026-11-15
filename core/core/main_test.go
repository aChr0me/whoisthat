package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestAcquireInstanceLock(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	ok, dir, err := acquireInstanceLock()
	if err != nil || !ok {
		t.Fatalf("first lock: ok=%v err=%v", ok, err)
	}
	if want := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "whoisthat"); dir != want {
		t.Fatalf("lock dir = %q, want %q", dir, want)
	}

	ok, dir, err = acquireInstanceLock()
	if err != nil {
		t.Fatalf("second lock returned an I/O error: %v", err)
	}
	if ok {
		t.Fatalf("second lock acquired in %q; expected (false, _, nil)", dir)
	}

	instanceLock.Close()
	instanceLock = nil

	ok, _, err = acquireInstanceLock()
	if err != nil || !ok {
		t.Fatalf("lock after release: ok=%v err=%v", ok, err)
	}
	instanceLock.Close()
	instanceLock = nil
}

func TestHandleArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code int
		done bool
	}{
		{"no arguments starts the daemon", nil, 0, false},
		{"--version", []string{"--version"}, 0, true},
		{"-v", []string{"-v"}, 0, true},
		{"version", []string{"version"}, 0, true},
		{"--help", []string{"--help"}, 0, true},
		{"-h", []string{"-h"}, 0, true},
		{"unknown argument", []string{"--foo"}, 2, true},
		{"only the first argument counts", []string{"--foo", "--help"}, 2, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, done := handleArgs(tc.args)
			if code != tc.code || done != tc.done {
				t.Fatalf("handleArgs(%q) = (%d, %v), want (%d, %v)", tc.args, code, done, tc.code, tc.done)
			}
		})
	}
}

func TestVersionLineFormat(t *testing.T) {
	// install.sh and release.yml parse this exact shape.
	pattern := regexp.MustCompile(`^whoisthat-core v\d+\.\d+\.\d+ \(protocol \d+\)\n$`)
	if !pattern.MatchString(versionLine()) {
		t.Fatalf("versionLine() = %q does not match %s", versionLine(), pattern)
	}
}

// K-7: the by-name conntrack cleanup must run on every shutdown path, also
// when the bounded cleanup hangs and is abandoned.
func TestShutdownRunsFinalCleanupAfterCompletedCleanup(t *testing.T) {
	var order []string
	runShutdown(func() { order = append(order, "cleanup") }, time.Second,
		func() { order = append(order, "final") })
	if len(order) != 2 || order[0] != "cleanup" || order[1] != "final" {
		t.Fatalf("order = %v", order)
	}
}

func TestShutdownRunsFinalCleanupWhenCleanupHangs(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	finalRan := false
	runShutdown(func() { <-release }, 20*time.Millisecond, func() { finalRan = true })
	if !finalRan {
		t.Fatal("final cleanup skipped after the bounded cleanup timed out")
	}
}

// K-14: the boot entry point arms from the config on disk, with no session,
// no database and no proxy IPs (traffic stays dead until the core connects).
func TestArmBootArmsFromDiskConfig(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "whoisthat")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := `{"kill-switch-enabled": true, "tun-name": "bootun0", "dns-servers": ["9.9.9.9", "not-an-ip"]}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	var gotIPs, gotDNS []string
	var gotTun string
	calls := 0
	armed, err := armBoot(dir, func(ips []string, tun string, dns []string) error {
		calls++
		gotIPs, gotTun, gotDNS = ips, tun, dns
		return nil
	})
	if err != nil || !armed || calls != 1 {
		t.Fatalf("armBoot = %v, %v (calls=%d)", armed, err, calls)
	}
	if len(gotIPs) != 0 || gotTun != "bootun0" || len(gotDNS) != 1 || gotDNS[0] != "9.9.9.9" {
		t.Fatalf("arm args = %v %q %v", gotIPs, gotTun, gotDNS)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json.bak")); err == nil {
		t.Error("boot arming must not write into the user's config dir")
	}
}

func TestArmBootDoesNothingWhenKillSwitchOffOrConfigMissing(t *testing.T) {
	for name, cfg := range map[string]string{
		"off":     `{"kill-switch-enabled": false}`,
		"missing": "",
	} {
		dir := filepath.Join(t.TempDir(), "whoisthat")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if cfg != "" {
			os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o600)
		}
		armed, err := armBoot(dir, func([]string, string, []string) error {
			t.Errorf("%s: arm must not run", name)
			return nil
		})
		if err != nil || armed {
			t.Errorf("%s: armBoot = %v, %v", name, armed, err)
		}
	}
}

func TestArmBootReportsArmFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "whoisthat")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"kill-switch-enabled": true}`), 0o600)
	_, err := armBoot(dir, func([]string, string, []string) error { return errors.New("nft broke") })
	if err == nil {
		t.Fatal("arm failure must surface so the unit fails visibly")
	}
}

func TestArmBootRejectsUnexpectedConfigDir(t *testing.T) {
	if _, err := armBoot("/tmp/elsewhere", func([]string, string, []string) error { return nil }); err == nil {
		t.Fatal("config dir must end in /whoisthat")
	}
}
