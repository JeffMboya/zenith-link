package spacecraft_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/absmach/zenith-link/pkg/ccsds/tcframe"
	"github.com/absmach/zenith-link/pkg/errors"
	"github.com/absmach/zenith-link/pkg/orbital"
	"github.com/absmach/zenith-link/pkg/zenith"
	"github.com/absmach/zenith-link/spacecraft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testKey = []byte("test-hmac-key-32-bytes-padded-xx")

func issConfig() spacecraft.Config {
	return spacecraft.Config{
		SCID:          0x5A,
		VCID:          0,
		TelemetryAPID: 0x100,
		HMACKey:       testKey,
		Elements: orbital.Elements{
			SemiMajorAxis: 6_788_000,
			Eccentricity:  0.0001,
			Inclination:   51.6 * math.Pi / 180,
			RAAN:          0.0,
			ArgPerigee:    0.0,
			MeanAnomaly:   0.0,
			Epoch:         time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		},
	}
}

func TestState(t *testing.T) {
	cfg := issConfig()
	svc := spacecraft.New(cfg)
	ctx := context.Background()
	t0 := cfg.Elements.Epoch

	tests := []struct {
		desc  string
		t     time.Time
		check func(t *testing.T, s spacecraft.State)
	}{
		{
			desc: "state at epoch has valid geodetic position",
			t:    t0,
			check: func(t *testing.T, s spacecraft.State) {
				assert.InDelta(t, 409_863, s.Geodetic.AltitudeM, 5_000)
				assert.GreaterOrEqual(t, s.Geodetic.LatitudeDeg, -90.0)
				assert.LessOrEqual(t, s.Geodetic.LatitudeDeg, 90.0)
				assert.GreaterOrEqual(t, s.Geodetic.LongitudeDeg, -180.0)
				assert.LessOrEqual(t, s.Geodetic.LongitudeDeg, 180.0)
				assert.Equal(t, t0, s.Time)
			},
		},
		{
			desc: "state after 45 minutes has valid position",
			t:    t0.Add(45 * time.Minute),
			check: func(t *testing.T, s spacecraft.State) {
				assert.InDelta(t, 409_863, s.Geodetic.AltitudeM, 10_000)
			},
		},
		{
			desc: "ECI magnitude equals semi-major axis",
			t:    t0,
			check: func(t *testing.T, s spacecraft.State) {
				r := math.Sqrt(s.ECI.X*s.ECI.X + s.ECI.Y*s.ECI.Y + s.ECI.Z*s.ECI.Z)
				assert.InDelta(t, cfg.Elements.SemiMajorAxis, r, 1000)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := svc.State(ctx, tc.t)
			require.NoError(t, err)
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

func TestTelemetry(t *testing.T) {
	cfg := issConfig()
	svc := spacecraft.New(cfg)
	ctx := context.Background()
	t0 := cfg.Elements.Epoch

	tests := []struct {
		desc  string
		t     time.Time
		check func(t *testing.T, tm zenith.Telemetry)
	}{
		{
			desc: "telemetry has position presence bit",
			t:    t0,
			check: func(t *testing.T, tm zenith.Telemetry) {
				assert.NotZero(t, tm.Presence&zenith.PresencePosition)
			},
		},
		{
			desc: "telemetry timestamp matches query time",
			t:    t0,
			check: func(t *testing.T, tm zenith.Telemetry) {
				assert.Equal(t, uint32(t0.Unix()), tm.Timestamp)
			},
		},
		{
			desc: "sequence increments on successive calls",
			t:    t0,
		},
		{
			desc: "battery voltage in reasonable range",
			t:    t0,
			check: func(t *testing.T, tm zenith.Telemetry) {
				assert.GreaterOrEqual(t, tm.BatV, uint16(3000))
				assert.LessOrEqual(t, tm.BatV, uint16(9000))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := svc.Telemetry(ctx, tc.t)
			require.NoError(t, err)
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}

	t.Run("sequence increments", func(t *testing.T) {
		svc2 := spacecraft.New(cfg)
		tm1, err := svc2.Telemetry(ctx, t0)
		require.NoError(t, err)
		tm2, err := svc2.Telemetry(ctx, t0)
		require.NoError(t, err)
		assert.Equal(t, tm1.Sequence+1, tm2.Sequence)
	})
}

func TestTelemetryFrame(t *testing.T) {
	cfg := issConfig()
	svc := spacecraft.New(cfg)
	ctx := context.Background()
	t0 := cfg.Elements.Epoch

	tests := []struct {
		desc  string
		t     time.Time
		check func(t *testing.T, b []byte)
	}{
		{
			desc: "frame is decodable with correct HMAC key",
			t:    t0,
			check: func(t *testing.T, b []byte) {
				tm, err := zenith.Decode(b, testKey)
				require.NoError(t, err)
				assert.NotZero(t, tm.Presence&zenith.PresencePosition)
			},
		},
		{
			desc: "frame fails decode with wrong HMAC key",
			t:    t0,
			check: func(t *testing.T, b []byte) {
				_, err := zenith.Decode(b, []byte("wrong"))
				assert.Error(t, err)
			},
		},
		{
			desc: "frame has minimum required size",
			t:    t0,
			check: func(t *testing.T, b []byte) {
				assert.GreaterOrEqual(t, len(b), zenith.MinFrameSize)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := svc.TelemetryFrame(ctx, tc.t)
			require.NoError(t, err)
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

func buildTC(t *testing.T, scid uint16, vcid uint8, data []byte) []byte {
	t.Helper()
	frame := tcframe.TransferFrame{
		Primary: tcframe.PrimaryHeader{
			SCID:                scid,
			VCID:                vcid,
			FrameSequenceNumber: 0,
		},
		DataField: data,
	}
	b, err := tcframe.Encode(frame)
	require.NoError(t, err)
	return b
}

func TestExecuteCommand(t *testing.T) {
	cfg := issConfig()
	ctx := context.Background()
	t0 := cfg.Elements.Epoch

	tests := []struct {
		desc    string
		tcData  []byte
		scid    uint16
		wantErr error
		check   func(t *testing.T, svc spacecraft.Service, res spacecraft.CommandResult)
	}{
		{
			desc:   "CmdInferenceRun accepted and inference appears in next telemetry",
			tcData: []byte{0x01},
			scid:   cfg.SCID,
			check: func(t *testing.T, svc spacecraft.Service, res spacecraft.CommandResult) {
				assert.True(t, res.Accepted)
				assert.Equal(t, spacecraft.CmdInferenceRun, res.CommandID)

				tm, err := svc.Telemetry(ctx, t0)
				require.NoError(t, err)
				assert.NotZero(t, tm.Presence&zenith.PresenceInference)
				assert.Less(t, tm.InferenceClass, uint8(7))
				assert.Greater(t, tm.InferenceConf, uint8(0))
			},
		},
		{
			desc:   "CmdReboot accepted, inference still present via auto-detect",
			tcData: []byte{0x02},
			scid:   cfg.SCID,
			check: func(t *testing.T, svc spacecraft.Service, res spacecraft.CommandResult) {
				assert.True(t, res.Accepted)
				assert.Equal(t, spacecraft.CmdReboot, res.CommandID)

				tm, err := svc.Telemetry(ctx, t0)
				require.NoError(t, err)
				assert.NotZero(t, tm.Presence&zenith.PresenceInference)
				assert.Equal(t, uint16(0), tm.Sequence)
			},
		},
		{
			desc:   "CmdSetMode with payload accepted",
			tcData: []byte{0x03, 0x02},
			scid:   cfg.SCID,
			check: func(t *testing.T, _ spacecraft.Service, res spacecraft.CommandResult) {
				assert.True(t, res.Accepted)
				assert.Equal(t, spacecraft.CmdSetMode, res.CommandID)
			},
		},
		{
			desc:   "CmdSetMode without payload — rejected gracefully",
			tcData: []byte{0x03},
			scid:   cfg.SCID,
			check: func(t *testing.T, _ spacecraft.Service, res spacecraft.CommandResult) {
				assert.False(t, res.Accepted)
			},
		},
		{
			desc:    "unknown command ID returns ErrCommandUnknown",
			tcData:  []byte{0xFF},
			scid:    cfg.SCID,
			wantErr: errors.ErrCommandUnknown,
		},
		{
			desc:    "wrong SCID returns ErrInvalidField",
			tcData:  []byte{0x01},
			scid:    cfg.SCID + 1,
			wantErr: errors.ErrInvalidField,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc := spacecraft.New(cfg)

			if tc.tcData[0] == 0x02 {
				inferFrame := buildTC(t, cfg.SCID, 0, []byte{0x01})
				_, err := svc.ExecuteCommand(ctx, inferFrame)
				require.NoError(t, err)
			}

			rawTC := buildTC(t, tc.scid, 0, tc.tcData)
			res, err := svc.ExecuteCommand(ctx, rawTC)
			if tc.wantErr != nil {
				assert.True(t, errors.Contains(err, tc.wantErr))
				return
			}
			require.NoError(t, err)
			if tc.check != nil {
				tc.check(t, svc, res)
			}
		})
	}
}

func TestWindows(t *testing.T) {
	cfg := issConfig()
	svc := spacecraft.New(cfg)
	ctx := context.Background()

	start := cfg.Elements.Epoch
	end := start.Add(12 * time.Hour)

	t.Run("returns contact windows for Nairobi", func(t *testing.T) {
		windows, err := svc.Windows(ctx, -1.2864, 36.8172, start, end, 5.0)
		require.NoError(t, err)
		assert.NotEmpty(t, windows)
		for _, w := range windows {
			assert.GreaterOrEqual(t, w.MaxElevationDeg, 5.0)
			assert.Positive(t, w.Duration())
		}
	})

	t.Run("invalid elements propagated as error", func(t *testing.T) {
		_, err := svc.Windows(ctx, -1.2864, 36.8172, end, start, 5.0)
		assert.Error(t, err)
	})
}

func TestTMFrame(t *testing.T) {
	cfg := issConfig()
	svc := spacecraft.New(cfg)
	ctx := context.Background()
	t0 := cfg.Elements.Epoch

	tests := []struct {
		desc      string
		frameSize int
		t         time.Time
		wantErr   bool
		check     func(t *testing.T, b []byte)
	}{
		{
			desc:      "valid 512-byte TM frame",
			frameSize: 512,
			t:         t0,
			check: func(t *testing.T, b []byte) {
				assert.Equal(t, 512, len(b))
			},
		},
		{
			desc:      "valid 1024-byte TM frame",
			frameSize: 1024,
			t:         t0,
			check: func(t *testing.T, b []byte) {
				assert.Equal(t, 1024, len(b))
			},
		},
		{
			desc:      "frame size too small returns error",
			frameSize: 5,
			t:         t0,
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := svc.TMFrame(ctx, tc.t, tc.frameSize)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

func TestCmdComputeJob(t *testing.T) {
	cfg := issConfig()
	ctx := context.Background()
	t0 := cfg.Elements.Epoch

	t.Run("JobHealthScan returns HEALTH_SCAN prefix", func(t *testing.T) {
		svc := spacecraft.New(cfg)
		rawTC := buildTC(t, cfg.SCID, 0, []byte{0x04, 0x01})
		res, err := svc.ExecuteCommand(ctx, rawTC)
		require.NoError(t, err)
		assert.True(t, res.Accepted)
		assert.Contains(t, res.Message, "HEALTH_SCAN")
		assert.Contains(t, res.Message, "status=")
	})

	t.Run("JobEclipseForecast returns ECLIPSE_FORECAST prefix", func(t *testing.T) {
		svc := spacecraft.New(cfg)
		rawTC := buildTC(t, cfg.SCID, 0, []byte{0x04, 0x02})
		res, err := svc.ExecuteCommand(ctx, rawTC)
		require.NoError(t, err)
		assert.True(t, res.Accepted)
		assert.Contains(t, res.Message, "ECLIPSE_FORECAST")
	})

	t.Run("JobLinkBudget returns LINK_BUDGET prefix with margins", func(t *testing.T) {
		svc := spacecraft.New(cfg)

		_, err := svc.State(ctx, t0)
		require.NoError(t, err)

		rawTC := buildTC(t, cfg.SCID, 0, []byte{0x04, 0x03})
		res, err := svc.ExecuteCommand(ctx, rawTC)
		require.NoError(t, err)
		assert.True(t, res.Accepted)
		assert.Contains(t, res.Message, "LINK_BUDGET")
		assert.Contains(t, res.Message, "best=")
	})

	t.Run("missing payload rejected", func(t *testing.T) {
		svc := spacecraft.New(cfg)
		rawTC := buildTC(t, cfg.SCID, 0, []byte{0x04})
		res, err := svc.ExecuteCommand(ctx, rawTC)
		require.NoError(t, err)
		assert.False(t, res.Accepted)
		assert.Contains(t, res.Message, "requires 1-byte")
	})

	t.Run("unknown job type rejected gracefully", func(t *testing.T) {
		svc := spacecraft.New(cfg)
		rawTC := buildTC(t, cfg.SCID, 0, []byte{0x04, 0xFF})
		res, err := svc.ExecuteCommand(ctx, rawTC)
		require.NoError(t, err)
		assert.False(t, res.Accepted)
		assert.Contains(t, res.Message, "unknown job type")
	})
}

func TestCmdDeployPayload(t *testing.T) {
	cfg := issConfig()
	ctx := context.Background()

	t.Run("valid profile 0x01 accepted", func(t *testing.T) {
		svc := spacecraft.New(cfg)
		rawTC := buildTC(t, cfg.SCID, 0, []byte{0x05, 0x01})
		res, err := svc.ExecuteCommand(ctx, rawTC)
		require.NoError(t, err)
		assert.True(t, res.Accepted)
		assert.Contains(t, res.Message, "anomaly-detector-v1")
		assert.Contains(t, res.Message, "RUNNING")
	})

	t.Run("valid profile 0x02 accepted", func(t *testing.T) {
		svc := spacecraft.New(cfg)
		rawTC := buildTC(t, cfg.SCID, 0, []byte{0x05, 0x02})
		res, err := svc.ExecuteCommand(ctx, rawTC)
		require.NoError(t, err)
		assert.True(t, res.Accepted)
		assert.Contains(t, res.Message, "telemetry-compressor-v1")
	})

	t.Run("unknown profile rejected with valid ID list", func(t *testing.T) {
		svc := spacecraft.New(cfg)
		rawTC := buildTC(t, cfg.SCID, 0, []byte{0x05, 0xFF})
		res, err := svc.ExecuteCommand(ctx, rawTC)
		require.NoError(t, err)
		assert.False(t, res.Accepted)
		assert.Contains(t, res.Message, "unknown profile")
		assert.Contains(t, res.Message, "0x01=")
	})

	t.Run("missing payload rejected", func(t *testing.T) {
		svc := spacecraft.New(cfg)
		rawTC := buildTC(t, cfg.SCID, 0, []byte{0x05})
		res, err := svc.ExecuteCommand(ctx, rawTC)
		require.NoError(t, err)
		assert.False(t, res.Accepted)
	})

	t.Run("PayloadState reflects deployed payload", func(t *testing.T) {
		svc := spacecraft.New(cfg)
		assert.Nil(t, svc.PayloadState())
		rawTC := buildTC(t, cfg.SCID, 0, []byte{0x05, 0x01})
		_, err := svc.ExecuteCommand(ctx, rawTC)
		require.NoError(t, err)
		p := svc.PayloadState()
		require.NotNil(t, p)
		assert.Equal(t, "anomaly-detector-v1", p.Name)
		assert.Equal(t, "RUNNING", p.Status)
	})
}
