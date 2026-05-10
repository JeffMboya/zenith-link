// Package spacecraft implements the Orbitron spacecraft service.
//
// The spacecraft service owns:
//   - Orbital propagation (current geodetic position from Keplerian elements)
//   - Telemetry generation (attitude, power, thermal, RSSI simulation)
//   - CCSDS TM Transfer Frame construction (space packet → TM frame)
//   - Orbitron v2 binary frame encoding with HMAC-SHA256
//   - TC uplink command processing (inference, reboot, mode select)
//   - Contact window computation for a configurable ground station
//   - Onboard pre-processing (channel masking + delta suppression)
//   - ISL cross-links: push frames to peer spacecraft when RF_DEGRADATION detected
//
// External dependencies: none beyond the standard library + pkg/.
package spacecraft

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/absmach/orbitron/pkg/ccsds/spacepacket"
	"github.com/absmach/orbitron/pkg/ccsds/tcframe"
	"github.com/absmach/orbitron/pkg/ccsds/tmframe"
	"github.com/absmach/orbitron/pkg/errors"
	"github.com/absmach/orbitron/pkg/orbital"
	"github.com/absmach/orbitron/pkg/orbitron"
	"github.com/absmach/orbitron/pkg/preprocess"
	"github.com/absmach/orbitron/pkg/spaceweather"
	"github.com/absmach/orbitron/spacecraft/inference"
)

type CommandID uint8

const (
	CmdInferenceRun  CommandID = 0x01
	CmdReboot        CommandID = 0x02
	CmdSetMode       CommandID = 0x03
	CmdComputeJob    CommandID = 0x04
	CmdDeployPayload CommandID = 0x05
)

const linkRateBytesPerSec = 20 * 1024

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

type DeployedPayload struct {
	Name       string    `json:"Name"`
	SizeBytes  int       `json:"SizeBytes"`
	Status     string    `json:"Status"`
	DeployedAt time.Time `json:"DeployedAt"`
	ExecOutput string    `json:"ExecOutput"`
}

type AutonomousEvent struct {
	At     time.Time `json:"at"`
	Class  string    `json:"class"`
	Action string    `json:"action"`
}

const maxAutonomousEvents = 20

const (
	JobHealthScan      uint8 = 0x01
	JobEclipseForecast uint8 = 0x02
	JobLinkBudget      uint8 = 0x03
)

type Command struct {
	ID      CommandID
	Payload []byte
}

type CommandResult struct {
	CommandID CommandID
	Accepted  bool
	Message   string
}

type Service interface {
	Telemetry(ctx context.Context, t time.Time) (orbitron.Telemetry, error)

	TelemetryFrame(ctx context.Context, t time.Time) ([]byte, error)

	TMFrame(ctx context.Context, t time.Time, frameSize int) ([]byte, error)

	State(ctx context.Context, t time.Time) (State, error)

	ExecuteCommand(ctx context.Context, tcFrame []byte) (CommandResult, error)

	Windows(ctx context.Context, gsLat, gsLon float64, start, end time.Time, minElevDeg float64) ([]orbital.ContactWindow, error)

	Events() []AutonomousEvent

	PayloadState() *DeployedPayload

	LastResult() inference.Result

	StormLevel() string

	KpIndex() float64

	// ISLReceive accepts a raw Orbitron frame from a peer spacecraft via a cross-link.
	ISLReceive(frame []byte, sourceSC string)

	// ISLNextFrame pops the next ISL-buffered frame and its originating spacecraft tag.
	ISLNextFrame() ([]byte, string, bool)

	// PreprocessStats returns onboard pre-processing reduction counters.
	PreprocessStats() preprocess.Stats
}

type State struct {
	Time       time.Time
	ECI        orbital.ECIState
	Geodetic   orbital.GeodeticPosition
	InSunlight bool
}

type Config struct {
	SCID uint16

	VCID uint8

	HMACKey []byte

	Elements orbital.Elements

	TelemetryAPID uint16

	SequenceCount uint16

	// ISLPeerAddrs are HTTP base URLs of peer spacecraft that this spacecraft
	// will push frames to when RF_DEGRADATION is detected.
	ISLPeerAddrs []string

	// ISLSourceTag is the "sc1"/"sc2"/"sc3" label sent in X-Source-SC headers
	// so that receiving relays can correctly attribute ISL-forwarded frames.
	ISLSourceTag string
}

type inferenceState struct {
	class uint8
	conf  uint8
}

type islFrame struct {
	payload  []byte
	sourceSC string
}

type service struct {
	cfg      Config
	mu       sync.Mutex
	detector *inference.Detector
	sw       *spaceweather.Monitor

	preprocessor preprocess.Filter
	islBuf       chan islFrame
	islClient    *http.Client

	seqCount uint16
	frameSeq uint16
	mcfc     uint8
	vcfc     uint8
	mode     uint8

	lastInference   *inferenceState
	deployedPayload *DeployedPayload

	autonomousEvents []AutonomousEvent
	prevClass        inference.Class
	eclipseComputeAt *time.Time
	autonomyOnce     sync.Once
}

