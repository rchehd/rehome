package lan

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/node"
)

func trust(keys ...ed25519.PublicKey) func(ed25519.PublicKey) bool {
	return func(k ed25519.PublicKey) bool {
		for _, key := range keys {
			if bytes.Equal(k, key) {
				return true
			}
		}

		return false
	}
}

func serve(t *testing.T, s *Server) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")

	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() { _ = s.Serve(ctx, ln) }()

	return ln.Addr().String()
}

type fixture struct {
	root           string
	owner, srv, cl *Identity
	addr           string
	server         *Server
}

// newFixture serves a root holding node a, signed by owner, and recipes, to the client identity.
func newFixture(t *testing.T) *fixture {
	f := &fixture{root: t.TempDir(), owner: testIdentity(t), srv: testIdentity(t), cl: testIdentity(t)}
	signedNode(t, f.root, f.owner, "a", 2)

	if err := os.WriteFile(config.FilePath(f.root), []byte("version: 2\napps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	f.server = &Server{Root: f.root, Self: f.srv, SelfPeer: config.Peer{ID: "id-s", Name: "s"}, Trusted: trust(f.cl.Public())}
	f.addr = serve(t, f.server)

	return f
}

func (f *fixture) client() *Client {
	return &Client{Self: f.cl, Trusted: trust(f.srv.Public())}
}

func TestTrustedPeerReadsIndexNodeAndRecipes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	entries, key, err := f.client().Index(ctx, f.addr)

	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 || entries[0].ID != "a" || entries[0].Revision != 2 || !bytes.Equal(key, f.srv.Public()) {
		t.Fatalf("index %+v", entries)
	}

	data, err := f.client().Node(ctx, f.addr, "a")

	if err != nil {
		t.Fatal(err)
	}

	dst := t.TempDir()

	if err := Extract(bytes.NewReader(data), dst, DefaultLimits); err != nil {
		t.Fatal(err)
	}

	if _, err := VerifyFolder(dst, f.owner.Public()); err != nil {
		t.Fatalf("served node does not verify: %v", err)
	}

	recipes, err := f.client().Recipes(ctx, f.addr)

	if err != nil || !strings.Contains(string(recipes), "version: 2") {
		t.Fatalf("recipes %q: %v", recipes, err)
	}
}

func TestUntrustedPeerGets403(t *testing.T) {
	f := newFixture(t)
	f.server.Trusted = trust()

	if _, _, err := f.client().Index(context.Background(), f.addr); err == nil || !strings.Contains(err.Error(), "not paired") {
		t.Fatalf("unpaired client read the index: %v", err)
	}
}

func TestClientRefusesUnpinnedServer(t *testing.T) {
	f := newFixture(t)
	c := f.client()
	c.Trusted = trust()

	if _, _, err := c.Index(context.Background(), f.addr); err == nil {
		t.Fatal("client talked to a server it never paired with")
	}
}

func TestPairClosedReturns404(t *testing.T) {
	f := newFixture(t)

	if _, err := f.client().Pair(context.Background(), f.addr, "123456", config.Peer{ID: "id-c", Name: "c"}); err == nil {
		t.Fatal("paired while pairing was closed")
	}
}

func TestPairOverTheWirePinsBothSides(t *testing.T) {
	f := newFixture(t)
	f.server.Window = OpenWindow(time.Now())
	var pinned config.Peer
	f.server.OnPair = func(p config.Peer) error { pinned = p; return nil }
	peer, err := f.client().Pair(context.Background(), f.addr, f.server.Window.Code, config.Peer{ID: "id-c", Name: "c"})

	if err != nil {
		t.Fatal(err)
	}

	if peer.ID != "id-s" || peer.Key != EncodeKey(f.srv.Public()) {
		t.Fatalf("client pinned %+v", peer)
	}

	if pinned.ID != "id-c" || pinned.Key != EncodeKey(f.cl.Public()) {
		t.Fatalf("server pinned %+v", pinned)
	}

	if _, err := f.client().Pair(context.Background(), f.addr, f.server.Window.Code, config.Peer{ID: "id-c", Name: "c"}); err == nil {
		t.Fatal("pairing code worked twice")
	}
}

func TestPairWrongCodesCloseTheWindow(t *testing.T) {
	f := newFixture(t)
	f.server.Window = OpenWindow(time.Now())
	wrong := "000000"

	if f.server.Window.Code == wrong {
		wrong = "111111"
	}

	for range 3 {
		if _, err := f.client().Pair(context.Background(), f.addr, wrong, config.Peer{ID: "id-c", Name: "c"}); err == nil {
			t.Fatal("paired with a wrong code")
		}
	}

	if _, err := f.client().Pair(context.Background(), f.addr, f.server.Window.Code, config.Peer{ID: "id-c", Name: "c"}); err == nil {
		t.Fatal("window still open after three attempts")
	}
}

func TestServerRefusesSecretLookingFiles(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.root, node.Dir, "a")
	token := "ghp_" + strings.Repeat("aB3", 12)

	if err := os.WriteFile(filepath.Join(dir, "files", "token.env"), []byte("GITHUB_TOKEN="+token+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, _ := node.Load(dir)

	if err := SignFolder(dir, n, f.owner); err != nil {
		t.Fatal(err)
	}

	if entries, _, err := f.client().Index(context.Background(), f.addr); err != nil || len(entries) != 0 {
		t.Fatalf("shared a node holding a secret: %+v %v", entries, err)
	}
}

func TestServerSkipsFolderChangedSinceSigning(t *testing.T) {
	f := newFixture(t)

	if err := os.WriteFile(filepath.Join(f.root, node.Dir, "a", "files", "zsh", ".zshrc"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if entries, _, err := f.client().Index(context.Background(), f.addr); err != nil || len(entries) != 0 {
		t.Fatalf("shared a folder that no longer matches its signature: %+v %v", entries, err)
	}

	if _, err := f.client().Node(context.Background(), f.addr, "a"); err == nil {
		t.Fatal("served the changed folder")
	}
}

func TestOwnNodeIsServedSignedOnly(t *testing.T) {
	f := newFixture(t)
	unsigned := &node.Node{ID: "b", Name: "b", Path: filepath.Join(f.root, node.Dir, "b")}

	if err := unsigned.Save(); err != nil {
		t.Fatal(err)
	}

	entries, _, err := f.client().Index(context.Background(), f.addr)

	if err != nil || len(entries) != 1 || entries[0].ID != "a" {
		t.Fatalf("index %+v %v", entries, err)
	}
}

func TestServerRejectsTLS12(t *testing.T) {
	f := newFixture(t)
	cfg := f.client().tlsConfig(false)
	cfg.MinVersion, cfg.MaxVersion = tls.VersionTLS12, tls.VersionTLS12

	if conn, err := tls.Dial("tcp", f.addr, cfg); err == nil {
		conn.Close()
		t.Fatal("server accepted TLS 1.2")
	}
}
