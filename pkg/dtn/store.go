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

// DropLowPriority removes the oldest low-priority bundles until the store depth
// is at or below maxDepth. Returns the number of bundles dropped.
// Used by the relay to shed load when cloud cover or outages cause the store to
// grow beyond a useful size.
func (s *Store) DropLowPriority(maxDepth int) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.bundles) <= maxDepth {
		return 0
	}

	type candidate struct {
		id  uint64
		age time.Time
	}
	var low []candidate
	for id, b := range s.bundles {
		if b.Priority < 2 {
			low = append(low, candidate{id, b.CreatedAt})
		}
	}
	sort.Slice(low, func(i, j int) bool {
		return low[i].age.Before(low[j].age)
	})

	dropped := 0
	for _, c := range low {
		if len(s.bundles) <= maxDepth {
			break
		}
		delete(s.bundles, c.id)
		dropped++
	}
	return dropped
}
