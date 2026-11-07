package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
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

func TestCheckPinRefusesStandIns(t *testing.T) {
	s := Settings{NodeID: strings.Repeat("a", 32)}
	desk := Peer{ID: strings.Repeat("b", 32), Name: "desk", Key: testKey(t)}
	s.Pin(desk)

	for name, p := range map[string]Peer{
		"bad id":         {ID: "desk", Name: "x", Key: testKey(t)},
		"own id":         {ID: s.NodeID, Name: "x", Key: testKey(t)},
		"pinned id":      {ID: desk.ID, Name: "x", Key: testKey(t)},
		"key as another": {ID: strings.Repeat("c", 32), Name: "x", Key: desk.Key},
	} {
		if err := s.CheckPin(p); err == nil {
			t.Errorf("%s: pinned", name)
		}
	}

	if err := s.CheckPin(Peer{ID: strings.Repeat("c", 32), Name: "laptop", Key: testKey(t)}); err != nil {
		t.Fatalf("refused a new machine: %v", err)
	}
}

func TestUnpairByAmbiguousNameRemovesNothing(t *testing.T) {
	var s Settings
	s.Pin(Peer{ID: "id-a", Name: "laptop", Key: testKey(t)})
	s.Pin(Peer{ID: "id-b", Name: "laptop", Key: testKey(t)})

	if s.Unpair("laptop") || len(s.LAN.Paired) != 2 {
		t.Fatalf("unpaired by an ambiguous name: %+v", s.LAN.Paired)
	}
}
