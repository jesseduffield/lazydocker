package signature

import (
	"encoding/json"
	"fmt"

	"go.podman.io/image/v5/signature/internal"
)

// PRSigstoreSignedOption is way to pass values to NewPRSigstoreSigned
type PRSigstoreSignedOption func(*prSigstoreSigned) error

// PRSigstoreSignedWithKeyPath specifies a value for the "keyPath" field when calling NewPRSigstoreSigned.
func PRSigstoreSignedWithKeyPath(keyPath string) PRSigstoreSignedOption {
	return func(pr *prSigstoreSigned) error {
		if pr.KeyPath != "" {
			return InvalidPolicyFormatError(`"keyPath" already specified`)
		}
		pr.KeyPath = keyPath
		return nil
	}
}

// PRSigstoreSignedWithKeyPaths specifies a value for the "keyPaths" field when calling NewPRSigstoreSigned.
func PRSigstoreSignedWithKeyPaths(keyPaths []string) PRSigstoreSignedOption {
	return func(pr *prSigstoreSigned) error {
		if pr.KeyPaths != nil {
			return InvalidPolicyFormatError(`"keyPaths" already specified`)
		}
		if len(keyPaths) == 0 {
			return InvalidPolicyFormatError(`"keyPaths" contains no entries`)
		}
		pr.KeyPaths = keyPaths
		return nil
	}
}

// PRSigstoreSignedWithKeyData specifies a value for the "keyData" field when calling NewPRSigstoreSigned.
func PRSigstoreSignedWithKeyData(keyData []byte) PRSigstoreSignedOption {
	return func(pr *prSigstoreSigned) error {
		if pr.KeyData != nil {
			return InvalidPolicyFormatError(`"keyData" already specified`)
		}
		pr.KeyData = keyData
		return nil
	}
}

// PRSigstoreSignedWithKeyDatas specifies a value for the "keyDatas" field when calling NewPRSigstoreSigned.
func PRSigstoreSignedWithKeyDatas(keyDatas [][]byte) PRSigstoreSignedOption {
	return func(pr *prSigstoreSigned) error {
		if pr.KeyDatas != nil {
			return InvalidPolicyFormatError(`"keyDatas" already specified`)
		}
		if len(keyDatas) == 0 {
			return InvalidPolicyFormatError(`"keyDatas" contains no entries`)
		}
		pr.KeyDatas = keyDatas
		return nil
	}
}

// PRSigstoreSignedWithFulcio specifies a value for the "fulcio" field when calling NewPRSigstoreSigned.
func PRSigstoreSignedWithFulcio(fulcio PRSigstoreSignedFulcio) PRSigstoreSignedOption {
	return func(pr *prSigstoreSigned) error {
		if pr.Fulcio != nil {
			return InvalidPolicyFormatError(`"fulcio" already specified`)
		}
		pr.Fulcio = fulcio
		return nil
	}
}

// PRSigstoreSignedWithPKI specifies a value for the "pki" field when calling NewPRSigstoreSigned.
func PRSigstoreSignedWithPKI(p PRSigstoreSignedPKI) PRSigstoreSignedOption {
	return func(pr *prSigstoreSigned) error {
		if pr.PKI != nil {
			return InvalidPolicyFormatError(`"pki" already specified`)
		}
		pr.PKI = p
		return nil
	}
}

// PRSigstoreSignedWithRekorPublicKeyPath specifies a value for the "rekorPublicKeyPath" field when calling NewPRSigstoreSigned.
func PRSigstoreSignedWithRekorPublicKeyPath(rekorPublicKeyPath string) PRSigstoreSignedOption {
	return func(pr *prSigstoreSigned) error {
		if pr.RekorPublicKeyPath != "" {
			return InvalidPolicyFormatError(`"rekorPublicKeyPath" already specified`)
		}
		pr.RekorPublicKeyPath = rekorPublicKeyPath
		return nil
	}
}

// PRSigstoreSignedWithRekorPublicKeyPaths specifies a value for the rRekorPublickeyPaths" field when calling NewPRSigstoreSigned.
func PRSigstoreSignedWithRekorPublicKeyPaths(rekorPublickeyPaths []string) PRSigstoreSignedOption {
	return func(pr *prSigstoreSigned) error {
		if pr.RekorPublicKeyPaths != nil {
			return InvalidPolicyFormatError(`"rekorPublickeyPaths" already specified`)
		}
		if len(rekorPublickeyPaths) == 0 {
			return InvalidPolicyFormatError(`"rekorPublickeyPaths" contains no entries`)
		}
		pr.RekorPublicKeyPaths = rekorPublickeyPaths
		return nil
	}
}

