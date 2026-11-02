package apt

import (
	"slices"
	"testing"
)

func TestAppendPaths(t *testing.T) {
	tests := []struct {
		name  string
		start []string
		value string
		want  []string
	}{
		{"comma", nil, "/a.gpg,/b.gpg", []string{"/a.gpg", "/b.gpg"}},
		{"space", nil, " /a.gpg  /b.gpg", []string{"/a.gpg", "/b.gpg"}},
		{"inline key and fingerprint", nil, "-----BEGIN PGP PUBLIC KEY BLOCK----- ABCD1234 /c.gpg", []string{"/c.gpg"}},
		{"duplicates", []string{"/a.gpg"}, "/a.gpg,/b.gpg,/b.gpg", []string{"/a.gpg", "/b.gpg"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := appendPaths(tt.start, tt.value); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseRepoTextList(t *testing.T) {
	text := "# comment\n\ndeb [arch=amd64 signed-by=/usr/share/keyrings/t1.gpg,/usr/share/keyrings/t2.gpg] https://repo.test/apt stable main\n"

	repo, err := parseRepoText(text, false)

	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(repo.URIs, []string{"https://repo.test/apt"}) {
		t.Errorf("uris = %v", repo.URIs)
	}

	if want := []string{"/usr/share/keyrings/t1.gpg", "/usr/share/keyrings/t2.gpg"}; !slices.Equal(repo.Keyrings, want) {
		t.Errorf("keyrings = %v", repo.Keyrings)
	}
}

func TestParseRepoTextDeb822(t *testing.T) {
	text := "Types: deb\nURIs: https://repo.test/apt https://mirror.test/apt\nSigned-By: /usr/share/keyrings/g.gpg\n"

	repo, err := parseRepoText(text, true)

	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"https://repo.test/apt", "https://mirror.test/apt"}; !slices.Equal(repo.URIs, want) {
		t.Errorf("uris = %v", repo.URIs)
	}

	if !slices.Equal(repo.Keyrings, []string{"/usr/share/keyrings/g.gpg"}) {
		t.Errorf("keyrings = %v", repo.Keyrings)
	}
}

func TestParseRepoTextInlineKey(t *testing.T) {
	text := "URIs: https://repo.test\nSigned-By: -----BEGIN PGP PUBLIC KEY BLOCK-----\n"

	repo, err := parseRepoText(text, true)

	if err != nil {
		t.Fatal(err)
	}

	if len(repo.Keyrings) != 0 {
		t.Errorf("keyrings = %v", repo.Keyrings)
	}
}

func TestReposNameFor(t *testing.T) {
	repos := Repos{
		{Name: "docker", URIs: []string{"https://download.docker.com/linux/ubuntu/"}},
		{Name: "chrome", URIs: []string{"https://dl.google.com/linux/chrome/deb"}},
	}

	tests := []struct{ uri, want string }{
		{"https://download.docker.com/linux/ubuntu", "docker"},
		{"https://download.docker.com/linux/ubuntu/dists/noble", "docker"},
		{"https://dl.google.com/linux/chrome/deb/", "chrome"},
		{"http://archive.ubuntu.com/ubuntu", ""},
		{"dpkg-only", ""},
		{"", ""},
		{"https://unknown.test/x", ""},
	}

	for _, tt := range tests {
		if got := repos.NameFor(tt.uri); got != tt.want {
			t.Errorf("NameFor(%q) = %q, want %q", tt.uri, got, tt.want)
		}
	}
}
