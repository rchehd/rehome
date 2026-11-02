package pipx

import "testing"

func TestPipxVersion(t *testing.T) {
	out := `{"venvs": {"crg": {"metadata": {"main_package": {"package": "code-review-graph", "package_or_url": "code-review-graph", "package_version": "1.4.2"}}}}}`

	if got := pipxVersion(out, "code-review-graph"); got != "1.4.2" {
		t.Errorf("version = %q", got)
	}

	if got := pipxVersion(out, "other"); got != "" {
		t.Errorf("unknown package = %q", got)
	}

	if got := pipxVersion("not json", "x"); got != "" {
		t.Errorf("bad json = %q", got)
	}
}
