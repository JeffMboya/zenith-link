// Package spacecraft implements the Zenith-Link spacecraft service.
//
// The spacecraft service owns:
//   - Orbital propagation (current geodetic position from Keplerian elements)
//   - Telemetry generation (attitude, power, thermal, RSSI simulation)
//   - CCSDS TM Transfer Frame construction (space packet → TM frame)
//   - Zenith-Link v2 binary frame encoding with HMAC-SHA256
//   - TC uplink command processing (inference, reboot, mode select)
//   - Contact window computation for a configurable ground station
//
// External dependencies: none beyond the standard library + pkg/.
package spacecraft

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/absmach/zenith-link/pkg/ccsds/spacepacket"
	"github.com/absmach/zenith-link/pkg/ccsds/tcframe"
	"github.com/absmach/zenith-link/pkg/ccsds/tmframe"
	"github.com/absmach/zenith-link/pkg/errors"
	"github.com/absmach/zenith-link/pkg/orbital"
	"github.com/absmach/zenith-link/pkg/zenith"
	"github.com/absmach/zenith-link/spacecraft/inference"
)

// CommandID identifies an uplink command type.
type CommandID uint8

const (
	CmdInferenceRun  CommandID = 0x01 // return current onboard health classification
	CmdReboot        CommandID = 0x02 // reset sequence counters and clear inference state
	CmdSetMode       CommandID = 0x03 // set operating mode (1-byte payload: mode ID)
	CmdComputeJob    CommandID = 0x04 // run an onboard compute job (1-byte job type in payload)
	CmdDeployPayload CommandID = 0x05 // deploy an in-orbit payload over the constrained ISL
)

// linkRateBytesPerSec mirrors Satlyt STL-01's 20 KB/s constrained uplink budget.
const linkRateBytesPerSec = 20 * 1024

// DeploymentProfile describes an in-orbit workload deployable over a constrained ISL.
type DeploymentProfile struct {
	ID          uint8
	Name        string
	SizeBytes   int
	Description string
}

var deploymentProfiles = map[uint8]DeploymentProfile{
	0x01: {0x01, "anomaly-detector-v1", 26112, "Statistical health monitor — 26KB Python-equivalent, mirrors Satlyt STL-01 deployment"},
	0x02: {0x02, "telemetry-compressor-v1", 12288, "Delta-compression module for constrained RF downlink"},
	0x03: {0x03, "edge-inference-agent-v1", 8192, "Statistical behavioral classifier — autonomous diagnostic agent, same contract as STL-02 Gemma deployment"},
	0x04: {0x04, "orbit-predictor-v1", 18432, "Autonomous orbit event scheduler — eclipse, perigee, apogee; drives ECLIPSE_COMPUTE windows"},
	0x05: {0x05, "federated-learner-v1", 40960, "Federated gradient accumulator — distributed model updates across satellite network, matches Satlyt federated mesh"},
	0x06: {0x06, "link-optimizer-v1", 15360, "Dynamic downlink scheduler — prioritizes frames by anomaly class and link margin; mirrors STL-01 downlink prioritization"},
}

// DeployedPayload tracks what's currently running on the spacecraft.
type DeployedPayload struct {
	Name       string    `json:"Name"`
	SizeBytes  int       `json:"SizeBytes"`
	Status     string    `json:"Status"`     // "RUNNING" | "FAILED"
	DeployedAt time.Time `json:"DeployedAt"`
	ExecOutput string    `json:"ExecOutput"`
}

// AutonomousEvent records an autonomous decision made by the spacecraft's onboard system.
type AutonomousEvent struct {
	At     time.Time `json:"at"`
	Class  string    `json:"class"`
	Action string    `json:"action"`
}

const maxAutonomousEvents = 20

// Compute job types for CmdComputeJob.
const (
	JobHealthScan      uint8 = 0x01 // scan last 30 frames, return anomaly summary
	JobEclipseForecast uint8 = 0x02 // compute next eclipse entry/exit from current orbit
	JobLinkBudget      uint8 = 0x03 // compute link margin for all ground stations
)

// Command holds a decoded command extracted from a TC Transfer Frame.
type Command struct {
	ID      CommandID
	Payload []byte
}

// CommandResult holds the outcome of an executed command.
type CommandResult struct {
	CommandID CommandID
	Accepted  bool
	Message   string
}

