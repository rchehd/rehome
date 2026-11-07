package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

func testKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)

	if err != nil {
		t.Fatal(err)
	}

	return base64.StdEncoding.EncodeToString(pub)
}

func TestLANPortDefault(t *testing.T) {
	if got := (Settings{}).LANPort(); got != 7474 {
		t.Fatalf("default port = %d", got)
	}

	if got := (Settings{LAN: LAN{Port: 9000}}).LANPort(); got != 9000 {
		t.Fatalf("port = %d", got)
	}
}

func TestPinReplacesAndUnpairByName(t *testing.T) {
	var s Settings
	s.Pin(Peer{ID: "id-a", Name: "laptop", Key: testKey(t)})
	newer := testKey(t)
	s.Pin(Peer{ID: "id-a", Name: "laptop", Key: newer})

	if len(s.LAN.Paired) != 1 || s.LAN.Paired[0].Key != newer {
		t.Fatalf("pin did not replace: %+v", s.LAN.Paired)
	}

	if _, ok := s.PairedKey("id-a"); !ok {
		t.Fatal("pinned key not found")
	}

	if !s.Unpair("laptop") {
		t.Fatal("unpair by name failed")
	}

	if _, ok := s.PairedKey("id-a"); ok {
		t.Fatal("key still trusted after unpair")
	}
}

func TestLinkedWithLANOnly(t *testing.T) {
	s := Settings{ClonePath: "/tmp/c"}

	if s.Linked() {
		t.Fatal("linked without repo or peers")
	}

	s.Pin(Peer{ID: "id-a", Name: "a", Key: testKey(t)})

	if !s.Linked() {
		t.Fatal("not linked with a paired peer")
	}
}
