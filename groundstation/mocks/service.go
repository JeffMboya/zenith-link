package mocks

import (
	"context"

	"github.com/absmach/orbitron/groundstation"
	"github.com/absmach/orbitron/pkg/orbitron"
	"github.com/stretchr/testify/mock"
)

var _ groundstation.Service = (*Service)(nil)

type Service struct {
	mock.Mock
}

func (m *Service) Receive(ctx context.Context, rawFrame []byte, sourceSC string) (orbitron.Telemetry, error) {
	args := m.Called(ctx, rawFrame, sourceSC)
	if tm, ok := args.Get(0).(orbitron.Telemetry); ok {
		return tm, args.Error(1)
	}
	return orbitron.Telemetry{}, args.Error(1)
}

func (m *Service) Latest(ctx context.Context) (groundstation.LatestState, error) {
	args := m.Called(ctx)
	if st, ok := args.Get(0).(groundstation.LatestState); ok {
		return st, args.Error(1)
	}
	return groundstation.LatestState{}, args.Error(1)
}

func (m *Service) Subscribe(ctx context.Context) (<-chan groundstation.TaggedTelemetry, error) {
	args := m.Called(ctx)
	if ch, ok := args.Get(0).(<-chan groundstation.TaggedTelemetry); ok {
		return ch, args.Error(1)
	}
	return nil, args.Error(1)
}
