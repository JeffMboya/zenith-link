// Package groundstation implements the Orbitron ground station service.
//
// The ground station service:
//   - Receives raw Orbitron v2 binary frames (over UDP or WebSocket)
//   - Verifies HMAC-SHA256 and decodes the frame
//   - Maintains the latest telemetry state for each spacecraft (by SCID)
//   - Broadcasts decoded telemetry to connected WebSocket clients
//   - Provides HTTP endpoints for querying the latest telemetry
package groundstation

import (
	"context"
	"sync"
	"time"

	"github.com/absmach/orbitron/pkg/errors"
	"github.com/absmach/orbitron/pkg/orbitron"
)

type Service interface {
	Receive(ctx context.Context, rawFrame []byte) (orbitron.Telemetry, error)

	Latest(ctx context.Context) (LatestState, error)

	Subscribe(ctx context.Context) (<-chan orbitron.Telemetry, error)
}

type LatestState struct {
	Telemetry   orbitron.Telemetry
	ReceivedAt  time.Time
	FrameNumber uint64
}

type Config struct {
	HMACKey []byte

	MaxSubscribers int
}

var ErrNoData = errors.New("no telemetry received yet")

type service struct {
	cfg        Config
	mu         sync.RWMutex
	latest     *LatestState
	frameCount uint64

	subscribers []chan orbitron.Telemetry
}

func New(cfg Config) Service {
	if cfg.MaxSubscribers <= 0 {
		cfg.MaxSubscribers = 64
	}
	return &service{cfg: cfg}
}

func (s *service) Receive(ctx context.Context, rawFrame []byte) (orbitron.Telemetry, error) {
	tm, err := orbitron.Decode(rawFrame, s.cfg.HMACKey)
	if err != nil {
		return orbitron.Telemetry{}, err
	}

	s.mu.Lock()
	s.frameCount++
	s.latest = &LatestState{
		Telemetry:   tm,
		ReceivedAt:  time.Now().UTC(),
		FrameNumber: s.frameCount,
	}
	subs := make([]chan orbitron.Telemetry, len(s.subscribers))
	copy(subs, s.subscribers)
	s.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- tm:
		default:

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

func (s *service) Subscribe(ctx context.Context) (<-chan orbitron.Telemetry, error) {
	s.mu.Lock()
	if len(s.subscribers) >= s.cfg.MaxSubscribers {
		s.mu.Unlock()
		return nil, errors.Wrap(errors.ErrInvalidField,
			errors.New("maximum subscriber limit reached"))
	}
	internal := make(chan orbitron.Telemetry, 16)
	s.subscribers = append(s.subscribers, internal)
	s.mu.Unlock()

	public := make(chan orbitron.Telemetry, 16)

	go func() {
		defer func() {

			s.mu.Lock()
			for i, sub := range s.subscribers {
				if sub == internal {
					s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
					break
				}
			}
			s.mu.Unlock()

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
