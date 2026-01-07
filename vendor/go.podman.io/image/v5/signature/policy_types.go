// Note: Consider the API unstable until the code supports at least three different image formats or transports.

// This defines types used to represent a signature verification policy in memory.
// Do not use the private types directly; either parse a configuration file, or construct a Policy from PolicyRequirements
// built using the constructor functions provided in policy_config.go.

package signature

// NOTE: Keep this in sync with docs/containers-policy.json.5.md!

// Policy defines requirements for considering a signature, or an image, valid.
type Policy struct {
	// Default applies to any image which does not have a matching policy in Transports.
	// Note that this can happen even if a matching PolicyTransportScopes exists in Transports
	// if the image matches none of the scopes.
	Default    PolicyRequirements               `json:"default"`
	Transports map[string]PolicyTransportScopes `json:"transports"`
}

// PolicyTransportScopes defines policies for images for a specific transport,
// for various scopes, the map keys.
// Scopes are defined by the transport (types.ImageReference.PolicyConfigurationIdentity etc.);
// there is one scope precisely matching to a single image, and namespace scopes as prefixes
// of the single-image scope. (e.g. hostname[/zero[/or[/more[/namespaces[/individualimage]]]]])
// The empty scope, if exists, is considered a parent namespace of all other scopes.
// Most specific scope wins, duplication is prohibited (hard failure).
type PolicyTransportScopes map[string]PolicyRequirements

// PolicyRequirements is a set of requirements applying to a set of images; each of them must be satisfied (though perhaps each by a different signature).
// Must not be empty, frequently will only contain a single element.
type PolicyRequirements []PolicyRequirement

// PolicyRequirement is a rule which must be satisfied by at least one of the signatures of an image.
// The type is public, but its definition is private.

// prCommon is the common type field in a JSON encoding of PolicyRequirement.
type prCommon struct {
	Type prTypeIdentifier `json:"type"`
}

// prTypeIdentifier is string designating a kind of a PolicyRequirement.
type prTypeIdentifier string

const (
	prTypeInsecureAcceptAnything prTypeIdentifier = "insecureAcceptAnything"
	prTypeReject                 prTypeIdentifier = "reject"
	prTypeSignedBy               prTypeIdentifier = "signedBy"
	prTypeSignedBaseLayer        prTypeIdentifier = "signedBaseLayer"
	prTypeSigstoreSigned         prTypeIdentifier = "sigstoreSigned"
)

// prInsecureAcceptAnything is a PolicyRequirement with type = prTypeInsecureAcceptAnything:
// every image is allowed to run.
// Note that because PolicyRequirements are implicitly ANDed, this is necessary only if it is the only rule (to make the list non-empty and the policy explicit).
// NOTE: This allows the image to run; it DOES NOT consider the signature verified (per IsSignatureAuthorAccepted).
// FIXME? Better name?
type prInsecureAcceptAnything struct {
	prCommon
}

// prReject is a PolicyRequirement with type = prTypeReject: every image is rejected.
type prReject struct {
	prCommon
}

// prSignedBy is a PolicyRequirement with type = prTypeSignedBy: the image is signed by trusted keys for a specified identity
type prSignedBy struct {
	prCommon

	// KeyType specifies what kind of key reference KeyPath/KeyPaths/KeyData is.
	// Acceptable values are “GPGKeys” | “signedByGPGKeys” “X.509Certificates” | “signedByX.509CAs”
	// FIXME: eventually also support GPGTOFU, X.509TOFU, with KeyPath only
	KeyType sbKeyType `json:"keyType"`

	// KeyPath is a pathname to a local file containing the trusted key(s). Exactly one of KeyPath, KeyPaths and KeyData must be specified.
	KeyPath string `json:"keyPath,omitempty"`
	// KeyPaths is a set of pathnames to local files containing the trusted key(s). Exactly one of KeyPath, KeyPaths and KeyData must be specified.
	KeyPaths []string `json:"keyPaths,omitempty"`
	// KeyData contains the trusted key(s), base64-encoded. Exactly one of KeyPath, KeyPaths and KeyData must be specified.
	KeyData []byte `json:"keyData,omitempty"`

	// SignedIdentity specifies what image identity the signature must be claiming about the image.
	// Defaults to "matchRepoDigestOrExact" if not specified.
	SignedIdentity PolicyReferenceMatch `json:"signedIdentity"`
}