func New(cfg Config) Service {
	sw := spaceweather.NewMonitor()
	return &service{
		cfg:       cfg,
		seqCount:  cfg.SequenceCount,
		detector:  inference.NewDetector(),
		sw:        sw,
		islBuf:    make(chan islFrame, 32),
		islClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (s *service) Telemetry(ctx context.Context, t time.Time) (orbitron.Telemetry, error) {
	s.autonomyOnce.Do(func() {
		go s.autonomyLoop()
		if len(s.cfg.ISLPeerAddrs) > 0 {
			go s.islPushLoop()
		}
	})

	st, err := s.State(ctx, t)
	if err != nil {
		return orbitron.Telemetry{}, err
	}

	s.mu.Lock()
	seq := s.nextSeqLocked()
	currentMode := s.mode
	s.mu.Unlock()

	presence := orbitron.PresencePosition |
		orbitron.PresenceBatV |
		orbitron.PresenceSolarV |
		orbitron.PresenceTempC |
		orbitron.PresenceRSSI
	if currentMode == 0 {
		presence |= orbitron.PresenceAttitude | orbitron.PresenceAngVel
	}

	batV := uint16(7400)
	solarV := uint16(0)
	if st.InSunlight {
		solarV = 5200
		batV = 7600
	}

	phi := float64(t.Unix()) / 60.0
	attRoll := int16(math.Round(math.Sin(phi*0.7) * 500))
	attPitch := int16(math.Round(math.Sin(phi*0.4+1.2) * 1000))
	attYaw := int16(math.Round(math.Sin(phi*0.15+0.8) * 18000))

	chassisC := int16(math.Round((20.0 + 3.0*math.Sin(phi*0.3)) * 100))

	angVelX := int16(math.Round(math.Sin(phi*2.1+0.5) * 15))
	angVelY := int16(math.Round(math.Sin(phi*1.7+1.1) * 12))
	angVelZ := int16(math.Round(math.Sin(phi*2.9+2.3) * 8))

	unix := t.Unix()

	bgNoise := math.Sin(float64(unix)*1.61803) * math.Sin(float64(unix)*2.71828)
	batV = uint16(int(batV) + int(math.Round(bgNoise*50)))
	chassisC += int16(math.Round(bgNoise * 30))
	rssiBase := -85.0 + bgNoise*2.0

	fast1 := math.Sin(float64(unix)*0.523) * math.Cos(float64(unix)*0.314)
	fast2 := math.Sin(float64(unix)*0.471) * math.Sin(float64(unix)*0.628)
	attRoll += int16(math.Round(fast1 * 250))
	attPitch += int16(math.Round(fast2 * 350))
	attYaw += int16(math.Round(fast1 * 600))
	angVelX += int16(math.Round(fast1 * 8))
	angVelY += int16(math.Round(fast2 * 6))
	angVelZ += int16(math.Round(fast1 * 4))

	rssiBase += s.sw.RSSIAdjustmentDB()

	if st.InSunlight {

		if unix%600 >= 30 && unix%600 < 52 {
			batV -= 150
		}

		if unix%720 >= 240 && unix%720 < 258 {
			chassisC += 320
		}

		if unix%480 >= 150 && unix%480 < 168 {
			rssiBase -= 8.5
		}

		if unix%540 >= 360 && unix%540 < 374 {
			attPitch += 2500
			angVelY += 600
		}
	}

	rssiWire := int16(math.Round(rssiBase * 10))

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

	frameFlags := uint8(0)
	if det.Class != inference.NOMINAL {
		frameFlags |= orbitron.FlagPriority
	}

	tm := orbitron.Telemetry{
		Sequence:       seq,
		Timestamp:      uint32(t.Unix()),
		Flags:          frameFlags,
		Presence:       presence | orbitron.PresenceInference,
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

	s.mu.Lock()
	infClass := inference.NOMINAL
	if s.lastInference != nil {
		infClass = inference.Class(s.lastInference.class)
	}
	s.mu.Unlock()

	filtered, transmit := s.preprocessor.Apply(tm, infClass)
	if !transmit {
		return nil, nil // suppressed — caller should return HTTP 204
	}
	return orbitron.Encode(filtered, s.cfg.HMACKey)
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

	if cmd.ID == CmdComputeJob {
		return s.executeComputeJob(cmd)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	switch cmd.ID {
	case CmdInferenceRun:

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

func (s *service) executeComputeJob(cmd Command) (CommandResult, error) {
	if len(cmd.Payload) < 1 {
		return CommandResult{CommandID: cmd.ID, Accepted: false, Message: "CmdComputeJob requires 1-byte job-type payload"}, nil
	}
	jobType := cmd.Payload[0]
	now := time.Now().UTC()

	switch jobType {
	case JobHealthScan:

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

		const (
			txPowdBm   = 30.0
			txGaindBi  = 2.0
			rxGaindBi  = 6.0
			sysLossdB  = 3.0
			noiseFldBm = -122.0
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

			const earthRadKm = 6371.0
			geo := orbital.ECEFToGeodetic(ecef)
			altKm := geo.AltitudeM / 1000.0
			sinElev := math.Sin(math.Max(elevRad, 5.0*math.Pi/180))

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
				linkMargin = 0
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

func (s *service) LastResult() inference.Result {
	return s.detector.LastResult()
}

func (s *service) PayloadState() *DeployedPayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deployedPayload == nil {
		return nil
	}
	cp := *s.deployedPayload
	return &cp
}

func (s *service) Events() []AutonomousEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AutonomousEvent, len(s.autonomousEvents))
	copy(out, s.autonomousEvents)
	return out
}

func (s *service) autonomyLoop() {

	ctx := context.Background()

	s.sw.Start(ctx)

	prevStormLevel := "QUIET"

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		inf := s.lastInference
		prev := s.prevClass
		s.mu.Unlock()

		if inf != nil {
			curr := inference.Class(inf.class)
			if curr != prev {

				s.handleClassTransition(prev, curr)
				s.mu.Lock()
				s.prevClass = curr
				s.mu.Unlock()
			}
		}

		currStorm := s.sw.StormLevel()
		if currStorm != prevStormLevel && currStorm != "QUIET" {
			s.pushEvent(currStorm, "space weather event")
		}
		prevStormLevel = currStorm
	}
}

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

func (s *service) runEclipseForecastLocked() (string, error) {
	entry, exit := computeEclipseForecast(s.cfg.Elements, time.Now().UTC())
	if entry.IsZero() {
		return "no eclipse in next 3h", nil
	}
	dur := exit.Sub(entry)
	return fmt.Sprintf("eclipse entry=%s dur=%s", entry.Format("15:04:05Z"), dur.Round(time.Second)), nil
}

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

func (s *service) StormLevel() string { return s.sw.StormLevel() }

func (s *service) KpIndex() float64 { return s.sw.Current().Kp }

// ISLReceive accepts a raw Orbitron frame forwarded from a peer spacecraft via
// an inter-satellite cross-link. The frame is buffered for the next relay poll.
func (s *service) ISLReceive(frame []byte, sourceSC string) {
	cp := make([]byte, len(frame))
	copy(cp, frame)
	select {
	case s.islBuf <- islFrame{payload: cp, sourceSC: sourceSC}:
	default:
		slog.Warn("spacecraft: ISL buffer full — peer frame dropped", slog.String("source_sc", sourceSC))
	}
}

// ISLNextFrame pops the next ISL-buffered frame (a frame this spacecraft received
// from a peer and is carrying on its behalf). Returns the payload, originating
// spacecraft tag, and whether a frame was available.
func (s *service) ISLNextFrame() ([]byte, string, bool) {
	select {
	case f := <-s.islBuf:
		return f.payload, f.sourceSC, true
	default:
		return nil, "", false
	}
}

// PreprocessStats returns a snapshot of the onboard pre-processing reduction counters.
func (s *service) PreprocessStats() preprocess.Stats {
	return s.preprocessor.Stats()
}

// islPushLoop fires when RF_DEGRADATION is detected and pushes the current frame
// directly to configured ISL peer spacecraft, bypassing the relay poll cycle.
func (s *service) islPushLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		inf := s.lastInference
		s.mu.Unlock()
		if inf == nil || inference.Class(inf.class) != inference.RF_DEGRADATION {
			continue
		}
		// Build a raw frame (bypasses the preprocessor's suppression — we need to transmit urgently).
		tm, err := s.Telemetry(context.Background(), time.Now().UTC())
		if err != nil {
			continue
		}
		frame, err := orbitron.Encode(tm, s.cfg.HMACKey)
		if err != nil || len(frame) == 0 {
			continue
		}
		for _, peer := range s.cfg.ISLPeerAddrs {
			go s.pushISLFrame(peer, frame)
		}
	}
}

func (s *service) pushISLFrame(peerAddr string, frame []byte) {
	url := peerAddr + "/isl/receive"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(frame))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if s.cfg.ISLSourceTag != "" {
		req.Header.Set("X-Source-SC", s.cfg.ISLSourceTag)
	}
	resp, err := s.islClient.Do(req)
	if err != nil {
		slog.Warn("spacecraft: ISL push failed", slog.String("peer", peerAddr), slog.Any("error", err))
		return
	}
	defer resp.Body.Close()
	slog.Info("spacecraft: ISL frame pushed to peer",
		slog.String("peer", peerAddr),
		slog.String("source_sc", s.cfg.ISLSourceTag),
		slog.Int("bytes", len(frame)),
	)
}

func (s *service) nextSeqLocked() uint16 {
	v := s.seqCount
	s.seqCount++
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
