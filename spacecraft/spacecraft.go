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