// PRSigstoreSignedWithRekorPublicKeyData specifies a value for the "rekorPublicKeyData" field when calling NewPRSigstoreSigned.
func PRSigstoreSignedWithRekorPublicKeyData(rekorPublicKeyData []byte) PRSigstoreSignedOption {
	return func(pr *prSigstoreSigned) error {
		if pr.RekorPublicKeyData != nil {
			return InvalidPolicyFormatError(`"rekorPublicKeyData" already specified`)
		}
		pr.RekorPublicKeyData = rekorPublicKeyData
		return nil
	}
}

// PRSigstoreSignedWithRekorPublicKeyDatas specifies a value for the "rekorPublickeyDatas" field when calling NewPRSigstoreSigned.
func PRSigstoreSignedWithRekorPublicKeyDatas(rekorPublickeyDatas [][]byte) PRSigstoreSignedOption {
	return func(pr *prSigstoreSigned) error {
		if pr.RekorPublicKeyDatas != nil {
			return InvalidPolicyFormatError(`"rekorPublickeyDatas" already specified`)
		}
		if len(rekorPublickeyDatas) == 0 {
			return InvalidPolicyFormatError(`"rekorPublickeyDatas" contains no entries`)
		}
		pr.RekorPublicKeyDatas = rekorPublickeyDatas
		return nil
	}
}

// PRSigstoreSignedWithSignedIdentity specifies a value for the "signedIdentity" field when calling NewPRSigstoreSigned.
func PRSigstoreSignedWithSignedIdentity(signedIdentity PolicyReferenceMatch) PRSigstoreSignedOption {
	return func(pr *prSigstoreSigned) error {
		if pr.SignedIdentity != nil {
			return InvalidPolicyFormatError(`"signedIdentity" already specified`)
		}
		pr.SignedIdentity = signedIdentity
		return nil
	}
}

// newPRSigstoreSigned is NewPRSigstoreSigned, except it returns the private type.
func newPRSigstoreSigned(options ...PRSigstoreSignedOption) (*prSigstoreSigned, error) {
	res := prSigstoreSigned{
		prCommon: prCommon{Type: prTypeSigstoreSigned},
	}
	for _, o := range options {
		if err := o(&res); err != nil {
			return nil, err
		}
	}

	keySources := 0
	if res.KeyPath != "" {
		keySources++
	}
	if res.KeyPaths != nil {
		keySources++
	}
	if res.KeyData != nil {
		keySources++
	}
	if res.KeyDatas != nil {
		keySources++
	}
	if res.Fulcio != nil {
		keySources++
	}
	if res.PKI != nil {
		keySources++
	}
	if keySources != 1 {
		return nil, InvalidPolicyFormatError("exactly one of keyPath, keyPaths, keyData, keyDatas, fulcio, and pki must be specified")
	}

	rekorSources := 0
	if res.RekorPublicKeyPath != "" {
		rekorSources++
	}
	if res.RekorPublicKeyPaths != nil {
		rekorSources++
	}
	if res.RekorPublicKeyData != nil {
		rekorSources++
	}
	if res.RekorPublicKeyDatas != nil {
		rekorSources++
	}
	if rekorSources > 1 {
		return nil, InvalidPolicyFormatError("at most one of rekorPublickeyPath, rekorPublicKeyPaths, rekorPublickeyData and rekorPublicKeyDatas can be used simultaneously")
	}
	if res.Fulcio != nil && rekorSources == 0 {
		return nil, InvalidPolicyFormatError("At least one of rekorPublickeyPath, rekorPublicKeyPaths, rekorPublickeyData and rekorPublicKeyDatas must be specified if fulcio is used")
	}
	if res.PKI != nil && rekorSources > 0 {
		return nil, InvalidPolicyFormatError("rekorPublickeyPath, rekorPublicKeyPaths, rekorPublickeyData and rekorPublicKeyDatas are not supported for pki")
	}

	if res.SignedIdentity == nil {
		return nil, InvalidPolicyFormatError("signedIdentity not specified")
	}

	return &res, nil
}

// NewPRSigstoreSigned returns a new "sigstoreSigned" PolicyRequirement based on options.
func NewPRSigstoreSigned(options ...PRSigstoreSignedOption) (PolicyRequirement, error) {
	return newPRSigstoreSigned(options...)
}

