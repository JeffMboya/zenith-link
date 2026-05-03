package orbitron_test

import (
	"encoding/json"
	"testing"

	"github.com/absmach/orbitron/pkg/orbitron"
)

var fullFrame = orbitron.Telemetry{
	Sequence:  1024,
	Timestamp: 1_750_000_000,
	Presence: orbitron.PresencePosition |
		orbitron.PresenceAttitude |
		orbitron.PresenceAngVel |
		orbitron.PresenceBatV |
		orbitron.PresenceSolarV |
		orbitron.PresenceTempC |
		orbitron.PresenceRSSI |
		orbitron.PresenceInference,
	LatE7:          -12864000,
	LonE7:          368172000,
	AltM:           410_000,
	AttRoll:        498,
	AttPitch:       -1000,
	AttYaw:         4065,
	AngVelX:        -15,
	AngVelY:        11,
	AngVelZ:        8,
	BatV:           7600,
	SolarV:         5200,
	TempC:          2000,
	RSSI:           -850,
	InferenceClass: 2,
	InferenceConf:  236,
}

var deltaFrame = orbitron.Telemetry{
	Sequence:  1025,
	Timestamp: 1_750_000_001,
	Presence:  orbitron.PresencePosition | orbitron.PresenceBatV | orbitron.PresenceSolarV,
	LatE7:     -12870000,
	LonE7:     368180000,
	AltM:      410_050,
	BatV:      7600,
	SolarV:    5200,
}

var benchKey = []byte("benchmark-key-32-bytes-xxxxxxxxxxx")

func BenchmarkZenithEncode_Full(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = orbitron.Encode(fullFrame, benchKey)
	}
}

func BenchmarkZenithEncode_Delta(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = orbitron.Encode(deltaFrame, benchKey)
	}
}

func BenchmarkZenithDecode_Full(b *testing.B) {
	frame, _ := orbitron.Encode(fullFrame, benchKey)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = orbitron.Decode(frame, benchKey)
	}
}

func BenchmarkZenithDecode_Delta(b *testing.B) {
	frame, _ := orbitron.Encode(deltaFrame, benchKey)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = orbitron.Decode(frame, benchKey)
	}
}

type jsonTelemetry struct {
	Sequence       uint16 `json:"sequence"`
	Timestamp      uint32 `json:"timestamp"`
	LatE7          int32  `json:"lat_e7"`
	LonE7          int32  `json:"lon_e7"`
	AltM           int32  `json:"alt_m"`
	AttRoll        int16  `json:"att_roll"`
	AttPitch       int16  `json:"att_pitch"`
	AttYaw         int16  `json:"att_yaw"`
	AngVelX        int16  `json:"ang_vel_x"`
	AngVelY        int16  `json:"ang_vel_y"`
	AngVelZ        int16  `json:"ang_vel_z"`
	BatV           uint16 `json:"bat_v"`
	SolarV         uint16 `json:"solar_v"`
	TempC          int16  `json:"temp_c"`
	RSSI           int16  `json:"rssi"`
	InferenceClass uint8  `json:"inference_class"`
	InferenceConf  uint8  `json:"inference_conf"`
}

func makeJSONFrame() jsonTelemetry {
	return jsonTelemetry{
		Sequence: fullFrame.Sequence, Timestamp: fullFrame.Timestamp,
		LatE7: fullFrame.LatE7, LonE7: fullFrame.LonE7, AltM: fullFrame.AltM,
		AttRoll: fullFrame.AttRoll, AttPitch: fullFrame.AttPitch, AttYaw: fullFrame.AttYaw,
		AngVelX: fullFrame.AngVelX, AngVelY: fullFrame.AngVelY, AngVelZ: fullFrame.AngVelZ,
		BatV: fullFrame.BatV, SolarV: fullFrame.SolarV, TempC: fullFrame.TempC,
		RSSI: fullFrame.RSSI, InferenceClass: fullFrame.InferenceClass, InferenceConf: fullFrame.InferenceConf,
	}
}

func BenchmarkJSONMarshal_Full(b *testing.B) {
	f := makeJSONFrame()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(f)
	}
}

func BenchmarkJSONUnmarshal_Full(b *testing.B) {
	f := makeJSONFrame()
	raw, _ := json.Marshal(f)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out jsonTelemetry
		_ = json.Unmarshal(raw, &out)
	}
}

func TestFrameSizeComparison(t *testing.T) {
	zenithFull, _ := orbitron.Encode(fullFrame, benchKey)
	zenithDelta, _ := orbitron.Encode(deltaFrame, benchKey)
	jsonFull, _ := json.Marshal(makeJSONFrame())

	t.Logf("")
	t.Logf("┌──────────────────────────────────────────────┐")
	t.Logf("│  Protocol Wire-Size Comparison               │")
	t.Logf("├─────────────────────────┬────────────────────┤")
	t.Logf("│  Format                 │  Bytes / frame     │")
	t.Logf("├─────────────────────────┼────────────────────┤")
	t.Logf("│  Orbitron v2 (full)  │  %3d               │", len(zenithFull))
	t.Logf("│  Orbitron v2 (delta) │  %3d               │", len(zenithDelta))
	t.Logf("│  JSON (stdlib, full)    │  %3d               │", len(jsonFull))
	t.Logf("├─────────────────────────┼────────────────────┤")
	t.Logf("│  Savings vs JSON        │  %.0f%%              │", float64(len(jsonFull)-len(zenithFull))/float64(len(jsonFull))*100)
	t.Logf("└─────────────────────────┴────────────────────┘")
	t.Logf("")
}
