// Note: Consider the API unstable until the code supports at least three different image formats or transports.

// NOTE: Keep this in sync with docs/atomic-signature.md and docs/atomic-signature-embedded.json!

package signature

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	digest "github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/signature/internal"
	"go.podman.io/image/v5/version"
)

const (
	signatureType = "atomic container signature"
)

// InvalidSignatureError is returned when parsing an invalid signature.
type InvalidSignatureError = internal.InvalidSignatureError

// Signature is a parsed content of a signature.
// The only way to get this structure from a blob should be as a return value from a successful call to verifyAndExtractSignature below.
type Signature struct {
	DockerManifestDigest digest.Digest
	DockerReference      string // FIXME: more precise type?
}

// untrustedSignature is a parsed content of a signature.
type untrustedSignature struct {
	untrustedDockerManifestDigest digest.Digest
	untrustedDockerReference      string // FIXME: more precise type?
	untrustedCreatorID            *string
	// This is intentionally an int64; the native JSON float64 type would allow to represent _some_ sub-second precision,
	// but not nearly enough (with current timestamp values, a single unit in the last place is on the order of hundreds of nanoseconds).
	// So, this is explicitly an int64, and we reject fractional values. If we did need more precise timestamps eventually,
	// we would add another field, UntrustedTimestampNS int64.
	untrustedTimestamp *int64
}

// UntrustedSignatureInformation is information available in an untrusted signature.
// This may be useful when debugging signature verification failures,
// or when managing a set of signatures on a single image.
//
// WARNING: Do not use the contents of this for ANY security decisions,
// and be VERY CAREFUL about showing this information to humans in any way which suggest that these values “are probably” reliable.
// There is NO REASON to expect the values to be correct, or not intentionally misleading
// (including things like “✅ Verified by $authority”)
type UntrustedSignatureInformation struct {
	UntrustedDockerManifestDigest digest.Digest
	UntrustedDockerReference      string // FIXME: more precise type?
	UntrustedCreatorID            *string
	UntrustedTimestamp            *time.Time
	UntrustedShortKeyIdentifier   string
}

// newUntrustedSignature returns an untrustedSignature object with
// the specified primary contents and appropriate metadata.
func newUntrustedSignature(dockerManifestDigest digest.Digest, dockerReference string) untrustedSignature {
	// Use intermediate variables for these values so that we can take their addresses.
	// Golang guarantees that they will have a new address on every execution.
	creatorID := "atomic " + version.Version
	timestamp := time.Now().Unix()
	return untrustedSignature{
		untrustedDockerManifestDigest: dockerManifestDigest,
		untrustedDockerReference:      dockerReference,
		untrustedCreatorID:            &creatorID,
		untrustedTimestamp:            &timestamp,
	}
}

// A compile-time check that untrustedSignature  and *untrustedSignature implements json.Marshaler
var _ json.Marshaler = untrustedSignature{}
var _ json.Marshaler = (*untrustedSignature)(nil)

// MarshalJSON implements the json.Marshaler interface.
func (s untrustedSignature) MarshalJSON() ([]byte, error) {
	if s.untrustedDockerManifestDigest == "" || s.untrustedDockerReference == "" {
		return nil, errors.New("Unexpected empty signature content")
	}
	critical := map[string]any{
		"type":     signatureType,
		"image":    map[string]string{"docker-manifest-digest": s.untrustedDockerManifestDigest.String()},
		"identity": map[string]string{"docker-reference": s.untrustedDockerReference},
	}
	optional := map[string]any{}
	if s.untrustedCreatorID != nil {
		optional["creator"] = *s.untrustedCreatorID
	}
	if s.untrustedTimestamp != nil {
		optional["timestamp"] = *s.untrustedTimestamp
	}
	signature := map[string]any{
		"critical": critical,
		"optional": optional,
	}
	return json.Marshal(signature)
}

// Compile-time check that untrustedSignature implements json.Unmarshaler
var _ json.Unmarshaler = (*untrustedSignature)(nil)

