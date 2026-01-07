package internal

import (
	"bytes"
	"crypto"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	digest "github.com/opencontainers/go-digest"
	sigstoreSignature "github.com/sigstore/sigstore/pkg/signature"
	"go.podman.io/image/v5/version"
)

const (
	sigstoreSignatureType         = "cosign container image signature"
	sigstoreHarcodedHashAlgorithm = crypto.SHA256
)

// UntrustedSigstorePayload is a parsed content of a sigstore signature payload (not the full signature)
type UntrustedSigstorePayload struct {
	untrustedDockerManifestDigest digest.Digest
	untrustedDockerReference      string // FIXME: more precise type?
	untrustedCreatorID            *string
	// This is intentionally an int64; the native JSON float64 type would allow to represent _some_ sub-second precision,
	// but not nearly enough (with current timestamp values, a single unit in the last place is on the order of hundreds of nanoseconds).
	// So, this is explicitly an int64, and we reject fractional values. If we did need more precise timestamps eventually,
	// we would add another field, UntrustedTimestampNS int64.
	untrustedTimestamp *int64
}

// NewUntrustedSigstorePayload returns an UntrustedSigstorePayload object with
// the specified primary contents and appropriate metadata.
func NewUntrustedSigstorePayload(dockerManifestDigest digest.Digest, dockerReference string) UntrustedSigstorePayload {
	// Use intermediate variables for these values so that we can take their addresses.
	// Golang guarantees that they will have a new address on every execution.
	creatorID := "containers/image " + version.Version
	timestamp := time.Now().Unix()
	return UntrustedSigstorePayload{
		untrustedDockerManifestDigest: dockerManifestDigest,
		untrustedDockerReference:      dockerReference,
		untrustedCreatorID:            &creatorID,
		untrustedTimestamp:            &timestamp,
	}
}

// A compile-time check that UntrustedSigstorePayload and *UntrustedSigstorePayload implements json.Marshaler
var _ json.Marshaler = UntrustedSigstorePayload{}
var _ json.Marshaler = (*UntrustedSigstorePayload)(nil)

