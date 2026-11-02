package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadOSRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "os-release")
	body := "NAME=\"Ubuntu\"\nID=ubuntu\nID_LIKE=debian\nVERSION_ID='24.04'\nVERSION_CODENAME=noble\nnot a pair\n"

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := readOSRelease(path)

	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{"NAME": "Ubuntu", "ID": "ubuntu", "ID_LIKE": "debian", "VERSION_ID": "24.04", "VERSION_CODENAME": "noble"}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestReadOSReleaseMissing(t *testing.T) {
	if _, err := readOSRelease(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("expected error")
	}
}

func TestPlatformKeys(t *testing.T) {
	tests := []struct {
		name     string
		p        Platform
		key      string
		fileKey  string
		isDebian bool
	}{
		{"ubuntu", Platform{OS: "linux", ID: "ubuntu", IDLike: []string{"debian"}}, "debian", "linux", true},
		{"debian", Platform{OS: "linux", ID: "debian"}, "debian", "linux", true},
		{"fedora", Platform{OS: "linux", ID: "fedora", IDLike: []string{"rhel"}}, "", "linux", false},
		{"darwin", Platform{OS: "darwin", ID: "macos"}, "macos", "macos", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Key(); got != tt.key {
				t.Errorf("Key = %q, want %q", got, tt.key)
			}

			if got := tt.p.FileKey(); got != tt.fileKey {
				t.Errorf("FileKey = %q, want %q", got, tt.fileKey)
			}

			if got := tt.p.Is("debian"); got != tt.isDebian {
				t.Errorf("Is(debian) = %v", got)
			}
		})
	}
}

func TestPlatformIsMatchesLike(t *testing.T) {
	p := Platform{ID: "mint", IDLike: []string{"ubuntu", "debian"}}

	if !p.Is("fedora", "ubuntu") || p.Is("fedora", "arch") {
		t.Error("Is should match ID or ID_LIKE entries")
	}
}
