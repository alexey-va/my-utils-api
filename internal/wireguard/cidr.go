package wireguard

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

type CIDR struct {
	prefix netip.Prefix
}

func ParseCIDR(value string) (CIDR, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil || !prefix.Addr().Is4() {
		return CIDR{}, errors.New("Client network must be an IPv4 CIDR")
	}
	if prefix.Bits() < 16 || prefix.Bits() > 29 {
		return CIDR{}, errors.New("Client CIDR prefix must be between /16 and /29")
	}
	prefix = prefix.Masked()
	if !prefix.Addr().IsPrivate() {
		return CIDR{}, errors.New("Client CIDR must use an RFC1918 private address")
	}
	return CIDR{prefix: prefix}, nil
}

func (c CIDR) String() string { return c.prefix.String() }

func (c CIDR) LastUsableHostOffset() int {
	return (1 << (32 - c.prefix.Bits())) - 2
}

func (c CIDR) HostAddress(offset int) (string, error) {
	if offset < 1 || offset > c.LastUsableHostOffset() {
		return "", errors.New("host offset is outside the usable CIDR range")
	}
	bytes := c.prefix.Addr().As4()
	base := uint32(bytes[0])<<24 | uint32(bytes[1])<<16 | uint32(bytes[2])<<8 | uint32(bytes[3])
	value := base + uint32(offset)
	return fmt.Sprintf("%d.%d.%d.%d", byte(value>>24), byte(value>>16), byte(value>>8), byte(value)), nil
}

func (c CIDR) Contains(address string) bool {
	parsed, err := netip.ParseAddr(address)
	return err == nil && c.prefix.Contains(parsed)
}
