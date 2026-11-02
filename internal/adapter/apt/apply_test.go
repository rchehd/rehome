package apt

import "testing"

func TestRetarget(t *testing.T) {
	docker := "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu noble stable"
	ngrok := "deb https://ngrok-agent.s3.amazonaws.com bookworm main"

	tests := []struct {
		name             string
		source, from, to string
		want             string
	}{
		{
			"list suite", docker, "noble", "resolute",
			"deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu resolute stable",
		},
		{"other distro release", ngrok, "noble", "resolute", ngrok},
		{"deb822 suite", "Suites: noble\nURIs: https://x.test", "noble", "resolute", "Suites: resolute\nURIs: https://x.test"},
		{"deb822 stable", "Suites: stable", "noble", "resolute", "Suites: stable"},
		{"no recorded release", docker, "", "resolute", "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu resolute stable"},
		{"same release", docker, "noble", "noble", docker},
		{"no target", docker, "noble", "", docker},
		{"comment untouched", "# deb https://x.test noble main\ndeb https://x.test noble main", "noble", "jammy", "# deb https://x.test noble main\ndeb https://x.test jammy main"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Retarget(tt.source, tt.from, tt.to); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKeyringPath(t *testing.T) {
	list := "deb [signed-by=/usr/share/keyrings/a.gpg,/opt/k/b.gpg] https://x.test noble main"
	deb822 := "URIs: https://x.test\nSigned-By: /usr/share/keyrings/c.gpg /opt/k/d.gpg\n"

	tests := []struct {
		name, source, base, want string
	}{
		{"list first", list, "a.gpg", "/usr/share/keyrings/a.gpg"},
		{"list second", list, "b.gpg", "/opt/k/b.gpg"},
		{"deb822 second", deb822, "d.gpg", "/opt/k/d.gpg"},
		{"deb822 first", deb822, "c.gpg", "/usr/share/keyrings/c.gpg"},
		{"default", list, "zzz.gpg", "/etc/apt/keyrings/zzz.gpg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keyringPath(tt.source, tt.base); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
