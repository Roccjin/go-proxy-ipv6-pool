//go:build !linux

package proxy

import "net"

func primeNeighbor(exitIP, destIP net.IP) {}

func connAlreadyDead(conn net.Conn) bool {
	return false
}
