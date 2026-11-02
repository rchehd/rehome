package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactReplacesSecrets(t *testing.T) {
	ghp := "ghp_" + strings.Repeat("aB3", 12)

	tests := []struct {
		name string
		file string
		in   string
		want string
	}{
		{"export api key", "x.sh", "export OPENAI_API_KEY=sk-abc1234567890abcdefXYZ", "export OPENAI_API_KEY=" + Placeholder},
		{"export password", "x.sh", "export DB_PASSWORD=Sup3r$ecret!x", "export DB_PASSWORD=" + Placeholder},
		{"quoted password", "x.env", `PASSWORD="p@ss,w0rd!xyz"`, `PASSWORD="` + Placeholder + `"`},
		{"spaced password", "x.toml", `password = "correct horse battery staple"`, `password = "` + Placeholder + `"`},
		{"key suffix", "x.sh", "export ANTHROPIC_KEY=abcdef123456", "export ANTHROPIC_KEY=" + Placeholder},
		{"npm _auth", ".npmrc", "_auth=dXNlcjpwYXNzd29yZA==", "_auth=" + Placeholder},
		{"npm authToken", ".npmrc", "//registry.npmjs.org/:_authToken=npm_abcdef1234567890", "//registry.npmjs.org/:_authToken=" + Placeholder},
		{"json authorization", "x.json", `"Authorization": "Basic dXNlcjpwYXNz1234",`, `"Authorization": "` + Placeholder + `",`},
		{"json flag value", "x.json", `"args": ["--api-key", "ctx7sk-1234567890abcdef"],`, `"args": ["--api-key", "` + Placeholder + `"],`},
		{"apt userinfo", "x.list", "deb https://abcdef0123456789abcd@packagecloud.io/x/y/ubuntu noble main", "deb https://" + Placeholder + "@packagecloud.io/x/y/ubuntu noble main"},
		{"git url credentials", "gitconfig", "url = https://user:hunter22@example.com/repo.git", "url = https://" + Placeholder + "@example.com/repo.git"},
		{"token shape anywhere", "x.txt", "see " + ghp + " here", "see " + Placeholder + " here"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, findings := Redact(tt.file, []byte(tt.in))

			if string(got) != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}

			if len(findings) == 0 {
				t.Error("expected a finding")
			}
		})
	}
}

func TestRedactPrivateKeyBlock(t *testing.T) {
	in := "before\n-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaA==\nAAAA\n-----END OPENSSH PRIVATE KEY-----\nafter\n"

	got, findings := Redact("id_ed25519", []byte(in))

	if want := "before\n" + Placeholder + "\nafter\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if len(findings) != 1 || findings[0].Key != "private key" {
		t.Errorf("findings = %+v", findings)
	}
}

func TestRedactLeavesBenignText(t *testing.T) {
	tests := []struct {
		name string
		file string
		in   string
	}{
		{"editor", "x.sh", "export EDITOR=vim"},
		{"alias", "x.sh", `alias ll="ls -la"`},
		{"json plain keys", "x.json", `"keymap": "default-vim-mode", "maxTokens": 40960, "author": "Roman Chekhaniuk"`},
		{"command substitution", "x.sh", "token=$(gh auth token)"},
		{"positional arg", "x.sh", `local token="$1"`},
		{"scp url", "gitconfig", "git@github.com:rchehd/rehome.git"},
		{"markdown pass", "x.md", "> PASS: {specific behavior}"},
		{"markdown prose", "x.md", `If all PASS: "No recommendations."`},
		{"js logic", "x.js", "const gitToken = tokens[i++];"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, findings := Redact(tt.file, []byte(tt.in))

			if string(got) != tt.in {
				t.Errorf("changed to %q", got)
			}

			if len(findings) != 0 {
				t.Errorf("findings = %+v", findings)
			}
		})
	}
}

func TestRedactIsIdempotent(t *testing.T) {
	in := strings.Join([]string{
		"export OPENAI_API_KEY=sk-abc1234567890abcdefXYZ",
		`PASSWORD="p@ss,w0rd!xyz"`,
		"_auth=dXNlcjpwYXNzd29yZA==",
		`"Authorization": "Basic dXNlcjpwYXNz1234",`,
		`"args": ["--api-key", "ctx7sk-1234567890abcdef"],`,
		"url = https://user:hunter22@example.com/repo.git",
		"-----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY-----",
	}, "\n")

	once, _ := Redact("x.conf", []byte(in))
	twice, findings := Redact("x.conf", once)

	if string(once) != string(twice) {
		t.Errorf("second pass changed output:\n%s\n---\n%s", once, twice)
	}

	if len(findings) != 0 {
		t.Errorf("findings on redacted output: %+v", findings)
	}
}

func TestRedactFindingFields(t *testing.T) {
	in := "ok=1\nexport DB_PASSWORD=Sup3r$ecret!x\n\nexport API_TOKEN=abcdef1234567890\n"

	_, findings := Redact("/etc/app.conf", []byte(in))

	want := []Finding{
		{Path: "/etc/app.conf", Key: "DB_PASSWORD", Line: 2},
		{Path: "/etc/app.conf", Key: "API_TOKEN", Line: 4},
	}

	if len(findings) != len(want) {
		t.Fatalf("findings = %+v", findings)
	}

	for i, w := range want {
		if findings[i] != w {
			t.Errorf("finding %d = %+v, want %+v", i, findings[i], w)
		}
	}
}

func TestRedactBinaryUnchanged(t *testing.T) {
	in := []byte("export API_TOKEN=abcdef1234567890\x00\x01")

	got, findings := Redact("x.bin", in)

	if string(got) != string(in) {
		t.Error("binary data was modified")
	}

	if len(findings) != 0 {
		t.Errorf("findings = %+v", findings)
	}
}

func TestIsBinary(t *testing.T) {
	late := append([]byte(strings.Repeat("a", 8000)), 0)

	if !IsBinary([]byte("a\x00b")) {
		t.Error("NUL should be binary")
	}

	if IsBinary([]byte("plain text")) || IsBinary(nil) {
		t.Error("text and empty input are not binary")
	}

	if IsBinary(late) {
		t.Error("NUL after 8000 bytes should be ignored")
	}
}

func TestStripUserinfo(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://user:pass@host/x", "https://host/x"},
		{"git+https://tok@github.com/a/b", "git+https://github.com/a/b"},
		{"https://host/x", "https://host/x"},
		{"git@github.com:a/b.git", "git@github.com:a/b.git"},
	}

	for _, tt := range tests {
		if got := StripUserinfo(tt.in); got != tt.want {
			t.Errorf("StripUserinfo(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestScanDirIgnoresGit(t *testing.T) {
	root := t.TempDir()
	token := "ghp_" + strings.Repeat("aB3", 12)
	write := func(rel, body string) {
		p := filepath.Join(root, rel)

		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("clean.txt", "nothing to see\n")
	write("leaky.txt", "line\n"+token+"\n")
	write(".git/config", token+"\n")

	findings, err := ScanDir(root)

	if err != nil {
		t.Fatal(err)
	}

	if len(findings) != 1 || findings[0].Path != "leaky.txt" || findings[0].Line != 2 {
		t.Errorf("findings = %+v", findings)
	}
}
