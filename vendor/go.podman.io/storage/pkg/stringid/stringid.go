// Package stringid provides helper functions for dealing with string identifiers
package stringid

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"math/big"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.podman.io/storage/pkg/regexp"
)

const shortLen = 12

var (
	validShortID = regexp.Delayed("^[a-f0-9]{12}$")
	validHex     = regexp.Delayed(`^[a-f0-9]{64}$`)

	rngLock sync.Mutex
	rng     *rand.Rand // A RNG with seeding properties we control. It can only be accessed with randLock held.
)

// IsShortID determines if an arbitrary string *looks like* a short ID.
func IsShortID(id string) bool {
	return validShortID.MatchString(id)
}

// TruncateID returns a shorthand version of a string identifier for convenience.
// A collision with other shorthands is very unlikely, but possible.
// In case of a collision a lookup with TruncIndex.Get() will fail, and the caller
// will need to use a longer prefix, or the full-length Id.
func TruncateID(id string) string {
	if i := strings.IndexRune(id, ':'); i >= 0 {
		id = id[i+1:]
	}
	if len(id) > shortLen {
		id = id[:shortLen]
	}
	return id
}

func generateID(r io.Reader) string {
	b := make([]byte, 32)
	for {
		if _, err := io.ReadFull(r, b); err != nil {
			panic(err) // This shouldn't happen
		}
		id := hex.EncodeToString(b)
		// if we try to parse the truncated for as an int and we don't have
		// an error then the value is all numeric and causes issues when
		// used as a hostname. ref #3869
		if _, err := strconv.ParseInt(TruncateID(id), 10, 64); err == nil {
			continue
		}
		return id
	}
}

// GenerateRandomID returns a pseudorandom 64-character hex string.
func GenerateRandomID() string {
	return generateID(cryptorand.Reader)
}

// GenerateNonCryptoID generates unique id without using cryptographically
// secure sources of random.
// It helps you to save entropy.
func GenerateNonCryptoID() string {
	rngLock.Lock()
	defer rngLock.Unlock()
	return generateID(readerFunc(rng.Read))
}

// ValidateID checks whether an ID string is a valid image ID.
func ValidateID(id string) error {
	if ok := validHex.MatchString(id); !ok {
		return fmt.Errorf("image ID %q is invalid", id)
	}
	return nil
}

func init() {
	// Initialize a private RNG so we generate random ids. Tries to use a
	// crypto seed before falling back to time.
	var seed int64
	if cryptoseed, err := cryptorand.Int(cryptorand.Reader, big.NewInt(math.MaxInt64)); err != nil {
		// This should not happen, but worst-case fallback to time-based seed.
		seed = time.Now().UnixNano()
	} else {
		seed = cryptoseed.Int64()
	}

	rng = rand.New(rand.NewSource(seed))
}

type readerFunc func(p []byte) (int, error)

func (fn readerFunc) Read(p []byte) (int, error) {
	return fn(p)
}
