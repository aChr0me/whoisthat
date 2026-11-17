package xraymgr

import (
	"encoding/binary"
	"strings"
	"testing"
)

func vfsCap(rev uint32, effective bool, permLo uint32) []byte {
	b := make([]byte, 20)
	magic := rev
	if effective {
		magic |= 1
	}
	binary.LittleEndian.PutUint32(b[0:], magic)
	binary.LittleEndian.PutUint32(b[4:], permLo)
	return b
}

func TestParseNetAdminCap(t *testing.T) {
	const netAdmin = 1 << 12
	const netRaw = 1 << 13
	cases := []struct {
		name string
		raw  []byte
		want bool
	}{
		{"v2 ep net_admin", vfsCap(0x02000000, true, netAdmin|netRaw), true},
		{"v3 ep net_admin", vfsCap(0x03000000, true, netAdmin), true},
		{"permitted but not effective", vfsCap(0x02000000, false, netAdmin), false},
		{"net_raw only", vfsCap(0x02000000, true, netRaw), false},
		{"too short", []byte{1, 2, 3}, false},
		{"empty", nil, false},
	}
	for _, c := range cases {
		if got := parseNetAdminCap(c.raw); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestEnsureTunCapsMessageIsActionable(t *testing.T) {
	restore := swapCapSeams(func(string) bool { return false }, func(string) error { return nil })
	defer restore()
	err := EnsureTunCaps("/x/xray")
	if err == nil {
		t.Fatal("expected error when caps stay missing")
	}
	for _, want := range []string{"/x/xray", "cap_net_admin", "setcap cap_net_admin,cap_net_raw=+ep /x/xray"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err.Error(), want)
		}
	}
}

func TestEnsureTunCapsReappliesWhenMissing(t *testing.T) {
	has, applied := false, 0
	restore := swapCapSeams(func(string) bool { return has }, func(string) error { applied++; has = true; return nil })
	defer restore()
	if err := EnsureTunCaps("/x/xray"); err != nil || applied != 1 {
		t.Fatalf("err=%v applied=%d", err, applied)
	}
	if err := EnsureTunCaps("/x/xray"); err != nil || applied != 1 {
		t.Fatalf("second call must be a no-op: err=%v applied=%d", err, applied)
	}
}
