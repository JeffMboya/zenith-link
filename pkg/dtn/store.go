package dtn

import (
	"sort"
	"sync"
	"time"
)

// Store is a thread-safe in-memory bundle store with TTL expiry.
// Implements the custody/forwarding queue for a DTN node.
type Store struct {
	mu      sync.Mutex
	bundles map[uint64]*Bundle
	// oldest bundle's CreatedAt for the health endpoint — refreshed on mutation.
	oldest time.Time
}

// NewStore creates a new empty Store.
func NewStore() *Store {
	return &Store{
		bundles: make(map[uint64]*Bundle),
	}
}

// Put adds a bundle to the store. If the store already has a bundle with the
// same ID, it is silently dropped (idempotent delivery).
func (s *Store) Put(b *Bundle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.bundles[b.ID]; exists {
		return
	}
	s.bundles[b.ID] = b
}

// Next returns the next bundle to forward: oldest non-expired with highest
// priority first. Returns nil if the store is empty or all bundles are expired.
func (s *Store) Next() *Bundle {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Collect all non-expired bundles.
	candidates := make([]*Bundle, 0, len(s.bundles))
	for _, b := range s.bundles {
		if !b.Expired() {
			candidates = append(candidates, b)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	// Sort: highest priority first, then oldest CreatedAt first (FIFO within priority).
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})

	return candidates[0]
}

// Remove removes a bundle by ID (called after successful delivery).
func (s *Store) Remove(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bundles, id)
}

// PruneExpired removes all expired bundles and returns the count removed.
func (s *Store) PruneExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for id, b := range s.bundles {
		if b.Expired() {
			delete(s.bundles, id)
			count++
		}
	}
	return count
}

// Len returns the number of bundles currently in the store.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bundles)
}

// HasData reports whether the store has at least one non-expired bundle.
func (s *Store) HasData() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range s.bundles {
		if !b.Expired() {
			return true
		}
	}
	return false
}

// OldestAge returns the age of the oldest non-expired bundle in the store.
// Returns 0 if the store has no non-expired bundles.
func (s *Store) OldestAge() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest time.Time
	for _, b := range s.bundles {
		if b.Expired() {
			continue
		}
		if oldest.IsZero() || b.CreatedAt.Before(oldest) {
			oldest = b.CreatedAt
		}
	}
	if oldest.IsZero() {
		return 0
	}
	return time.Since(oldest)
}
