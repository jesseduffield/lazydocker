package goodkey

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/letsencrypt/boulder/core"

	"github.com/titanous/rocacheck"
)

// To generate, run: primes 2 752 | tr '\n' ,
var smallPrimeInts = []int64{
	2, 3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37, 41, 43, 47,
	53, 59, 61, 67, 71, 73, 79, 83, 89, 97, 101, 103, 107,
	109, 113, 127, 131, 137, 139, 149, 151, 157, 163, 167,
	173, 179, 181, 191, 193, 197, 199, 211, 223, 227, 229,
	233, 239, 241, 251, 257, 263, 269, 271, 277, 281, 283,
	293, 307, 311, 313, 317, 331, 337, 347, 349, 353, 359,
	367, 373, 379, 383, 389, 397, 401, 409, 419, 421, 431,
	433, 439, 443, 449, 457, 461, 463, 467, 479, 487, 491,
	499, 503, 509, 521, 523, 541, 547, 557, 563, 569, 571,
	577, 587, 593, 599, 601, 607, 613, 617, 619, 631, 641,
	643, 647, 653, 659, 661, 673, 677, 683, 691, 701, 709,
	719, 727, 733, 739, 743, 751,
}

// singleton defines the object of a Singleton pattern
var (
	smallPrimesSingleton sync.Once
	smallPrimesProduct   *big.Int
)

type Config struct {
	// AllowedKeys enables or disables specific key algorithms and sizes. If
	// nil, defaults to just those keys allowed by the Let's Encrypt CPS.
	AllowedKeys *AllowedKeys
	// WeakKeyFile is the path to a JSON file containing truncated modulus hashes
	// of known weak RSA keys. If this config value is empty, then RSA modulus
	// hash checking will be disabled.
	WeakKeyFile string
	// BlockedKeyFile is the path to a YAML file containing base64-encoded SHA256
	// hashes of PKIX Subject Public Keys that should be blocked. If this config
	// value is empty, then blocked key checking will be disabled.
	BlockedKeyFile string
	// FermatRounds is an integer number of rounds of Fermat's factorization
	// method that should be performed to attempt to detect keys whose modulus can
	// be trivially factored because the two factors are very close to each other.
	// If this config value is empty (0), no factorization will be attempted.
	FermatRounds int
}

// AllowedKeys is a map of six specific key algorithm and size combinations to
// booleans indicating whether keys of that type are considered good.
type AllowedKeys struct {
	// Baseline Requirements, Section 6.1.5 requires key size >= 2048 and a multiple
	// of 8 bits: https://github.com/cabforum/servercert/blob/main/docs/BR.md#615-key-sizes
	// Baseline Requirements, Section 6.1.1.3 requires that we reject any keys which
	// have a known method to easily compute their private key, such as Debian Weak
	// Keys. Our enforcement mechanism relies on enumerating all Debian Weak Keys at
	// common key sizes, so we restrict all issuance to those common key sizes.
	RSA2048 bool
	RSA3072 bool
	RSA4096 bool
	// Baseline Requirements, Section 6.1.5 requires that ECDSA keys be valid
	// points on the NIST P-256, P-384, or P-521 elliptic curves.
	ECDSAP256 bool
	ECDSAP384 bool
	ECDSAP521 bool
}

// LetsEncryptCPS encodes the five key algorithms and sizes allowed by the Let's
// Encrypt CPS CV-SSL Subscriber Certificate Profile: RSA 2048, RSA 3076, RSA
// 4096, ECDSA 256 and ECDSA P384.
// https://github.com/letsencrypt/cp-cps/blob/main/CP-CPS.md#dv-ssl-subscriber-certificate
// If this is ever changed, the CP/CPS MUST be changed first.
func LetsEncryptCPS() AllowedKeys {
	return AllowedKeys{
		RSA2048:   true,
		RSA3072:   true,
		RSA4096:   true,
		ECDSAP256: true,
		ECDSAP384: true,
	}
}

// ErrBadKey represents an error with a key. It is distinct from the various
// ways in which an ACME request can have an erroneous key (BadPublicKeyError,
// BadCSRError) because this library is used to check both JWS signing keys and
// keys in CSRs.
var ErrBadKey = errors.New("")

func badKey(msg string, args ...interface{}) error {
	return fmt.Errorf("%w%s", ErrBadKey, fmt.Errorf(msg, args...))
}

// BlockedKeyCheckFunc is used to pass in the sa.BlockedKey functionality to KeyPolicy,
// rather than storing a full sa.SQLStorageAuthority. This allows external
// users who don’t want to import all of boulder/sa, and makes testing
// significantly simpler.
// On success, the function returns a boolean which is true if the key is blocked.
type BlockedKeyCheckFunc func(ctx context.Context, keyHash []byte) (bool, error)

