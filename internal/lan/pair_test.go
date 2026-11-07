package lan

import (
	"regexp"
	"testing"
	"time"

	"github.com/rchehd/rehome/internal/config"
)

type pairSides struct {
	client, server              *Identity
	clientPeer, serverPeer      config.Peer
	clientExporter, srvExporter []byte
}

func newPairSides(t *testing.T) pairSides {
	c, s := testIdentity(t), testIdentity(t)
	exporter := []byte("same-tls-channel-exporter-value!")

	return pairSides{
		client: c, server: s,
		clientPeer: config.Peer{ID: "id-b", Name: "b"}, serverPeer: config.Peer{ID: "id-a", Name: "a"},
		clientExporter: exporter, srvExporter: exporter,
	}
}

// pair runs the whole exchange; it returns what each side pinned and the first error.
func (p pairSides) pair(clientCode, serverCode string) (config.Peer, config.Peer, error) {
	start, client, err := StartPair(clientCode, p.client.Public(), p.server.Public(), p.clientExporter)

	if err != nil {
		return config.Peer{}, config.Peer{}, err
	}

	reply, server, err := AnswerPair(serverCode, p.client.Public(), p.server.Public(), p.srvExporter, start)

	if err != nil {
		return config.Peer{}, config.Peer{}, err
	}

	finish, err := client.Finish(reply, p.clientPeer)

	if err != nil {
		return config.Peer{}, config.Peer{}, err
	}

	byServer, done, err := server.Check(finish, p.serverPeer)

	if err != nil {
		return config.Peer{}, config.Peer{}, err
	}

	byClient, err := client.Done(done)

	return byClient, byServer, err
}

func TestPairSameCodeTrustsBothKeys(t *testing.T) {
	p := newPairSides(t)
	byClient, byServer, err := p.pair("482913", "482913")

	if err != nil {
		t.Fatal(err)
	}

	if byClient.ID != "id-a" || byClient.Key != EncodeKey(p.server.Public()) {
		t.Fatalf("client pinned %+v", byClient)
	}

	if byServer.ID != "id-b" || byServer.Key != EncodeKey(p.client.Public()) {
		t.Fatalf("server pinned %+v", byServer)
	}
}

func TestPairWrongCodeFailsBothSides(t *testing.T) {
	if _, _, err := newPairSides(t).pair("111111", "482913"); err == nil {
		t.Fatal("paired with a wrong code")
	}
}

func TestPairDifferentExporterFails(t *testing.T) {
	p := newPairSides(t)
	p.srvExporter = []byte("another-tls-channel-exporter-val")

	if _, _, err := p.pair("482913", "482913"); err == nil {
		t.Fatal("paired across two TLS channels, as a relaying man in the middle would")
	}
}

func TestPairSwappedKeyFails(t *testing.T) {
	p := newPairSides(t)
	start, client, err := StartPair("482913", p.client.Public(), p.server.Public(), p.clientExporter)

	if err != nil {
		t.Fatal(err)
	}

	reply, _, err := AnswerPair("482913", p.client.Public(), testIdentity(t).Public(), p.srvExporter, start)

	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.Finish(reply, p.clientPeer); err == nil {
		t.Fatal("accepted a reply made for another server key")
	}
}

func TestWindowExpiresAfterFiveMinutes(t *testing.T) {
	now := time.Now()
	w := OpenWindow(now)

	if err := w.Allow(now.Add(4 * time.Minute)); err != nil {
		t.Fatal(err)
	}

	if err := w.Allow(now.Add(5*time.Minute + time.Second)); err == nil {
		t.Fatal("window still open after five minutes")
	}
}

func TestWindowClosesAfterThreeFailures(t *testing.T) {
	now := time.Now()
	w := OpenWindow(now)

	for range 3 {
		w.Fail()
	}

	if err := w.Allow(now); err == nil {
		t.Fatal("window open after three wrong codes")
	}
}

func TestWindowClosesAfterPairing(t *testing.T) {
	now := time.Now()
	w := OpenWindow(now)
	w.Close()

	if err := w.Allow(now); err == nil {
		t.Fatal("code usable twice")
	}
}

func TestNewCodeIsSixDigits(t *testing.T) {
	for range 100 {
		if code := NewCode(); !regexp.MustCompile(`^[0-9]{6}$`).MatchString(code) {
			t.Fatalf("code %q", code)
		}
	}
}
