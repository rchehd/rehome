package lan

import (
	"context"
	"testing"
	"time"
)

func withLookup(t *testing.T, found []Found) {
	t.Helper()
	old := lookup
	lookup = func(context.Context, time.Duration) []Found { return found }
	t.Cleanup(func() { lookup = old })
}

func TestBrowseFallsBackToPeers(t *testing.T) {
	withLookup(t, nil)
	got := Browse(context.Background(), time.Second, []string{"192.168.1.5:7474"})

	if len(got) != 1 || got[0].Addr != "192.168.1.5:7474" || got[0].ID != "" {
		t.Fatalf("peers not returned without mDNS: %+v", got)
	}
}

func TestBrowseDeduplicatesAddresses(t *testing.T) {
	withLookup(t, []Found{{Addr: "192.168.1.5:7474", ID: "id-a"}, {Addr: "192.168.1.5:7474", ID: "id-a"}})
	got := Browse(context.Background(), time.Second, []string{"192.168.1.5:7474", "10.0.0.2:7474"})

	if len(got) != 2 || got[0].ID != "id-a" || got[1].Addr != "10.0.0.2:7474" {
		t.Fatalf("browse %+v", got)
	}
}
