package cli

import (
	"context"
	"io"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lan"
)

func lanHome(t *testing.T, s config.Settings) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
}

func pairingServer(t *testing.T) (string, *lan.Window) {
	t.Helper()
	id, err := lan.LoadIdentity(t.TempDir())

	if err != nil {
		t.Fatal(err)
	}

	window := lan.OpenWindow(time.Now())
	srv := &lan.Server{Root: t.TempDir(), Self: id, SelfPeer: config.Peer{ID: "id-s", Name: "s"}, Window: window, OnPair: func(config.Peer) error { return nil }}
	ln, err := net.Listen("tcp", "127.0.0.1:0")

	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() { _ = srv.Serve(ctx, ln) }()

	return ln.Addr().String(), window
}

func TestLinkLANKeepsGit(t *testing.T) {
	lanHome(t, config.Settings{RepoURL: "git@example.test:me/config.git", ClonePath: t.TempDir(), NodeID: "id-b", NodeName: "b"})
	addr, window := pairingServer(t)
	cmd := newLinkCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(io.Discard)

	if err := linkLAN(cmd, addr, window.Code); err != nil {
		t.Fatal(err)
	}

	s, err := config.LoadSettings()

	if err != nil {
		t.Fatal(err)
	}

	if s.RepoURL != "git@example.test:me/config.git" {
		t.Fatalf("git link lost: %q", s.RepoURL)
	}

	if _, ok := s.PairedKey("id-s"); !ok || !slices.Contains(s.LAN.Peers, addr) {
		t.Fatalf("pairing not saved: %+v", s.LAN)
	}
}

func TestUnpairDropsPeer(t *testing.T) {
	var s config.Settings
	s.Pin(config.Peer{ID: "id-a", Name: "a", Key: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="})
	lanHome(t, s)
	cmd := newNodesUnpairCmd()
	cmd.SetOut(io.Discard)

	if err := unpair(cmd, "a"); err != nil {
		t.Fatal(err)
	}

	after, err := config.LoadSettings()

	if err != nil || len(after.LAN.Paired) != 0 {
		t.Fatalf("still paired: %+v %v", after.LAN, err)
	}

	if err := unpair(cmd, "a"); err == nil {
		t.Fatal("unpaired a machine that was not paired")
	}
}