// UnmarshalJSON implements the json.Unmarshaler interface
func (s *untrustedSignature) UnmarshalJSON(data []byte) error {
	return internal.JSONFormatToInvalidSignatureError(s.strictUnmarshalJSON(data))
}

// strictUnmarshalJSON is UnmarshalJSON, except that it may return the internal.JSONFormatError error type.
// Splitting it into a separate function allows us to do the internal.JSONFormatError → InvalidSignatureError in a single place, the caller.
func (s *untrustedSignature) strictUnmarshalJSON(data []byte) error {
	var critical, optional json.RawMessage
	if err := internal.ParanoidUnmarshalJSONObjectExactFields(data, map[string]any{
		"critical": &critical,
		"optional": &optional,
	}); err != nil {
		return err
	}

	var creatorID string
	var timestamp float64
	var gotCreatorID, gotTimestamp = false, false
	if err := internal.ParanoidUnmarshalJSONObject(optional, func(key string) any {
		switch key {
		case "creator":
			gotCreatorID = true
			return &creatorID
		case "timestamp":
			gotTimestamp = true
			return &timestamp
		default:
			var ignore any
			return &ignore
		}
	}); err != nil {
		return err
	}
	if gotCreatorID {
		s.untrustedCreatorID = &creatorID
	}
	if gotTimestamp {
		intTimestamp := int64(timestamp)
		if float64(intTimestamp) != timestamp {
			return internal.NewInvalidSignatureError("Field optional.timestamp is not an integer")
		}
		s.untrustedTimestamp = &intTimestamp
	}

	var t string
	var image, identity json.RawMessage
	if err := internal.ParanoidUnmarshalJSONObjectExactFields(critical, map[string]any{
		"type":     &t,
		"image":    &image,
		"identity": &identity,
	}); err != nil {
		return err
	}
	if t != signatureType {
		return internal.NewInvalidSignatureError(fmt.Sprintf("Unrecognized signature type %s", t))
	}

	var digestString string
	if err := internal.ParanoidUnmarshalJSONObjectExactFields(image, map[string]any{
		"docker-manifest-digest": &digestString,
	}); err != nil {
		return err
	}
	digestValue, err := digest.Parse(digestString)
	if err != nil {
		return internal.NewInvalidSignatureError(fmt.Sprintf(`invalid docker-manifest-digest value %q: %v`, digestString, err))
	}
	s.untrustedDockerManifestDigest = digestValue

	return internal.ParanoidUnmarshalJSONObjectExactFields(identity, map[string]any{
		"docker-reference": &s.untrustedDockerReference,
	})
}

// Sign formats the signature and returns a blob signed using mech and keyIdentity
// (If it seems surprising that this is a method on untrustedSignature, note that there
// isn’t a good reason to think that a key used by the user is trusted by any component
// of the system just because it is a private key — actually the presence of a private key
// on the system increases the likelihood of an a successful attack on that private key
// on that particular system.)
func (s untrustedSignature) sign(mech SigningMechanism, keyIdentity string, passphrase string) ([]byte, error) {
	json, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}

	if newMech, ok := mech.(signingMechanismWithPassphrase); ok {
		return newMech.SignWithPassphrase(json, keyIdentity, passphrase)
	}

	if passphrase != "" {
		return nil, errors.New("signing mechanism does not support passphrases")
	}

	return mech.Sign(json, keyIdentity)
}

// signatureAcceptanceRules specifies how to decide whether an untrusted signature is acceptable.
// We centralize the actual parsing and data extraction in verifyAndExtractSignature; this supplies
// the policy.  We use an object instead of supplying func parameters to verifyAndExtractSignature
// because the functions have the same or similar types, so there is a risk of exchanging the functions;
// named members of this struct are more explicit.
type signatureAcceptanceRules struct {
	acceptedKeyIdentities              []string
	validateSignedDockerReference      func(string) error
	validateSignedDockerManifestDigest func(digest.Digest) error
}