// Service defines the spacecraft domain operations exposed to API layers.
type Service interface {
	// Telemetry returns the current telemetry snapshot at time t.
	Telemetry(ctx context.Context, t time.Time) (zenith.Telemetry, error)

	// TelemetryFrame returns a fully encoded Zenith-Link v2 frame (with HMAC).
	TelemetryFrame(ctx context.Context, t time.Time) ([]byte, error)

	// TMFrame returns a CCSDS TM Transfer Frame carrying the current telemetry
	// as a Space Packet in the data field. frameSize is the total TM frame size
	// in bytes (including primary header, data field, FECF).
	TMFrame(ctx context.Context, t time.Time, frameSize int) ([]byte, error)

	// State returns the current orbital state (ECI + geodetic position).
	State(ctx context.Context, t time.Time) (State, error)

	// ExecuteCommand decodes a raw CCSDS TC Transfer Frame, validates the SCID,
	// and executes the embedded command.
	// Returns ErrCommandUnknown for unrecognised command IDs.
	ExecuteCommand(ctx context.Context, tcFrame []byte) (CommandResult, error)

	// Windows returns contact windows for a ground station at (gsLat, gsLon)
	// over [start, end] with minimum elevation angle minElevDeg.
	Windows(ctx context.Context, gsLat, gsLon float64, start, end time.Time, minElevDeg float64) ([]orbital.ContactWindow, error)

	// Events returns recent autonomous spacecraft events, newest first.
	Events() []AutonomousEvent

	// PayloadState returns the deployed onboard payload, nil if none.
	PayloadState() *DeployedPayload

	// LastResult returns the cached inference result from the most recent telemetry frame.
	// Used by the /inference/state endpoint for per-channel z-scores and pre-fault signals.
	LastResult() inference.Result
}

// State captures the combined orbital and telemetry state.
type State struct {
	Time       time.Time
	ECI        orbital.ECIState
	Geodetic   orbital.GeodeticPosition
	InSunlight bool
}

// Config holds configuration for the spacecraft service.
type Config struct {
	// SCID is the 10-bit CCSDS Spacecraft ID.
	SCID uint16
	// VCID is the 6-bit Virtual Channel ID used in TM frames.
	VCID uint8
	// HMACKey is the shared key for Zenith-Link v2 HMAC-SHA256 authentication.
	HMACKey []byte
	// Elements are the initial Keplerian orbital elements.
	Elements orbital.Elements
	// TelemetryAPID is the APID used for telemetry space packets.
	TelemetryAPID uint16
	// SequenceCount is the initial space-packet sequence count (wraps at 0xFFFF).
	SequenceCount uint16
}

// inferenceState holds the result of the last onboard health classification.
type inferenceState struct {
	class uint8
	conf  uint8
}

type service struct {
	cfg      Config
	mu       sync.Mutex
	detector *inference.Detector

	seqCount uint16
	frameSeq uint16
	mcfc     uint8
	vcfc     uint8
	mode     uint8

	lastInference *inferenceState
	deployedPayload *DeployedPayload

	autonomousEvents []AutonomousEvent // newest-first ring, capped at maxAutonomousEvents
	prevClass        inference.Class
	eclipseComputeAt *time.Time // non-nil while in eclipse compute window
	autonomyOnce     sync.Once
}

// New creates a new spacecraft Service.
func New(cfg Config) Service {
	return &service{
		cfg:      cfg,
		seqCount: cfg.SequenceCount,
		detector: &inference.Detector{},
	}
}

