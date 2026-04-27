// Package errors provides error types and helpers that mirror the Magistrala
// error infrastructure: sentinel errors with Err* prefixes, Wrap(), Contains(),
// and Is() for structured error chain inspection.
package errors

import "errors"

// Error is a string-keyed sentinel with an optional cause chain.
type Error struct {
	msg string
	err error
}

// New creates a new standalone error.
func New(msg string) error {
	return &Error{msg: msg}
}

func (e *Error) Error() string {
	if e.err == nil {
		return e.msg
	}
	return e.msg + ": " + e.err.Error()
}

func (e *Error) Unwrap() error { return e.err }

// Wrap attaches a cause to a sentinel error, returning a new error that
// preserves both messages in its Error() string.
func Wrap(sentinel, cause error) error {
	if sentinel == nil {
		return cause
	}
	if cause == nil {
		return sentinel
	}
	// Unwrap sentinel to its base message so repeated wrapping stays flat.
	var se *Error
	if errors.As(sentinel, &se) {
		return &Error{msg: se.msg, err: cause}
	}
	return &Error{msg: sentinel.Error(), err: cause}
}

// Contains reports whether err contains target anywhere in its unwrap chain,
// matching on the *message* of Error values so the chain survives multiple
// Wrap calls.
func Contains(err, target error) bool {
	if err == nil || target == nil {
		return err == target
	}
	if errors.Is(err, target) {
		return true
	}
	var te *Error
	var ee *Error
	if errors.As(target, &te) && errors.As(err, &ee) {
		if ee.msg == te.msg {
			return true
		}
	}
	var unwrapped interface{ Unwrap() error }
	if errors.As(err, &unwrapped) {
		return Contains(unwrapped.Unwrap(), target)
	}
	return false
}

// Is implements the errors.Is contract so that errors.Is(Wrap(e, cause), e)
// returns true.
func Is(err, target error) bool { return Contains(err, target) }

// Sentinel errors used across the entire Zenith-Link stack.
var (
	ErrMalformedFrame   = New("malformed frame")
	ErrBadMagic         = New("bad magic word")
	ErrBadVersion       = New("unsupported protocol version")
	ErrHMACFailure      = New("HMAC authentication failure")
	ErrCRCMismatch      = New("CRC mismatch")
	ErrSequenceGap      = New("sequence number gap")
	ErrBufferTooSmall   = New("buffer too small")
	ErrInvalidAPID      = New("invalid APID")
	ErrInvalidSeqCount  = New("sequence count exceeds 14-bit maximum")
	ErrDataLenMismatch  = New("packet data length field mismatch")
	ErrFrameTooLarge    = New("frame exceeds CCSDS maximum")
	ErrFrameTooSmall    = New("frame below CCSDS minimum")
	ErrInvalidField     = New("invalid field value")
	ErrOrbitalPropagate = New("orbital propagation failure")
	ErrCommandUnknown   = New("unknown command ID")
)