// NewPRSigstoreSignedKeyPath returns a new "sigstoreSigned" PolicyRequirement using a KeyPath
func NewPRSigstoreSignedKeyPath(keyPath string, signedIdentity PolicyReferenceMatch) (PolicyRequirement, error) {
	return NewPRSigstoreSigned(
		PRSigstoreSignedWithKeyPath(keyPath),
		PRSigstoreSignedWithSignedIdentity(signedIdentity),
	)
}

// NewPRSigstoreSignedKeyData returns a new "sigstoreSigned" PolicyRequirement using a KeyData
func NewPRSigstoreSignedKeyData(keyData []byte, signedIdentity PolicyReferenceMatch) (PolicyRequirement, error) {
	return NewPRSigstoreSigned(
		PRSigstoreSignedWithKeyData(keyData),
		PRSigstoreSignedWithSignedIdentity(signedIdentity),
	)
}

// Compile-time check that prSigstoreSigned implements json.Unmarshaler.
var _ json.Unmarshaler = (*prSigstoreSigned)(nil)

// UnmarshalJSON implements the json.Unmarshaler interface.
func (pr *prSigstoreSigned) UnmarshalJSON(data []byte) error {
	*pr = prSigstoreSigned{}
	var tmp prSigstoreSigned
	var gotKeyPath, gotKeyPaths, gotKeyData, gotKeyDatas, gotFulcio, gotPKI bool
	var gotRekorPublicKeyPath, gotRekorPublicKeyPaths, gotRekorPublicKeyData, gotRekorPublicKeyDatas bool
	var fulcio prSigstoreSignedFulcio
	var pki prSigstoreSignedPKI
	var signedIdentity json.RawMessage
	if err := internal.ParanoidUnmarshalJSONObject(data, func(key string) any {
		switch key {
		case "type":
			return &tmp.Type
		case "keyPath":
			gotKeyPath = true
			return &tmp.KeyPath
		case "keyPaths":
			gotKeyPaths = true
			return &tmp.KeyPaths
		case "keyData":
			gotKeyData = true
			return &tmp.KeyData
		case "keyDatas":
			gotKeyDatas = true
			return &tmp.KeyDatas
		case "fulcio":
			gotFulcio = true
			return &fulcio
		case "rekorPublicKeyPath":
			gotRekorPublicKeyPath = true
			return &tmp.RekorPublicKeyPath
		case "rekorPublicKeyPaths":
			gotRekorPublicKeyPaths = true
			return &tmp.RekorPublicKeyPaths
		case "rekorPublicKeyData":
			gotRekorPublicKeyData = true
			return &tmp.RekorPublicKeyData
		case "rekorPublicKeyDatas":
			gotRekorPublicKeyDatas = true
			return &tmp.RekorPublicKeyDatas
		case "pki":
			gotPKI = true
			return &pki
		case "signedIdentity":
			return &signedIdentity
		default:
			return nil
		}
	}); err != nil {
		return err
	}

	if tmp.Type != prTypeSigstoreSigned {
		return InvalidPolicyFormatError(fmt.Sprintf("Unexpected policy requirement type %q", tmp.Type))
	}
	if signedIdentity == nil {
		tmp.SignedIdentity = NewPRMMatchRepoDigestOrExact()
	} else {
		si, err := newPolicyReferenceMatchFromJSON(signedIdentity)
		if err != nil {
			return err
		}
		tmp.SignedIdentity = si
	}

	var opts []PRSigstoreSignedOption
	if gotKeyPath {
		opts = append(opts, PRSigstoreSignedWithKeyPath(tmp.KeyPath))
	}
	if gotKeyPaths {
		opts = append(opts, PRSigstoreSignedWithKeyPaths(tmp.KeyPaths))
	}
	if gotKeyData {
		opts = append(opts, PRSigstoreSignedWithKeyData(tmp.KeyData))
	}
	if gotKeyDatas {
		opts = append(opts, PRSigstoreSignedWithKeyDatas(tmp.KeyDatas))
	}
	if gotFulcio {
		opts = append(opts, PRSigstoreSignedWithFulcio(&fulcio))
	}
	if gotRekorPublicKeyPath {
		opts = append(opts, PRSigstoreSignedWithRekorPublicKeyPath(tmp.RekorPublicKeyPath))
	}
	if gotRekorPublicKeyPaths {
		opts = append(opts, PRSigstoreSignedWithRekorPublicKeyPaths(tmp.RekorPublicKeyPaths))
	}
	if gotRekorPublicKeyData {
		opts = append(opts, PRSigstoreSignedWithRekorPublicKeyData(tmp.RekorPublicKeyData))
	}
	if gotRekorPublicKeyDatas {
		opts = append(opts, PRSigstoreSignedWithRekorPublicKeyDatas(tmp.RekorPublicKeyDatas))
	}
	if gotPKI {
		opts = append(opts, PRSigstoreSignedWithPKI(&pki))
	}
	opts = append(opts, PRSigstoreSignedWithSignedIdentity(tmp.SignedIdentity))

	res, err := newPRSigstoreSigned(opts...)
	if err != nil {
		return err
	}
	*pr = *res
	return nil
}

