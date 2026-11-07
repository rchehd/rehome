package lan

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/brutella/dnssd"
)

const serviceType = "_rehome._tcp"

// Found is a reachable peer address; ID is only the mDNS hint, identity comes from the pinned key.
type Found struct {
	Addr string
	ID   string
	Name string
}

// Announce advertises this machine on mDNS until ctx ends.
func Announce(ctx context.Context, name, id string, port int) error {
	sv, err := dnssd.NewService(dnssd.Config{Name: name, Type: serviceType, Port: port, Text: map[string]string{"id": id, "v": "1"}})

	if err != nil {
		return err
	}

	rp, err := dnssd.NewResponder()

	if err != nil {
		return err
	}

	if _, err := rp.Add(sv); err != nil {
		return err
	}

	if err := rp.Respond(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}

// Browse returns the peers mDNS finds within timeout plus the configured addresses; mDNS failing still returns those.
func Browse(ctx context.Context, timeout time.Duration, peers []string) []Found {
	return merge(lookup(ctx, timeout), peers)
}

var lookup = func(ctx context.Context, timeout time.Duration) []Found {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var mu sync.Mutex
	var found []Found
	add := func(e dnssd.BrowseEntry) {
		if addr := entryAddr(e); addr != "" {
			mu.Lock()
			found = append(found, Found{Addr: addr, ID: e.Text["id"], Name: e.Name})
			mu.Unlock()
		}
	}
	_ = dnssd.LookupType(ctx, serviceType+".local.", add, func(dnssd.BrowseEntry) {})

	mu.Lock()
	defer mu.Unlock()

	return found
}

// entryAddr prefers an IPv4 address; an IPv6 one keeps its interface zone.
func entryAddr(e dnssd.BrowseEntry) string {
	port := strconv.Itoa(e.Port)

	for _, ip := range e.IPs {
		if ip.To4() != nil {
			return net.JoinHostPort(ip.String(), port)
		}
	}

	for _, ip := range e.IPs {
		host := ip.String()

		if ip.IsLinkLocalUnicast() && e.IfaceName != "" {
			host += "%" + e.IfaceName
		}

		return net.JoinHostPort(host, port)
	}

	return ""
}

func merge(found []Found, peers []string) []Found {
	seen := map[string]bool{}
	var out []Found

	for _, f := range found {
		if !seen[f.Addr] {
			seen[f.Addr] = true
			out = append(out, f)
		}
	}

	for _, p := range peers {
		if !seen[p] {
			seen[p] = true
			out = append(out, Found{Addr: p})
		}
	}

	return out
}
