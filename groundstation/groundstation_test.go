package groundstation_test

import (
	"context"
	"testing"
	"time"

	"github.com/absmach/zenith-link/groundstation"
	"github.com/absmach/zenith-link/pkg/errors"
	"github.com/absmach/zenith-link/pkg/zenith"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testKey = []byte("test-hmac-key-32-bytes-padded-xx")

func makeFrame(t *testing.T, tm zenith.Telemetry) []byte {
	t.Helper()
	b, err := zenith.Encode(tm, testKey)
	require.NoError(t, err)
	return b
}

func defaultTelemetry() zenith.Telemetry {
	return zenith.Telemetry{
		Sequence:  1,
		Timestamp: uint32(time.Now().Unix()),
		Presence:  zenith.PresencePosition,
		LatE7:     377_498_000,
		LonE7:     -1_224_194_000,
		AltM:      415_000,
	}
}

// ─── Receive ────────────────────────────────────────────────────────────────

func TestReceive(t *testing.T) {
	tests := []struct {
		desc    string
		frame   func(t *testing.T) []byte
		wantErr error
		check   func(t *testing.T, tm zenith.Telemetry)
	}{
		{
			desc: "valid frame is decoded and stored",
			frame: func(t *testing.T) []byte {
				return makeFrame(t, defaultTelemetry())
			},
			check: func(t *testing.T, tm zenith.Telemetry) {
				assert.Equal(t, uint16(1), tm.Sequence)
				assert.NotZero(t, tm.Presence&zenith.PresencePosition)
				assert.Equal(t, int32(377_498_000), tm.LatE7)
			},
		},
		{
			desc: "HMAC failure on corrupted frame",
			frame: func(t *testing.T) []byte {
				b := makeFrame(t, defaultTelemetry())
				b[len(b)-1] ^= 0xFF
				return b
			},
			wantErr: errors.ErrHMACFailure,
		},
		{
			desc: "frame too short",
			frame: func(t *testing.T) []byte {
				return []byte{0x5A, 0x4C, 0x02}
			},
			wantErr: errors.ErrFrameTooSmall,
		},
		{
			desc: "wrong HMAC key",
			frame: func(t *testing.T) []byte {
				// Encode with a different key.
				b, err := zenith.Encode(defaultTelemetry(), []byte("different-key"))
				require.NoError(t, err)
				return b
			},
			wantErr: errors.ErrHMACFailure,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc := groundstation.New(groundstation.Config{HMACKey: testKey})
			got, err := svc.Receive(context.Background(), tc.frame(t))
			if tc.wantErr != nil {
				assert.True(t, errors.Contains(err, tc.wantErr))
				return
			}
			require.NoError(t, err)
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

// ─── Latest ─────────────────────────────────────────────────────────────────

func TestLatest(t *testing.T) {
	t.Run("returns ErrNoData when no frame received", func(t *testing.T) {
		svc := groundstation.New(groundstation.Config{HMACKey: testKey})
		_, err := svc.Latest(context.Background())
		assert.True(t, errors.Contains(err, groundstation.ErrNoData))
	})

	t.Run("returns last received telemetry", func(t *testing.T) {
		svc := groundstation.New(groundstation.Config{HMACKey: testKey})
		tm := defaultTelemetry()
		frame := makeFrame(t, tm)

		_, err := svc.Receive(context.Background(), frame)
		require.NoError(t, err)

		latest, err := svc.Latest(context.Background())
		require.NoError(t, err)
		assert.Equal(t, tm.Sequence, latest.Telemetry.Sequence)
		assert.Equal(t, uint64(1), latest.FrameNumber)
		assert.True(t, time.Since(latest.ReceivedAt) < time.Second)
	})

	t.Run("frame counter increments on successive receives", func(t *testing.T) {
		svc := groundstation.New(groundstation.Config{HMACKey: testKey})

		for i := range 3 {
			tm := defaultTelemetry()
			tm.Sequence = uint16(i)
			_, err := svc.Receive(context.Background(), makeFrame(t, tm))
			require.NoError(t, err)
		}

		latest, err := svc.Latest(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(3), latest.FrameNumber)
	})
}

// ─── Subscribe ───────────────────────────────────────────────────────────────

func TestSubscribe(t *testing.T) {
	t.Run("subscriber receives broadcast on Receive", func(t *testing.T) {
		svc := groundstation.New(groundstation.Config{HMACKey: testKey})
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		ch, err := svc.Subscribe(ctx)
		require.NoError(t, err)

		tm := defaultTelemetry()
		_, err = svc.Receive(context.Background(), makeFrame(t, tm))
		require.NoError(t, err)

		select {
		case got := <-ch:
			assert.Equal(t, tm.Sequence, got.Sequence)
		case <-ctx.Done():
			t.Fatal("timed out waiting for telemetry broadcast")
		}
	})

	t.Run("channel is closed when context is cancelled", func(t *testing.T) {
		svc := groundstation.New(groundstation.Config{HMACKey: testKey})
		ctx, cancel := context.WithCancel(context.Background())

		ch, err := svc.Subscribe(ctx)
		require.NoError(t, err)

		cancel()

		// The goroutine removes the subscriber and closes the channel asynchronously.
		// Wait up to 100 ms for the close.
		deadline := time.After(100 * time.Millisecond)
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					return // channel closed as expected
				}
			case <-deadline:
				t.Fatal("channel not closed after context cancellation")
			}
		}
	})

	t.Run("concurrent Receive and context cancel does not panic", func(t *testing.T) {
		// Regression: Receive snapshots subscribers then sends outside the lock;
		// a concurrent cancel+close creates a "send on closed channel" window.
		svc := groundstation.New(groundstation.Config{HMACKey: testKey})

		tm := defaultTelemetry()
		frame := makeFrame(t, tm)

		for range 20 {
			ctx, cancel := context.WithCancel(context.Background())
			_, err := svc.Subscribe(ctx)
			require.NoError(t, err)

			go cancel()
			go func() { _, _ = svc.Receive(context.Background(), frame) }()
		}
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("max subscriber limit is enforced", func(t *testing.T) {
		svc := groundstation.New(groundstation.Config{
			HMACKey:        testKey,
			MaxSubscribers: 2,
		})
		ctx := context.Background()

		_, err := svc.Subscribe(ctx)
		require.NoError(t, err)
		_, err = svc.Subscribe(ctx)
		require.NoError(t, err)

		// Third subscriber should fail.
		_, err = svc.Subscribe(ctx)
		assert.Error(t, err)
	})
}
