// Package rpc pool.go provides a thread-safe client pool for reusing RPC clients.
// This avoids creating new HTTP clients for each request, reducing allocation overhead.
package rpc

import (
	"sync"
	"time"
)

// ClientPool manages a pool of RPC clients keyed by provider name.
// It uses double-checked locking to safely handle concurrent access
// while minimizing lock contention for reads.
type ClientPool struct {
	clients map[string]*Client
	mu      sync.RWMutex
}

// NewClientPool creates a new empty client pool.
func NewClientPool() *ClientPool {
	return &ClientPool{
		clients: make(map[string]*Client),
	}
}

// GetOrCreate returns an existing client for the given provider name,
// or creates a new one if it doesn't exist. This method is thread-safe
// and uses double-checked locking to minimize lock contention.
//
// Parameters:
//   - name: Provider identifier (used as cache key)
//   - url: RPC endpoint URL
//   - timeout: HTTP request timeout
//   - maxRetries: Maximum retry attempts
//
// Returns:
//   - *Client: Existing or newly created client
func (p *ClientPool) GetOrCreate(name, url string, timeout time.Duration, maxRetries int) *Client {
	// First check with read lock (fast path for existing clients)
	p.mu.RLock()
	if client, exists := p.clients[name]; exists {
		p.mu.RUnlock()
		return client
	}
	p.mu.RUnlock()

	// Client not found, acquire write lock
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check: another goroutine may have created it while we waited for the lock
	if client, exists := p.clients[name]; exists {
		return client
	}

	// Create and store new client
	client := NewClient(name, url, timeout, maxRetries)
	p.clients[name] = client
	return client
}

// Get returns an existing client for the given provider name, or nil if not found.
// This method is thread-safe.
func (p *ClientPool) Get(name string) *Client {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.clients[name]
}

// Clear removes all clients from the pool.
// This method is thread-safe.
func (p *ClientPool) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clients = make(map[string]*Client)
}
