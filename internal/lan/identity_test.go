package lan

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestLoadIdentityCreatesOnceWithPrivateModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lan")
	first, err := LoadIdentity(dir)

	if err != nil {
		t.Fatal(err)
	}

	if info, _ := os.Stat(dir); info.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", info.Mode().Perm())
	}

	if info, _ := os.Stat(filepath.Join(dir, keyFile)); info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %v", info.Mode().Perm())
	}

	second, err := LoadIdentity(dir)

	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first.Public(), second.Public()) {
		t.Fatal("second load made a new key")
	}
}

func TestLoadIdentityRefusesOpenKeyFile(t *testing.T) {
	dir := t.TempDir()

	if _, err := LoadIdentity(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(filepath.Join(dir, keyFile), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadIdentity(dir); err == nil {
		t.Fatal("loaded a key readable by others")
	}
}

func TestCertCarriesTheNodeKey(t *testing.T) {
	id, err := LoadIdentity(t.TempDir())

	if err != nil {
		t.Fatal(err)
	}

	leaf := id.Cert.Leaf

	if pub, ok := leaf.PublicKey.(ed25519.PublicKey); !ok || !bytes.Equal(pub, id.Public()) {
		t.Fatal("certificate does not carry the node key")
	}

	if err := leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature); err != nil {
		t.Fatalf("not self-signed: %v", err)
	}

	if leaf.NotAfter.Before(time.Now().AddDate(10, 0, 0)) {
		t.Fatalf("expires too soon: %v", leaf.NotAfter)
	}
}

func TestKeyEncodingRoundTrip(t *testing.T) {
	id, err := LoadIdentity(t.TempDir())

	if err != nil {
		t.Fatal(err)
	}

	got, err := DecodeKey(EncodeKey(id.Public()))

	if err != nil || !bytes.Equal(got, id.Public()) {
		t.Fatalf("round trip: %v", err)
	}

	if _, err := DecodeKey("c2hvcnQ="); err == nil {
		t.Fatal("accepted a short key")
	}
}

func TestFingerprintFormat(t *testing.T) {
	id, err := LoadIdentity(t.TempDir())

	if err != nil {
		t.Fatal(err)
	}

	if fp := Fingerprint(id.Public()); !regexp.MustCompile(`^[0-9a-f]{4}( [0-9a-f]{4}){3}$`).MatchString(fp) {
		t.Fatalf("fingerprint %q", fp)
	}
}
