package gotool

import "testing"

func TestModuleVersion(t *testing.T) {
	out := "/home/u/go/bin/gopls: go1.26.5\n\tpath\tgolang.org/x/tools/gopls\n\tmod\tgolang.org/x/tools/gopls\tv0.20.0\th1:abc=\n"

	if got := moduleVersion(out); got != "v0.20.0" {
		t.Errorf("version = %q", got)
	}

	if got := moduleVersion("\tmod\texample.com/x\t(devel)\t\n"); got != "" {
		t.Errorf("devel build = %q", got)
	}
}
