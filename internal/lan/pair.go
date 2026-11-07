package lan

import (
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"time"

	"filippo.io/cpace"

	"github.com/rchehd/rehome/internal/config"
)

const (
	pairLabel    = "rehome-pair-v1"
	pairWindow   = 5 * time.Minute
	pairFailures = 3
)

var errWrongCode = errors.New("pairing failed: the code does not match")

// NewCode is a fresh 6-digit pairing code.
func NewCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))

	if err != nil {
		panic(err)
	}

	return fmt.Sprintf("%06d", n.Int64())
}

// Window is the time `serve --pair` accepts one pairing with Code.
type Window struct {
	Code     string
	Expires  time.Time
	Failures int
	used     bool
}

func OpenWindow(now time.Time) *Window {
	return &Window{Code: NewCode(), Expires: now.Add(pairWindow)}
}

// Allow reports why a pairing attempt is refused now, if it is.
func (w *Window) Allow(now time.Time) error {
	switch {
	case w == nil || w.used:
		return errors.New("pairing is not open; run rehome serve --pair")
	case now.After(w.Expires):
		return errors.New("the pairing code expired; run rehome serve --pair again")
	case w.Failures >= pairFailures:
		return errors.New("too many wrong codes; run rehome serve --pair again")
	}

	return nil
}

func (w *Window) Fail() { w.Failures++ }

// Close ends the window after a pairing, so its code is never used twice.
func (w *Window) Close() { w.used = true }

// Pairing messages: identities travel only after both sides proved they know the code.
type PairStart struct {
	MsgA []byte `json:"msg_a"`
}

type PairReply struct {
	MsgB    []byte `json:"msg_b"`
	Confirm []byte `json:"confirm"`
}

type PairFinish struct {
	Confirm []byte `json:"confirm"`
	ID      string `json:"id"`
	Name    string `json:"name"`
}

type PairDone struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Confirm []byte `json:"confirm"`
}

type PairClient struct {
	state     *cpace.State
	serverKey ed25519.PublicKey
	msgA      []byte
	key       []byte
}

type PairServer struct {
	clientKey ed25519.PublicKey
	msgA      []byte
	msgB      []byte
	key       []byte
}

// pairContext binds both TLS keys and the TLS channel itself into the exchange.
func pairContext(clientKey, serverKey ed25519.PublicKey, exporter []byte) *cpace.ContextInfo {
	ad := append([]byte(pairLabel), exporter...)
	ad = append(append(ad, clientKey...), serverKey...)

	return cpace.NewContextInfo(EncodeKey(clientKey), EncodeKey(serverKey), ad)
}

func StartPair(code string, clientKey, serverKey ed25519.PublicKey, exporter []byte) (PairStart, *PairClient, error) {
	msgA, state, err := cpace.Start(code, pairContext(clientKey, serverKey, exporter))

	if err != nil {
		return PairStart{}, nil, err
	}

	return PairStart{MsgA: msgA}, &PairClient{state: state, serverKey: serverKey, msgA: msgA}, nil
}

func AnswerPair(code string, clientKey, serverKey ed25519.PublicKey, exporter []byte, s PairStart) (PairReply, *PairServer, error) {
	msgB, shared, err := cpace.Exchange(code, pairContext(clientKey, serverKey, exporter), s.MsgA)

	if err != nil {
		return PairReply{}, nil, err
	}

	key, err := confirmKey(shared)

	if err != nil {
		return PairReply{}, nil, err
	}

	srv := &PairServer{clientKey: clientKey, msgA: s.MsgA, msgB: msgB, key: key}

	return PairReply{MsgB: msgB, Confirm: mac(key, "server", s.MsgA, msgB)}, srv, nil
}

// Finish checks that the server knew the code, then proves the client did and names it.
func (c *PairClient) Finish(r PairReply, self config.Peer) (PairFinish, error) {
	shared, err := c.state.Finish(r.MsgB)

	if err != nil {
		return PairFinish{}, err
	}

	if c.key, err = confirmKey(shared); err != nil {
		return PairFinish{}, err
	}

	if !hmac.Equal(r.Confirm, mac(c.key, "server", c.msgA, r.MsgB)) {
		return PairFinish{}, errWrongCode
	}

	return PairFinish{ID: self.ID, Name: self.Name, Confirm: mac(c.key, "client", c.msgA, r.MsgB, []byte(self.ID), []byte(self.Name))}, nil
}

// Check trusts the client once it proved the code, and names the server in return.
func (s *PairServer) Check(f PairFinish, self config.Peer) (config.Peer, PairDone, error) {
	if !hmac.Equal(f.Confirm, mac(s.key, "client", s.msgA, s.msgB, []byte(f.ID), []byte(f.Name))) {
		return config.Peer{}, PairDone{}, errWrongCode
	}

	peer := config.Peer{ID: f.ID, Name: f.Name, Key: EncodeKey(s.clientKey), PairedAt: time.Now().UTC().Truncate(time.Second)}
	done := PairDone{ID: self.ID, Name: self.Name, Confirm: mac(s.key, "done", []byte(self.ID), []byte(self.Name))}

	return peer, done, nil
}

// Done trusts the server once its name is proven to come from the same exchange.
func (c *PairClient) Done(d PairDone) (config.Peer, error) {
	if c.key == nil || !hmac.Equal(d.Confirm, mac(c.key, "done", []byte(d.ID), []byte(d.Name))) {
		return config.Peer{}, errWrongCode
	}

	return config.Peer{ID: d.ID, Name: d.Name, Key: EncodeKey(c.serverKey), PairedAt: time.Now().UTC().Truncate(time.Second)}, nil
}

func confirmKey(shared []byte) ([]byte, error) {
	return hkdf.Expand(sha256.New, shared, pairLabel+" confirm", 32)
}

// mac authenticates parts as length-prefixed fields under label.
func mac(key []byte, label string, parts ...[]byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(label))

	for _, p := range parts {
		h.Write([]byte{byte(len(p) >> 24), byte(len(p) >> 16), byte(len(p) >> 8), byte(len(p))})
		h.Write(p)
	}

	return h.Sum(nil)
}
