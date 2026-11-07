package lan

import (
	"bufio"
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
	"time"

	"github.com/rchehd/rehome/internal/config"
)

var errUntrusted = errors.New("this machine is not paired with that address")

// Client reads from pinned peers; Trusted decides which server keys it talks to.
type Client struct {
	Self    *Identity
	Trusted func(ed25519.PublicKey) bool
}

func (c *Client) tlsConfig(pinned bool) *tls.Config {
	// The certificate chain is self-signed by design: trust comes from the pinned key, checked here.
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{c.Self.Cert},
		InsecureSkipVerify: true, // #nosec G402 -- self-signed peers; VerifyConnection checks the pinned key
		VerifyConnection: func(cs tls.ConnectionState) error {
			key, ok := PeerKey(cs)

			if !ok {
				return errUntrusted
			}

			if pinned && (c.Trusted == nil || !c.Trusted(key)) {
				return errUntrusted
			}

			return nil
		},
	}
}

func (c *Client) http(peer *ed25519.PublicKey) *http.Client {
	cfg := c.tlsConfig(true)
	verify := cfg.VerifyConnection
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if err := verify(cs); err != nil {
			return err
		}

		*peer, _ = PeerKey(cs)

		return nil
	}

	return &http.Client{Timeout: requestLimit, Transport: &http.Transport{TLSClientConfig: cfg, DisableKeepAlives: true}}
}

// get reads path from addr, at most limit bytes, and says which pinned key answered.
func (c *Client) get(ctx context.Context, addr, path string, limit int64) ([]byte, ed25519.PublicKey, error) {
	var peer ed25519.PublicKey
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+addr+path, nil)

	if err != nil {
		return nil, nil, err
	}

	resp, err := c.http(&peer).Do(req)

	if err != nil {
		return nil, nil, err
	}

	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))

	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("%s%s: %s", addr, path, bytes.TrimSpace(data))
	}

	if int64(len(data)) > limit {
		return nil, nil, fmt.Errorf("%s%s: answer larger than %d bytes", addr, path, limit)
	}

	return data, peer, nil
}

// Index lists the nodes addr shares and the key it answered with.
func (c *Client) Index(ctx context.Context, addr string) ([]NodeEntry, ed25519.PublicKey, error) {
	data, peer, err := c.get(ctx, addr, "/v1/nodes", 8<<20)

	if err != nil {
		return nil, nil, err
	}

	var entries []NodeEntry

	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, nil, err
	}

	if len(entries) > maxNodes {
		entries = entries[:maxNodes]
	}

	return entries, peer, nil
}

func (c *Client) Node(ctx context.Context, addr, id string) ([]byte, error) {
	data, _, err := c.get(ctx, addr, "/v1/nodes/"+id+".tar.gz", DefaultLimits.TotalBytes+1<<20)

	return data, err
}

func (c *Client) Recipes(ctx context.Context, addr string) ([]byte, error) {
	data, _, err := c.get(ctx, addr, "/v1/recipes", 8<<20)

	return data, err
}

// Pair runs the pairing exchange with addr over one TLS connection and returns the machine to pin.
func (c *Client) Pair(ctx context.Context, addr, code string, self config.Peer) (config.Peer, error) {
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: c.tlsConfig(false)}
	raw, err := dialer.DialContext(ctx, "tcp", addr)

	if err != nil {
		return config.Peer{}, err
	}

	conn := raw.(*tls.Conn)
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(requestLimit))
	cs := conn.ConnectionState()
	serverKey, _ := PeerKey(cs)
	exporter, err := cs.ExportKeyingMaterial(pairLabel, nil, 32)

	if err != nil {
		return config.Peer{}, err
	}

	start, client, err := StartPair(code, c.Self.Public(), serverKey, exporter)

	if err != nil {
		return config.Peer{}, err
	}

	reader := bufio.NewReader(conn)
	var reply PairReply

	if err := roundTrip(conn, reader, addr, "/v1/pair/start", start, &reply); err != nil {
		return config.Peer{}, err
	}

	finish, err := client.Finish(reply, self)

	if err != nil {
		return config.Peer{}, err
	}

	var done PairDone

	if err := roundTrip(conn, reader, addr, "/v1/pair/finish", finish, &done); err != nil {
		return config.Peer{}, err
	}

	return client.Done(done)
}

// roundTrip posts in as JSON on conn and decodes the answer into out; both steps share one channel.
func roundTrip(conn net.Conn, reader *bufio.Reader, addr, path string, in, out any) error {
	body, err := json.Marshal(in)

	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, "https://"+addr+path, bytes.NewReader(body))

	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	if err := req.Write(conn); err != nil {
		return err
	}

	resp, err := http.ReadResponse(reader, req)

	if err != nil {
		return err
	}

	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, pairBody))

	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pairing refused: %s", bytes.TrimSpace(data))
	}

	return json.Unmarshal(data, out)
}