func (s *service) Telemetry(ctx context.Context, t time.Time) (zenith.Telemetry, error) {
	s.autonomyOnce.Do(func() { go s.autonomyLoop() })

	st, err := s.State(ctx, t)
	if err != nil {
		return zenith.Telemetry{}, err
	}

	s.mu.Lock()
	seq := s.nextSeqLocked()
	currentMode := s.mode
	s.mu.Unlock()

	// Safe mode (mode=1) sheds high-rate telemetry to conserve link budget:
	// attitude and angular velocity are dropped, matching real safe-mode behaviour
	// where ADCS spins down to a minimum tumble-arrest rate.
	presence := zenith.PresencePosition |
		zenith.PresenceBatV |
		zenith.PresenceSolarV |
		zenith.PresenceTempC |
		zenith.PresenceRSSI
	if currentMode == 0 {
		presence |= zenith.PresenceAttitude | zenith.PresenceAngVel
	}

	batV := uint16(7400)
	solarV := uint16(0)
	if st.InSunlight {
		solarV = 5200
		batV = 7600
	}

	// Simulate attitude as slow oscillations keyed on mission time.
	// Roll ±5°, Pitch ±10°, Yaw ±180° (slow drift). Units: 0.01° LSB.
	phi := float64(t.Unix()) / 60.0 // one full oscillation per 2π minutes
	attRoll := int16(math.Round(math.Sin(phi*0.7)*500))         // ±5°
	attPitch := int16(math.Round(math.Sin(phi*0.4+1.2)*1000))   // ±10°
	attYaw := int16(math.Round(math.Sin(phi*0.15+0.8) * 18000)) // ±180°
	// Chassis 20°C ±3°C slow oscillation. Units: 0.01°C LSB.
	chassisC := int16(math.Round((20.0 + 3.0*math.Sin(phi*0.3)) * 100))

	// Angular velocity small residuals (reaction wheel noise). Units: 0.01 deg/s.
	angVelX := int16(math.Round(math.Sin(phi*2.1+0.5) * 15)) // ±0.15 deg/s
	angVelY := int16(math.Round(math.Sin(phi*1.7+1.1) * 12)) // ±0.12 deg/s
	angVelZ := int16(math.Round(math.Sin(phi*2.9+2.3) * 8))  // ±0.08 deg/s

	// Deterministic noise injection, keyed on mission time.
	//
	// The base simulation is noiseless sine waves — a purely constant input
	// produces z-score ≈ 0 on every channel and the anomaly detector can never
	// trigger POWER_ANOMALY, THERMAL_EVENT, RF_DEGRADATION, or
	// ATTITUDE_INSTABILITY. These injections add periodic, reproducible fault
	// signatures so all seven health classes are reachable during a demo run.
	//
	// Periods are chosen so anomalies are well-separated from eclipse transitions.
	unix := t.Unix()

	// Background noise — low-amplitude, gives the rolling window non-zero stddev
	// even in quiet periods so thresholds are proportionally meaningful.
	bgNoise := math.Sin(float64(unix)*1.61803) * math.Sin(float64(unix)*2.71828)
	batV = uint16(int(batV) + int(math.Round(bgNoise*12)))   // ±12 mV background
	chassisC += int16(math.Round(bgNoise * 20))               // ±0.20°C background
	rssiBase := -85.0 + bgNoise*1.2                           // ±1.2 dBm background

	// Periodic fault injections — each anomaly class fires for ~20 s every N min.
	// Only inject during sunlight to keep eclipse and power anomalies separate.
	if st.InSunlight {
		// POWER_ANOMALY: 150 mV voltage sag every 10 min → z ≈ -3 against 7600 V baseline
		if unix%600 >= 30 && unix%600 < 52 {
			batV -= 150
		}
		// THERMAL_EVENT: +3.2°C chassis spike every 12 min → z ≈ +3.2
		if unix%720 >= 240 && unix%720 < 258 {
			chassisC += 320 // +3.20°C in 0.01°C units
		}
		// RF_DEGRADATION: −8.5 dBm RSSI drop every 8 min → z ≈ −2.8
		if unix%480 >= 150 && unix%480 < 168 {
			rssiBase -= 8.5
		}
		// ATTITUDE_INSTABILITY: reaction wheel saturation event — 25° pitch offset
		// every 9 min pushes |roll|+|pitch| well above the rolling baseline
		if unix%540 >= 360 && unix%540 < 374 {
			attPitch += 2500 // +25° in 0.01° units
			angVelY += 600   // +6 deg/s transient
		}
	}

	rssiWire := int16(math.Round(rssiBase * 10)) // convert dBm → wire int16 (dBm × 10)

	// Push frame to onboard health detector — runs on every telemetry generation,
	// matching STL-01 continuous anomaly monitoring behaviour.
	angVelMag := math.Sqrt(float64(angVelX*angVelX+angVelY*angVelY+angVelZ*angVelZ)) / 100.0
	det := s.detector.Push(inference.Frame{
		BatV:      float64(batV),
		SolarV:    float64(solarV),
		TempC:     float64(chassisC) / 100.0,
		RSSI:      rssiBase,
		AttRoll:   float64(attRoll) / 100.0,
		AttPitch:  float64(attPitch) / 100.0,
		AngVelMag: angVelMag,
	})

	s.mu.Lock()
	s.lastInference = &inferenceState{class: uint8(det.Class), conf: det.Confidence}
	s.mu.Unlock()

	// Set priority flag for non-nominal classifications so ground station can triage.
	frameFlags := uint8(0)
	if det.Class != inference.NOMINAL {
		frameFlags |= zenith.FlagPriority
	}

	tm := zenith.Telemetry{
		Sequence:       seq,
		Timestamp:      uint32(t.Unix()),
		Flags:          frameFlags,
		Presence:       presence | zenith.PresenceInference,
		LatE7:          int32(st.Geodetic.LatitudeDeg * 1e7),
		LonE7:          int32(st.Geodetic.LongitudeDeg * 1e7),
		AltM:           int32(st.Geodetic.AltitudeM),
		AttRoll:        attRoll,
		AttPitch:       attPitch,
		AttYaw:         attYaw,
		AngVelX:        angVelX,
		AngVelY:        angVelY,
		AngVelZ:        angVelZ,
		BatV:           batV,
		SolarV:         solarV,
		TempC:          chassisC,
		RSSI:           rssiWire,
		InferenceClass: uint8(det.Class),
		InferenceConf:  det.Confidence,
	}

	return tm, nil
}

