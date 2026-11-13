package xray

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"whoisthat-core/utils"
)

const xrayCapsHelperEnv = "WHOISTHAT_XRAY_CAPS_HELPER"

// The helper is a re-executed copy of the test binary so raising ambient caps
// never touches the real test process. It mirrors the daemon: raise the
// ambient set, then spawn a child with the xray process attributes.
func TestMain(m *testing.M) {
	utils.HandleNoCapsExec(os.Args[1:]) // the shim re-executes this binary
	if os.Getenv(xrayCapsHelperEnv) == "1" {
		utils.RaiseAmbientCaps()
		fmt.Println("parent", capLine("CapAmb"))
		cmd, err := xrayCommand(context.Background(), "/bin/sh", "-c", "grep '^CapAmb' /proc/self/status")
		if err != nil {
			fmt.Println("child-error", err)
			os.Exit(1)
		}
		out, err := cmd.Output()
		if err != nil {
			fmt.Println("child-error", err)
			os.Exit(1)
		}
		fmt.Print("child ", strings.TrimSpace(string(out)), "\n")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func capLine(key string) string {
	data, _ := os.ReadFile("/proc/self/status")
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, key+":") {
			return strings.TrimSpace(strings.TrimPrefix(line, key+":"))
		}
	}
	return ""
}

// F-01: a provider-supplied xray config must not run with net_admin/net_raw.
func TestXrayChildGetsNoAmbientCaps(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), xrayCapsHelperEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper: %v\n%s", err, out)
	}
	var parent, child string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		switch {
		case strings.HasPrefix(line, "parent "):
			parent = strings.TrimSpace(strings.TrimPrefix(line, "parent "))
		case strings.HasPrefix(line, "child CapAmb:"):
			child = strings.TrimSpace(strings.TrimPrefix(line, "child CapAmb:"))
		}
	}
	if parent == "" || strings.Trim(parent, "0") == "" {
		t.Skipf("helper could not raise ambient caps (CapAmb=%q): test would be vacuous", parent)
	}
	if child == "" || strings.Trim(child, "0") != "" {
		t.Fatalf("xray child CapAmb = %q (parent %q), want all zero\n%s", child, parent, out)
	}
}