// PRSigstoreSignedFulcioOption is a way to pass values to NewPRSigstoreSignedFulcio
type PRSigstoreSignedFulcioOption func(*prSigstoreSignedFulcio) error

// PRSigstoreSignedFulcioWithCAPath specifies a value for the "caPath" field when calling NewPRSigstoreSignedFulcio
func PRSigstoreSignedFulcioWithCAPath(caPath string) PRSigstoreSignedFulcioOption {
	return func(f *prSigstoreSignedFulcio) error {
		if f.CAPath != "" {
			return InvalidPolicyFormatError(`"caPath" already specified`)
		}
		f.CAPath = caPath
		return nil
	}
}

// PRSigstoreSignedFulcioWithCAData specifies a value for the "caData" field when calling NewPRSigstoreSignedFulcio
func PRSigstoreSignedFulcioWithCAData(caData []byte) PRSigstoreSignedFulcioOption {
	return func(f *prSigstoreSignedFulcio) error {
		if f.CAData != nil {
			return InvalidPolicyFormatError(`"caData" already specified`)
		}
		f.CAData = caData
		return nil
	}
}

// PRSigstoreSignedFulcioWithOIDCIssuer specifies a value for the "oidcIssuer" field when calling NewPRSigstoreSignedFulcio
func PRSigstoreSignedFulcioWithOIDCIssuer(oidcIssuer string) PRSigstoreSignedFulcioOption {
	return func(f *prSigstoreSignedFulcio) error {
		if f.OIDCIssuer != "" {
			return InvalidPolicyFormatError(`"oidcIssuer" already specified`)
		}
		f.OIDCIssuer = oidcIssuer
		return nil
	}
}

// PRSigstoreSignedFulcioWithSubjectEmail specifies a value for the "subjectEmail" field when calling NewPRSigstoreSignedFulcio
func PRSigstoreSignedFulcioWithSubjectEmail(subjectEmail string) PRSigstoreSignedFulcioOption {
	return func(f *prSigstoreSignedFulcio) error {
		if f.SubjectEmail != "" {
			return InvalidPolicyFormatError(`"subjectEmail" already specified`)
		}
		f.SubjectEmail = subjectEmail
		return nil
	}
}

// newPRSigstoreSignedFulcio is NewPRSigstoreSignedFulcio, except it returns the private type
func newPRSigstoreSignedFulcio(options ...PRSigstoreSignedFulcioOption) (*prSigstoreSignedFulcio, error) {
	res := prSigstoreSignedFulcio{}
	for _, o := range options {
		if err := o(&res); err != nil {
			return nil, err
		}
	}

	if res.CAPath != "" && res.CAData != nil {
		return nil, InvalidPolicyFormatError("caPath and caData cannot be used simultaneously")
	}
	if res.CAPath == "" && res.CAData == nil {
		return nil, InvalidPolicyFormatError("At least one of caPath and caData must be specified")
	}
	if res.OIDCIssuer == "" {
		return nil, InvalidPolicyFormatError("oidcIssuer not specified")
	}
	if res.SubjectEmail == "" {
		return nil, InvalidPolicyFormatError("subjectEmail not specified")
	}

	return &res, nil
}

// NewPRSigstoreSignedFulcio returns a PRSigstoreSignedFulcio based on options.
func NewPRSigstoreSignedFulcio(options ...PRSigstoreSignedFulcioOption) (PRSigstoreSignedFulcio, error) {
	return newPRSigstoreSignedFulcio(options...)
}

// Compile-time check that prSigstoreSignedFulcio implements json.Unmarshaler.
var _ json.Unmarshaler = (*prSigstoreSignedFulcio)(nil)

