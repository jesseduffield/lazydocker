package core

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"expvar"
	"fmt"
	"io"
	"math/big"
	mrand "math/rand"
	"os"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/go-jose/go-jose/v4"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const Unspecified = "Unspecified"

// Package Variables Variables

// BuildID is set by the compiler (using -ldflags "-X core.BuildID $(git rev-parse --short HEAD)")
// and is used by GetBuildID
var BuildID string

// BuildHost is set by the compiler and is used by GetBuildHost
var BuildHost string

// BuildTime is set by the compiler and is used by GetBuildTime
var BuildTime string

func init() {
	expvar.NewString("BuildID").Set(BuildID)
	expvar.NewString("BuildTime").Set(BuildTime)
}

// Random stuff

type randSource interface {
	Read(p []byte) (n int, err error)
}

// RandReader is used so that it can be replaced in tests that require
// deterministic output
var RandReader randSource = rand.Reader

// RandomString returns a randomly generated string of the requested length.
func RandomString(byteLength int) string {
	b := make([]byte, byteLength)
	_, err := io.ReadFull(RandReader, b)
	if err != nil {
		panic(fmt.Sprintf("Error reading random bytes: %s", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// NewToken produces a random string for Challenges, etc.
func NewToken() string {
	return RandomString(32)
}

var tokenFormat = regexp.MustCompile(`^[\w-]{43}$`)

// looksLikeAToken checks whether a string represents a 32-octet value in
// the URL-safe base64 alphabet.
func looksLikeAToken(token string) bool {
	return tokenFormat.MatchString(token)
}

// Fingerprints

// Fingerprint256 produces an unpadded, URL-safe Base64-encoded SHA256 digest
// of the data.
func Fingerprint256(data []byte) string {
	d := sha256.New()
	_, _ = d.Write(data) // Never returns an error
	return base64.RawURLEncoding.EncodeToString(d.Sum(nil))
}

type Sha256Digest [sha256.Size]byte

// KeyDigest produces the SHA256 digest of a provided public key.
func KeyDigest(key crypto.PublicKey) (Sha256Digest, error) {
	switch t := key.(type) {
	case *jose.JSONWebKey:
		if t == nil {
			return Sha256Digest{}, errors.New("cannot compute digest of nil key")
		}
		return KeyDigest(t.Key)
	case jose.JSONWebKey:
		return KeyDigest(t.Key)
	default:
		keyDER, err := x509.MarshalPKIXPublicKey(key)
		if err != nil {
			return Sha256Digest{}, err
		}
		return sha256.Sum256(keyDER), nil
	}
}

// KeyDigestB64 produces a padded, standard Base64-encoded SHA256 digest of a
// provided public key.
func KeyDigestB64(key crypto.PublicKey) (string, error) {
	digest, err := KeyDigest(key)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(digest[:]), nil
}

// KeyDigestEquals determines whether two public keys have the same digest.
func KeyDigestEquals(j, k crypto.PublicKey) bool {
	digestJ, errJ := KeyDigestB64(j)
	digestK, errK := KeyDigestB64(k)
	// Keys that don't have a valid digest (due to marshalling problems)
	// are never equal. So, e.g. nil keys are not equal.
	if errJ != nil || errK != nil {
		return false
	}
	return digestJ == digestK
}

// PublicKeysEqual determines whether two public keys are identical.
func PublicKeysEqual(a, b crypto.PublicKey) (bool, error) {
	switch ak := a.(type) {
	case *rsa.PublicKey:
		return ak.Equal(b), nil
	case *ecdsa.PublicKey:
		return ak.Equal(b), nil
	default:
		return false, fmt.Errorf("unsupported public key type %T", ak)
	}
}

// SerialToString converts a certificate serial number (big.Int) to a String
// consistently.
func SerialToString(serial *big.Int) string {
	return fmt.Sprintf("%036x", serial)
}

// StringToSerial converts a string into a certificate serial number (big.Int)
// consistently.
func StringToSerial(serial string) (*big.Int, error) {
	var serialNum big.Int
	if !ValidSerial(serial) {
		return &serialNum, fmt.Errorf("invalid serial number %q", serial)
	}
	_, err := fmt.Sscanf(serial, "%036x", &serialNum)
	return &serialNum, err
}

// ValidSerial tests whether the input string represents a syntactically
// valid serial number, i.e., that it is a valid hex string between 32
// and 36 characters long.
func ValidSerial(serial string) bool {
	// Originally, serial numbers were 32 hex characters long. We later increased
	// them to 36, but we allow the shorter ones because they exist in some
	// production databases.
	if len(serial) != 32 && len(serial) != 36 {
		return false
	}
	_, err := hex.DecodeString(serial)
	return err == nil
}

// GetBuildID identifies what build is running.
func GetBuildID() (retID string) {
	retID = BuildID
	if retID == "" {
		retID = Unspecified
	}
	return
}

// GetBuildTime identifies when this build was made
func GetBuildTime() (retID string) {
	retID = BuildTime
	if retID == "" {
		retID = Unspecified
	}
	return
}

// GetBuildHost identifies the building host
func GetBuildHost() (retID string) {
	retID = BuildHost
	if retID == "" {
		retID = Unspecified
	}
	return
}

// IsAnyNilOrZero returns whether any of the supplied values are nil, or (if not)
// if any of them is its type's zero-value. This is useful for validating that
// all required fields on a proto message are present.
func IsAnyNilOrZero(vals ...interface{}) bool {
	for _, val := range vals {
		switch v := val.(type) {
		case nil:
			return true
		case bool:
			if !v {
				return true
			}
		case string:
			if v == "" {
				return true
			}
		case []string:
			if len(v) == 0 {
				return true
			}
		case byte:
			// Byte is an alias for uint8 and will cover that case.
			if v == 0 {
				return true
			}
		case []byte:
			if len(v) == 0 {
				return true
			}
		case int:
			if v == 0 {
				return true
			}
		case int8:
			if v == 0 {
				return true
			}
		case int16:
			if v == 0 {
				return true
			}
		case int32:
			if v == 0 {
				return true
			}
		case int64:
			if v == 0 {
				return true
			}
		case uint:
			if v == 0 {
				return true
			}
		case uint16:
			if v == 0 {
				return true
			}
		case uint32:
			if v == 0 {
				return true
			}
		case uint64:
			if v == 0 {
				return true
			}
		case float32:
			if v == 0 {
				return true
			}
		case float64:
			if v == 0 {
				return true
			}
		case time.Time:
			if v.IsZero() {
				return true
			}
		case *timestamppb.Timestamp:
			if v == nil || v.AsTime().IsZero() {
				return true
			}
		case *durationpb.Duration:
			if v == nil || v.AsDuration() == time.Duration(0) {
				return true
			}
		default:
			if reflect.ValueOf(v).IsZero() {
				return true
			}
		}
	}
	return false
}

// UniqueLowerNames returns the set of all unique names in the input after all
// of them are lowercased. The returned names will be in their lowercased form
// and sorted alphabetically.
func UniqueLowerNames(names []string) (unique []string) {
	nameMap := make(map[string]int, len(names))
	for _, name := range names {
		nameMap[strings.ToLower(name)] = 1
	}

	unique = make([]string, 0, len(nameMap))
	for name := range nameMap {
		unique = append(unique, name)
	}
	sort.Strings(unique)
	return
}

// HashNames returns a hash of the names requested. This is intended for use
// when interacting with the orderFqdnSets table and rate limiting.
func HashNames(names []string) []byte {
	names = UniqueLowerNames(names)
	hash := sha256.Sum256([]byte(strings.Join(names, ",")))
	return hash[:]
}

// LoadCert loads a PEM certificate specified by filename or returns an error
func LoadCert(filename string) (*x509.Certificate, error) {
	certPEM, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("no data in cert PEM file %q", filename)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	return cert, nil
}

// retryJitter is used to prevent bunched retried queries from falling into lockstep
const retryJitter = 0.2

// RetryBackoff calculates a backoff time based on number of retries, will always
// add jitter so requests that start in unison won't fall into lockstep. Because of
// this the returned duration can always be larger than the maximum by a factor of
// retryJitter. Adapted from
// https://github.com/grpc/grpc-go/blob/v1.11.3/backoff.go#L77-L96
func RetryBackoff(retries int, base, max time.Duration, factor float64) time.Duration {
	if retries == 0 {
		return 0
	}
	backoff, fMax := float64(base), float64(max)
	for backoff < fMax && retries > 1 {
		backoff *= factor
		retries--
	}
	if backoff > fMax {
		backoff = fMax
	}
	// Randomize backoff delays so that if a cluster of requests start at
	// the same time, they won't operate in lockstep.
	backoff *= (1 - retryJitter) + 2*retryJitter*mrand.Float64()
	return time.Duration(backoff)
}

// IsASCII determines if every character in a string is encoded in
// the ASCII character set.
func IsASCII(str string) bool {
	for _, r := range str {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func Command() string {
	return path.Base(os.Args[0])
}
