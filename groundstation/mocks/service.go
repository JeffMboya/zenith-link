package mocks

import (
	"context"

	"github.com/absmach/zenith-link/groundstation"
	"github.com/absmach/zenith-link/pkg/zenith"
	"github.com/stretchr/testify/mock"
)

var _ groundstation.Service = (*Service)(nil)

// Service is a mock of the groundstation Service interface.
type Service struct {
	mock.Mock
}

func (m *Service) Receive(ctx context.Context, rawFrame []byte) (zenith.Telemetry, error) {
	args := m.Called(ctx, rawFrame)
	if tm, ok := args.Get(0).(zenith.Telemetry); ok {
		return tm, args.Error(1)
	}
	return zenith.Telemetry{}, args.Error(1)
}

func (m *Service) Latest(ctx context.Context) (groundstation.LatestState, error) {
	args := m.Called(ctx)
	if st, ok := args.Get(0).(groundstation.LatestState); ok {
		return st, args.Error(1)
	}
	return groundstation.LatestState{}, args.Error(1)
}

func (m *Service) Subscribe(ctx context.Context) (<-chan zenith.Telemetry, error) {
	args := m.Called(ctx)
	if ch, ok := args.Get(0).(<-chan zenith.Telemetry); ok {
		return ch, args.Error(1)
	}
	return nil, args.Error(1)
}
