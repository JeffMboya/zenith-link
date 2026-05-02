package mocks

import (
	"context"
	"time"

	"github.com/absmach/zenith-link/pkg/orbital"
	"github.com/absmach/zenith-link/pkg/zenith"
	"github.com/absmach/zenith-link/spacecraft"
	"github.com/absmach/zenith-link/spacecraft/inference"
	"github.com/stretchr/testify/mock"
)

var _ spacecraft.Service = (*Service)(nil)

type Service struct {
	mock.Mock
}

func (m *Service) Telemetry(ctx context.Context, t time.Time) (zenith.Telemetry, error) {
	args := m.Called(ctx, t)
	return args.Get(0).(zenith.Telemetry), args.Error(1)
}

func (m *Service) TelemetryFrame(ctx context.Context, t time.Time) ([]byte, error) {
	args := m.Called(ctx, t)
	if b, ok := args.Get(0).([]byte); ok {
		return b, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *Service) TMFrame(ctx context.Context, t time.Time, frameSize int) ([]byte, error) {
	args := m.Called(ctx, t, frameSize)
	if b, ok := args.Get(0).([]byte); ok {
		return b, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *Service) State(ctx context.Context, t time.Time) (spacecraft.State, error) {
	args := m.Called(ctx, t)
	if st, ok := args.Get(0).(spacecraft.State); ok {
		return st, args.Error(1)
	}
	return spacecraft.State{}, args.Error(1)
}

func (m *Service) ExecuteCommand(ctx context.Context, tcFrame []byte) (spacecraft.CommandResult, error) {
	args := m.Called(ctx, tcFrame)
	if cr, ok := args.Get(0).(spacecraft.CommandResult); ok {
		return cr, args.Error(1)
	}
	return spacecraft.CommandResult{}, args.Error(1)
}

func (m *Service) Windows(ctx context.Context, gsLat, gsLon float64, start, end time.Time, minElevDeg float64) ([]orbital.ContactWindow, error) {
	args := m.Called(ctx, gsLat, gsLon, start, end, minElevDeg)
	if ws, ok := args.Get(0).([]orbital.ContactWindow); ok {
		return ws, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *Service) Events() []spacecraft.AutonomousEvent {
	args := m.Called()
	if ev, ok := args.Get(0).([]spacecraft.AutonomousEvent); ok {
		return ev
	}
	return nil
}

func (m *Service) PayloadState() *spacecraft.DeployedPayload {
	args := m.Called()
	if p, ok := args.Get(0).(*spacecraft.DeployedPayload); ok {
		return p
	}
	return nil
}

func (m *Service) LastResult() inference.Result {
	args := m.Called()
	if r, ok := args.Get(0).(inference.Result); ok {
		return r
	}
	return inference.Result{}
}

func (m *Service) StormLevel() string {
	args := m.Called()
	return args.String(0)
}

func (m *Service) KpIndex() float64 {
	args := m.Called()
	return args.Get(0).(float64)
}
