// Package rpc provides HTTP client functionality for Ethereum JSON-RPC endpoints.
package rpc

import (
	"sync"
	"time"
)

// ClientPool manages reusable RPC clients keyed by provider name.
//
// The monitor command is long-running and polls providers repeatedly; pooling avoids
// recreating http.Client instances each cycle.
type ClientPool struct {
	clients map[string]*Client
	mu      sync.RWMutex
}

// NewClientPool creates an empty client pool.
func NewClientPool() *ClientPool {
	return &ClientPool{clients: make(map[string]*Client)}
}

// Get returns a cached client for the given provider name or creates a new one.
//
// Callers should treat the returned client as shared and safe for concurrent use.
func (p *ClientPool) Get(name, url string, timeout time.Duration, maxRetries int) *Client {
	p.mu.RLock()
	if c, ok := p.clients[name]; ok {
		p.mu.RUnlock()
		return c
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	// Double-check after acquiring write lock.
	if c, ok := p.clients[name]; ok {
		return c
	}

	c := NewClient(name, url, timeout, maxRetries)
	p.clients[name] = c
	return c
}