func (s *service) TelemetryFrame(ctx context.Context, t time.Time) ([]byte, error) {
	tm, err := s.Telemetry(ctx, t)
	if err != nil {
		return nil, err
	}
	return zenith.Encode(tm, s.cfg.HMACKey)
}

func (s *service) TMFrame(ctx context.Context, t time.Time, frameSize int) ([]byte, error) {
	zlFrame, err := s.TelemetryFrame(ctx, t)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	spSeq := s.nextSpSeqLocked()
	mc := s.nextMCFCLocked()
	vc := s.nextVCFCLocked()
	s.mu.Unlock()

	sh := spacepacket.CDSFromTime(t)
	pkt := spacepacket.SpacePacket{
		Primary: spacepacket.PrimaryHeader{
			Type:               spacepacket.Telemetry,
			HasSecondaryHeader: true,
			APID:               s.cfg.TelemetryAPID,
			GroupingFlags:      spacepacket.Unsegmented,
			SequenceCount:      spSeq,
		},
		Secondary: &sh,
		UserData:  zlFrame,
	}

	encoded, err := spacepacket.Encode(pkt)
	if err != nil {
		return nil, err
	}

	hdr := tmframe.PrimaryHeader{
		SCID:                     s.cfg.SCID,
		VCID:                     s.cfg.VCID,
		MasterChannelFrameCount:  mc,
		VirtualChannelFrameCount: vc,
		SegmentLengthID:          0b11,
		FirstHeaderPointer:       0,
	}

	frame := tmframe.TransferFrame{
		Primary:   hdr,
		DataField: encoded,
	}

	return tmframe.Encode(frame, frameSize)
}

func (s *service) State(ctx context.Context, t time.Time) (State, error) {
	eci, err := orbital.Propagate(s.cfg.Elements, t)
	if err != nil {
		return State{}, err
	}
	ecef := orbital.ECIToECEF(eci, t)
	geo := orbital.ECEFToGeodetic(ecef)
	inSun := orbital.InSunlight(eci, t)

	return State{
		Time:       t,
		ECI:        eci,
		Geodetic:   geo,
		InSunlight: inSun,
	}, nil
}

func (s *service) ExecuteCommand(_ context.Context, rawTC []byte) (CommandResult, error) {
	frame, err := tcframe.Decode(rawTC)
	if err != nil {
		return CommandResult{}, errors.Wrap(errors.ErrMalformedFrame, err)
	}

	if frame.Primary.SCID != s.cfg.SCID {
		return CommandResult{}, errors.Wrap(errors.ErrInvalidField,
			errors.New(fmt.Sprintf("TC frame SCID %d does not match spacecraft SCID %d",
				frame.Primary.SCID, s.cfg.SCID)))
	}

	if len(frame.DataField) == 0 {
		return CommandResult{}, errors.Wrap(errors.ErrMalformedFrame,
			errors.New("TC data field is empty — no command ID"))
	}

	cmd := Command{
		ID:      CommandID(frame.DataField[0]),
		Payload: frame.DataField[1:],
	}

	return s.execute(cmd)
}

