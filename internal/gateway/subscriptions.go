package gateway

import "sync"

// SubscriptionRegistry tracks active channel subscriptions to prevent duplicates.
type SubscriptionRegistry struct {
	mu       sync.Mutex
	channels map[string]struct{}
	max      int
}

func NewSubscriptionRegistry(max int) *SubscriptionRegistry {
	return &SubscriptionRegistry{
		channels: make(map[string]struct{}),
		max:      max,
	}
}

// Add registers a channel. Returns false if already subscribed or at capacity.
func (r *SubscriptionRegistry) Add(channel string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.channels[channel]; exists {
		return false
	}
	if len(r.channels) >= r.max {
		return false
	}
	r.channels[channel] = struct{}{}
	return true
}

func (r *SubscriptionRegistry) Remove(channel string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.channels, channel)
}

func (r *SubscriptionRegistry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.channels)
}

func (r *SubscriptionRegistry) Has(channel string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.channels[channel]
	return ok
}

func (r *SubscriptionRegistry) All() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.channels))
	for ch := range r.channels {
		out = append(out, ch)
	}
	return out
}

func (r *SubscriptionRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channels = make(map[string]struct{})
}
