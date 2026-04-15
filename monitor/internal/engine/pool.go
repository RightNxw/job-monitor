//go:build solver

package engine

import (
	"sync"

	v8 "github.com/tommie/v8go"
)

// EnginePool manages a pool of pre-warmed V8 isolates to avoid the ~40-60ms
// cost of creating a new isolate on each solve request. Isolates are created
// eagerly at pool init time and returned to the pool after use.
type EnginePool struct {
	mu       sync.Mutex
	isolates chan *v8.Isolate
	size     int
	closed   bool
}

// NewEnginePool creates a pool and pre-warms `size` V8 isolates.
func NewEnginePool(size int) *EnginePool {
	if size < 1 {
		size = 1
	}
	p := &EnginePool{
		isolates: make(chan *v8.Isolate, size*2), // 2x buffer for burst
		size:     size,
	}
	for i := 0; i < size; i++ {
		p.isolates <- v8.NewIsolate()
	}
	return p
}

// Get returns a pre-warmed isolate from the pool. If the pool is empty,
// a new isolate is created on demand (non-blocking).
func (p *EnginePool) Get() *v8.Isolate {
	select {
	case iso := <-p.isolates:
		return iso
	default:
		return v8.NewIsolate()
	}
}

// Put returns an isolate to the pool for reuse. If the pool buffer is full,
// the isolate is disposed to avoid unbounded memory growth.
//
// IMPORTANT: Callers must ensure no V8 contexts or values reference this
// isolate before returning it. Engine.Close() disposes the context first,
// making the isolate safe to recycle.
func (p *EnginePool) Put(iso *v8.Isolate) {
	if iso == nil {
		return
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		iso.Dispose()
		return
	}
	select {
	case p.isolates <- iso:
		// returned to pool
	default:
		// pool full, dispose
		iso.Dispose()
	}
}

// Close disposes all pooled isolates. After Close, Get still works
// (creates new isolates) but Put disposes instead of pooling.
func (p *EnginePool) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()

	// Drain and dispose all pooled isolates
	for {
		select {
		case iso := <-p.isolates:
			iso.Dispose()
		default:
			return
		}
	}
}

// Len returns the number of isolates currently available in the pool.
func (p *EnginePool) Len() int {
	return len(p.isolates)
}
