// Package crc implements CRC-16/CCITT-FALSE as mandated by the CCSDS Frame
// Error Control specification (CCSDS 132.0-B-3 section 4.1.5).
//
// Algorithm parameters:
//
//	Polynomial : 0x1021
//	Initial value : 0xFFFF
//	Input reflection : false
//	Output reflection : false
//	Final XOR : 0x0000
//
// Disambiguation: this is NOT CRC-16/CCITT (which uses init=0x0000) and NOT
// CRC-16/KERMIT (which bit-reflects). The test vector "123456789" must produce
// 0x29B1; any other value indicates the wrong variant.
package crc

// table holds the precomputed CRC lookup table for polynomial 0x1021.
var table [256]uint16

func init() {
	for i := 0; i < 256; i++ {
		crc := uint16(i) << 8
		for j := 0; j < 8; j++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
		table[i] = crc
	}
}

// CCITT16 computes CRC-16/CCITT-FALSE over data.
// The test vector []byte("123456789") must return 0x29B1.
func CCITT16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc = (crc<<8) ^ table[byte(crc>>8)^b]
	}
	return crc
}

// Append appends data followed by its two-byte big-endian CRC to dst and
// returns the extended slice. The returned slice is dst + data + [crc_hi, crc_lo].
func Append(dst, data []byte) []byte {
	v := CCITT16(data)
	dst = append(dst, data...)
	return append(dst, byte(v>>8), byte(v))
}

// Verify returns true when the last two bytes of frame are the correct
// CRC-16/CCITT-FALSE for the preceding bytes.
// At least 2 bytes are required (0 data bytes + 2 CRC bytes is the minimum).
func Verify(frame []byte) bool {
	if len(frame) < 2 {
		return false
	}
	payload := frame[:len(frame)-2]
	got := CCITT16(payload)
	want := uint16(frame[len(frame)-2])<<8 | uint16(frame[len(frame)-1])
	return got == want
}