func (s *service) execute(cmd Command) (CommandResult, error) {
	// CmdComputeJob manages its own locking to avoid holding s.mu during
	// the 360-step orbital propagation in JobEclipseForecast.
	if cmd.ID == CmdComputeJob {
		return s.executeComputeJob(cmd)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	switch cmd.ID {
	case CmdInferenceRun:
		// Return the current onboard health classification from the live detector.
		// Inference runs automatically on every telemetry frame; this command surfaces
		// the latest result on demand — matching STL-02 autonomous diagnostics pattern.
		inf := s.lastInference
		if inf == nil {
			return CommandResult{CommandID: cmd.ID, Accepted: true, Message: "inference: warming up — no baseline yet"}, nil
		}
		return CommandResult{
			CommandID: cmd.ID,
			Accepted:  true,
			Message: fmt.Sprintf("health: class=%s conf=%d%%",
				inference.ClassName(inference.Class(inf.class)), int(inf.conf)*100/255),
		}, nil

	case CmdDeployPayload:
		return s.executeDeployPayload(cmd)

	case CmdReboot:
		s.seqCount = 0
		s.frameSeq = 0
		s.mcfc = 0
		s.vcfc = 0
		s.lastInference = nil
		s.mode = 0
		return CommandResult{CommandID: cmd.ID, Accepted: true, Message: "reboot complete"}, nil

	case CmdSetMode:
		if len(cmd.Payload) < 1 {
			return CommandResult{CommandID: cmd.ID, Accepted: false, Message: "CmdSetMode requires 1-byte payload"}, nil
		}
		s.mode = cmd.Payload[0]
		return CommandResult{
			CommandID: cmd.ID,
			Accepted:  true,
			Message:   fmt.Sprintf("mode set to %d", s.mode),
		}, nil

	default:
		return CommandResult{}, errors.Wrap(errors.ErrCommandUnknown,
			errors.New(fmt.Sprintf("command ID 0x%02X is not recognised", uint8(cmd.ID))))
	}
}

// executeComputeJob handles CmdComputeJob — in-orbit task execution analogous
// to Satlyt STL-02's onboard compute model ("system interpreted inputs, responded
// to conditions"). Manages its own locking per job type; must NOT be called
// while s.mu is already held.
func (s *service) executeComputeJob(cmd Command) (CommandResult, error) {
	if len(cmd.Payload) < 1 {
		return CommandResult{CommandID: cmd.ID, Accepted: false, Message: "CmdComputeJob requires 1-byte job-type payload"}, nil
	}
	jobType := cmd.Payload[0]
	now := time.Now().UTC()

	switch jobType {
	case JobHealthScan:
		// Scan the last N detector frames and surface the current health state.
		result := s.detector.LastResult()
		s.mu.Lock()
		inf := s.lastInference
		s.mu.Unlock()
		classStr := inference.ClassName(result.Class)
		confPct := int(result.Confidence) * 100 / 255
		var detail string
		if inf != nil && inf.class != uint8(inference.NOMINAL) {
			detail = fmt.Sprintf(" | last_anomaly=%s", inference.ClassName(inference.Class(inf.class)))
		}
		return CommandResult{
			CommandID: cmd.ID,
			Accepted:  true,
			Message:   fmt.Sprintf("HEALTH_SCAN: status=%s conf=%d%%%s", classStr, confPct, detail),
		}, nil

	case JobEclipseForecast:
		// Compute the next eclipse entry and exit from current orbital elements.
		// s.cfg.Elements is read-only after construction — no lock needed.
		entry, exit := computeEclipseForecast(s.cfg.Elements, now)
		if entry.IsZero() {
			return CommandResult{CommandID: cmd.ID, Accepted: true, Message: "ECLIPSE_FORECAST: no eclipse in next 3h"}, nil
		}
		dur := exit.Sub(entry)
		return CommandResult{
			CommandID: cmd.ID,
			Accepted:  true,
			Message: fmt.Sprintf("ECLIPSE_FORECAST: entry=%s dur=%s | ECLIPSE_COMPUTE window available",
				entry.Format("15:04:05Z"), dur.Round(time.Second)),
		}, nil

	case JobLinkBudget:
		// Real UHF link budget using the Friis transmission equation.
		//   Margin = Ptx + Gtx + Grx − FSPL(d,f) − Lsys − Nfloor
		// where FSPL(dB) = 20·log10(d_km) + 20·log10(f_MHz) + 32.44
		// Link parameters (typical 1U CubeSat UHF):
		//   Ptx = 30 dBm (1 W), Gtx = 2 dBi (monopole), Grx = 6 dBi (yagi)
		//   System losses = 3 dB, Noise floor = kTB at 9600 bps + 5 dB NF ≈ −122 dBm
		const (
			txPowdBm   = 30.0
			txGaindBi  =  2.0
			rxGaindBi  =  6.0
			sysLossdB  =  3.0
			noiseFldBm = -122.0 // kTB(9600 Hz) + 5 dB NF
			freqMHz    = 437.0
		)

		eci, err := orbital.Propagate(s.cfg.Elements, now)
		if err != nil {
			return CommandResult{CommandID: cmd.ID, Accepted: false, Message: "LINK_BUDGET: orbital propagation failed"}, nil
		}
		ecef := orbital.ECIToECEF(eci, now)

		type gsEntry struct {
			name string
			lat  float64
			lon  float64
		}
		stations := []gsEntry{
			{"Nairobi", -1.2864, 36.8172},
			{"Svalbard", 78.2297, 15.3975},
			{"Punta-Arenas", -53.163, -70.9171},
		}

		bestGS := "none"
		bestMargin := -999.0
		msgs := make([]string, 0, len(stations))
		for _, gs := range stations {
			elevRad, _ := orbital.ElevationAzimuth(ecef, gs.lat, gs.lon)
			elevDeg := elevRad * 180 / math.Pi

			// Slant range from spacecraft to ground station via elevation geometry.
			// For elevation < 5° (below horizon), use horizon slant as worst-case.
			const earthRadKm = 6371.0
			geo := orbital.ECEFToGeodetic(ecef)
			altKm := geo.AltitudeM / 1000.0
			sinElev := math.Sin(math.Max(elevRad, 5.0*math.Pi/180))
			// Slant range from horizon geometry: d = sqrt((R+h)²−R²·cos²θ) − R·sinθ
			// Simplified closed form for elevation angle θ:
			slantKm := math.Sqrt((earthRadKm+altKm)*(earthRadKm+altKm)-
				earthRadKm*earthRadKm*(1-sinElev*sinElev)) - earthRadKm*sinElev

			fspl := 20*math.Log10(slantKm) + 20*math.Log10(freqMHz) + 32.44
			rxPowdBm := txPowdBm + txGaindBi + rxGaindBi - fspl - sysLossdB
			linkMargin := rxPowdBm - noiseFldBm

			suffix := ""
			if elevDeg >= 5.0 {
				suffix = fmt.Sprintf("@%.0f°", elevDeg)
			} else {
				suffix = "(below horizon)"
				linkMargin = 0 // no link
			}
			msgs = append(msgs, fmt.Sprintf("%s:%.1fdB%s", gs.name, linkMargin, suffix))
			if linkMargin > bestMargin {
				bestMargin = linkMargin
				bestGS = gs.name
			}
		}
		inContact, _ := orbital.IsInContact(s.cfg.Elements, stations[0].lat, stations[0].lon, now, 5.0)
		contactStr := "no"
		if inContact {
			contactStr = "yes"
		}
		return CommandResult{
			CommandID: cmd.ID,
			Accepted:  true,
			Message: fmt.Sprintf("LINK_BUDGET: best=%s(%.1fdB) margins=[%s %s %s] nairobi_contact=%s",
				bestGS, bestMargin, msgs[0], msgs[1], msgs[2], contactStr),
		}, nil

	default:
		return CommandResult{CommandID: cmd.ID, Accepted: false,
			Message: fmt.Sprintf("unknown job type 0x%02X — valid: 0x01=HEALTH_SCAN 0x02=ECLIPSE_FORECAST 0x03=LINK_BUDGET", jobType),
		}, nil
	}
}

func (s *service) Windows(_ context.Context, gsLat, gsLon float64, start, end time.Time, minElevDeg float64) ([]orbital.ContactWindow, error) {
	return orbital.ContactWindows(s.cfg.Elements, gsLat, gsLon, start, end, minElevDeg)
}

// executeDeployPayload simulates an in-orbit payload deployment over a 20 KB/s
// constrained ISL — mirrors Satlyt STL-01's 26KB Python system upload.
// Caller must hold s.mu.
func (s *service) executeDeployPayload(cmd Command) (CommandResult, error) {
	if len(cmd.Payload) < 1 {
		return CommandResult{CommandID: cmd.ID, Accepted: false, Message: "CmdDeployPayload requires 1-byte profile ID"}, nil
	}
	profileID := cmd.Payload[0]
	profile, ok := deploymentProfiles[profileID]
	if !ok {
		var parts []string
		for id, p := range deploymentProfiles {
			parts = append(parts, fmt.Sprintf("0x%02x=%s", id, p.Name))
		}
		slices.Sort(parts)
		return CommandResult{
			CommandID: cmd.ID,
			Accepted:  false,
			Message:   "unknown profile — valid IDs: " + strings.Join(parts, " "),
		}, nil
	}
	// Profile 0x03 (edge-inference-agent) is optimised for the ECLIPSE_COMPUTE window.
	// Gate deployment to eclipse periods unless DEMO_ECLIPSE_OVERRIDE is set.
	if profileID == 0x03 && s.eclipseComputeAt == nil && os.Getenv("DEMO_ECLIPSE_OVERRIDE") == "" {
		return CommandResult{
			CommandID: cmd.ID,
			Accepted:  false,
			Message:   "DEPLOY: profile 0x03 requires ECLIPSE_COMPUTE window (set DEMO_ECLIPSE_OVERRIDE=1 to bypass)",
		}, nil
	}
	linkSec := float64(profile.SizeBytes) / linkRateBytesPerSec
	now := time.Now().UTC()
	s.deployedPayload = &DeployedPayload{
		Name:       profile.Name,
		SizeBytes:  profile.SizeBytes,
		Status:     "RUNNING",
		DeployedAt: now,
		ExecOutput: "nominal exec",
	}
	return CommandResult{
		CommandID: cmd.ID,
		Accepted:  true,
		Message: fmt.Sprintf("DEPLOY: %s (%d bytes) · upload_sim=%.1fs@20KB/s · status=RUNNING",
			profile.Name, profile.SizeBytes, linkSec),
	}, nil
}

// LastResult returns the cached inference result from the most recent telemetry frame.
func (s *service) LastResult() inference.Result {
	return s.detector.LastResult()
}

// PayloadState returns a copy of the currently deployed payload, or nil if none.
func (s *service) PayloadState() *DeployedPayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deployedPayload == nil {
		return nil
	}
	cp := *s.deployedPayload
	return &cp
}

