//go:build linux

package ippool

import "syscall"

const (
	_SOL_IPV6       = 0x29
	_IPV6_FREEBIND  = 0x4e
	_IP_TRANSPARENT = 19
)

func controlFunc(network, address string, c syscall.RawConn) error {
	return c.Control(func(fd uintptr) {
		syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_FREEBIND, 1)
		syscall.SetsockoptInt(int(fd), syscall.SOL_IP, _IP_TRANSPARENT, 1)
		syscall.SetsockoptInt(int(fd), _SOL_IPV6, _IPV6_FREEBIND, 1)
	})
}
