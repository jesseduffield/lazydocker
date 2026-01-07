package internal

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

// This is the github.com/sigstore/rekor/pkg/generated/models.Hashedrekord.APIVersion for github.com/sigstore/rekor/pkg/generated/models.HashedrekordV001Schema.
// We could alternatively use github.com/sigstore/rekor/pkg/types/hashedrekord.APIVERSION, but that subpackage adds too many dependencies.
const RekorHashedRekordV001APIVersion = "0.0.1"

// UntrustedRekorSET is a parsed content of the sigstore-signature Rekor SET
// (note that this a signature-specific format, not a format directly used by the Rekor API).
// This corresponds to github.com/sigstore/cosign/bundle.RekorBundle, but we impose a stricter decoder.
type UntrustedRekorSET struct {
	UntrustedSignedEntryTimestamp []byte // A signature over some canonical JSON form of UntrustedPayload
	UntrustedPayload              json.RawMessage
}

type UntrustedRekorPayload struct {
	Body           []byte // In cosign, this is an any, but only a string works
	IntegratedTime int64
	LogIndex       int64
	LogID          string
}

// A compile-time check that UntrustedRekorSET implements json.Unmarshaler
var _ json.Unmarshaler = (*UntrustedRekorSET)(nil)

// UnmarshalJSON implements the json.Unmarshaler interface
func (s *UntrustedRekorSET) UnmarshalJSON(data []byte) error {
	return JSONFormatToInvalidSignatureError(s.strictUnmarshalJSON(data))
}

// strictUnmarshalJSON is UnmarshalJSON, except that it may return the internal JSONFormatError error type.
// Splitting it into a separate function allows us to do the JSONFormatError → InvalidSignatureError in a single place, the caller.
func (s *UntrustedRekorSET) strictUnmarshalJSON(data []byte) error {
	return ParanoidUnmarshalJSONObjectExactFields(data, map[string]any{
		"SignedEntryTimestamp": &s.UntrustedSignedEntryTimestamp,
		"Payload":              &s.UntrustedPayload,
	})
}

// A compile-time check that UntrustedRekorSET and *UntrustedRekorSET implements json.Marshaler
var _ json.Marshaler = UntrustedRekorSET{}
var _ json.Marshaler = (*UntrustedRekorSET)(nil)

// MarshalJSON implements the json.Marshaler interface.
func (s UntrustedRekorSET) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"SignedEntryTimestamp": s.UntrustedSignedEntryTimestamp,
		"Payload":              s.UntrustedPayload,
	})
}

// A compile-time check that UntrustedRekorPayload implements json.Unmarshaler
var _ json.Unmarshaler = (*UntrustedRekorPayload)(nil)

// UnmarshalJSON implements the json.Unmarshaler interface
func (p *UntrustedRekorPayload) UnmarshalJSON(data []byte) error {
	return JSONFormatToInvalidSignatureError(p.strictUnmarshalJSON(data))
}

// strictUnmarshalJSON is UnmarshalJSON, except that it may return the internal JSONFormatError error type.
// Splitting it into a separate function allows us to do the JSONFormatError → InvalidSignatureError in a single place, the caller.
func (p *UntrustedRekorPayload) strictUnmarshalJSON(data []byte) error {
	return ParanoidUnmarshalJSONObjectExactFields(data, map[string]any{
		"body":           &p.Body,
		"integratedTime": &p.IntegratedTime,
		"logIndex":       &p.LogIndex,
		"logID":          &p.LogID,
	})
}

// A compile-time check that UntrustedRekorPayload and *UntrustedRekorPayload implements json.Marshaler
var _ json.Marshaler = UntrustedRekorPayload{}
var _ json.Marshaler = (*UntrustedRekorPayload)(nil)

// MarshalJSON implements the json.Marshaler interface.
func (p UntrustedRekorPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"body":           p.Body,
		"integratedTime": p.IntegratedTime,
		"logIndex":       p.LogIndex,
		"logID":          p.LogID,
	})
}

