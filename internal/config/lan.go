package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
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
	// OwnRevision is the highest revision of this machine's node seen on peers, so a reinstall continues past it.
	OwnRevision int `yaml:"own_revision,omitempty"`
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

var nodeIDShape = regexp.MustCompile(`^[0-9a-f]{32}$`)

// IsNodeID reports whether s has the shape of a node id, which EnsureNode makes.
func IsNodeID(s string) bool {
	return nodeIDShape.MatchString(s)
}

// CheckPin refuses a pairing that would let one machine stand in for another.
func (s Settings) CheckPin(p Peer) error {
	switch {
	case !IsNodeID(p.ID):
		return fmt.Errorf("the other machine sent an invalid node id %q", p.ID)
	case p.ID == s.NodeID:
		return errors.New("the other machine claims this machine's own node id")
	}

	for _, old := range s.LAN.Paired {
		switch {
		case old.ID == p.ID && old.Key != p.Key:
			return fmt.Errorf("%s is already paired with another key; run rehome nodes unpair %s first", old.Name, old.ID)
		case old.ID != p.ID && old.Key == p.Key:
			return fmt.Errorf("this key is already paired as %s", old.Name)
		}
	}

	return nil
}

// Pin trusts p, replacing an earlier pairing with the same machine.
func (s *Settings) Pin(p Peer) {
	s.LAN.Paired = slices.DeleteFunc(s.LAN.Paired, func(old Peer) bool { return old.ID == p.ID })
	s.LAN.Paired = append(s.LAN.Paired, p)
}

// Unpair forgets the machine with this node id, or with this name when exactly one has it.
func (s *Settings) Unpair(ref string) bool {
	byName := 0

	for _, p := range s.LAN.Paired {
		if p.Name == ref {
			byName++
		}
	}

	before := len(s.LAN.Paired)
	s.LAN.Paired = slices.DeleteFunc(s.LAN.Paired, func(p Peer) bool { return p.ID == ref || p.Name == ref && byName == 1 })

	return len(s.LAN.Paired) < before
}
