// Package link provides ISL (Inter-Satellite Link) communication utilities.
// The throttle simulates the constrained 20 KB/s link budget of Satlyt's STL-01 mission.
package link

import (
	"io"
	"time"
)

// Throttle wraps an io.Reader to enforce a maximum read rate in bytes/sec.
// Models the constrained 20 KB/s ISL link in the Zenith-Link demo.
type Throttle struct {
	r         io.Reader
	rateBps   float64
	lastRead  time.Time
	bytesRead int64
}

// NewThrottle wraps r with a rate-limited reader at rateBytesPerSec bytes/second.
func NewThrottle(r io.Reader, rateBytesPerSec float64) *Throttle {
	return &Throttle{
		r:        r,
		rateBps:  rateBytesPerSec,
		lastRead: time.Now(),
	}
}

// Read implements io.Reader. Sleeps to enforce the configured rate limit.
// The sleep is computed as: delay = bytesRead / rateBps - elapsed
func (t *Throttle) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 {
		t.bytesRead += int64(n)
		// How long this many bytes should have taken at the configured rate.
		expected := time.Duration(float64(t.bytesRead) / t.rateBps * float64(time.Second))
		elapsed := time.Since(t.lastRead)
		if delay := expected - elapsed; delay > 0 {
			time.Sleep(delay)
		}
	}
	return n, err
}

// BytesRead returns total bytes read so far.
func (t *Throttle) BytesRead() int64 {
	return t.bytesRead
}

// FrameTransferDuration returns how long it would take to transfer n bytes
// at the given link rate (bytes/sec). Used for display in the UI.
func FrameTransferDuration(sizeBytes int, rateBytesPerSec float64) time.Duration {
	if rateBytesPerSec <= 0 {
		return 0
	}
	return time.Duration(float64(sizeBytes) / rateBytesPerSec * float64(time.Second))
}
