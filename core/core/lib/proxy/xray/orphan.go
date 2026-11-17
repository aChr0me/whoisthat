package xray

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"whoisthat-core/lib/logger"
)

// Test seams.
var (
	procRoot = "/proc"
	killPid  = func(pid int) error { return syscall.Kill(pid, syscall.SIGKILL) }
)

// reapOrphanXray kills xray processes left behind by a previous core. A leftover
// holds the TUN device and ports, so a fresh instance would lose the race.
//
// Deliberately narrow: the executable must be exactly bin, the command must be
// `run`, and the parent must be init (pid 1). Children of this core (including
// concurrent test probes) have our pid as parent and are never touched.
func reapOrphanXray(bin string) int {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return 0
	}
	want, err := filepath.EvalSymlinks(bin)
	if err != nil {
		want = bin
	}
	reaped := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 || pid == os.Getpid() {
			continue
		}
		dir := filepath.Join(procRoot, e.Name())
		exe, err := os.Readlink(filepath.Join(dir, "exe"))
		if err != nil || exe != want {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil {
			continue
		}
		args := bytes.Split(bytes.TrimRight(cmdline, "\x00"), []byte{0})
		if len(args) != 2 || string(args[1]) != "run" {
			continue
		}
		if ppid(dir) != 1 {
			continue
		}
		if err := killPid(pid); err != nil {
			logger.Warnf("xray: cannot reap orphan pid=%d: %v", pid, err)
			continue
		}
		logger.Warnf("xray: reaped orphan instance pid=%d", pid)
		reaped++
	}
	if reaped > 0 {
		time.Sleep(100 * time.Millisecond) // let the kernel release the tun fd/ports
	}
	return reaped
}

// ppid reads the parent pid from /proc/<pid>/stat (comm may contain spaces, so
// parse after the last ')'). Returns -1 when unreadable.
func ppid(dir string) int {
	b, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return -1
	}
	s := string(b)
	i := strings.LastIndex(s, ")")
	if i < 0 {
		return -1
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 2 {
		return -1
	}
	n, err := strconv.Atoi(f[1])
	if err != nil {
		return -1
	}
	return n
}
