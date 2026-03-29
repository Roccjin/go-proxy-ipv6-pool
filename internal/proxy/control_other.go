//go:build !linux

package proxy

import "syscall"

func controlFunc(network, address string, c syscall.RawConn) error {
	return nil
}
