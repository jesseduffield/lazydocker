package chunked

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/docker/go-units"
)

const bloomFilterMaxLength = 100 * units.MB // max size for bloom filter

type bloomFilter struct {
	bitArray []uint64
	k        uint32
}

func newBloomFilter(size int, k uint32) *bloomFilter {
	numElements := (size + 63) / 64
	if numElements == 0 {
		numElements = 1
	}
	return &bloomFilter{
		bitArray: make([]uint64, numElements),
		k:        k,
	}
}

func newBloomFilterFromArray(bitArray []uint64, k uint32) *bloomFilter {
	return &bloomFilter{
		bitArray: bitArray,
		k:        k,
	}
}

func (bf *bloomFilter) hashFn(item []byte, seed uint32) (uint64, uint64) {
	if len(item) == 0 {
		return 0, 1
	}
	mod := uint32(len(bf.bitArray) * 64)
	seedSplit := seed % uint32(len(item))
	hash := (crc32.ChecksumIEEE(item[:seedSplit]) ^ crc32.ChecksumIEEE(item[seedSplit:])) % mod
	return uint64(hash / 64), uint64(1 << (hash % 64))
}

func (bf *bloomFilter) add(item []byte) {
	for i := uint32(0); i < bf.k; i++ {
		index, mask := bf.hashFn(item, i)
		bf.bitArray[index] |= mask
	}
}

func (bf *bloomFilter) maybeContains(item []byte) bool {
	for i := uint32(0); i < bf.k; i++ {
		index, mask := bf.hashFn(item, i)
		if bf.bitArray[index]&mask == 0 {
			return false
		}
	}
	return true
}

func (bf *bloomFilter) writeTo(writer io.Writer) error {
	if err := binary.Write(writer, binary.LittleEndian, uint64(len(bf.bitArray))); err != nil {
		return err
	}
	if err := binary.Write(writer, binary.LittleEndian, bf.k); err != nil {
		return err
	}
	if err := binary.Write(writer, binary.LittleEndian, bf.bitArray); err != nil {
		return err
	}
	return nil
}

func readBloomFilter(reader io.Reader) (*bloomFilter, error) {
	var bloomFilterLen uint64
	var k uint32

	if err := binary.Read(reader, binary.LittleEndian, &bloomFilterLen); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &k); err != nil {
		return nil, err
	}
	// sanity check
	if bloomFilterLen > bloomFilterMaxLength {
		return nil, fmt.Errorf("bloom filter length %d exceeds max length %d", bloomFilterLen, bloomFilterMaxLength)
	}
	bloomFilterArray := make([]uint64, bloomFilterLen)
	if err := binary.Read(reader, binary.LittleEndian, &bloomFilterArray); err != nil {
		return nil, err
	}
	return newBloomFilterFromArray(bloomFilterArray, k), nil
}
