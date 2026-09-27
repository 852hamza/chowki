// Package netguard decides which network addresses the gateway may connect
// to. Provider base URLs come from the admin, but a mistake or a malicious
// URL must not let the gateway reach cloud metadata services or, unless
// allowed, the internal network (server-side request forgery).
package netguard

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"syscall"
)

var (
	// metadataPrefixes are cloud metadata services outside the link-local
	// range, which is always blocked anyway (169.254.169.254 serves AWS,
	// Google Cloud, Azure and others).
	metadataPrefixes = []netip.Prefix{
		netip.MustParsePrefix("100.100.100.200/32"), // Alibaba Cloud
		netip.MustParsePrefix("fd00:ec2::254/128"),  // AWS over IPv6
	}
	// sharedPrefix is the carrier-grade NAT range of RFC 6598, which is
	// internal like the private ranges.
	sharedPrefix = netip.MustParsePrefix("100.64.0.0/10")
)

// Policy decides whether the gateway may connect to an address.
type Policy struct {
	// AllowPrivate allows loopback, private (RFC 1918 and RFC 4193) and
	// shared (RFC 6598) addresses, for example a local Ollama server.
	AllowPrivate bool
}

// Check returns an error if the policy forbids connecting to ip.
func (p Policy) Check(ip netip.Addr) error {
	ip = ip.Unmap()
	switch {
	case !ip.IsValid():
		return errors.New("invalid IP address")
	case ip.IsUnspecified() || ip.IsMulticast():
		return fmt.Errorf("%s is not a unicast address", ip)
	case ip.IsLinkLocalUnicast() || inAny(ip, metadataPrefixes):
		return fmt.Errorf("%s is a link-local or cloud metadata address, which is always blocked", ip)
	case !p.AllowPrivate && (ip.IsLoopback() || ip.IsPrivate() || sharedPrefix.Contains(ip)):
		return fmt.Errorf("%s is a private address; set security.allow_private_upstreams to allow it", ip)
	}
	return nil
}

// CheckHost checks the host part of a URL. An IP literal or "localhost" is
// checked right away; other host names are checked by Control once they
// resolve.
func (p Policy) CheckHost(host string) error {
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return p.Check(netip.IPv6Loopback())
	}
	ip, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"))
	if err != nil {
		return nil //nolint:nilerr // a host name, checked at dial time
	}
	return p.Check(ip)
}

// Control is a net.Dialer Control function. It checks the resolved address
// of every connection, so a host name that resolves to a forbidden address,
// including through DNS rebinding, is refused too.
func (p Policy) Control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("netguard: %w", err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("netguard: %w", err)
	}
	if err := p.Check(ip); err != nil {
		return fmt.Errorf("netguard: %w", err)
	}
	return nil
}

func inAny(ip netip.Addr, prefixes []netip.Prefix) bool {
	for _, pfx := range prefixes {
		if pfx.Contains(ip) {
			return true
		}
	}
	return false
}
