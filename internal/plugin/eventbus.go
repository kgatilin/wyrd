package plugin

import "sync"

// EventBus is the broadcast primitive that backs Host.Subscribe. The
// daemon owns one EventBus per State (created during bootstrap) and
// publishes to it on fsnotify-driven reloads, overlay reloads, and
// target switches.
//
// Delivery is synchronous from the caller's goroutine: Publish does
// not start any new goroutines, and handlers are invoked in
// registration order. Plugins must keep handlers cheap and
// non-blocking; long-running work belongs in a goroutine the
// handler launches.
//
// The bus is safe for concurrent Subscribe / Publish / Unsubscribe.
type EventBus struct {
	mu     sync.RWMutex
	nextID uint64
	subs   map[uint64]func(ModelEvent)
}

// NewEventBus returns an empty bus.
func NewEventBus() *EventBus {
	return &EventBus{subs: make(map[uint64]func(ModelEvent))}
}

// Subscribe registers handler. Returns an Unsubscribe that detaches
// the handler. Calling Unsubscribe twice (or after Close) is safe.
func (b *EventBus) Subscribe(handler func(ModelEvent)) Unsubscribe {
	if handler == nil {
		// Nil handlers are accepted as a no-op so callers can
		// unconditionally Subscribe(nil) and still get a cancel
		// closure.
		return func() {}
	}
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	if b.subs == nil {
		b.subs = make(map[uint64]func(ModelEvent))
	}
	b.subs[id] = handler
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, id)
			b.mu.Unlock()
		})
	}
}

// Publish delivers ev to every subscribed handler in registration
// order. A snapshot of the subscribers is taken under the read lock
// so handlers may unsubscribe themselves without deadlocking.
func (b *EventBus) Publish(ev ModelEvent) {
	b.mu.RLock()
	// Capture a sorted snapshot so dispatch order is deterministic
	// across runs (handlers are keyed by an increasing id, which
	// matches subscription order).
	ids := make([]uint64, 0, len(b.subs))
	for id := range b.subs {
		ids = append(ids, id)
	}
	// Insertion order is preserved by walking ids in ascending
	// numeric order — cheap and avoids a full sort import here.
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j-1] > ids[j]; j-- {
			ids[j-1], ids[j] = ids[j], ids[j-1]
		}
	}
	handlers := make([]func(ModelEvent), 0, len(ids))
	for _, id := range ids {
		handlers = append(handlers, b.subs[id])
	}
	b.mu.RUnlock()

	for _, h := range handlers {
		h(ev)
	}
}

// Len reports the current number of subscribers. Useful for tests.
func (b *EventBus) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