// Events returns recent autonomous spacecraft events, newest first.
func (s *service) Events() []AutonomousEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AutonomousEvent, len(s.autonomousEvents))
	copy(out, s.autonomousEvents)
	return out
}

// autonomyLoop watches for inference class transitions and dispatches autonomous
// responses. Runs as a background goroutine started lazily from the first
// Telemetry() call — matching Satlyt STL-02's "system interpreted inputs,
// responded to conditions, adapted in real time" behaviour.
func (s *service) autonomyLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		inf := s.lastInference
		prev := s.prevClass
		s.mu.Unlock()

		if inf == nil {
			continue
		}
		curr := inference.Class(inf.class)
		if curr == prev {
			continue
		}

		// Class has changed — dispatch autonomous response.
		s.handleClassTransition(prev, curr)

		s.mu.Lock()
		s.prevClass = curr
		s.mu.Unlock()
	}
}

// handleClassTransition runs OUTSIDE s.mu and dispatches autonomous responses
// for each class transition.
func (s *service) handleClassTransition(from, to inference.Class) {
	fromName := inference.ClassName(from)
	toName := inference.ClassName(to)

	switch to {
	case inference.ECLIPSE_COMPUTE:
		now := time.Now().UTC()
		efResult, _ := s.runEclipseForecastLocked()
		hsResult := s.detector.LastResult()
		hsLabel := inference.ClassName(hsResult.Class)
		s.mu.Lock()
		s.eclipseComputeAt = &now
		s.mu.Unlock()
		s.pushEvent(toName, fmt.Sprintf(
			"AUTO-DISPATCH: HEALTH_SCAN→%s · %s · eclipse compute window open",
			hsLabel, efResult))

	case inference.ECLIPSE_ENTRY:
		s.pushEvent(toName, "eclipse shadow entry detected — monitoring power transition, compute window pending")

	case inference.POWER_ANOMALY:
		s.mu.Lock()
		s.mode = 1
		s.mu.Unlock()
		s.pushEvent(toName, "AUTO: power anomaly → entering SAFE MODE, reducing subsystem load")

	case inference.THERMAL_EVENT:
		s.pushEvent(toName, "AUTO: thermal event → reducing CPU workload, flagging for ground review")

	case inference.ATTITUDE_INSTABILITY:
		s.pushEvent(toName, "AUTO: attitude instability → initiating reaction wheel desaturation routine")

	case inference.RF_DEGRADATION:
		s.pushEvent(toName, "AUTO: RF degradation → switching to delta-only downlink, preserving link margin")

	case inference.NOMINAL:
		if from == inference.ECLIPSE_COMPUTE || from == inference.ECLIPSE_ENTRY {
			s.mu.Lock()
			s.eclipseComputeAt = nil
			s.mu.Unlock()
			s.pushEvent(toName, fmt.Sprintf("eclipse window closed → NOMINAL [was %s]", fromName))
		} else if from != inference.NOMINAL {
			s.pushEvent(toName, fmt.Sprintf("anomaly resolved → NOMINAL [was %s]", fromName))
		}
	}
}

