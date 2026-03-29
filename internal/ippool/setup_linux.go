//go:build linux

package ippool

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
)

// SetupLocalRoutes adds "ip -6 route add local <prefix> dev lo" for each
// prefix in the pool. This tells the kernel to accept incoming packets for
// ANY address within the prefix, which is required for IP_FREEBIND to work
// end-to-end (outgoing bind succeeds AND return packets are delivered).
func (p *Pool) SetupLocalRoutes() error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, pfx := range p.prefixes {
		if pfx.Bits == 128 {
			// Single IP — should already be on the interface
			continue
		}
		cidr := fmt.Sprintf("%s::/%d", pfx.Prefix, pfx.Bits)
		// Check if route already exists
		out, _ := exec.Command("ip", "-6", "route", "show", "table", "local", cidr).CombinedOutput()
		if strings.Contains(string(out), "local") {
			log.Printf("Local route already exists for %s", cidr)
			continue
		}
		cmd := exec.Command("ip", "-6", "route", "add", "local", cidr, "dev", "lo")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to add local route for %s: %w", cidr, err)
		}
		log.Printf("Added local route: %s", cidr)
	}
	return nil
}

// EnsureAddrs binds all pool IPv6 addresses to the specified network interface.
// This is the traditional approach — each address is added via "ip -6 addr add".
// For large pools, prefer SetupLocalRoutes() instead.
func (p *Pool) EnsureAddrs(iface string) (added int, err error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Get currently assigned addresses on the interface
	existing := make(map[string]bool)
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return 0, fmt.Errorf("interface %s not found: %w", iface, err)
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return 0, fmt.Errorf("failed to list addrs on %s: %w", iface, err)
	}
	for _, a := range addrs {
		ip, _, _ := net.ParseCIDR(a.String())
		if ip != nil {
			existing[ip.String()] = true
		}
	}

	for _, pfx := range p.prefixes {
		pfxAddrs := p.allAddrs[pfx.Prefix]
		for _, ip := range pfxAddrs {
			if existing[ip.String()] {
				continue
			}
			cidr := fmt.Sprintf("%s/%d", ip.String(), pfx.Bits)
			cmd := exec.Command("ip", "-6", "addr", "add", cidr, "dev", iface)
			if out, e := cmd.CombinedOutput(); e != nil {
				// "RTNETLINK answers: File exists" is fine — already added
				if !strings.Contains(string(out), "File exists") {
					log.Printf("Warning: failed to add %s to %s: %s", cidr, iface, string(out))
				}
			} else {
				added++
			}
		}
	}
	if added > 0 {
		log.Printf("Bound %d new IPv6 addresses to %s", added, iface)
	}
	return added, nil
}
