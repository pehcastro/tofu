package anthropic

import (
	"encoding/binary"
	"math/bits"
)

const (
	xxPrime1 uint64 = 11400714785074694791
	xxPrime2 uint64 = 14029467366897019727
	xxPrime3 uint64 = 1609587929392839161
	xxPrime4 uint64 = 9650029242287828579
	xxPrime5 uint64 = 2870177450012600261
)

func xxRound(acc, input uint64) uint64 {
	return bits.RotateLeft64(acc+input*xxPrime2, 31) * xxPrime1
}

func xxMergeRound(acc, value uint64) uint64 {
	return (acc^xxRound(0, value))*xxPrime1 + xxPrime4
}

func xxHash64(data []byte, seed uint64) uint64 {
	length := uint64(len(data))
	var hash uint64
	if len(data) >= 32 {
		one, two := seed+xxPrime1+xxPrime2, seed+xxPrime2
		three, four := seed, seed-xxPrime1
		for len(data) >= 32 {
			one = xxRound(one, binary.LittleEndian.Uint64(data[0:8]))
			two = xxRound(two, binary.LittleEndian.Uint64(data[8:16]))
			three = xxRound(three, binary.LittleEndian.Uint64(data[16:24]))
			four = xxRound(four, binary.LittleEndian.Uint64(data[24:32]))
			data = data[32:]
		}
		hash = bits.RotateLeft64(one, 1) + bits.RotateLeft64(two, 7) +
			bits.RotateLeft64(three, 12) + bits.RotateLeft64(four, 18)
		hash = xxMergeRound(hash, one)
		hash = xxMergeRound(hash, two)
		hash = xxMergeRound(hash, three)
		hash = xxMergeRound(hash, four)
	} else {
		hash = seed + xxPrime5
	}

	hash += length
	for len(data) >= 8 {
		hash = bits.RotateLeft64(hash^xxRound(0, binary.LittleEndian.Uint64(data[0:8])), 27)*xxPrime1 + xxPrime4
		data = data[8:]
	}
	if len(data) >= 4 {
		hash = bits.RotateLeft64(hash^uint64(binary.LittleEndian.Uint32(data[0:4]))*xxPrime1, 23)*xxPrime2 + xxPrime3
		data = data[4:]
	}
	for _, b := range data {
		hash = bits.RotateLeft64(hash^uint64(b)*xxPrime5, 11) * xxPrime1
	}

	hash ^= hash >> 33
	hash *= xxPrime2
	hash ^= hash >> 29
	hash *= xxPrime3
	hash ^= hash >> 32
	return hash
}