func (f *prSigstoreSignedFulcio) UnmarshalJSON(data []byte) error {
	*f = prSigstoreSignedFulcio{}
	var tmp prSigstoreSignedFulcio
	var gotCAPath, gotCAData, gotOIDCIssuer, gotSubjectEmail bool // = false...
	if err := internal.ParanoidUnmarshalJSONObject(data, func(key string) any {
		switch key {
		case "caPath":
			gotCAPath = true
			return &tmp.CAPath
		case "caData":
			gotCAData = true
			return &tmp.CAData
		case "oidcIssuer":
			gotOIDCIssuer = true
			return &tmp.OIDCIssuer
		case "subjectEmail":
			gotSubjectEmail = true
			return &tmp.SubjectEmail
		default:
			return nil
		}
	}); err != nil {
		return err
	}

	var opts []PRSigstoreSignedFulcioOption
	if gotCAPath {
		opts = append(opts, PRSigstoreSignedFulcioWithCAPath(tmp.CAPath))
	}
	if gotCAData {
		opts = append(opts, PRSigstoreSignedFulcioWithCAData(tmp.CAData))
	}
	if gotOIDCIssuer {
		opts = append(opts, PRSigstoreSignedFulcioWithOIDCIssuer(tmp.OIDCIssuer))
	}
	if gotSubjectEmail {
		opts = append(opts, PRSigstoreSignedFulcioWithSubjectEmail(tmp.SubjectEmail))
	}

	res, err := newPRSigstoreSignedFulcio(opts...)
	if err != nil {
		return err
	}

	*f = *res
	return nil
}

// PRSigstoreSignedPKIOption is a way to pass values to NewPRSigstoreSignedPKI
type PRSigstoreSignedPKIOption func(*prSigstoreSignedPKI) error

// PRSigstoreSignedPKIWithCARootsPath specifies a value for the "caRootsPath" field when calling NewPRSigstoreSignedPKI
func PRSigstoreSignedPKIWithCARootsPath(caRootsPath string) PRSigstoreSignedPKIOption {
	return func(p *prSigstoreSignedPKI) error {
		if p.CARootsPath != "" {
			return InvalidPolicyFormatError(`"caRootsPath" already specified`)
		}
		p.CARootsPath = caRootsPath
		return nil
	}
}

// PRSigstoreSignedPKIWithCARootsData specifies a value for the "caRootsData" field when calling NewPRSigstoreSignedPKI
func PRSigstoreSignedPKIWithCARootsData(caRootsData []byte) PRSigstoreSignedPKIOption {
	return func(p *prSigstoreSignedPKI) error {
		if p.CARootsData != nil {
			return InvalidPolicyFormatError(`"caRootsData" already specified`)
		}
		p.CARootsData = caRootsData
		return nil
	}
}

// PRSigstoreSignedPKIWithCAIntermediatesPath specifies a value for the "caIntermediatesPath" field when calling NewPRSigstoreSignedPKI
func PRSigstoreSignedPKIWithCAIntermediatesPath(caIntermediatesPath string) PRSigstoreSignedPKIOption {
	return func(p *prSigstoreSignedPKI) error {
		if p.CAIntermediatesPath != "" {
			return InvalidPolicyFormatError(`"caIntermediatesPath" already specified`)
		}
		p.CAIntermediatesPath = caIntermediatesPath
		return nil
	}
}

// PRSigstoreSignedPKIWithCAIntermediatesData specifies a value for the "caIntermediatesData" field when calling NewPRSigstoreSignedPKI
func PRSigstoreSignedPKIWithCAIntermediatesData(caIntermediatesData []byte) PRSigstoreSignedPKIOption {
	return func(p *prSigstoreSignedPKI) error {
		if p.CAIntermediatesData != nil {
			return InvalidPolicyFormatError(`"caIntermediatesData" already specified`)
		}
		p.CAIntermediatesData = caIntermediatesData
		return nil
	}
}

// PRSigstoreSignedPKIWithSubjectEmail specifies a value for the "subjectEmail" field when calling NewPRSigstoreSignedPKI
func PRSigstoreSignedPKIWithSubjectEmail(subjectEmail string) PRSigstoreSignedPKIOption {
	return func(p *prSigstoreSignedPKI) error {
		if p.SubjectEmail != "" {
			return InvalidPolicyFormatError(`"subjectEmail" already specified`)
		}
		p.SubjectEmail = subjectEmail
		return nil
	}
}

