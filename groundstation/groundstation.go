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
func New(cfg Config) Service {
	if cfg.MaxSubscribers <= 0 {
		cfg.MaxSubscribers = 64
	}
	return &service{cfg: cfg}
}

func (s *service) Receive(ctx context.Context, rawFrame []byte) (zenith.Telemetry, error) {
	tm, err := zenith.Decode(rawFrame, s.cfg.HMACKey)
	if err != nil {
		return zenith.Telemetry{}, err
	}

	s.mu.Lock()
	s.frameCount++
	s.latest = &LatestState{
		Telemetry:   tm,
		ReceivedAt:  time.Now().UTC(),
		FrameNumber: s.frameCount,
	}
	subs := make([]chan zenith.Telemetry, len(s.subscribers))
	copy(subs, s.subscribers)
	s.mu.Unlock()

	// Non-blocking broadcast to internal fan-out channels. These channels are
	// never closed by anyone other than the pipe goroutine in Subscribe (after
	// removal from s.subscribers), so no concurrent-close risk here.
	for _, ch := range subs {
		select {
		case ch <- tm:
		default:
			// Slow subscriber: frame dropped rather than blocking.
		}
	}

	return tm, nil
}

func (s *service) Latest(_ context.Context) (LatestState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.latest == nil {
		return LatestState{}, ErrNoData
	}
	return *s.latest, nil
}

// Subscribe registers a new telemetry subscriber. It returns a public channel
// that the caller owns. A pipe goroutine forwards frames from an internal
// fan-out channel to the public channel, and closes the public channel when
// ctx is cancelled. The internal channel is removed from the broadcast list
// before it is closed, so Receive() never sends to a closed channel.
func (s *service) Subscribe(ctx context.Context) (<-chan zenith.Telemetry, error) {
	s.mu.Lock()
	if len(s.subscribers) >= s.cfg.MaxSubscribers {
		s.mu.Unlock()
		return nil, errors.Wrap(errors.ErrInvalidField,
			errors.New("maximum subscriber limit reached"))
	}
	internal := make(chan zenith.Telemetry, 16)
	s.subscribers = append(s.subscribers, internal)
	s.mu.Unlock()

	public := make(chan zenith.Telemetry, 16)

	go func() {
		defer func() {
			// Remove the internal channel from the broadcast list so future
			// Receive() snapshots do not include it.
			s.mu.Lock()
			for i, sub := range s.subscribers {
				if sub == internal {
					s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
					break
				}
			}
			s.mu.Unlock()
			// Do NOT close internal here. In-flight Receive() goroutines may
			// have already snapshotted it; those sends are non-blocking, so
			// they will either succeed (frames discarded into a now-unread
			// buffer) or drop via the default branch — no concurrent-close panic.
			// internal is garbage-collected once all snapshots release it.
			close(public)
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case tm := <-internal:
				select {
				case public <- tm:
				default:
				}
			}
		}
	}()

	return public, nil
}
