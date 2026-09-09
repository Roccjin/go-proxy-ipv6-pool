//go:build linux

package proxy

import (
	"bufio"
	"errors"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var (
	egressOnce  sync.Once
	egressIface *net.Interface
	egressGW    net.IP
)

func primeNeighbor(exitIP, destIP net.IP) {
	if exitIP == nil || exitIP.To16() == nil || exitIP.To4() != nil {
		return
	}
	sendUnsolicitedNA(exitIP)
	nudgeUDP(exitIP, destIP)
}

func connAlreadyDead(conn net.Conn) bool {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return false
	}
	raw, err := tcpConn.SyscallConn()
	if err != nil {
		return false
	}
	dead := false
	_ = raw.Control(func(fd uintptr) {
		socketErr, sockErr := unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_ERROR)
		if sockErr == nil && socketErr != 0 {
			dead = true
			return
		}
		buf := make([]byte, 1)
		n, _, recvErr := unix.Recvfrom(int(fd), buf, unix.MSG_PEEK|unix.MSG_DONTWAIT)
		if recvErr == nil {
			if n == 0 {
				dead = true
			}
			return
		}
		if errors.Is(recvErr, unix.EAGAIN) || errors.Is(recvErr, unix.EWOULDBLOCK) || errors.Is(recvErr, unix.EINTR) {
			return
		}
		dead = true
	})
	return dead
}

func sendUnsolicitedNA(exitIP net.IP) {
	iface, gateway := ipv6Egress()
	if iface == nil {
		return
	}
	payload := encodeNeighborAdvertisement(exitIP, iface.HardwareAddr)
	if len(payload) == 0 {
		return
	}

	fd, err := unix.Socket(unix.AF_INET6, unix.SOCK_RAW, unix.IPPROTO_ICMPV6)
	if err != nil {
		return
	}
	defer unix.Close(fd)

	_ = unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_UNICAST_HOPS, 255)
	_ = unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_MULTICAST_HOPS, 255)
	_ = unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_CHECKSUM, 2)
	_ = unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_FREEBIND, 1)
	_ = unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface.Name)

	bindAddr := &unix.SockaddrInet6{ZoneId: uint32(iface.Index)}
	copy(bindAddr.Addr[:], exitIP.To16())
	_ = unix.Bind(fd, bindAddr)

	destinations := make([]*unix.SockaddrInet6, 0, 2)
	if gateway != nil {
		if ip16 := gateway.To16(); ip16 != nil {
			sa := &unix.SockaddrInet6{ZoneId: uint32(iface.Index)}
			copy(sa.Addr[:], ip16)
			destinations = append(destinations, sa)
		}
	}
	allNodes := &unix.SockaddrInet6{ZoneId: uint32(iface.Index)}
	copy(allNodes.Addr[:], net.ParseIP("ff02::1").To16())
	destinations = append(destinations, allNodes)

	for _, dest := range destinations {
		_ = unix.Sendto(fd, payload, 0, dest)
	}
}

func nudgeUDP(exitIP, destIP net.IP) {
	if destIP == nil || destIP.To16() == nil || destIP.To4() != nil {
		return
	}
	dialer := &net.Dialer{
		LocalAddr: &net.UDPAddr{IP: exitIP},
		Timeout:   200 * time.Millisecond,
		Control:   controlFunc,
	}
	conn, err := dialer.Dial("udp6", net.JoinHostPort(destIP.String(), "53"))
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write([]byte{0x00})
}

func ipv6Egress() (*net.Interface, net.IP) {
	egressOnce.Do(func() {
		egressIface, egressGW = detectIPv6Egress()
	})
	return egressIface, egressGW
}

func detectIPv6Egress() (*net.Interface, net.IP) {
	if iface, gw := defaultRouteFromProc(); iface != nil {
		return iface, gw
	}
	return firstUpIPv6Interface()
}

func defaultRouteFromProc() (*net.Interface, net.IP) {
	file, err := os.Open("/proc/net/ipv6_route")
	if err != nil {
		return nil, nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		gateway, ifaceName, ok := parseDefaultIPv6RouteLine(scanner.Text())
		if !ok {
			continue
		}
		iface, err := net.InterfaceByName(ifaceName)
		if err != nil {
			continue
		}
		return iface, gateway
	}
	return nil, nil
}

func firstUpIPv6Interface() (*net.Interface, net.IP) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, nil
	}
	for i := range ifaces {
		iface := &ifaces[i]
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.To4() != nil || ipNet.IP.IsLinkLocalUnicast() {
				continue
			}
			return iface, nil
		}
	}
	return nil, nil
}
