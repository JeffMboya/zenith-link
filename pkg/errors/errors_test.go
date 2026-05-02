package errors_test

import (
	"testing"

	"github.com/absmach/satlyt-demo/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	tests := []struct {
		desc string
		msg  string
	}{
		{desc: "simple message", msg: "something went wrong"},
		{desc: "empty message", msg: ""},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := errors.New(tc.msg)
			assert.NotNil(t, err)
			assert.Equal(t, tc.msg, err.Error())
		})
	}
}

func TestWrap(t *testing.T) {
	sentinel := errors.New("sentinel")
	cause := errors.New("root cause")

	tests := []struct {
		desc     string
		sentinel error
		cause    error
		wantNil  bool
		wantMsg  string
	}{
		{
			desc:     "wrap sentinel with cause",
			sentinel: sentinel,
			cause:    cause,
			wantMsg:  "sentinel: root cause",
		},
		{
			desc:     "wrap nil sentinel returns cause",
			sentinel: nil,
			cause:    cause,
			wantMsg:  "root cause",
		},
		{
			desc:     "wrap with nil cause returns sentinel",
			sentinel: sentinel,
			cause:    nil,
			wantMsg:  "sentinel",
		},
		{
			desc:    "wrap both nil returns nil",
			wantNil: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got := errors.Wrap(tc.sentinel, tc.cause)
			if tc.wantNil {
				assert.Nil(t, got)
				return
			}
			assert.NotNil(t, got)
			assert.Equal(t, tc.wantMsg, got.Error())
		})
	}
}

func TestContains(t *testing.T) {
	sentinel := errors.New("sentinel")
	other := errors.New("other")
	cause := errors.New("root cause")
	wrapped := errors.Wrap(sentinel, cause)

	tests := []struct {
		desc   string
		err    error
		target error
		want   bool
	}{
		{desc: "exact match sentinel", err: sentinel, target: sentinel, want: true},
		{desc: "wrapped contains sentinel", err: wrapped, target: sentinel, want: true},
		{desc: "wrapped contains cause", err: wrapped, target: cause, want: true},
		{desc: "wrapped does not contain other", err: wrapped, target: other, want: false},
		{desc: "nil err and nil target", err: nil, target: nil, want: true},
		{desc: "nil err non-nil target", err: nil, target: sentinel, want: false},
		{desc: "non-nil err nil target", err: sentinel, target: nil, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got := errors.Contains(tc.err, tc.target)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSentinels(t *testing.T) {
	sentinels := []struct {
		desc string
		err  error
	}{
		{"ErrMalformedFrame", errors.ErrMalformedFrame},
		{"ErrBadMagic", errors.ErrBadMagic},
		{"ErrHMACFailure", errors.ErrHMACFailure},
		{"ErrCRCMismatch", errors.ErrCRCMismatch},
		{"ErrSequenceGap", errors.ErrSequenceGap},
		{"ErrBufferTooSmall", errors.ErrBufferTooSmall},
	}
	for _, tc := range sentinels {
		t.Run(tc.desc, func(t *testing.T) {
			assert.NotNil(t, tc.err)
			assert.NotEmpty(t, tc.err.Error())
		})
	}
}

func TestSentinelInequality(t *testing.T) {
	tests := []struct {
		desc string
		a, b error
	}{
		{"ErrBadMagic vs ErrBadVersion", errors.ErrBadMagic, errors.ErrBadVersion},
		{"ErrHMACFailure vs ErrCRCMismatch", errors.ErrHMACFailure, errors.ErrCRCMismatch},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			assert.False(t, errors.Contains(tc.a, tc.b))
			assert.False(t, errors.Contains(tc.b, tc.a))
		})
	}
}
