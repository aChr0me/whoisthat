package utils

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// NoCapsExecArg selects the shim mode: `whoisthat-core __exec-nocaps <bin> args...`
// clears the ambient and inheritable capability sets, then execs <bin>.
//
// Why a shim: the Go runtime never clears ambient caps for a child, it only
// raises the ones listed in SysProcAttr.AmbientCaps. Whatever is ambient in the
// core (net_admin, net_raw) is therefore inherited by every child, including
// xray running a provider-supplied config (F-01). Clearing it in the core
// itself would starve the ip/nft children that need it, so the clear happens
// in a short-lived copy of the core between fork and the xray exec.
const NoCapsExecArg = "__exec-nocaps"

// HandleNoCapsExec runs the shim when args selects it and never returns in
// that case: it either execs the target or exits 126. It returns false for
// any other args so normal startup continues.
func HandleNoCapsExec(args []string) bool {
	if len(args) == 0 || args[0] != NoCapsExecArg {
		return false
	}
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "whoisthat-core: "+NoCapsExecArg+": missing target")
		os.Exit(126)
	}
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil {
		fmt.Fprintf(os.Stderr, "whoisthat-core: %s: clearing ambient caps: %v\n", NoCapsExecArg, err)
		os.Exit(126) // fail closed: never start the child with caps we meant to drop
	}
	var hdr unix.CapUserHeader
	hdr.Version = unix.LINUX_CAPABILITY_VERSION_3
	var data [2]unix.CapUserData
	if err := unix.Capget(&hdr, &data[0]); err != nil {
		fmt.Fprintf(os.Stderr, "whoisthat-core: %s: capget: %v\n", NoCapsExecArg, err)
		os.Exit(126)
	}
	data[0].Inheritable, data[1].Inheritable = 0, 0
	if err := unix.Capset(&hdr, &data[0]); err != nil {
		fmt.Fprintf(os.Stderr, "whoisthat-core: %s: capset: %v\n", NoCapsExecArg, err)
		os.Exit(126)
	}
	err := unix.Exec(args[1], args[1:], os.Environ())
	fmt.Fprintf(os.Stderr, "whoisthat-core: %s: exec %s: %v\n", NoCapsExecArg, args[1], err)
	os.Exit(126)
	return true
}
