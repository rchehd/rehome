package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"slices"
	"time"
)

const DefaultLANPort = 7474

// LAN is how this machine syncs with others on the local network.
type LAN struct {
	Port  int      `yaml:"port,omitempty"`
	Peers []string `yaml:"peers,omitempty"`
	// Paired are the machines this one paired with directly; only their keys are trusted.
	Paired []Peer `yaml:"paired,omitempty"`
}

// Peer is a paired machine; Key is its ed25519 public key in standard base64.
type Peer struct {
	ID       string    `yaml:"id"`
	Name     string    `yaml:"name"`
	Key      string    `yaml:"key"`
	PairedAt time.Time `yaml:"paired_at"`
}

func (s Settings) LANPort() int {
	if s.LAN.Port == 0 {
		return DefaultLANPort
	}

	return s.LAN.Port
}

// PairedKey is the pinned key of the machine with node id, if it was paired directly.
func (s Settings) PairedKey(id string) (ed25519.PublicKey, bool) {
	for _, p := range s.LAN.Paired {
		if p.ID != id {
			continue
		}

		key, err := base64.StdEncoding.DecodeString(p.Key)

		if err != nil || len(key) != ed25519.PublicKeySize {
			return nil, false
		}

		return ed25519.PublicKey(key), true
	}

	return nil, false
}

// Pin trusts p, replacing an earlier pairing with the same machine.
func (s *Settings) Pin(p Peer) {
	s.LAN.Paired = slices.DeleteFunc(s.LAN.Paired, func(old Peer) bool { return old.ID == p.ID })
	s.LAN.Paired = append(s.LAN.Paired, p)
}

// Unpair forgets the machine with this node id or name; it reports whether one was paired.
func (s *Settings) Unpair(ref string) bool {
	before := len(s.LAN.Paired)
	s.LAN.Paired = slices.DeleteFunc(s.LAN.Paired, func(p Peer) bool { return p.ID == ref || p.Name == ref })

	return len(s.LAN.Paired) < before
}
