package plugin

import (
	"sync"
	"testing"
)

func TestEventBus_PublishesToAllSubscribers(t *testing.T) {
	bus := NewEventBus()
	var got1, got2 []ModelEventKind

	bus.Subscribe(func(ev ModelEvent) { got1 = append(got1, ev.Kind) })
	bus.Subscribe(func(ev ModelEvent) { got2 = append(got2, ev.Kind) })

	bus.Publish(ModelEvent{Kind: ModelEventKindOverlayReload})
	bus.Publish(ModelEvent{Kind: ModelEventKindTargetSwitch, Target: "v1"})

	if want := []ModelEventKind{ModelEventKindOverlayReload, ModelEventKindTargetSwitch}; !equalKinds(got1, want) {
		t.Errorf("got1 = %v, want %v", got1, want)
	}
	if want := []ModelEventKind{ModelEventKindOverlayReload, ModelEventKindTargetSwitch}; !equalKinds(got2, want) {
		t.Errorf("got2 = %v, want %v", got2, want)
	}
}

func TestEventBus_UnsubscribeStopsDelivery(t *testing.T) {
	bus := NewEventBus()
	var calls int
	cancel := bus.Subscribe(func(ev ModelEvent) { calls++ })

	bus.Publish(ModelEvent{Kind: ModelEventKindOverlayReload})
	if calls != 1 {
		t.Fatalf("after first publish: calls = %d, want 1", calls)
	}

	cancel()
	bus.Publish(ModelEvent{Kind: ModelEventKindOverlayReload})
	if calls != 1 {
		t.Errorf("after unsubscribe + publish: calls = %d, want 1", calls)
	}

	// Double-unsubscribe must be safe.
	cancel()
}

func TestEventBus_NilHandlerIsSafe(t *testing.T) {
	bus := NewEventBus()
	cancel := bus.Subscribe(nil)
	bus.Publish(ModelEvent{Kind: ModelEventKindPackageReload})
	cancel()
	if got := bus.Len(); got != 0 {
		t.Errorf("Len after publish/cancel = %d, want 0", got)
	}
}

func TestEventBus_ConcurrentPublishAndSubscribe(t *testing.T) {
	bus := NewEventBus()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cancel := bus.Subscribe(func(ev ModelEvent) {})
			cancel()
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Publish(ModelEvent{Kind: ModelEventKindOverlayReload})
		}()
	}
	wg.Wait()
	// Test passes if there's no race / panic; bus.Len() should be 0
	// after every subscriber cancelled.
	if got := bus.Len(); got != 0 {
		t.Errorf("Len after subscribe/cancel storm = %d, want 0", got)
	}
}

func equalKinds(a, b []ModelEventKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
