package secrets

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os/exec"
	"regexp"
	"strings"

	"github.com/rchehd/rehome/internal/runner"
)

const sshKeySelector = "sshKey/privateKey"

var (
	bitwardenRef = regexp.MustCompile(`^bw:([[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12})/([A-Za-z0-9_.-]+|sshKey/privateKey)$`)
	inlineValue  = regexp.MustCompile(`^[A-Za-z0-9._~+/=@%:-]+$`)
)

func ValidBitwardenRef(ref string) bool {
	return bitwardenRef.MatchString(ref)
}

func IsBitwardenSSHKey(ref string) bool {
	return strings.HasSuffix(ref, "/"+sshKeySelector)
}

func BitwardenValue(ctx context.Context, session, ref string) (string, error) {
	parts := bitwardenRef.FindStringSubmatch(ref)

	if parts == nil {
		return "", errors.New("invalid Bitwarden reference")
	}

	if session == "" {
		return "", errors.New("unlock Bitwarden and supply BW_SESSION")
	}

	path, err := runner.LookPath("bw")

	if err != nil {
		return "", errors.New("install the Bitwarden CLI (bw)")
	}

	cmd := exec.CommandContext(ctx, path, "get", "item", parts[1], "--raw", "--nointeraction")
	cmd.Env = append(cmd.Environ(), "BW_SESSION="+session)
	data, err := cmd.Output()

	if err != nil {
		return "", errors.New("could not read the Bitwarden item; check the vault session and item ID")
	}

	var item struct {
		ID     string
		Type   int
		Login  struct{ Password string }
		Fields []struct{ Name, Value string }
		SSHKey struct{ PrivateKey string }
	}

	if err := json.Unmarshal(data, &item); err != nil {
		return "", errors.New("could not decode the Bitwarden item")
	}

	if !strings.EqualFold(item.ID, parts[1]) {
		return "", errors.New("Bitwarden returned a different item ID")
	}

	field := parts[2]

	if field == sshKeySelector {
		if item.Type != 5 {
			return "", errors.New("Bitwarden item is not an SSH key")
		}

		return sshPrivateKey(item.SSHKey.PrivateKey)
	}

	value := item.Login.Password

	if field != "login.password" {
		value = ""
		found := false

		for _, f := range item.Fields {
			if f.Name == field {
				if found {
					return "", errors.New("Bitwarden item has duplicate field names")
				}

				value = f.Value
				found = true
			}
		}
	}

	if !inlineValue.MatchString(value) {
		return "", errors.New("Bitwarden field is missing, empty, or contains characters unsupported in inline values")
	}

	return value, nil
}

func sshPrivateKey(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
	block, rest := pem.Decode([]byte(value))

	if block != nil && len(block.Bytes) > 0 && len(rest) == 0 &&
		strings.HasPrefix(value, "-----BEGIN "+block.Type+"-----\n") && strings.Count(value, "-----BEGIN ") == 1 {
		switch block.Type {
		case "OPENSSH PRIVATE KEY", "RSA PRIVATE KEY", "EC PRIVATE KEY", "DSA PRIVATE KEY", "PRIVATE KEY", "ENCRYPTED PRIVATE KEY":
			return value + "\n", nil
		}
	}

	return "", errors.New("Bitwarden SSH item does not contain a supported private key")
}