// PRSigstoreSignedPKIWithSubjectHostname specifies a value for the "subjectHostname" field when calling NewPRSigstoreSignedPKI
func PRSigstoreSignedPKIWithSubjectHostname(subjectHostname string) PRSigstoreSignedPKIOption {
	return func(p *prSigstoreSignedPKI) error {
		if p.SubjectHostname != "" {
			return InvalidPolicyFormatError(`"subjectHostname" already specified`)
		}
		p.SubjectHostname = subjectHostname
		return nil
	}
}

// newPRSigstoreSignedPKI is NewPRSigstoreSignedPKI, except it returns the private type
func newPRSigstoreSignedPKI(options ...PRSigstoreSignedPKIOption) (*prSigstoreSignedPKI, error) {
	res := prSigstoreSignedPKI{}
	for _, o := range options {
		if err := o(&res); err != nil {
			return nil, err
		}
	}

	if res.CARootsPath != "" && res.CARootsData != nil {
		return nil, InvalidPolicyFormatError("caRootsPath and caRootsData cannot be used simultaneously")
	}
	if res.CARootsPath == "" && res.CARootsData == nil {
		return nil, InvalidPolicyFormatError("At least one of caRootsPath and caRootsData must be specified")
	}

	if res.CAIntermediatesPath != "" && res.CAIntermediatesData != nil {
		return nil, InvalidPolicyFormatError("caIntermediatesPath and caIntermediatesData cannot be used simultaneously")
	}

	if res.SubjectEmail == "" && res.SubjectHostname == "" {
		return nil, InvalidPolicyFormatError("At least one of subjectEmail, subjectHostname must be specified")
	}

	return &res, nil
}

// NewPRSigstoreSignedPKI returns a PRSigstoreSignedPKI based on options.
func NewPRSigstoreSignedPKI(options ...PRSigstoreSignedPKIOption) (PRSigstoreSignedPKI, error) {
	return newPRSigstoreSignedPKI(options...)
}

// Compile-time check that prSigstoreSignedPKI implements json.Unmarshaler.
var _ json.Unmarshaler = (*prSigstoreSignedPKI)(nil)

func (p *prSigstoreSignedPKI) UnmarshalJSON(data []byte) error {
	*p = prSigstoreSignedPKI{}
	var tmp prSigstoreSignedPKI
	var gotCARootsPath, gotCARootsData, gotCAIntermediatesPath, gotCAIntermediatesData, gotSubjectEmail, gotSubjectHostname bool
	if err := internal.ParanoidUnmarshalJSONObject(data, func(key string) any {
		switch key {
		case "caRootsPath":
			gotCARootsPath = true
			return &tmp.CARootsPath
		case "caRootsData":
			gotCARootsData = true
			return &tmp.CARootsData
		case "caIntermediatesPath":
			gotCAIntermediatesPath = true
			return &tmp.CAIntermediatesPath
		case "caIntermediatesData":
			gotCAIntermediatesData = true
			return &tmp.CAIntermediatesData
		case "subjectEmail":
			gotSubjectEmail = true
			return &tmp.SubjectEmail
		case "subjectHostname":
			gotSubjectHostname = true
			return &tmp.SubjectHostname
		default:
			return nil
		}
	}); err != nil {
		return err
	}

	var opts []PRSigstoreSignedPKIOption
	if gotCARootsPath {
		opts = append(opts, PRSigstoreSignedPKIWithCARootsPath(tmp.CARootsPath))
	}
	if gotCARootsData {
		opts = append(opts, PRSigstoreSignedPKIWithCARootsData(tmp.CARootsData))
	}
	if gotCAIntermediatesPath {
		opts = append(opts, PRSigstoreSignedPKIWithCAIntermediatesPath(tmp.CAIntermediatesPath))
	}
	if gotCAIntermediatesData {
		opts = append(opts, PRSigstoreSignedPKIWithCAIntermediatesData(tmp.CAIntermediatesData))
	}
	if gotSubjectEmail {
		opts = append(opts, PRSigstoreSignedPKIWithSubjectEmail(tmp.SubjectEmail))
	}
	if gotSubjectHostname {
		opts = append(opts, PRSigstoreSignedPKIWithSubjectHostname(tmp.SubjectHostname))
	}

	res, err := newPRSigstoreSignedPKI(opts...)
	if err != nil {
		return err
	}

	*p = *res
	return nil
}