// KeyPolicy determines which types of key may be used with various boulder
// operations.
type KeyPolicy struct {
	allowedKeys  AllowedKeys
	weakRSAList  *WeakRSAKeys
	blockedList  *blockedKeys
	fermatRounds int
	blockedCheck BlockedKeyCheckFunc
}

// NewPolicy returns a key policy based on the given configuration, with sane
// defaults. If the config's AllowedKeys is nil, the LetsEncryptCPS AllowedKeys
// is used. If the config's WeakKeyFile or BlockedKeyFile paths are empty, those
// checks are disabled. If the config's FermatRounds is 0, Fermat Factorization
// is disabled.
func NewPolicy(config *Config, bkc BlockedKeyCheckFunc) (KeyPolicy, error) {
	if config == nil {
		config = &Config{}
	}
	kp := KeyPolicy{
		blockedCheck: bkc,
	}
	if config.AllowedKeys == nil {
		kp.allowedKeys = LetsEncryptCPS()
	} else {
		kp.allowedKeys = *config.AllowedKeys
	}
	if config.WeakKeyFile != "" {
		keyList, err := LoadWeakRSASuffixes(config.WeakKeyFile)
		if err != nil {
			return KeyPolicy{}, err
		}
		kp.weakRSAList = keyList
	}
	if config.BlockedKeyFile != "" {
		blocked, err := loadBlockedKeysList(config.BlockedKeyFile)
		if err != nil {
			return KeyPolicy{}, err
		}
		kp.blockedList = blocked
	}
	if config.FermatRounds < 0 {
		return KeyPolicy{}, fmt.Errorf("Fermat factorization rounds cannot be negative: %d", config.FermatRounds)
	}
	kp.fermatRounds = config.FermatRounds
	return kp, nil
}

// GoodKey returns true if the key is acceptable for both TLS use and account
// key use (our requirements are the same for either one), according to basic
// strength and algorithm checking. GoodKey only supports pointers: *rsa.PublicKey
// and *ecdsa.PublicKey. It will reject non-pointer types.
// TODO: Support JSONWebKeys once go-jose migration is done.
func (policy *KeyPolicy) GoodKey(ctx context.Context, key crypto.PublicKey) error {
	// Early rejection of unacceptable key types to guard subsequent checks.
	switch t := key.(type) {
	case *rsa.PublicKey, *ecdsa.PublicKey:
		break
	default:
		return badKey("unsupported key type %T", t)
	}
	// If there is a blocked list configured then check if the public key is one
	// that has been administratively blocked.
	if policy.blockedList != nil {
		if blocked, err := policy.blockedList.blocked(key); err != nil {
			return fmt.Errorf("error checking blocklist for key: %v", key)
		} else if blocked {
			return badKey("public key is forbidden")
		}
	}
	if policy.blockedCheck != nil {
		digest, err := core.KeyDigest(key)
		if err != nil {
			return badKey("%w", err)
		}
		exists, err := policy.blockedCheck(ctx, digest[:])
		if err != nil {
			return err
		} else if exists {
			return badKey("public key is forbidden")
		}
	}
	switch t := key.(type) {
	case *rsa.PublicKey:
		return policy.goodKeyRSA(t)
	case *ecdsa.PublicKey:
		return policy.goodKeyECDSA(t)
	default:
		return badKey("unsupported key type %T", key)
	}
}

