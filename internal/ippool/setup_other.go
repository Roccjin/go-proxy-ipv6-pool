//go:build !linux

package ippool

// SetupLocalRoutes is a no-op on non-Linux platforms.
func (p *Pool) SetupLocalRoutes() error { return nil }

// EnsureAddrs is a no-op on non-Linux platforms.
func (p *Pool) EnsureAddrs(iface string) (int, error) { return 0, nil }