// VerifyRekorSET verifies that unverifiedRekorSET is correctly signed by publicKey and matches the rest of the data.
// Returns bundle upload time on success.
func VerifyRekorSET(publicKeys []*ecdsa.PublicKey, unverifiedRekorSET []byte, unverifiedKeyOrCertBytes []byte, unverifiedBase64Signature string, unverifiedPayloadBytes []byte) (time.Time, error) {
	// FIXME: Should the publicKey parameter hard-code ecdsa?

	// == Parse SET bytes
	var untrustedSET UntrustedRekorSET
	// Sadly. we need to parse and transform untrusted data before verifying a cryptographic signature...
	if err := json.Unmarshal(unverifiedRekorSET, &untrustedSET); err != nil {
		return time.Time{}, NewInvalidSignatureError(err.Error())
	}
	// == Verify SET signature
	// Cosign unmarshals and re-marshals UntrustedPayload; that seems unnecessary,
	// assuming jsoncanonicalizer is designed to operate on untrusted data.
	untrustedSETPayloadCanonicalBytes, err := jsoncanonicalizer.Transform(untrustedSET.UntrustedPayload)
	if err != nil {
		return time.Time{}, NewInvalidSignatureError(fmt.Sprintf("canonicalizing Rekor SET JSON: %v", err))
	}
	untrustedSETPayloadHash := sha256.Sum256(untrustedSETPayloadCanonicalBytes)
	publicKeymatched := false
	for _, pk := range publicKeys {
		if ecdsa.VerifyASN1(pk, untrustedSETPayloadHash[:], untrustedSET.UntrustedSignedEntryTimestamp) {
			publicKeymatched = true
			break
		}
	}
	if !publicKeymatched {
		return time.Time{}, NewInvalidSignatureError("cryptographic signature verification of Rekor SET failed")
	}

	// == Parse SET payload
	// Parse the cryptographically-verified canonicalized variant, NOT the originally-delivered representation,
	// to decrease risk of exploiting the JSON parser. Note that if there were an arbitrary execution vulnerability, the attacker
	// could have exploited the parsing of unverifiedRekorSET above already; so this, at best, ensures more consistent processing
	// of the SET payload.
	var rekorPayload UntrustedRekorPayload
	if err := json.Unmarshal(untrustedSETPayloadCanonicalBytes, &rekorPayload); err != nil {
		return time.Time{}, NewInvalidSignatureError(fmt.Sprintf("parsing Rekor SET payload: %v", err.Error()))
	}
	// FIXME: Consider being much more strict about decoding JSON.
	var hashedRekord RekorHashedrekord
	if err := json.Unmarshal(rekorPayload.Body, &hashedRekord); err != nil {
		return time.Time{}, NewInvalidSignatureError(fmt.Sprintf("decoding the body of a Rekor SET payload: %v", err))
	}
	// The decode of HashedRekord validates the "kind": "hashedrecord" field, which is otherwise invisible to us.
	if hashedRekord.APIVersion == nil {
		return time.Time{}, NewInvalidSignatureError("missing Rekor SET Payload API version")
	}
	if *hashedRekord.APIVersion != RekorHashedRekordV001APIVersion {
		return time.Time{}, NewInvalidSignatureError(fmt.Sprintf("unsupported Rekor SET Payload hashedrekord version %#v", hashedRekord.APIVersion))
	}
	var hashedRekordV001 RekorHashedrekordV001Schema
	if err := json.Unmarshal(hashedRekord.Spec, &hashedRekordV001); err != nil {
		return time.Time{}, NewInvalidSignatureError(fmt.Sprintf("decoding hashedrekod spec: %v", err))
	}

	// == Match unverifiedKeyOrCertBytes
	if hashedRekordV001.Signature == nil {
		return time.Time{}, NewInvalidSignatureError(`Missing "signature" field in hashedrekord`)
	}
	if hashedRekordV001.Signature.PublicKey == nil {
		return time.Time{}, NewInvalidSignatureError(`Missing "signature.publicKey" field in hashedrekord`)

	}
	rekorKeyOrCertPEM, rest := pem.Decode(hashedRekordV001.Signature.PublicKey.Content)
	if rekorKeyOrCertPEM == nil {
		return time.Time{}, NewInvalidSignatureError("publicKey in Rekor SET is not in PEM format")
	}
	if len(rest) != 0 {
		return time.Time{}, NewInvalidSignatureError("publicKey in Rekor SET has trailing data")
	}
	// FIXME: For public keys, let the caller provide the DER-formatted blob instead
	// of round-tripping through PEM.
	unverifiedKeyOrCertPEM, rest := pem.Decode(unverifiedKeyOrCertBytes)
	if unverifiedKeyOrCertPEM == nil {
		return time.Time{}, NewInvalidSignatureError("public key or cert to be matched against publicKey in Rekor SET is not in PEM format")
	}
	if len(rest) != 0 {
		return time.Time{}, NewInvalidSignatureError("public key or cert to be matched against publicKey in Rekor SET has trailing data")
	}
	// NOTE: This compares the PEM payload, but not the object type or headers.
	if !bytes.Equal(rekorKeyOrCertPEM.Bytes, unverifiedKeyOrCertPEM.Bytes) {
		return time.Time{}, NewInvalidSignatureError("publicKey in Rekor SET does not match")
	}
	// == Match unverifiedSignatureBytes
	unverifiedSignatureBytes, err := base64.StdEncoding.DecodeString(unverifiedBase64Signature)
	if err != nil {
		return time.Time{}, NewInvalidSignatureError(fmt.Sprintf("decoding signature base64: %v", err))
	}
	if !bytes.Equal(hashedRekordV001.Signature.Content, unverifiedSignatureBytes) {
		return time.Time{}, NewInvalidSignatureError(fmt.Sprintf("signature in Rekor SET does not match: %#v vs. %#v",
			string(hashedRekordV001.Signature.Content), string(unverifiedSignatureBytes)))
	}

	// == Match unverifiedPayloadBytes
	if hashedRekordV001.Data == nil {
		return time.Time{}, NewInvalidSignatureError(`Missing "data" field in hashedrekord`)
	}
	if hashedRekordV001.Data.Hash == nil {
		return time.Time{}, NewInvalidSignatureError(`Missing "data.hash" field in hashedrekord`)
	}
	if hashedRekordV001.Data.Hash.Algorithm == nil {
		return time.Time{}, NewInvalidSignatureError(`Missing "data.hash.algorithm" field in hashedrekord`)
	}
	// FIXME: Rekor 1.3.5 has added SHA-386 and SHA-512 as recognized values.
	// Eventually we should support them as well.
	// Short-term, Cosign (as of 2024-02 and Cosign 2.2.3) only produces and accepts SHA-256, so right now that’s not a compatibility
	// issue.
	if *hashedRekordV001.Data.Hash.Algorithm != RekorHashedrekordV001SchemaDataHashAlgorithmSha256 {
		return time.Time{}, NewInvalidSignatureError(fmt.Sprintf(`Unexpected "data.hash.algorithm" value %#v`, *hashedRekordV001.Data.Hash.Algorithm))
	}
	if hashedRekordV001.Data.Hash.Value == nil {
		return time.Time{}, NewInvalidSignatureError(`Missing "data.hash.value" field in hashedrekord`)
	}
	rekorPayloadHash, err := hex.DecodeString(*hashedRekordV001.Data.Hash.Value)
	if err != nil {
		return time.Time{}, NewInvalidSignatureError(fmt.Sprintf(`Invalid "data.hash.value" field in hashedrekord: %v`, err))

	}
	unverifiedPayloadHash := sha256.Sum256(unverifiedPayloadBytes)
	if !bytes.Equal(rekorPayloadHash, unverifiedPayloadHash[:]) {
		return time.Time{}, NewInvalidSignatureError("payload in Rekor SET does not match")
	}

	// == All OK; return the relevant time.
	return time.Unix(rekorPayload.IntegratedTime, 0), nil
}
