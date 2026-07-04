package events

import "sync"

// Bus is a simple in-process fan-out event bus.
// Publish is non-blocking: subscribers with full buffers will miss events.
type Bus struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

// NewBus creates an empty event bus.
func NewBus() *Bus {
	return &Bus{subs: make(map[chan struct{}]struct{})}
}

// Subscribe returns a buffered channel that receives an empty struct on Publish.
func (b *Bus) Subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a channel and closes it.
func (b *Bus) Unsubscribe(ch chan struct{}) {
	b.mu.Lock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
	b.mu.Unlock()
}

// Publish signals all subscribers without blocking.
func (b *Bus) Publish() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- struct{}{}:
		default:
			// subscriber not ready, drop
		}
	}
}

// Global is a package-level singleton event bus for stats updates.
var Global = NewBus()
