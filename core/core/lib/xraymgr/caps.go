package xraymgr

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"

	"whoisthat-core/lib/logger"
	"whoisthat-core/utils"
)

// Xray's TUN inbound needs CAP_NET_ADMIN (verified: without it xray exits 255
// with "failed to start proxy > operation not permitted", with it alone TUN
// forwards). The core hands xray no ambient caps (F-01), so the capability has
// to live on the runtime file. Every download replaces the file and wipes it.
// Writing security.capability needs CAP_SETFCAP, which the core lacks, so the
// re-apply goes through setcap and then pkexec. There is no root-free path:
// file caps written from a user namespace are namespaced and do not apply to
// the init namespace.
const (
	xrayCapSet     = "cap_net_admin,cap_net_raw=+ep"
	capNetAdminBit = 12
	vfsCapEffect   = 0x1
)

var (
	hasNetAdminFileCap = readNetAdminFileCap
	applyFileCaps      = setcapRuntime
)

func swapCapSeams(has func(string) bool, apply func(string) error) func() {
	oh, oa := hasNetAdminFileCap, applyFileCaps
	hasNetAdminFileCap, applyFileCaps = has, apply
	return func() { hasNetAdminFileCap, applyFileCaps = oh, oa }
}

// parseNetAdminCap reports whether a raw security.capability xattr grants
// CAP_NET_ADMIN as permitted+effective (layout shared by v2 and v3).
func parseNetAdminCap(raw []byte) bool {
	if len(raw) < 12 {
		return false
	}
	magic := binary.LittleEndian.Uint32(raw[0:4])
	permitted := binary.LittleEndian.Uint32(raw[4:8])
	return magic&vfsCapEffect != 0 && permitted&(1<<capNetAdminBit) != 0
}

func readNetAdminFileCap(bin string) bool {
	buf := make([]byte, 64)
	n, err := unix.Getxattr(bin, "security.capability", buf)
	if err != nil || n <= 0 {
		return false
	}
	return parseNetAdminCap(buf[:n])
}

// HasTunCaps reports whether the runtime binary carries cap_net_admin.
func HasTunCaps(bin string) bool { return hasNetAdminFileCap(bin) }

func setcapRuntime(bin string) error {
	if err := utils.SafeCommand("setcap", xrayCapSet, bin).Run(); err == nil {
		return nil
	}
	// The unprivileged core cannot write file caps; ask polkit once.
	out, err := utils.SafeCommand("pkexec", "setcap", xrayCapSet, bin).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pkexec setcap: %w (%s)", err, string(out))
	}
	return nil
}

// EnsureTunCaps guarantees the xray runtime can open a TUN device, re-applying
// the file capabilities when a download wiped them. The error text is shown to
// the user verbatim, so it names the exact fix.
func EnsureTunCaps(bin string) error {
	if hasNetAdminFileCap(bin) {
		return nil
	}
	logger.Warnf("xraymgr: %s lacks cap_net_admin, re-applying", bin)
	applyErr := applyFileCaps(bin)
	if hasNetAdminFileCap(bin) {
		logger.Infof("xraymgr: capabilities restored on %s", bin)
		return nil
	}
	reason := "could not be re-applied automatically"
	if applyErr != nil {
		reason = applyErr.Error()
	}
	return fmt.Errorf("xray runtime %s lacks cap_net_admin, which TUN mode needs (%s). Fix: sudo setcap %s %s", bin, reason, xrayCapSet, bin)
}
