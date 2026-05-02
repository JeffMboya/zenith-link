// Package link provides ISL (Inter-Satellite Link) communication utilities.
// The throttle simulates the constrained 20 KB/s link budget of Satlyt's STL-01 mission.
package link

import (
	"io"
	"time"
)

type Throttle struct {
	r         io.Reader
	rateBps   float64
	lastRead  time.Time
	bytesRead int64
}

func NewThrottle(r io.Reader, rateBytesPerSec float64) *Throttle {
	return &Throttle{
		r:        r,
		rateBps:  rateBytesPerSec,
		lastRead: time.Now(),
	}
}

func (t *Throttle) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 {
		t.bytesRead += int64(n)

		expected := time.Duration(float64(t.bytesRead) / t.rateBps * float64(time.Second))
		elapsed := time.Since(t.lastRead)
		if delay := expected - elapsed; delay > 0 {
			time.Sleep(delay)
		}
	}
	return n, err
}

func (t *Throttle) BytesRead() int64 {
	return t.bytesRead
}

func FrameTransferDuration(sizeBytes int, rateBytesPerSec float64) time.Duration {
	if rateBytesPerSec <= 0 {
		return 0
	}
	return time.Duration(float64(sizeBytes) / rateBytesPerSec * float64(time.Second))
}
