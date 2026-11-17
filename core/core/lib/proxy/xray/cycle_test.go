package xray

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// After several start/stop transitions exactly one xray must be alive; a check
// right after the first spawn would not catch a leak that accumulates.
func TestRepeatedCyclesLeaveOneInstance(t *testing.T) {
	skipIfRoot(t)
	self, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer self.Close()
	bin := filepath.Join(t.TempDir(), "xray")
	out, err := os.OpenFile(bin, os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, self); err != nil {
		t.Fatal(err)
	}
	out.Close()
	useFakeBinary(t, bin)

	x := &XrayCore{}
	t.Cleanup(x.Stop)
	for i := 0; i < 6; i++ {
		if err := x.Start(nil, t.TempDir()); err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
		if n := countXray(bin); n != 1 {
			t.Fatalf("cycle %d: %d xray alive after start, want 1", i, n)
		}
		x.Stop()
		if n := countXray(bin); n != 0 {
			t.Fatalf("cycle %d: %d xray alive after stop, want 0", i, n)
		}
	}
}
