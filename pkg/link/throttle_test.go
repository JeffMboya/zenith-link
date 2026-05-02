package link_test

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/absmach/satlyt-demo/pkg/link"
)

func TestThrottle_RateLimited(t *testing.T) {
	const (
		dataSize    = 4096
		rateBps     = 4096.0
		minDuration = 800 * time.Millisecond
		maxDuration = 2000 * time.Millisecond
	)

	data := make([]byte, dataSize)
	for i := range data {
		data[i] = byte(i % 256)
	}

	throttled := link.NewThrottle(bytes.NewReader(data), rateBps)

	start := time.Now()
	out, err := io.ReadAll(throttled)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if len(out) != dataSize {
		t.Fatalf("expected %d bytes, got %d", dataSize, len(out))
	}
	if elapsed < minDuration {
		t.Errorf("transfer completed too fast: %v (expected >= %v)", elapsed, minDuration)
	}
	if elapsed > maxDuration {
		t.Errorf("transfer took too long: %v (expected <= %v)", elapsed, maxDuration)
	}
	if throttled.BytesRead() != dataSize {
		t.Errorf("BytesRead() = %d, want %d", throttled.BytesRead(), dataSize)
	}
}

func TestFrameTransferDuration(t *testing.T) {
	const (
		size = 20480
		rate = 20480.0
		want = 1 * time.Second
	)

	got := link.FrameTransferDuration(size, rate)
	if got != want {
		t.Errorf("FrameTransferDuration(%d, %.0f) = %v, want %v", size, rate, got, want)
	}
}
