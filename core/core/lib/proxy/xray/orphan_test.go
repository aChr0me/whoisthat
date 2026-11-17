package xray

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func fakeProc(t *testing.T, root string, pid int, exe, cmdline string, parent int) {
	t.Helper()
	d := filepath.Join(root, fmt.Sprint(pid))
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, filepath.Join(d, "exe")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(d, "cmdline"), []byte(cmdline), 0o644)
	os.WriteFile(filepath.Join(d, "stat"), []byte(fmt.Sprintf("%d (xray run) S %d 1 1 0", pid, parent)), 0o644)
}

func TestReapOrphanXrayOnlyKillsInitParentedRunInstances(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(t.TempDir(), "xray")
	os.WriteFile(bin, nil, 0o755)
	other := filepath.Join(t.TempDir(), "other")
	os.WriteFile(other, nil, 0o755)

	fakeProc(t, root, 100, bin, "xray\x00run\x00", 1)           // orphan: reaped
	fakeProc(t, root, 101, bin, "xray\x00run\x00", os.Getpid()) // our child/probe: kept
	fakeProc(t, root, 102, bin, "xray\x00run\x00-test\x00", 1)  // wrong subcommand: kept
	fakeProc(t, root, 103, other, "xray\x00run\x00", 1)         // other binary: kept
	fakeProc(t, root, 104, bin, "xray\x00run\x00", 4242)        // parented elsewhere: kept

	oldRoot, oldKill := procRoot, killPid
	procRoot = root
	var killed []int
	killPid = func(pid int) error { killed = append(killed, pid); return nil }
	t.Cleanup(func() { procRoot, killPid = oldRoot, oldKill })

	if n := reapOrphanXray(bin); n != 1 || len(killed) != 1 || killed[0] != 100 {
		t.Fatalf("reaped=%d killed=%v, want only pid 100", n, killed)
	}
}
