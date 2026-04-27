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
	"sync"
	"time"

	"github.com/absmach/zenith-link/pkg/ccsds/spacepacket"
	"github.com/absmach/zenith-link/pkg/ccsds/tcframe"
	"github.com/absmach/zenith-link/pkg/ccsds/tmframe"
	"github.com/absmach/zenith-link/pkg/errors"
	"github.com/absmach/zenith-link/pkg/orbital"
	"github.com/absmach/zenith-link/pkg/zenith"
)

// CommandID identifies an uplink command type.
type CommandID uint8

const (
	CmdInferenceRun CommandID = 0x01 // trigger onboard AI inference
	CmdReboot       CommandID = 0x02 // reset sequence counters and clear inference state
	CmdSetMode      CommandID = 0x03 // set operating mode (1-byte payload: mode ID)
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

// inferenceState holds the result of the last onboard AI inference run.
type inferenceState struct {
	class uint8
	conf  uint8
}

// inferenceClassNames used internally to simulate varied results.
var inferenceClassNames = [7]string{
	"cloud", "ocean", "land", "urban", "vegetation", "ice", "desert",
}

type service struct {
	cfg  Config
	mu   sync.Mutex

	seqCount uint16
	frameSeq uint16
	mcfc     uint8
	vcfc     uint8
	mode     uint8

	lastInference *inferenceState
}

// New creates a new spacecraft Service.
func New(cfg Config) Service {
	return &service{
		cfg:      cfg,
		seqCount: cfg.SequenceCount,
	}
}

func (s *service) Telemetry(ctx context.Context, t time.Time) (zenith.Telemetry, error) {
	st, err := s.State(ctx, t)
	if err != nil {
		return zenith.Telemetry{}, err
	}

	s.mu.Lock()
	seq := s.nextSeqLocked()
	inf := s.lastInference
	s.mu.Unlock()

	presence := zenith.PresencePosition |
		zenith.PresenceAttitude |
		zenith.PresenceBatV |
		zenith.PresenceSolarV |
		zenith.PresenceTempC |
		zenith.PresenceRSSI

	batV := uint16(7400)
	solarV := uint16(0)
	if st.InSunlight {
		solarV = 5200
		batV = 7600
	}

	tm := zenith.Telemetry{
		Sequence:  seq,
		Timestamp: uint32(t.Unix()),
		Presence:  presence,
		LatE7:     int32(st.Geodetic.LatitudeDeg * 1e7),
		LonE7:     int32(st.Geodetic.LongitudeDeg * 1e7),
		AltM:      int32(st.Geodetic.AltitudeM),
		AttRoll:   0,
		AttPitch:  0,
		AttYaw:    0,
		BatV:      batV,
		SolarV:    solarV,
		TempC:     2000,
		RSSI:      -850,
	}

	if inf != nil {
		tm.Presence |= zenith.PresenceInference
		tm.InferenceClass = inf.class
		tm.InferenceConf = inf.conf
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
	s.mu.Lock()
	defer s.mu.Unlock()

	switch cmd.ID {
	case CmdInferenceRun:
		// Simulate deterministic inference result rotating through classes.
		nowUnix := time.Now().Unix()
		classIdx := uint8((nowUnix / 30) % 7)
		conf := uint8(180 + (nowUnix % 75))
		s.lastInference = &inferenceState{class: classIdx, conf: conf}
		return CommandResult{
			CommandID: cmd.ID,
			Accepted:  true,
			Message:   fmt.Sprintf("inference complete: class=%s conf=%d%%", inferenceClassNames[classIdx], int(conf)*100/255),
		}, nil

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

func (s *service) Windows(_ context.Context, gsLat, gsLon float64, start, end time.Time, minElevDeg float64) ([]orbital.ContactWindow, error) {
	return orbital.ContactWindows(s.cfg.Elements, gsLat, gsLon, start, end, minElevDeg)
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
