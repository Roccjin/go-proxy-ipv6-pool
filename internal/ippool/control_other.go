//go:build !linux

package ippool

import "syscall"

func controlFunc(network, address string, c syscall.RawConn) error {
	return nil
}