// verifyAndExtractSignature verifies that unverifiedSignature has been signed, and that its principal components
// match expected values, both as specified by rules.
// Returns the signature, and an identity of the key that signed it.
func verifyAndExtractSignature(mech SigningMechanism, unverifiedSignature []byte, rules signatureAcceptanceRules) (*Signature, string, error) {
	signed, keyIdentity, err := mech.Verify(unverifiedSignature)
	if err != nil {
		return nil, "", err
	}
	if !slices.Contains(rules.acceptedKeyIdentities, keyIdentity) {
		withLookup, ok := mech.(signingMechanismWithVerificationIdentityLookup)
		if !ok {
			return nil, "", internal.NewInvalidSignatureError(fmt.Sprintf("signature by key %s is not accepted", keyIdentity))
		}

		primaryKey, err := withLookup.keyIdentityForVerificationKeyIdentity(keyIdentity)
		if err != nil {
			// Coverage: This only fails if lookup by keyIdentity fails, but we just found and used that key.
			// Or maybe on some unexpected I/O error.
			return nil, "", err
		}
		if !slices.Contains(rules.acceptedKeyIdentities, primaryKey) {
			return nil, "", internal.NewInvalidSignatureError(fmt.Sprintf("signature by key %s of %s is not accepted", keyIdentity, primaryKey))
		}
		keyIdentity = primaryKey
	}

	var unmatchedSignature untrustedSignature
	if err := json.Unmarshal(signed, &unmatchedSignature); err != nil {
		return nil, "", internal.NewInvalidSignatureError(err.Error())
	}
	if err := rules.validateSignedDockerManifestDigest(unmatchedSignature.untrustedDockerManifestDigest); err != nil {
		return nil, "", err
	}
	if err := rules.validateSignedDockerReference(unmatchedSignature.untrustedDockerReference); err != nil {
		return nil, "", err
	}
	// signatureAcceptanceRules have accepted this value.
	return &Signature{
		DockerManifestDigest: unmatchedSignature.untrustedDockerManifestDigest,
		DockerReference:      unmatchedSignature.untrustedDockerReference,
	}, keyIdentity, nil
}

// GetUntrustedSignatureInformationWithoutVerifying extracts information available in an untrusted signature,
// WITHOUT doing any cryptographic verification.
// This may be useful when debugging signature verification failures,
// or when managing a set of signatures on a single image.
//
// WARNING: Do not use the contents of this for ANY security decisions,
// and be VERY CAREFUL about showing this information to humans in any way which suggest that these values “are probably” reliable.
// There is NO REASON to expect the values to be correct, or not intentionally misleading
// (including things like “✅ Verified by $authority”)
func GetUntrustedSignatureInformationWithoutVerifying(untrustedSignatureBytes []byte) (*UntrustedSignatureInformation, error) {
	// NOTE: This should eventually do format autodetection.
	mech, _, err := NewEphemeralGPGSigningMechanism([]byte{})
	if err != nil {
		return nil, err
	}
	defer mech.Close()

	untrustedContents, shortKeyIdentifier, err := mech.UntrustedSignatureContents(untrustedSignatureBytes)
	if err != nil {
		return nil, err
	}
	var untrustedDecodedContents untrustedSignature
	if err := json.Unmarshal(untrustedContents, &untrustedDecodedContents); err != nil {
		return nil, internal.NewInvalidSignatureError(err.Error())
	}

	var timestamp *time.Time // = nil
	if untrustedDecodedContents.untrustedTimestamp != nil {
		ts := time.Unix(*untrustedDecodedContents.untrustedTimestamp, 0)
		timestamp = &ts
	}
	return &UntrustedSignatureInformation{
		UntrustedDockerManifestDigest: untrustedDecodedContents.untrustedDockerManifestDigest,
		UntrustedDockerReference:      untrustedDecodedContents.untrustedDockerReference,
		UntrustedCreatorID:            untrustedDecodedContents.untrustedCreatorID,
		UntrustedTimestamp:            timestamp,
		UntrustedShortKeyIdentifier:   shortKeyIdentifier,
	}, nil
}
