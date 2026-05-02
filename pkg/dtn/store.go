package dtn

import (
	"sort"
	"sync"
	"time"
)

type Store struct {
	mu      sync.Mutex
	bundles map[uint64]*Bundle

	oldest time.Time
}

func NewStore() *Store {
	return &Store{
		bundles: make(map[uint64]*Bundle),
	}
}

func (s *Store) Put(b *Bundle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.bundles[b.ID]; exists {
		return
	}
	s.bundles[b.ID] = b
}

func (s *Store) Next() *Bundle {
	s.mu.Lock()
	defer s.mu.Unlock()

	candidates := make([]*Bundle, 0, len(s.bundles))
	for _, b := range s.bundles {
		if !b.Expired() {
			candidates = append(candidates, b)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})

	return candidates[0]
}

func (s *Store) Remove(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bundles, id)
}

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

func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bundles)
}

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
