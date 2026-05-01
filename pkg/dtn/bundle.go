// Package dtn implements a subset of the Bundle Protocol (RFC 9171) for
// delay-tolerant inter-satellite networking. Bundles wrap Zenith-Link telemetry
// frames and carry metadata required for store-and-forward routing across
// contact gaps in the ISL mesh.
package dtn

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/absmach/zenith-link/pkg/errors"
)

// EID is a simplified endpoint identifier. We use the ipn scheme: ipn:node.service.
// Nodes: 0=groundstation, 1=SC-1, 2=SC-2/Relay-1, 3=SC-3/Relay-2.
// Services: 1=telemetry, 2=command.
type EID struct {
	Node    uint32
	Service uint32
}

// String returns the IPN URI representation of the EID.
func (e EID) String() string {
	return fmt.Sprintf("ipn:%d.%d", e.Node, e.Service)
}

// ParseEID parses an EID from "ipn:N.S" format.
func ParseEID(s string) (EID, error) {
	var node, service uint32
	_, err := fmt.Sscanf(s, "ipn:%d.%d", &node, &service)
	if err != nil {
		return EID{}, errors.Wrap(errors.ErrInvalidField,
			errors.New("EID must be in ipn:N.S format"))
	}
	return EID{Node: node, Service: service}, nil
}

// Bundle is a single DTN bundle (simplified RFC 9171 primary block + payload block).
type Bundle struct {
	ID          uint64        // monotonically increasing bundle sequence number
	Source      EID
	Destination EID
	CreatedAt   time.Time
	Lifetime    time.Duration // default 2h
	HopCount    uint8         // incremented at each store-and-forward hop; drop if > 8
	Payload     []byte        // raw Zenith-Link v2 frame bytes
	Priority    uint8         // 0=bulk, 1=normal, 2=expedited (maps to frame Flags priority bit)
}

// Expired reports whether the bundle has exceeded its lifetime.
func (b *Bundle) Expired() bool { return time.Since(b.CreatedAt) > b.Lifetime }

// encodedHeaderSize is the fixed portion of the wire encoding before the payload.
// [8] ID + [4] Source.Node + [4] Source.Service + [4] Dest.Node + [4] Dest.Service
// + [8] CreatedAt Unix seconds + [4] Lifetime seconds + [1] HopCount + [1] Priority
// + [4] PayloadLen = 42 bytes.
const encodedHeaderSize = 8 + 4 + 4 + 4 + 4 + 8 + 4 + 1 + 1 + 4

// Encode serialises the bundle to a compact binary format (NOT full CBOR — a
// pragmatic binary encoding sufficient for the ISL demo).
//
// Wire format (all big-endian):
//
//	[8] ID
//	[4] Source.Node
//	[4] Source.Service
//	[4] Dest.Node
//	[4] Dest.Service
//	[8] CreatedAt Unix seconds
//	[4] Lifetime seconds
//	[1] HopCount
//	[1] Priority
//	[4] PayloadLen
//	[N] Payload
func (b *Bundle) Encode() []byte {
	payloadLen := len(b.Payload)
	buf := make([]byte, encodedHeaderSize+payloadLen)

	binary.BigEndian.PutUint64(buf[0:], b.ID)
	binary.BigEndian.PutUint32(buf[8:], b.Source.Node)
	binary.BigEndian.PutUint32(buf[12:], b.Source.Service)
	binary.BigEndian.PutUint32(buf[16:], b.Destination.Node)
	binary.BigEndian.PutUint32(buf[20:], b.Destination.Service)
	binary.BigEndian.PutUint64(buf[24:], uint64(b.CreatedAt.Unix()))
	binary.BigEndian.PutUint32(buf[32:], uint32(b.Lifetime.Seconds()))
	buf[36] = b.HopCount
	buf[37] = b.Priority
	binary.BigEndian.PutUint32(buf[38:], uint32(payloadLen))
	copy(buf[42:], b.Payload)

	return buf
}

// DecodeBundle deserialises a bundle from the binary format produced by Encode.
func DecodeBundle(data []byte) (*Bundle, error) {
	if len(data) < encodedHeaderSize {
		return nil, errors.Wrap(errors.ErrFrameTooSmall,
			errors.New("bundle data shorter than fixed header"))
	}

	b := &Bundle{}
	b.ID = binary.BigEndian.Uint64(data[0:])
	b.Source.Node = binary.BigEndian.Uint32(data[8:])
	b.Source.Service = binary.BigEndian.Uint32(data[12:])
	b.Destination.Node = binary.BigEndian.Uint32(data[16:])
	b.Destination.Service = binary.BigEndian.Uint32(data[20:])
	createdUnix := binary.BigEndian.Uint64(data[24:])
	b.CreatedAt = time.Unix(int64(createdUnix), 0).UTC()
	lifetimeSec := binary.BigEndian.Uint32(data[32:])
	b.Lifetime = time.Duration(lifetimeSec) * time.Second
	b.HopCount = data[36]
	b.Priority = data[37]
	payloadLen := int(binary.BigEndian.Uint32(data[38:]))

	if len(data) < encodedHeaderSize+payloadLen {
		return nil, errors.Wrap(errors.ErrDataLenMismatch,
			errors.New("bundle data shorter than declared payload length"))
	}

	b.Payload = make([]byte, payloadLen)
	copy(b.Payload, data[42:42+payloadLen])

	return b, nil
}
