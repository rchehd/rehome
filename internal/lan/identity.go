// Package lan syncs rehome nodes directly between paired machines on a local network.
package lan

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const keyFile = "node.key"

// Identity is this machine's LAN key and the self-signed certificate it presents.
type Identity struct {
	Private ed25519.PrivateKey
	Cert    tls.Certificate
}

// Dir holds this machine's LAN key.
func Dir() (string, error) {
	dir, err := os.UserConfigDir()

	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "rehome", "lan"), nil
}

// LoadIdentity reads the key in dir, creating it on first use.
func LoadIdentity(dir string) (*Identity, error) {
	path := filepath.Join(dir, keyFile)
	data, err := os.ReadFile(path) // #nosec G304 -- the LAN key file in rehome's own config dir

	if errors.Is(err, os.ErrNotExist) {
		return createIdentity(dir)
	}

	if err != nil {
		return nil, err
	}

	if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s must be readable by its owner only (chmod 600)", path)
	}

	block, _ := pem.Decode(data)

	if block == nil {
		return nil, fmt.Errorf("%s: not a PEM key", path)
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)

	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	private, ok := key.(ed25519.PrivateKey)

	if !ok {
		return nil, fmt.Errorf("%s: not an ed25519 key", path)
	}

	return newIdentity(private)
}

// HasIdentity reports whether this machine already has a LAN key, i.e. uses LAN sync.
func HasIdentity(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, keyFile))

	return err == nil
}

func createIdentity(dir string) (*Identity, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)

	if err != nil {
		return nil, err
	}

	der, err := x509.MarshalPKCS8PrivateKey(private)

	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	if err := os.Chmod(dir, 0o700); err != nil { // #nosec G302 -- a directory: 0700 is owner-only
		return nil, err
	}

	data := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	if err := os.WriteFile(filepath.Join(dir, keyFile), data, 0o600); err != nil {
		return nil, err
	}

	return newIdentity(private)
}

func newIdentity(private ed25519.PrivateKey) (*Identity, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))

	if err != nil {
		return nil, err
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "rehome node"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(20, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, private.Public(), private)

	if err != nil {
		return nil, err
	}

	leaf, err := x509.ParseCertificate(der)

	if err != nil {
		return nil, err
	}

	return &Identity{Private: private, Cert: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: private, Leaf: leaf}}, nil
}

func (id *Identity) Public() ed25519.PublicKey {
	return id.Private.Public().(ed25519.PublicKey)
}

func EncodeKey(key ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(key)
}

func DecodeKey(s string) (ed25519.PublicKey, error) {
	key, err := base64.StdEncoding.DecodeString(s)

	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, errors.New("not an ed25519 public key")
	}

	return ed25519.PublicKey(key), nil
}

// Fingerprint is a short readable form of a key: 16 hex digits of its SHA-256 in groups of four.
func Fingerprint(key ed25519.PublicKey) string {
	sum := sha256.Sum256(key)
	h := hex.EncodeToString(sum[:8])

	return strings.Join([]string{h[0:4], h[4:8], h[8:12], h[12:16]}, " ")
}

// PeerKey is the ed25519 key of the certificate the other side presented.
func PeerKey(cs tls.ConnectionState) (ed25519.PublicKey, bool) {
	if len(cs.PeerCertificates) == 0 {
		return nil, false
	}

	key, ok := cs.PeerCertificates[0].PublicKey.(ed25519.PublicKey)

	return key, ok
}
