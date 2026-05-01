package dtn

import (
	"testing"
	"time"
)

// TestBundle_EncodeDecode verifies that all Bundle fields round-trip through
// Encode → DecodeBundle without loss.
func TestBundle_EncodeDecode(t *testing.T) {
	original := &Bundle{
		ID:          42_000_000_000,
		Source:      EID{Node: 1, Service: 1},
		Destination: EID{Node: 0, Service: 1},
		CreatedAt:   time.Unix(1_700_000_000, 0).UTC(),
		Lifetime:    2 * time.Hour,
		HopCount:    3,
		Priority:    2,
		Payload:     []byte{0x5A, 0x4C, 0x02, 0x04, 0xDE, 0xAD, 0xBE, 0xEF},
	}

	encoded := original.Encode()
	decoded, err := DecodeBundle(encoded)
	if err != nil {
		t.Fatalf("DecodeBundle failed: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID: got %d, want %d", decoded.ID, original.ID)
	}
	if decoded.Source != original.Source {
		t.Errorf("Source: got %v, want %v", decoded.Source, original.Source)
	}
	if decoded.Destination != original.Destination {
		t.Errorf("Destination: got %v, want %v", decoded.Destination, original.Destination)
	}
	if !decoded.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", decoded.CreatedAt, original.CreatedAt)
	}
	if decoded.Lifetime != original.Lifetime {
		t.Errorf("Lifetime: got %v, want %v", decoded.Lifetime, original.Lifetime)
	}
	if decoded.HopCount != original.HopCount {
		t.Errorf("HopCount: got %d, want %d", decoded.HopCount, original.HopCount)
	}
	if decoded.Priority != original.Priority {
		t.Errorf("Priority: got %d, want %d", decoded.Priority, original.Priority)
	}
	if string(decoded.Payload) != string(original.Payload) {
		t.Errorf("Payload: got %v, want %v", decoded.Payload, original.Payload)
	}
}

// TestBundle_Expired verifies that a bundle with a very short lifetime expires
// immediately after creation.
func TestBundle_Expired(t *testing.T) {
	b := &Bundle{
		ID:        1,
		CreatedAt: time.Now().Add(-10 * time.Millisecond),
		Lifetime:  1 * time.Millisecond,
	}
	if !b.Expired() {
		t.Error("expected bundle with 1ms lifetime to be expired")
	}

	fresh := &Bundle{
		ID:        2,
		CreatedAt: time.Now(),
		Lifetime:  2 * time.Hour,
	}
	if fresh.Expired() {
		t.Error("expected bundle with 2h lifetime to not be expired")
	}
}

// TestStore_PriorityOrder verifies that Next() returns highest-priority bundle first.
func TestStore_PriorityOrder(t *testing.T) {
	s := NewStore()

	now := time.Now()
	s.Put(&Bundle{ID: 10, Priority: 0, CreatedAt: now, Lifetime: 2 * time.Hour, Payload: []byte{0}})
	s.Put(&Bundle{ID: 11, Priority: 2, CreatedAt: now, Lifetime: 2 * time.Hour, Payload: []byte{0}})
	s.Put(&Bundle{ID: 12, Priority: 1, CreatedAt: now, Lifetime: 2 * time.Hour, Payload: []byte{0}})

	first := s.Next()
	if first == nil {
		t.Fatal("Next() returned nil, expected priority-2 bundle")
	}
	if first.Priority != 2 {
		t.Errorf("expected priority 2, got %d", first.Priority)
	}

	s.Remove(first.ID)
	second := s.Next()
	if second == nil {
		t.Fatal("Next() returned nil on second call")
	}
	if second.Priority != 1 {
		t.Errorf("expected priority 1, got %d", second.Priority)
	}

	s.Remove(second.ID)
	third := s.Next()
	if third == nil {
		t.Fatal("Next() returned nil on third call")
	}
	if third.Priority != 0 {
		t.Errorf("expected priority 0, got %d", third.Priority)
	}
}

// TestStore_ExpiredPruned verifies that expired bundles are not returned by Next().
func TestStore_ExpiredPruned(t *testing.T) {
	s := NewStore()

	// An already-expired bundle.
	s.Put(&Bundle{
		ID:        20,
		Priority:  2,
		CreatedAt: time.Now().Add(-10 * time.Second),
		Lifetime:  1 * time.Millisecond,
		Payload:   []byte{0},
	})

	if s.Next() != nil {
		t.Error("expected Next() to return nil when only expired bundles are present")
	}

	pruned := s.PruneExpired()
	if pruned != 1 {
		t.Errorf("PruneExpired: expected 1 removed, got %d", pruned)
	}
	if s.Len() != 0 {
		t.Errorf("Len: expected 0 after prune, got %d", s.Len())
	}
}

// TestStore_Idempotent verifies that putting the same bundle ID twice doesn't
// create a duplicate.
func TestStore_Idempotent(t *testing.T) {
	s := NewStore()

	b := &Bundle{
		ID:        30,
		Priority:  1,
		CreatedAt: time.Now(),
		Lifetime:  2 * time.Hour,
		Payload:   []byte{0xAA},
	}
	s.Put(b)
	s.Put(b) // second Put with same ID should be a no-op

	if s.Len() != 1 {
		t.Errorf("expected Len() == 1 after duplicate Put, got %d", s.Len())
	}
}

// TestEID_String verifies that EID.String() and ParseEID round-trip correctly.
func TestEID_String(t *testing.T) {
	original := EID{Node: 1, Service: 1}
	str := original.String()
	if str != "ipn:1.1" {
		t.Errorf("String(): got %q, want %q", str, "ipn:1.1")
	}

	parsed, err := ParseEID(str)
	if err != nil {
		t.Fatalf("ParseEID(%q) failed: %v", str, err)
	}
	if parsed != original {
		t.Errorf("ParseEID round-trip: got %v, want %v", parsed, original)
	}

	// Additional cases.
	cases := []struct {
		input string
		want  EID
	}{
		{"ipn:0.1", EID{Node: 0, Service: 1}},
		{"ipn:3.2", EID{Node: 3, Service: 2}},
	}
	for _, tc := range cases {
		got, err := ParseEID(tc.input)
		if err != nil {
			t.Errorf("ParseEID(%q) unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseEID(%q): got %v, want %v", tc.input, got, tc.want)
		}
	}

	// Invalid format should return error.
	if _, err := ParseEID("invalid"); err == nil {
		t.Error("ParseEID(\"invalid\"): expected error, got nil")
	}
}
