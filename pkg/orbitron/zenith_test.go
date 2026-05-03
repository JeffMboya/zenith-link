package orbitron_test

import (
	"testing"

	"github.com/absmach/orbitron/pkg/errors"
	"github.com/absmach/orbitron/pkg/orbitron"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testKey = []byte("test-hmac-key-32-bytes-padded-xx")

func fullPresence() uint16 {
	return orbitron.PresencePosition |
		orbitron.PresenceAttitude |
		orbitron.PresenceAngVel |
		orbitron.PresenceBatV |
		orbitron.PresenceSolarV |
		orbitron.PresenceTempC |
		orbitron.PresenceRSSI |
		orbitron.PresenceInference
}

func fullTelemetry() orbitron.Telemetry {
	return orbitron.Telemetry{
		Sequence:       42,
		Timestamp:      1_700_000_000,
		Flags:          0,
		Presence:       fullPresence(),
		LatE7:          377_498_000,
		LonE7:          -1_224_194_000,
		AltM:           415_000,
		AttRoll:        150,
		AttPitch:       -200,
		AttYaw:         3600,
		AngVelX:        10,
		AngVelY:        -5,
		AngVelZ:        3,
		BatV:           7400,
		SolarV:         5000,
		TempC:          2150,
		RSSI:           -900,
		InferenceClass: 2,
		InferenceConf:  220,
	}
}

func TestEncode(t *testing.T) {
	tests := []struct {
		desc    string
		tm      orbitron.Telemetry
		key     []byte
		wantErr error
		check   func(t *testing.T, b []byte)
	}{
		{
			desc: "minimal frame — no payload fields",
			tm: orbitron.Telemetry{
				Sequence:  1,
				Timestamp: 1_000_000,
				Presence:  0,
			},
			key: testKey,
			check: func(t *testing.T, b []byte) {
				assert.Equal(t, orbitron.MinFrameSize, len(b), "minimal frame size")

				assert.Equal(t, uint8(0x5A), b[0])
				assert.Equal(t, uint8(0x4C), b[1])

				assert.Equal(t, uint8(2), b[2])
			},
		},
		{
			desc: "full telemetry — all fields present",
			tm:   fullTelemetry(),
			key:  testKey,
			check: func(t *testing.T, b []byte) {

				assert.Equal(t, 78, len(b))
			},
		},
		{
			desc: "inference only",
			tm: orbitron.Telemetry{
				Presence:       orbitron.PresenceInference,
				InferenceClass: 3,
				InferenceConf:  200,
			},
			key: testKey,
			check: func(t *testing.T, b []byte) {

				assert.Equal(t, 46, len(b))
			},
		},
		{
			desc: "position only",
			tm: orbitron.Telemetry{
				Presence: orbitron.PresencePosition,
				LatE7:    100_000_000,
				LonE7:    200_000_000,
				AltM:     500_000,
			},
			key: testKey,
			check: func(t *testing.T, b []byte) {

				assert.Equal(t, 56, len(b))
			},
		},
		{
			desc: "store-and-forward flag set",
			tm: orbitron.Telemetry{
				Flags:    orbitron.FlagStoreFwd,
				Presence: 0,
			},
			key: testKey,
			check: func(t *testing.T, b []byte) {
				assert.Equal(t, orbitron.FlagStoreFwd, b[3])
			},
		},
		{
			desc:    "empty HMAC key returns error",
			tm:      orbitron.Telemetry{},
			key:     []byte{},
			wantErr: errors.ErrHMACFailure,
		},
		{
			desc:    "nil HMAC key returns error",
			tm:      orbitron.Telemetry{},
			key:     nil,
			wantErr: errors.ErrHMACFailure,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := orbitron.Encode(tc.tm, tc.key)
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

func TestDecode(t *testing.T) {
	goodFrame := func() []byte {
		b, err := orbitron.Encode(fullTelemetry(), testKey)
		if err != nil {
			panic(err)
		}
		return b
	}()

	tests := []struct {
		desc    string
		frame   []byte
		key     []byte
		wantErr error
		check   func(t *testing.T, tm orbitron.Telemetry)
	}{
		{
			desc:  "valid full frame decodes correctly",
			frame: goodFrame,
			key:   testKey,
			check: func(t *testing.T, tm orbitron.Telemetry) {
				expected := fullTelemetry()
				assert.Equal(t, expected.Sequence, tm.Sequence)
				assert.Equal(t, expected.Timestamp, tm.Timestamp)
				assert.Equal(t, expected.Presence, tm.Presence)
				assert.Equal(t, expected.LatE7, tm.LatE7)
				assert.Equal(t, expected.LonE7, tm.LonE7)
				assert.Equal(t, expected.AltM, tm.AltM)
				assert.Equal(t, expected.AttRoll, tm.AttRoll)
				assert.Equal(t, expected.AttPitch, tm.AttPitch)
				assert.Equal(t, expected.AttYaw, tm.AttYaw)
				assert.Equal(t, expected.AngVelX, tm.AngVelX)
				assert.Equal(t, expected.AngVelY, tm.AngVelY)
				assert.Equal(t, expected.AngVelZ, tm.AngVelZ)
				assert.Equal(t, expected.BatV, tm.BatV)
				assert.Equal(t, expected.SolarV, tm.SolarV)
				assert.Equal(t, expected.TempC, tm.TempC)
				assert.Equal(t, expected.RSSI, tm.RSSI)
				assert.Equal(t, expected.InferenceClass, tm.InferenceClass)
				assert.Equal(t, expected.InferenceConf, tm.InferenceConf)
			},
		},
		{
			desc:    "wrong HMAC key — HMAC failure",
			frame:   goodFrame,
			key:     []byte("wrong-key"),
			wantErr: errors.ErrHMACFailure,
		},
		{
			desc:    "frame too short",
			frame:   []byte{0x5A, 0x4C, 0x02},
			key:     testKey,
			wantErr: errors.ErrFrameTooSmall,
		},
		{
			desc: "HMAC tampered — byte flipped",
			frame: func() []byte {
				b := make([]byte, len(goodFrame))
				copy(b, goodFrame)
				b[len(b)-1] ^= 0xFF
				return b
			}(),
			key:     testKey,
			wantErr: errors.ErrHMACFailure,
		},
		{
			desc: "wrong magic bytes",
			frame: func() []byte {
				b := make([]byte, len(goodFrame))
				copy(b, goodFrame)

				b[0] = 0x00
				return b
			}(),
			key:     testKey,
			wantErr: errors.ErrHMACFailure,
		},
		{
			desc:    "empty HMAC key returns error",
			frame:   goodFrame,
			key:     []byte{},
			wantErr: errors.ErrHMACFailure,
		},
		{
			desc:    "nil HMAC key returns error",
			frame:   goodFrame,
			key:     nil,
			wantErr: errors.ErrHMACFailure,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := orbitron.Decode(tc.frame, tc.key)
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

func TestEncodeDecodeRoundTrip(t *testing.T) {
	tests := []struct {
		desc string
		tm   orbitron.Telemetry
	}{
		{
			desc: "minimal frame — no fields",
			tm: orbitron.Telemetry{
				Sequence: 1, Timestamp: 1000, Presence: 0,
			},
		},
		{
			desc: "position only",
			tm: orbitron.Telemetry{
				Sequence: 10, Timestamp: 1_700_000_000,
				Presence: orbitron.PresencePosition,
				LatE7:    -338_680_000, LonE7: 1_511_090_000, AltM: 0,
			},
		},
		{
			desc: "attitude only",
			tm: orbitron.Telemetry{
				Presence: orbitron.PresenceAttitude,
				AttRoll:  -3600, AttPitch: 100, AttYaw: 18000,
			},
		},
		{
			desc: "angular velocity only",
			tm: orbitron.Telemetry{
				Presence: orbitron.PresenceAngVel,
				AngVelX:  100, AngVelY: -50, AngVelZ: 25,
			},
		},
		{
			desc: "power fields only",
			tm: orbitron.Telemetry{
				Presence: orbitron.PresenceBatV | orbitron.PresenceSolarV,
				BatV:     3700, SolarV: 4200,
			},
		},
		{
			desc: "environmental only",
			tm: orbitron.Telemetry{
				Presence: orbitron.PresenceTempC | orbitron.PresenceRSSI,
				TempC:    -5050, RSSI: -1100,
			},
		},
		{
			desc: "negative position fields",
			tm: orbitron.Telemetry{
				Presence: orbitron.PresencePosition,
				LatE7:    -900_000_000, LonE7: -1_800_000_000, AltM: -100,
			},
		},
		{
			desc: "full telemetry with all fields",
			tm:   fullTelemetry(),
		},
		{
			desc: "store-and-forward flag set",
			tm: orbitron.Telemetry{
				Flags: orbitron.FlagStoreFwd, Sequence: 255, Timestamp: 0xFFFFFFFF,
				Presence: orbitron.PresencePosition,
				LatE7:    0, LonE7: 0, AltM: 0,
			},
		},
		{
			desc: "inference result only",
			tm: orbitron.Telemetry{
				Sequence: 99, Timestamp: 1_800_000_000,
				Presence:       orbitron.PresenceInference,
				InferenceClass: 4,
				InferenceConf:  245,
			},
		},
		{
			desc: "inference result with position",
			tm: orbitron.Telemetry{
				Presence:       orbitron.PresencePosition | orbitron.PresenceInference,
				LatE7:          -12_864_000,
				LonE7:          36_817_200,
				AltM:           410_000,
				InferenceClass: 1,
				InferenceConf:  180,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			encoded, err := orbitron.Encode(tc.tm, testKey)
			require.NoError(t, err)

			decoded, err := orbitron.Decode(encoded, testKey)
			require.NoError(t, err)

			assert.Equal(t, tc.tm.Sequence, decoded.Sequence)
			assert.Equal(t, tc.tm.Timestamp, decoded.Timestamp)
			assert.Equal(t, tc.tm.Flags, decoded.Flags)
			assert.Equal(t, tc.tm.Presence, decoded.Presence)

			if tc.tm.Presence&orbitron.PresencePosition != 0 {
				assert.Equal(t, tc.tm.LatE7, decoded.LatE7)
				assert.Equal(t, tc.tm.LonE7, decoded.LonE7)
				assert.Equal(t, tc.tm.AltM, decoded.AltM)
			}
			if tc.tm.Presence&orbitron.PresenceAttitude != 0 {
				assert.Equal(t, tc.tm.AttRoll, decoded.AttRoll)
				assert.Equal(t, tc.tm.AttPitch, decoded.AttPitch)
				assert.Equal(t, tc.tm.AttYaw, decoded.AttYaw)
			}
			if tc.tm.Presence&orbitron.PresenceAngVel != 0 {
				assert.Equal(t, tc.tm.AngVelX, decoded.AngVelX)
				assert.Equal(t, tc.tm.AngVelY, decoded.AngVelY)
				assert.Equal(t, tc.tm.AngVelZ, decoded.AngVelZ)
			}
			if tc.tm.Presence&orbitron.PresenceBatV != 0 {
				assert.Equal(t, tc.tm.BatV, decoded.BatV)
			}
			if tc.tm.Presence&orbitron.PresenceSolarV != 0 {
				assert.Equal(t, tc.tm.SolarV, decoded.SolarV)
			}
			if tc.tm.Presence&orbitron.PresenceTempC != 0 {
				assert.Equal(t, tc.tm.TempC, decoded.TempC)
			}
			if tc.tm.Presence&orbitron.PresenceRSSI != 0 {
				assert.Equal(t, tc.tm.RSSI, decoded.RSSI)
			}
			if tc.tm.Presence&orbitron.PresenceInference != 0 {
				assert.Equal(t, tc.tm.InferenceClass, decoded.InferenceClass)
				assert.Equal(t, tc.tm.InferenceConf, decoded.InferenceConf)
			}
		})
	}
}

func TestInferenceClassName(t *testing.T) {
	tests := []struct {
		class uint8
		want  string
	}{
		{0, "NOMINAL"},
		{1, "POWER_ANOMALY"},
		{2, "THERMAL_EVENT"},
		{3, "ATTITUDE_INSTABILITY"},
		{4, "RF_DEGRADATION"},
		{5, "ECLIPSE_ENTRY"},
		{6, "ECLIPSE_COMPUTE"},
		{7, "UNKNOWN"},
		{255, "UNKNOWN"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, orbitron.InferenceClassName(tc.class))
		})
	}
}
