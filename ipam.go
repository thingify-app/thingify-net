package main

import (
	"errors"
	"net/netip"
	"sync"
)

type IPAMPool struct {
	mu        sync.Mutex
	available []netip.Addr
	allocated map[netip.Addr]bool
}

// NewIPAMPool creates a pool starting from a base IP up to a specific capacity
func NewIPAMPool(startIPStr string, count int) (*IPAMPool, error) {
	startIP, err := netip.ParseAddr(startIPStr)
	if err != nil {
		return nil, err
	}

	pool := &IPAMPool{
		available: make([]netip.Addr, 0, count),
		allocated: make(map[netip.Addr]bool),
	}

	curr := startIP
	for i := 0; i < count; i++ {
		pool.available = append(pool.available, curr)
		curr = curr.Next()
	}

	return pool, nil
}

// Lease pulls an available IP from the pool
func (p *IPAMPool) Lease() (netip.Addr, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.available) == 0 {
		return netip.Addr{}, errors.New("ip pool exhausted")
	}

	// Pop the next IP out of the slice
	ip := p.available[0]
	p.available = p.available[1:]
	p.allocated[ip] = true

	return ip, nil
}

// Release returns an IP back to the pool for reuse
func (p *IPAMPool) Release(ip netip.Addr) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.allocated[ip] {
		delete(p.allocated, ip)
		p.available = append(p.available, ip)
	}
}
