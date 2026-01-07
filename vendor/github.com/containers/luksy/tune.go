package luksy

import (
	"hash"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/pbkdf2"
)

func durationOf(f func()) time.Duration {
	start := time.Now()
	f()
	return time.Since(start)
}

func IterationsPBKDF2(salt []byte, keyLen int, h func() hash.Hash) int {
	iterations := 2
	var d time.Duration
	for d < time.Second {
		d = durationOf(func() {
			_ = pbkdf2.Key([]byte{}, salt, iterations, keyLen, h)
		})
		if d < time.Second/10 {
			iterations *= 2
		} else {
			return iterations * int(time.Second) / int(d)
		}
	}
	return iterations
}

func memoryCostArgon2(salt []byte, keyLen, timeCost, threadsCost int, kdf func([]byte, []byte, uint32, uint32, uint8, uint32) []byte) int {
	memoryCost := 2
	var d time.Duration
	for d < time.Second {
		d = durationOf(func() {
			_ = kdf([]byte{}, salt, uint32(timeCost), uint32(memoryCost), uint8(threadsCost), uint32(keyLen))
		})
		if d < time.Second/10 {
			memoryCost *= 2
		} else {
			return memoryCost * int(float64(time.Second)/float64(d))
		}
	}
	return memoryCost
}

func MemoryCostArgon2(salt []byte, keyLen, timeCost, threadsCost int) int {
	return memoryCostArgon2(salt, keyLen, timeCost, threadsCost, argon2.Key)
}

func MemoryCostArgon2i(salt []byte, keyLen, timeCost, threadsCost int) int {
	return memoryCostArgon2(salt, keyLen, timeCost, threadsCost, argon2.IDKey)
}