// computeEclipseForecast returns the next eclipse entry and exit times within a 3h
// horizon at 30-second resolution. Pure orbital math — no shared state, safe to call
// without s.mu held.
func computeEclipseForecast(elem orbital.Elements, now time.Time) (entry, exit time.Time) {
	step := 30 * time.Second
	horizon := 3 * time.Hour
	wasInSun := true
	for dt := time.Duration(0); dt <= horizon; dt += step {
		t := now.Add(dt)
		eci, err := orbital.Propagate(elem, t)
		if err != nil {
			break
		}
		inSun := orbital.InSunlight(eci, t)
		if wasInSun && !inSun && entry.IsZero() {
			entry = t
		}
		if !wasInSun && inSun && !entry.IsZero() && exit.IsZero() {
			exit = t
			break
		}
		wasInSun = inSun
	}
	return
}

// runEclipseForecastLocked formats the eclipse forecast for use in autonomous event messages.
// s.cfg.Elements is read-only after construction — safe without s.mu.
func (s *service) runEclipseForecastLocked() (string, error) {
	entry, exit := computeEclipseForecast(s.cfg.Elements, time.Now().UTC())
	if entry.IsZero() {
		return "no eclipse in next 3h", nil
	}
	dur := exit.Sub(entry)
	return fmt.Sprintf("eclipse entry=%s dur=%s", entry.Format("15:04:05Z"), dur.Round(time.Second)), nil
}