// GoodKeyECDSA determines if an ECDSA pubkey meets our requirements
func (policy *KeyPolicy) goodKeyECDSA(key *ecdsa.PublicKey) (err error) {
	// Check the curve.
	//
	// The validity of the curve is an assumption for all following tests.
	err = policy.goodCurve(key.Curve)
	if err != nil {
		return err
	}

	// Key validation routine adapted from NIST SP800-56A § 5.6.2.3.2.
	// <http://nvlpubs.nist.gov/nistpubs/SpecialPublications/NIST.SP.800-56Ar2.pdf>
	//
	// Assuming a prime field since a) we are only allowing such curves and b)
	// crypto/elliptic only supports prime curves. Where this assumption
	// simplifies the code below, it is explicitly stated and explained. If ever
	// adapting this code to support non-prime curves, refer to NIST SP800-56A §
	// 5.6.2.3.2 and adapt this code appropriately.
	params := key.Params()

	// SP800-56A § 5.6.2.3.2 Step 1.
	// Partial check of the public key for an invalid range in the EC group:
	// Verify that key is not the point at infinity O.
	// This code assumes that the point at infinity is (0,0), which is the
	// case for all supported curves.
	if isPointAtInfinityNISTP(key.X, key.Y) {
		return badKey("key x, y must not be the point at infinity")
	}

	// SP800-56A § 5.6.2.3.2 Step 2.
	//   "Verify that x_Q and y_Q are integers in the interval [0,p-1] in the
	//    case that q is an odd prime p, or that x_Q and y_Q are bit strings
	//    of length m bits in the case that q = 2**m."
	//
	// Prove prime field: ASSUMED.
	// Prove q != 2: ASSUMED. (Curve parameter. No supported curve has q == 2.)
	// Prime field && q != 2  => q is an odd prime p
	// Therefore "verify that x, y are in [0, p-1]" satisfies step 2.
	//
	// Therefore verify that both x and y of the public key point have the unique
	// correct representation of an element in the underlying field by verifying
	// that x and y are integers in [0, p-1].
	if key.X.Sign() < 0 || key.Y.Sign() < 0 {
		return badKey("key x, y must not be negative")
	}

	if key.X.Cmp(params.P) >= 0 || key.Y.Cmp(params.P) >= 0 {
		return badKey("key x, y must not exceed P-1")
	}

	// SP800-56A § 5.6.2.3.2 Step 3.
	//   "If q is an odd prime p, verify that (y_Q)**2 === (x_Q)***3 + a*x_Q + b (mod p).
	//    If q = 2**m, verify that (y_Q)**2 + (x_Q)*(y_Q) == (x_Q)**3 + a*(x_Q)*2 + b in
	//    the finite field of size 2**m.
	//    (Ensures that the public key is on the correct elliptic curve.)"
	//
	// q is an odd prime p: proven/assumed above.
	// a = -3 for all supported curves.
	//
	// Therefore step 3 is satisfied simply by showing that
	//   y**2 === x**3 - 3*x + B (mod P).
	//
	// This proves that the public key is on the correct elliptic curve.
	// But in practice, this test is provided by crypto/elliptic, so use that.
	if !key.Curve.IsOnCurve(key.X, key.Y) {
		return badKey("key point is not on the curve")
	}

	// SP800-56A § 5.6.2.3.2 Step 4.
	//   "Verify that n*Q == Ø.
	//    (Ensures that the public key has the correct order. Along with check 1,
	//     ensures that the public key is in the correct range in the correct EC
	//     subgroup, that is, it is in the correct EC subgroup and is not the
	//     identity element.)"
	//
	// Ensure that public key has the correct order:
	// verify that n*Q = Ø.
	//
	// n*Q = Ø iff n*Q is the point at infinity (see step 1).
	ox, oy := key.Curve.ScalarMult(key.X, key.Y, params.N.Bytes())
	if !isPointAtInfinityNISTP(ox, oy) {
		return badKey("public key does not have correct order")
	}

	// End of SP800-56A § 5.6.2.3.2 Public Key Validation Routine.
	// Key is valid.
	return nil
}

// Returns true iff the point (x,y) on NIST P-256, NIST P-384 or NIST P-521 is
// the point at infinity. These curves all have the same point at infinity
// (0,0). This function must ONLY be used on points on curves verified to have
// (0,0) as their point at infinity.
func isPointAtInfinityNISTP(x, y *big.Int) bool {
	return x.Sign() == 0 && y.Sign() == 0
}

// GoodCurve determines if an elliptic curve meets our requirements.
func (policy *KeyPolicy) goodCurve(c elliptic.Curve) (err error) {
	// Simply use a whitelist for now.
	params := c.Params()
	switch {
	case policy.allowedKeys.ECDSAP256 && params == elliptic.P256().Params():
		return nil
	case policy.allowedKeys.ECDSAP384 && params == elliptic.P384().Params():
		return nil
	case policy.allowedKeys.ECDSAP521 && params == elliptic.P521().Params():
		return nil
	default:
		return badKey("ECDSA curve %v not allowed", params.Name)
	}
}

// GoodKeyRSA determines if a RSA pubkey meets our requirements
func (policy *KeyPolicy) goodKeyRSA(key *rsa.PublicKey) error {
	modulus := key.N

	err := policy.goodRSABitLen(key)
	if err != nil {
		return err
	}

	if policy.weakRSAList != nil && policy.weakRSAList.Known(key) {
		return badKey("key is on a known weak RSA key list")
	}

	// Rather than support arbitrary exponents, which significantly increases
	// the size of the key space we allow, we restrict E to the defacto standard
	// RSA exponent 65537. There is no specific standards document that specifies
	// 65537 as the 'best' exponent, but ITU X.509 Annex C suggests there are
	// notable merits for using it if using a fixed exponent.
	//
	// The CABF Baseline Requirements state:
	//   The CA SHALL confirm that the value of the public exponent is an
	//   odd number equal to 3 or more. Additionally, the public exponent
	//   SHOULD be in the range between 2^16 + 1 and 2^256-1.
	//
	// By only allowing one exponent, which fits these constraints, we satisfy
	// these requirements.
	if key.E != 65537 {
		return badKey("key exponent must be 65537")
	}

	// The modulus SHOULD also have the following characteristics: an odd
	// number, not the power of a prime, and have no factors smaller than 752.
	// TODO: We don't yet check for "power of a prime."
	if checkSmallPrimes(modulus) {
		return badKey("key divisible by small prime")
	}
	// Check for weak keys generated by Infineon hardware
	// (see https://crocs.fi.muni.cz/public/papers/rsa_ccs17)
	if rocacheck.IsWeak(key) {
		return badKey("key generated by vulnerable Infineon-based hardware")
	}
	// Check if the key can be easily factored via Fermat's factorization method.
	if policy.fermatRounds > 0 {
		err := checkPrimeFactorsTooClose(modulus, policy.fermatRounds)
		if err != nil {
			return badKey("key generated with factors too close together: %w", err)
		}
	}

	return nil
}