// sbKeyType are the allowed values for prSignedBy.KeyType
type sbKeyType string

const (
	// SBKeyTypeGPGKeys refers to keys contained in a GPG keyring
	SBKeyTypeGPGKeys sbKeyType = "GPGKeys"
	// SBKeyTypeSignedByGPGKeys refers to keys signed by keys in a GPG keyring
	SBKeyTypeSignedByGPGKeys sbKeyType = "signedByGPGKeys"
	// SBKeyTypeX509Certificates refers to keys in a set of X.509 certificates
	// FIXME: PEM, DER?
	SBKeyTypeX509Certificates sbKeyType = "X509Certificates"
	// SBKeyTypeSignedByX509CAs refers to keys signed by one of the X.509 CAs
	// FIXME: PEM, DER?
	SBKeyTypeSignedByX509CAs sbKeyType = "signedByX509CAs"
)

// prSignedBaseLayer is a PolicyRequirement with type = prSignedBaseLayer: the image has a specified, correctly signed, base image.
type prSignedBaseLayer struct {
	prCommon
	// BaseLayerIdentity specifies the base image to look for. "match-exact" is rejected, "match-repository" is unlikely to be useful.
	BaseLayerIdentity PolicyReferenceMatch `json:"baseLayerIdentity"`
}

// prSigstoreSigned is a PolicyRequirement with type = prTypeSigstoreSigned: the image is signed by trusted keys for a specified identity
type prSigstoreSigned struct {
	prCommon

	// KeyPath is a pathname to a local file containing the trusted key. Exactly one of KeyPath, KeyPaths, KeyData, KeyDatas, Fulcio, and PKI must be specified.
	KeyPath string `json:"keyPath,omitempty"`
	// KeyPaths is a set of pathnames to local files containing the trusted key(s). Exactly one of KeyPath, KeyPaths, KeyData, KeyDatas, Fulcio, and PKI must be specified.
	KeyPaths []string `json:"keyPaths,omitempty"`
	// KeyData contains the trusted key, base64-encoded. Exactly one of KeyPath, KeyPaths, KeyData, KeyDatas, Fulcio, and PKI must be specified.
	KeyData []byte `json:"keyData,omitempty"`
	// KeyDatas is a set of trusted keys, base64-encoded. Exactly one of KeyPath, KeyPaths, KeyData, KeyDatas, Fulcio, and PKI must be specified.
	KeyDatas [][]byte `json:"keyDatas,omitempty"`

	// Fulcio specifies which Fulcio-generated certificates are accepted. Exactly one of KeyPath, KeyPaths, KeyData, KeyDatas, Fulcio, and PKI must be specified.
	// If Fulcio is specified, one of RekorPublicKeyPath or RekorPublicKeyData must be specified as well.
	Fulcio PRSigstoreSignedFulcio `json:"fulcio,omitempty"`

	// RekorPublicKeyPath is a pathname to local file containing a public key of a Rekor server which must record acceptable signatures.
	// If Fulcio is used, one of RekorPublicKeyPath, RekorPublicKeyPaths, RekorPublicKeyData and RekorPublicKeyDatas must be specified as well;
	// otherwise it is optional (and Rekor inclusion is not required if a Rekor public key is not specified).
	RekorPublicKeyPath string `json:"rekorPublicKeyPath,omitempty"`
	// RekorPublicKeyPaths is a set of pathnames to local files, each containing a public key of a Rekor server. One of the keys must record acceptable signatures.
	// If Fulcio is used, one of RekorPublicKeyPath, RekorPublicKeyPaths, RekorPublicKeyData and RekorPublicKeyDatas must be specified as well;
	// otherwise it is optional (and Rekor inclusion is not required if a Rekor public key is not specified).
	RekorPublicKeyPaths []string `json:"rekorPublicKeyPaths,omitempty"`
	// RekorPublicKeyPath contain a base64-encoded public key of a Rekor server which must record acceptable signatures.
	// If Fulcio is used, one of RekorPublicKeyPath, RekorPublicKeyPaths, RekorPublicKeyData and RekorPublicKeyDatas must be specified as well;
	// otherwise it is optional (and Rekor inclusion is not required if a Rekor public key is not specified).
	RekorPublicKeyData []byte `json:"rekorPublicKeyData,omitempty"`
	// RekorPublicKeyDatas each contain a base64-encoded public key of a Rekor server. One of the keys must record acceptable signatures.
	// If Fulcio is used, one of RekorPublicKeyPath, RekorPublicKeyPaths, RekorPublicKeyData and RekorPublicKeyDatas must be specified as well;
	// otherwise it is optional (and Rekor inclusion is not required if a Rekor public key is not specified).
	RekorPublicKeyDatas [][]byte `json:"rekorPublicKeyDatas,omitempty"`

	// PKI specifies which PKI-generated certificates are accepted. Exactly one of KeyPath, KeyPaths, KeyData, KeyDatas, Fulcio, and PKI must be specified.
	PKI PRSigstoreSignedPKI `json:"pki,omitempty"`

	// SignedIdentity specifies what image identity the signature must be claiming about the image.
	// Defaults to "matchRepoDigestOrExact" if not specified.
	// Note that /usr/bin/cosign interoperability might require using repo-only matching.
	SignedIdentity PolicyReferenceMatch `json:"signedIdentity"`
}

