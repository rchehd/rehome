package lan

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rchehd/rehome/internal/config"
	"github.com/rchehd/rehome/internal/node"
	"github.com/rchehd/rehome/internal/secrets"
)

const (
	maxConns     = 3
	maxNodes     = 256
	requestLimit = 30 * time.Second
	pairPending  = 30 * time.Second
	pairBody     = 64 << 10
)

// NodeEntry is one node a server offers; only Signed is trusted, after the client verifies it.
type NodeEntry struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Revision int             `json:"revision"`
	Signed   json.RawMessage `json:"signed"`
}

// Server shares the config clone's signed nodes and recipes with pinned peers, read-only.
type Server struct {
	Root     string
	Self     *Identity
	SelfPeer config.Peer
	Trusted  func(ed25519.PublicKey) bool
	// Window is open only under serve --pair; OnPair pins the machine that paired.
	Window *Window
	OnPair func(config.Peer) error
	Log    io.Writer

	mu      sync.Mutex
	pending *pendingPair
}

type pendingPair struct {
	exporter []byte
	server   *PairServer
	expires  time.Time
}

func (s *Server) TLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{s.Self.Cert}, ClientAuth: tls.RequireAnyClientCert}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/nodes", s.trusted(s.index))
	mux.HandleFunc("GET /v1/nodes/{file}", s.trusted(s.nodeArchive))
	mux.HandleFunc("GET /v1/recipes", s.trusted(s.recipes))
	mux.HandleFunc("POST /v1/pair/start", s.pairStart)
	mux.HandleFunc("POST /v1/pair/finish", s.pairFinish)

	return mux
}

// Serve answers on ln until ctx ends: TLS 1.3, a client certificate, a few connections at a time.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           http.TimeoutHandler(s.Handler(), requestLimit, "request took too long"),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       requestLimit,
		WriteTimeout:      requestLimit,
		IdleTimeout:       requestLimit,
	}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	err := srv.Serve(tls.NewListener(limitListener(ln, maxConns), s.TLSConfig()))

	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}

func (s *Server) trusted(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if key, ok := PeerKey(*r.TLS); !ok || s.Trusted == nil || !s.Trusted(key) {
			http.Error(w, "this machine is not paired", http.StatusForbidden)

			return
		}

		next(w, r)
	}
}

func (s *Server) index(w http.ResponseWriter, _ *http.Request) {
	nodes, err := node.List(s.Root)

	if err != nil {
		http.Error(w, "cannot read nodes", http.StatusInternalServerError)

		return
	}

	entries := []NodeEntry{}

	for _, n := range nodes {
		if len(entries) == maxNodes {
			break
		}

		signed, err := s.shareable(n)

		if err != nil {
			s.logf("not sharing node %s: %v", n.Name, err)

			continue
		}

		entries = append(entries, NodeEntry{ID: n.ID, Name: n.Name, Revision: n.Revision, Signed: signed})
	}

	writeJSON(w, entries)
}

func (s *Server) nodeArchive(w http.ResponseWriter, r *http.Request) {
	id, ok := strings.CutSuffix(r.PathValue("file"), ".tar.gz")
	nodes, err := node.List(s.Root)

	if !ok || err != nil {
		http.NotFound(w, r)

		return
	}

	for _, n := range nodes {
		if n.ID != id {
			continue
		}

		if _, err := s.shareable(n); err != nil {
			http.Error(w, "node is not shareable now", http.StatusConflict)

			return
		}

		data, err := Pack(n.Path)

		if err != nil {
			http.Error(w, "cannot pack node", http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(data)

		return
	}

	http.NotFound(w, r)
}

func (s *Server) recipes(w http.ResponseWriter, _ *http.Request) {
	data, err := os.ReadFile(config.FilePath(s.Root))

	if err != nil {
		http.Error(w, "no recipes", http.StatusNotFound)

		return
	}

	if _, findings := secrets.Redact(config.FileName, data); len(findings) > 0 {
		http.Error(w, "recipes hold secret-looking values; not shared", http.StatusConflict)

		return
	}

	_, _ = w.Write(data)
}

// shareable returns a node's signed manifest if the folder still matches it and holds no secrets.
func (s *Server) shareable(n *node.Node) ([]byte, error) {
	key, err := DecodeKey(n.Key)

	if err != nil {
		return nil, errors.New("not signed")
	}

	if _, err := VerifyFolder(n.Path, key); err != nil {
		return nil, err
	}

	if findings, err := secrets.ScanDir(n.Path); err != nil || len(findings) > 0 {
		return nil, fmt.Errorf("%d secret-looking values", len(findings))
	}

	return ReadSigned(n.Path)
}

func (s *Server) pairStart(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	if s.Window == nil {
		http.NotFound(w, r)

		return
	}

	if err := s.Window.Allow(now); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)

		return
	}

	exporter, clientKey, ok := channel(r)

	if !ok {
		http.Error(w, "no client certificate", http.StatusBadRequest)

		return
	}

	var start PairStart

	if err := readJSON(r, &start); err != nil {
		http.Error(w, "bad pairing request", http.StatusBadRequest)

		return
	}

	// Every start counts, since the reply alone tells the client whether its guess was right; a new one replaces the pending one.
	s.Window.Fail()
	reply, srv, err := AnswerPair(s.Window.Code, clientKey, s.Self.Public(), exporter, start)

	if err != nil {
		http.Error(w, "bad pairing request", http.StatusBadRequest)

		return
	}

	s.pending = &pendingPair{exporter: exporter, server: srv, expires: now.Add(pairPending)}
	writeJSON(w, reply)
}

func (s *Server) pairFinish(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	exporter, _, ok := channel(r)

	if s.Window == nil || s.pending == nil || !ok || !bytes.Equal(s.pending.exporter, exporter) || time.Now().After(s.pending.expires) {
		http.NotFound(w, r)

		return
	}

	pending := s.pending
	s.pending = nil
	var finish PairFinish

	if err := readJSON(r, &finish); err != nil {
		http.Error(w, "bad pairing request", http.StatusBadRequest)

		return
	}

	peer, done, err := pending.server.Check(finish, s.SelfPeer)

	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)

		return
	}

	if s.OnPair != nil {
		if err := s.OnPair(peer); err != nil {
			http.Error(w, "cannot save the pairing", http.StatusInternalServerError)

			return
		}
	}

	s.Window.Close()
	writeJSON(w, done)
}

func (s *Server) logf(format string, args ...any) {
	if s.Log != nil {
		fmt.Fprintf(s.Log, format+"\n", args...)
	}
}

// channel is the TLS exporter value and client key of the connection r came on.
func channel(r *http.Request) ([]byte, ed25519.PublicKey, bool) {
	key, ok := PeerKey(*r.TLS)

	if !ok {
		return nil, nil, false
	}

	exporter, err := r.TLS.ExportKeyingMaterial(pairLabel, nil, 32)

	return exporter, key, err == nil
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, pairBody)).Decode(v)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// limitListener accepts at most n connections at once.
func limitListener(ln net.Listener, n int) net.Listener {
	return &limited{Listener: ln, slots: make(chan struct{}, n)}
}

type limited struct {
	net.Listener
	slots chan struct{}
}

func (l *limited) Accept() (net.Conn, error) {
	l.slots <- struct{}{}
	conn, err := l.Listener.Accept()

	if err != nil {
		<-l.slots

		return nil, err
	}

	return &slotConn{Conn: conn, release: func() { <-l.slots }}, nil
}

type slotConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *slotConn) Close() error {
	c.once.Do(c.release)

	return c.Conn.Close()
}