func (policy *KeyPolicy) goodRSABitLen(key *rsa.PublicKey) error {
	// See comment on AllowedKeys above.
	modulusBitLen := key.N.BitLen()
	switch {
	case modulusBitLen == 2048 && policy.allowedKeys.RSA2048:
		return nil
	case modulusBitLen == 3072 && policy.allowedKeys.RSA3072:
		return nil
	case modulusBitLen == 4096 && policy.allowedKeys.RSA4096:
		return nil
	default:
		return badKey("key size not supported: %d", modulusBitLen)
	}
}

// Returns true iff integer i is divisible by any of the primes in smallPrimes.
//
// Short circuits; execution time is dependent on i. Do not use this on secret
// values.
//
// Rather than checking each prime individually (invoking Mod on each),
// multiply the primes together and let GCD do our work for us: if the
// GCD between <key> and <product of primes> is not one, we know we have
// a bad key. This is substantially faster than checking each prime
// individually.
func checkSmallPrimes(i *big.Int) bool {
	smallPrimesSingleton.Do(func() {
		smallPrimesProduct = big.NewInt(1)
		for _, prime := range smallPrimeInts {
			smallPrimesProduct.Mul(smallPrimesProduct, big.NewInt(prime))
		}
	})

	// When the GCD is 1, i and smallPrimesProduct are coprime, meaning they
	// share no common factors. When the GCD is not one, it is the product of
	// all common factors, meaning we've identified at least one small prime
	// which invalidates i as a valid key.

	var result big.Int
	result.GCD(nil, nil, i, smallPrimesProduct)
	return result.Cmp(big.NewInt(1)) != 0
}

// Returns an error if the modulus n is able to be factored into primes p and q
// via Fermat's factorization method. This method relies on the two primes being
// very close together, which means that they were almost certainly not picked
// independently from a uniform random distribution. Basically, if we can factor
// the key this easily, so can anyone else.
func checkPrimeFactorsTooClose(n *big.Int, rounds int) error {
	// Pre-allocate some big numbers that we'll use a lot down below.
	one := big.NewInt(1)
	bb := new(big.Int)

	// Any odd integer is equal to a difference of squares of integers:
	//   n = a^2 - b^2 = (a + b)(a - b)
	// Any RSA public key modulus is equal to a product of two primes:
	//   n = pq
	// Here we try to find values for a and b, since doing so also gives us the
	// prime factors p = (a + b) and q = (a - b).

	// We start with a close to the square root of the modulus n, to start with
	// two candidate prime factors that are as close together as possible and
	// work our way out from there. Specifically, we set a = ceil(sqrt(n)), the
	// first integer greater than the square root of n. Unfortunately, big.Int's
	// built-in square root function takes the floor, so we have to add one to get
	// the ceil.
	a := new(big.Int)
	a.Sqrt(n).Add(a, one)

	// We calculate b2 to see if it is a perfect square (i.e. b^2), and therefore
	// b is an integer. Specifically, b2 = a^2 - n.
	b2 := new(big.Int)
	b2.Mul(a, a).Sub(b2, n)

	for range rounds {
		// To see if b2 is a perfect square, we take its square root, square that,
		// and check to see if we got the same result back.
		bb.Sqrt(b2).Mul(bb, bb)
		if b2.Cmp(bb) == 0 {
			// b2 is a perfect square, so we've found integer values of a and b,
			// and can easily compute p and q as their sum and difference.
			bb.Sqrt(bb)
			p := new(big.Int).Add(a, bb)
			q := new(big.Int).Sub(a, bb)
			return fmt.Errorf("public modulus n = pq factored into p: %s; q: %s", p, q)
		}

		// Set up the next iteration by incrementing a by one and recalculating b2.
		a.Add(a, one)
		b2.Mul(a, a).Sub(b2, n)
	}
	return nil
}