// PRSigstoreSignedFulcio contains Fulcio configuration options for a "sigstoreSigned" PolicyRequirement.
// This is a public type with a single private implementation.
type PRSigstoreSignedFulcio interface {
	// toFulcioTrustRoot creates a fulcioTrustRoot from the input data.
	// (This also prevents external implementations of this interface, ensuring that prSigstoreSignedFulcio is the only one.)
	prepareTrustRoot() (*fulcioTrustRoot, error)
}

// prSigstoreSignedFulcio collects Fulcio configuration options for prSigstoreSigned
type prSigstoreSignedFulcio struct {
	// CAPath a path to a file containing accepted CA root certificates, in PEM format. Exactly one of CAPath and CAData must be specified.
	CAPath string `json:"caPath,omitempty"`
	// CAData contains accepted CA root certificates in PEM format, all of that base64-encoded. Exactly one of CAPath and CAData must be specified.
	CAData []byte `json:"caData,omitempty"`
	// OIDCIssuer specifies the expected OIDC issuer, recorded by Fulcio into the generated certificates.
	OIDCIssuer string `json:"oidcIssuer,omitempty"`
	// SubjectEmail specifies the expected email address of the authenticated OIDC identity, recorded by Fulcio into the generated certificates.
	SubjectEmail string `json:"subjectEmail,omitempty"`
}

// PRSigstoreSignedPKI contains PKI configuration options for a "sigstoreSigned" PolicyRequirement.
type PRSigstoreSignedPKI interface {
	// prepareTrustRoot creates a pkiTrustRoot from the input data.
	// (This also prevents external implementations of this interface, ensuring that prSigstoreSignedPKI is the only one.)
	prepareTrustRoot() (*pkiTrustRoot, error)
}

