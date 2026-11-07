package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/lan"
	"github.com/rchehd/rehome/internal/node"
	"github.com/rchehd/rehome/internal/tui"
	"github.com/rchehd/rehome/internal/ui"
)

const browseTimeout = time.Second

func lanIdentity() (*lan.Identity, error) {
	dir, err := lan.Dir()

	if err != nil {
		return nil, err
	}

	return lan.LoadIdentity(dir)
}

// lanSettings loads the settings with a node id and a config folder, which LAN sync needs even without git.
func lanSettings() (config.Settings, error) {
	s, err := config.LoadSettings()

	if err != nil {
		return s, err
	}

	changed, err := s.EnsureNode()

	if err != nil {
		return s, err
	}

	if s.ClonePath == "" {
		if s.ClonePath, err = config.DefaultClonePath(); err != nil {
			return s, err
		}

		if err := os.MkdirAll(s.ClonePath, 0o755); err != nil {
			return s, err
		}

		changed = true
	}

	if changed {
		return s, s.Save()
	}

	return s, nil
}

// pins holds the paired machines; serve --pair adds to it while it runs.
type pins struct {
	mu sync.Mutex
	s  config.Settings
	// paired reports a machine that just paired, for serve to show.
	paired func(config.Peer)
}

func (p *pins) trusted(key ed25519.PublicKey) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, peer := range p.s.LAN.Paired {
		if k, err := lan.DecodeKey(peer.Key); err == nil && bytes.Equal(k, key) {
			return true
		}
	}

	return false
}

func (p *pins) pin(peer config.Peer) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	fresh, err := config.LoadSettings()

	if err != nil {
		return err
	}

	if err := checkPin(fresh, peer); err != nil {
		return err
	}

	fresh.Pin(peer)
	p.s = fresh

	if err := fresh.Save(); err != nil {
		return err
	}

	if p.paired != nil {
		p.paired(peer)
	}

	return nil
}

// checkPin adds to the settings' checks that a node already recorded here keeps the key it was signed with.
func checkPin(s config.Settings, peer config.Peer) error {
	if err := s.CheckPin(peer); err != nil {
		return err
	}

	nodes, _ := node.List(s.ClonePath)

	if n := node.FindID(nodes, peer.ID); n != nil && n.Key != "" && n.Key != peer.Key {
		return fmt.Errorf("node %s here was signed with another key; run rehome nodes forget %s first if that machine was reinstalled", lan.Clean(n.Name), n.Name)
	}

	return nil
}

func selfPeer(s config.Settings, id *lan.Identity) config.Peer {
	return config.Peer{ID: s.NodeID, Name: s.NodeName, Key: lan.EncodeKey(id.Public())}
}

// share serves this machine's config to paired peers and announces it until ctx ends.
func share(ctx context.Context, s config.Settings, id *lan.Identity, window *lan.Window, log io.Writer, paired func(config.Peer)) (*pins, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.LANPort()))

	if err != nil {
		return nil, fmt.Errorf("cannot listen on port %d: %w", s.LANPort(), err)
	}

	p := &pins{s: s, paired: paired}
	srv := &lan.Server{Root: s.ClonePath, Self: id, SelfPeer: selfPeer(s, id), Trusted: p.trusted, Window: window, OnPair: p.pin, Log: log}

	go func() {
		if err := srv.Serve(ctx, ln); err != nil {
			fmt.Fprintln(log, "serve:", err)
		}
	}()

	go func() {
		if err := lan.Announce(ctx, s.NodeName, s.NodeID, s.LANPort()); err != nil {
			fmt.Fprintln(log, "mDNS announce:", err)
		}
	}()

	return p, nil
}

// peers are the reachable addresses other than this machine's own announcement.
func peers(ctx context.Context, s config.Settings) []lan.Found {
	var out []lan.Found

	for _, f := range lan.Browse(ctx, browseTimeout, s.LAN.Peers) {
		if f.ID != s.NodeID {
			out = append(out, f)
		}
	}

	return out
}

func linkLAN(cmd *cobra.Command, addr, code string) error {
	s, err := lanSettings()

	if err != nil {
		return err
	}

	id, err := lanIdentity()

	if err != nil {
		return err
	}

	manual := addr != ""

	if !manual {
		if addr, err = pickPeer(cmd, s); err != nil {
			return err
		}
	}

	if code == "" {
		if !interactive() {
			return fmt.Errorf("pass the code shown by rehome serve --pair with --code")
		}

		var ok bool

		if code, ok, err = tui.Input("Pairing code", "the 6 digits rehome serve --pair shows on the other machine"); err != nil || !ok {
			return err
		}
	}

	client := &lan.Client{Self: id}
	peer, err := client.Pair(cmd.Context(), addr, strings.TrimSpace(code), selfPeer(s, id))

	if err != nil {
		return err
	}

	if err := checkPin(s, peer); err != nil {
		return err
	}

	s.Pin(peer)

	if manual && !slices.Contains(s.LAN.Peers, addr) {
		s.LAN.Peers = append(s.LAN.Peers, addr)
	}

	if err := s.Save(); err != nil {
		return err
	}

	key, _ := lan.DecodeKey(peer.Key)
	ui.New(cmd.OutOrStdout()).Done(fmt.Sprintf("paired with %s, node %s, key %s; rehome sync now pulls from it", lan.Clean(peer.Name), peer.ID, lan.Fingerprint(key)))

	return nil
}

func pickPeer(cmd *cobra.Command, s config.Settings) (string, error) {
	found := peers(cmd.Context(), s)

	if len(found) == 0 {
		return "", fmt.Errorf("no machine running rehome serve --pair found; pass its address: rehome link lan <host:port>")
	}

	if len(found) == 1 || !interactive() {
		return found[0].Addr, nil
	}

	var choices []ui.Choice

	for _, f := range found {
		choices = append(choices, ui.Choice{Key: f.Addr, Label: lan.Clean(strings.TrimSpace(f.Name + " " + f.Addr))})
	}

	addr, err := ui.Choose("Pair with which machine?", choices...)

	if err == nil && addr == "" {
		err = fmt.Errorf("cancelled")
	}

	return addr, err
}

// firewallHint is the command that opens the LAN port, when a Linux firewall is active.
func firewallHint(port int) string {
	if runtime.GOOS != "linux" {
		return ""
	}

	active := func(unit string) bool {
		return exec.Command("systemctl", "is-active", "--quiet", unit).Run() == nil
	}

	switch {
	case active("ufw"):
		return fmt.Sprintf("sudo ufw allow %d/tcp", port)
	case active("firewalld"):
		return fmt.Sprintf("sudo firewall-cmd --add-port=%d/tcp", port)
	}

	return ""
}
