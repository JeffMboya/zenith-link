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

type EID struct {
	Node    uint32
	Service uint32
}

func (e EID) String() string {
	return fmt.Sprintf("ipn:%d.%d", e.Node, e.Service)
}

func ParseEID(s string) (EID, error) {
	var node, service uint32
	_, err := fmt.Sscanf(s, "ipn:%d.%d", &node, &service)
	if err != nil {
		return EID{}, errors.Wrap(errors.ErrInvalidField,
			errors.New("EID must be in ipn:N.S format"))
	}
	return EID{Node: node, Service: service}, nil
}

type Bundle struct {
	ID          uint64
	Source      EID
	Destination EID
	CreatedAt   time.Time
	Lifetime    time.Duration
	HopCount    uint8
	Payload     []byte
	Priority    uint8
}

func (b *Bundle) Expired() bool { return time.Since(b.CreatedAt) > b.Lifetime }

const encodedHeaderSize = 8 + 4 + 4 + 4 + 4 + 8 + 4 + 1 + 1 + 4

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