// pushEvent appends an autonomous event to the ring buffer, newest-first.
func (s *service) pushEvent(class, action string) {
	ev := AutonomousEvent{At: time.Now().UTC(), Class: class, Action: action}
	slog.Info("[AUTONOMY]", slog.String("class", class), slog.String("action", action))
	s.mu.Lock()
	s.autonomousEvents = append([]AutonomousEvent{ev}, s.autonomousEvents...)
	if len(s.autonomousEvents) > maxAutonomousEvents {
		s.autonomousEvents = s.autonomousEvents[:maxAutonomousEvents]
	}
	s.mu.Unlock()
}

// ─── locked sequence helpers ─────────────────────────────────────────────────
// All callers must hold s.mu.

func (s *service) nextSeqLocked() uint16 {
	v := s.seqCount
	s.seqCount++ // wraps at 0xFFFF via natural uint16 overflow
	return v
}

func (s *service) nextSpSeqLocked() uint16 {
	v := s.frameSeq
	s.frameSeq = (s.frameSeq + 1) & 0x3FFF
	return v
}

func (s *service) nextMCFCLocked() uint8 {
	v := s.mcfc
	s.mcfc++
	return v
}

func (s *service) nextVCFCLocked() uint8 {
	v := s.vcfc
	s.vcfc++
	return v
}
