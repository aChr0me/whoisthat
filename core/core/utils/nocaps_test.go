package utils

import "testing"

func TestHandleNoCapsExecIgnoresOtherArgs(t *testing.T) {
	for _, args := range [][]string{nil, {}, {"--version"}, {"run"}} {
		if HandleNoCapsExec(args) {
			t.Errorf("HandleNoCapsExec(%v) = true, want false", args)
		}
	}
}
