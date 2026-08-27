package proxy

import (
	"fmt"
	"log"
	"sync"
)

// PortInfo describes a running proxy listener.
type PortInfo struct {
	Addr    string `json:"addr"`
	Type    string `json:"type"` // "http" or "socks5"
	IsMain  bool   `json:"is_main"`
	Running bool   `json:"running"`
}

type portEntry struct {
	info   PortInfo
	stopFn func() error
	statFn func() map[string]int64
}

// PortManager manages dynamic HTTP and SOCKS5 proxy listeners.
type PortManager struct {
	mu    sync.RWMutex
	ports []portEntry
	rt    *Runtime

	nextHTTP   int
	nextSOCKS5 int
}

func NewPortManager(rt *Runtime, httpServer *Server, socks5Server *Socks5Server) *PortManager {
	pm := &PortManager{
		rt:         rt,
		nextHTTP:   30000,
		nextSOCKS5: 31000,
	}
	pm.ports = append(pm.ports, portEntry{
		info:   PortInfo{Addr: httpServer.Addr(), Type: "http", IsMain: true, Running: true},
		stopFn: httpServer.Stop,
		statFn: httpServer.GetStats,
	})
	pm.ports = append(pm.ports, portEntry{
		info:   PortInfo{Addr: socks5Server.Addr(), Type: "socks5", IsMain: true, Running: true},
		stopFn: socks5Server.Stop,
		statFn: socks5Server.GetStats,
	})
	return pm
}

func (pm *PortManager) Ports() []PortInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	out := make([]PortInfo, len(pm.ports))
	for i, p := range pm.ports {
		out[i] = p.info
	}
	return out
}

func (pm *PortManager) Expand(n int) []PortInfo {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	var added []PortInfo
	for i := 0; i < n; i++ {
		httpAddr := fmt.Sprintf("0.0.0.0:%d", pm.nextHTTP)
		httpSrv := NewServer(httpAddr, pm.rt)
		go func(addr string) {
			if err := httpSrv.Start(); err != nil {
				log.Printf("PortManager: HTTP %s failed: %v", addr, err)
			}
		}(httpAddr)
		info := PortInfo{Addr: httpAddr, Type: "http", IsMain: false, Running: true}
		pm.ports = append(pm.ports, portEntry{
			info:   info,
			stopFn: httpSrv.Stop,
			statFn: httpSrv.GetStats,
		})
		added = append(added, info)
		pm.nextHTTP++

		socksAddr := fmt.Sprintf("0.0.0.0:%d", pm.nextSOCKS5)
		socksSrv := NewSocks5Server(socksAddr, pm.rt)
		go func(addr string) {
			if err := socksSrv.Start(); err != nil {
				log.Printf("PortManager: SOCKS5 %s failed: %v", addr, err)
			}
		}(socksAddr)
		sInfo := PortInfo{Addr: socksAddr, Type: "socks5", IsMain: false, Running: true}
		pm.ports = append(pm.ports, portEntry{
			info:   sInfo,
			stopFn: socksSrv.Stop,
			statFn: socksSrv.GetStats,
		})
		added = append(added, sInfo)
		pm.nextSOCKS5++
	}
	return added
}

func (pm *PortManager) StopPort(addr string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for i, p := range pm.ports {
		if p.info.Addr == addr {
			if p.info.IsMain {
				return fmt.Errorf("cannot stop main listener %s", addr)
			}
			if !p.info.Running {
				return fmt.Errorf("listener %s already stopped", addr)
			}
			if err := p.stopFn(); err != nil {
				return err
			}
			pm.ports[i].info.Running = false
			return nil
		}
	}
	return fmt.Errorf("listener %s not found", addr)
}

func (pm *PortManager) AggregateHTTPStats() map[string]int64 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	agg := map[string]int64{}
	for _, p := range pm.ports {
		if p.info.Type == "http" && p.info.Running {
			for k, v := range p.statFn() {
				agg[k] += v
			}
		}
	}
	return agg
}

func (pm *PortManager) AggregateSocks5Stats() map[string]int64 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	agg := map[string]int64{}
	for _, p := range pm.ports {
		if p.info.Type == "socks5" && p.info.Running {
			for k, v := range p.statFn() {
				agg[k] += v
			}
		}
	}
	return agg
}
