// Package groundstation implements the Zenith-Link ground station service.
//
// The ground station service:
//   - Receives raw Zenith-Link v2 binary frames (over UDP or WebSocket)
//   - Verifies HMAC-SHA256 and decodes the frame
//   - Maintains the latest telemetry state for each spacecraft (by SCID)
//   - Broadcasts decoded telemetry to connected WebSocket clients
//   - Provides HTTP endpoints for querying the latest telemetry
package groundstation

import (
	"context"
	"sync"
	"time"

	"github.com/absmach/zenith-link/pkg/errors"
	"github.com/absmach/zenith-link/pkg/zenith"
)

// Service defines the ground station domain operations.
type Service interface {
	// Receive decodes a raw Zenith-Link v2 frame and stores the telemetry.
	// Returns ErrHMACFailure if the frame HMAC is invalid.
	Receive(ctx context.Context, rawFrame []byte) (zenith.Telemetry, error)

	// Latest returns the most recently received telemetry.
	// Returns ErrNoData if no frame has been received yet.
	Latest(ctx context.Context) (LatestState, error)

	// Subscribe registers a channel to receive telemetry updates.
	// The channel is closed when ctx is cancelled.
	Subscribe(ctx context.Context) (<-chan zenith.Telemetry, error)
}

// LatestState holds the most recently received telemetry and its reception time.
type LatestState struct {
	Telemetry   zenith.Telemetry
	ReceivedAt  time.Time
	FrameNumber uint64
}

// Config holds configuration for the ground station service.
type Config struct {
	// HMACKey is the shared key for authenticating incoming Zenith-Link frames.
	HMACKey []byte
	// MaxSubscribers is the maximum number of concurrent WebSocket subscribers.
	MaxSubscribers int
}

var ErrNoData = errors.New("no telemetry received yet")

type service struct {
	cfg        Config
	mu         sync.RWMutex
	latest     *LatestState
	frameCount uint64
	// subscribers holds internal fan-out channels. Receive() sends here; each
	// subscriber's pipe goroutine reads from here and forwards to the public
	// channel returned to callers. Internal channels are never closed by
	// Receive(), eliminating the concurrent-close/concurrent-send race.
	subscribers []chan zenith.Telemetry
}

// New creates a new ground station Service.