// MarshalJSON implements the json.Marshaler interface.
func (s UntrustedSigstorePayload) MarshalJSON() ([]byte, error) {
	if s.untrustedDockerManifestDigest == "" || s.untrustedDockerReference == "" {
		return nil, errors.New("Unexpected empty signature content")
	}
	critical := map[string]any{
		"type":     sigstoreSignatureType,
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

// Compile-time check that UntrustedSigstorePayload implements json.Unmarshaler
var _ json.Unmarshaler = (*UntrustedSigstorePayload)(nil)

// UnmarshalJSON implements the json.Unmarshaler interface
func (s *UntrustedSigstorePayload) UnmarshalJSON(data []byte) error {
	return JSONFormatToInvalidSignatureError(s.strictUnmarshalJSON(data))
}

// strictUnmarshalJSON is UnmarshalJSON, except that it may return the internal JSONFormatError error type.
// Splitting it into a separate function allows us to do the JSONFormatError → InvalidSignatureError in a single place, the caller.
func (s *UntrustedSigstorePayload) strictUnmarshalJSON(data []byte) error {
	var critical, optional json.RawMessage
	if err := ParanoidUnmarshalJSONObjectExactFields(data, map[string]any{
		"critical": &critical,
		"optional": &optional,
	}); err != nil {
		return err
	}

	var creatorID string
	var timestamp float64
	var gotCreatorID, gotTimestamp = false, false
	// /usr/bin/cosign generates "optional": null if there are no user-specified annotations.
	if !bytes.Equal(optional, []byte("null")) {
		if err := ParanoidUnmarshalJSONObject(optional, func(key string) any {
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
	}
	if gotCreatorID {
		s.untrustedCreatorID = &creatorID
	}
	if gotTimestamp {
		intTimestamp := int64(timestamp)
		if float64(intTimestamp) != timestamp {
			return NewInvalidSignatureError("Field optional.timestamp is not an integer")
		}
		s.untrustedTimestamp = &intTimestamp
	}

	var t string
	var image, identity json.RawMessage
	if err := ParanoidUnmarshalJSONObjectExactFields(critical, map[string]any{
		"type":     &t,
		"image":    &image,
		"identity": &identity,
	}); err != nil {
		return err
	}
	if t != sigstoreSignatureType {
		return NewInvalidSignatureError(fmt.Sprintf("Unrecognized signature type %s", t))
	}

	var digestString string
	if err := ParanoidUnmarshalJSONObjectExactFields(image, map[string]any{
		"docker-manifest-digest": &digestString,
	}); err != nil {
		return err
	}
	digestValue, err := digest.Parse(digestString)
	if err != nil {
		return NewInvalidSignatureError(fmt.Sprintf(`invalid docker-manifest-digest value %q: %v`, digestString, err))
	}
	s.untrustedDockerManifestDigest = digestValue

	return ParanoidUnmarshalJSONObjectExactFields(identity, map[string]any{
		"docker-reference": &s.untrustedDockerReference,
	})
}

// SigstorePayloadAcceptanceRules specifies how to decide whether an untrusted payload is acceptable.
// We centralize the actual parsing and data extraction in VerifySigstorePayload; this supplies
// the policy.  We use an object instead of supplying func parameters to verifyAndExtractSignature
// because the functions have the same or similar types, so there is a risk of exchanging the functions;
// named members of this struct are more explicit.
type SigstorePayloadAcceptanceRules struct {
	ValidateSignedDockerReference      func(string) error
	ValidateSignedDockerManifestDigest func(digest.Digest) error
}

// verifySigstorePayloadBlobSignature verifies unverifiedSignature of unverifiedPayload was correctly created
// by any of the public keys in publicKeys.
//
// This is an internal implementation detail of VerifySigstorePayload and should have no other callers.
// It is INSUFFICIENT alone to consider the signature acceptable.
func verifySigstorePayloadBlobSignature(publicKeys []crypto.PublicKey, unverifiedPayload, unverifiedSignature []byte) error {
	if len(publicKeys) == 0 {
		return errors.New("Need at least one public key to verify the sigstore payload, but got 0")
	}

	verifiers := make([]sigstoreSignature.Verifier, 0, len(publicKeys))
	for _, key := range publicKeys {
		// Failing to load a verifier indicates that something is really, really
		// invalid about the public key; prefer to fail even if the signature might be
		// valid with other keys, so that users fix their fallback keys before they need them.
		// For that reason, we even initialize all verifiers before trying to validate the signature
		// with any key.
		verifier, err := sigstoreSignature.LoadVerifier(key, sigstoreHarcodedHashAlgorithm)
		if err != nil {
			return err
		}
		verifiers = append(verifiers, verifier)
	}

	var failures []string
	for _, verifier := range verifiers {
		// github.com/sigstore/cosign/pkg/cosign.verifyOCISignature uses signatureoptions.WithContext(),
		// which seems to be not used by anything. So we don’t bother.
		err := verifier.VerifySignature(bytes.NewReader(unverifiedSignature), bytes.NewReader(unverifiedPayload))
		if err == nil {
			return nil
		}

		failures = append(failures, err.Error())
	}

	if len(failures) == 0 {
		// Coverage: We have checked there is at least one public key, any success causes an early return,
		// and any failure adds an entry to failures => there must be at least one error.
		return fmt.Errorf("Internal error: signature verification failed but no errors have been recorded")
	}
	return NewInvalidSignatureError("cryptographic signature verification failed: " + strings.Join(failures, ", "))
}

// VerifySigstorePayload verifies unverifiedBase64Signature of unverifiedPayload was correctly created by any of the public keys in publicKeys, and that its principal components
// match expected values, both as specified by rules, and returns it.
// We return an *UntrustedSigstorePayload, although nothing actually uses it,
// just to double-check against stupid typos.
func VerifySigstorePayload(publicKeys []crypto.PublicKey, unverifiedPayload []byte, unverifiedBase64Signature string, rules SigstorePayloadAcceptanceRules) (*UntrustedSigstorePayload, error) {
	unverifiedSignature, err := base64.StdEncoding.DecodeString(unverifiedBase64Signature)
	if err != nil {
		return nil, NewInvalidSignatureError(fmt.Sprintf("base64 decoding: %v", err))
	}

	if err := verifySigstorePayloadBlobSignature(publicKeys, unverifiedPayload, unverifiedSignature); err != nil {
		return nil, err
	}

	var unmatchedPayload UntrustedSigstorePayload
	if err := json.Unmarshal(unverifiedPayload, &unmatchedPayload); err != nil {
		return nil, NewInvalidSignatureError(err.Error())
	}
	if err := rules.ValidateSignedDockerManifestDigest(unmatchedPayload.untrustedDockerManifestDigest); err != nil {
		return nil, err
	}
	if err := rules.ValidateSignedDockerReference(unmatchedPayload.untrustedDockerReference); err != nil {
		return nil, err
	}
	// SigstorePayloadAcceptanceRules have accepted this value.
	return &unmatchedPayload, nil
}
