//go:build linux

package proxy

import "syscall"

const (
	_SOL_IPV6       = 0x29 // syscall.SOL_IPV6 not exported on all Go versions
	_IPV6_FREEBIND  = 0x4e // IPV6_FREEBIND = 78
	_IP_TRANSPARENT = 19
)

func controlFunc(network, address string, c syscall.RawConn) error {
	return c.Control(func(fd uintptr) {
		// Allow binding to non-local IPv4/IPv6 addresses
		syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_FREEBIND, 1)
		// IP_TRANSPARENT allows receiving packets for non-local addresses
		syscall.SetsockoptInt(int(fd), syscall.SOL_IP, _IP_TRANSPARENT, 1)
		// IPv6-specific freebind
		syscall.SetsockoptInt(int(fd), _SOL_IPV6, _IPV6_FREEBIND, 1)
	})
}
