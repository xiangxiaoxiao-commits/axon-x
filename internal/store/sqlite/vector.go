package sqlite

import (
	"encoding/binary"
	"fmt"
	"math"
)

// encodeVector serializes a float32 vector to a raw little-endian byte slice,
// the on-disk BLOB format for memory embeddings.
func encodeVector(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// decodeVector parses a little-endian float32 BLOB back into a vector. The byte
// length must be a multiple of 4, otherwise the BLOB is malformed.
func decodeVector(buf []byte) ([]float32, error) {
	if len(buf)%4 != 0 {
		return nil, fmt.Errorf("decode vector: blob length %d is not a multiple of 4", len(buf))
	}
	vec := make([]float32, len(buf)/4)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
	}
	return vec, nil
}
