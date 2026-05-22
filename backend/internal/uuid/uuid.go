package uuid

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func NewV7() string {
	var random [10]byte
	_, _ = rand.Read(random[:])

	ms := uint64(time.Now().UTC().UnixMilli())
	b := [16]byte{
		byte(ms >> 40),
		byte(ms >> 32),
		byte(ms >> 24),
		byte(ms >> 16),
		byte(ms >> 8),
		byte(ms),
		0x70 | (random[0] & 0x0f),
		random[1],
		0x80 | (random[2] & 0x3f),
		random[3],
		random[4],
		random[5],
		random[6],
		random[7],
		random[8],
		random[9],
	}

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3]),
		uint16(b[4])<<8|uint16(b[5]),
		uint16(b[6])<<8|uint16(b[7]),
		uint16(b[8])<<8|uint16(b[9]),
		uint64(b[10])<<40|uint64(b[11])<<32|uint64(b[12])<<24|uint64(b[13])<<16|uint64(b[14])<<8|uint64(b[15]),
	)
}

func NewToken() string {
	var buf [24]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}