// prSigstoreSignedPKI contains non-fulcio certificate PKI configuration options for prSigstoreSigned
type prSigstoreSignedPKI struct {
	// CARootsPath a path to a file containing accepted CA root certificates, in PEM format. Exactly one of CARootsPath and CARootsData must be specified.
	CARootsPath string `json:"caRootsPath,omitempty"`
	// CARootsData contains accepted CA root certificates in PEM format, all of that base64-encoded. Exactly one of CARootsPath and CARootsData must be specified.
	CARootsData []byte `json:"caRootsData,omitempty"`
	// CAIntermediatesPath a path to a file containing accepted CA intermediate certificates, in PEM format. Only one of CAIntermediatesPath or CAIntermediatesData can be specified, not both.
	CAIntermediatesPath string `json:"caIntermediatesPath,omitempty"`
	// CAIntermediatesData contains accepted CA intermediate certificates in PEM format, all of that base64-encoded. Only one of CAIntermediatesPath or CAIntermediatesData can be specified, not both.
	CAIntermediatesData []byte `json:"caIntermediatesData,omitempty"`

	// SubjectEmail specifies the expected email address imposed on the subject to which the certificate was issued. At least one of SubjectEmail and SubjectHostname must be specified.
	SubjectEmail string `json:"subjectEmail,omitempty"`
	// SubjectHostname specifies the expected hostname imposed on the subject to which the certificate was issued. At least one of SubjectEmail and SubjectHostname must be specified.
	SubjectHostname string `json:"subjectHostname,omitempty"`
}

// PolicyReferenceMatch specifies a set of image identities accepted in PolicyRequirement.
// The type is public, but its implementation is private.

// prmCommon is the common type field in a JSON encoding of PolicyReferenceMatch.
type prmCommon struct {
	Type prmTypeIdentifier `json:"type"`
}

// prmTypeIdentifier is string designating a kind of a PolicyReferenceMatch.
type prmTypeIdentifier string

const (
	prmTypeMatchExact             prmTypeIdentifier = "matchExact"
	prmTypeMatchRepoDigestOrExact prmTypeIdentifier = "matchRepoDigestOrExact"
	prmTypeMatchRepository        prmTypeIdentifier = "matchRepository"
	prmTypeExactReference         prmTypeIdentifier = "exactReference"
	prmTypeExactRepository        prmTypeIdentifier = "exactRepository"
	prmTypeRemapIdentity          prmTypeIdentifier = "remapIdentity"
)

// prmMatchExact is a PolicyReferenceMatch with type = prmMatchExact: the two references must match exactly.
type prmMatchExact struct {
	prmCommon
}

// prmMatchRepoDigestOrExact is a PolicyReferenceMatch with type = prmMatchExactOrDigest: the two references must match exactly,
// except that digest references are also accepted if the repository name matches (regardless of tag/digest) and the signature applies to the referenced digest
type prmMatchRepoDigestOrExact struct {
	prmCommon
}

// prmMatchRepository is a PolicyReferenceMatch with type = prmMatchRepository: the two references must use the same repository, may differ in the tag.
type prmMatchRepository struct {
	prmCommon
}

// prmExactReference is a PolicyReferenceMatch with type = prmExactReference: matches a specified reference exactly.
type prmExactReference struct {
	prmCommon
	DockerReference string `json:"dockerReference"`
}

// prmExactRepository is a PolicyReferenceMatch with type = prmExactRepository: matches a specified repository, with any tag.
type prmExactRepository struct {
	prmCommon
	DockerRepository string `json:"dockerRepository"`
}

// prmRemapIdentity is a PolicyReferenceMatch with type = prmRemapIdentity: like prmMatchRepoDigestOrExact,
// except that a namespace (at least a host:port, at most a single repository) is substituted before matching the two references.
type prmRemapIdentity struct {
	prmCommon
	Prefix       string `json:"prefix"`
	SignedPrefix string `json:"signedPrefix"`
	// Possibly let the users make a choice for tag/digest matching behavior
	// similar to prmMatchExact/prmMatchRepository?
}
