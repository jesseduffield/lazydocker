// Copyright 2022 The Sigstore Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package certificate

import (
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
)

var (
	// Deprecated: Use OIDIssuerV2
	OIDIssuer = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}
	// Deprecated: Use OIDBuildTrigger
	OIDGitHubWorkflowTrigger = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 2}
	// Deprecated: Use OIDSourceRepositoryDigest
	OIDGitHubWorkflowSHA = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 3}
	// Deprecated: Use OIDBuildConfigURI or OIDBuildConfigDigest
	OIDGitHubWorkflowName = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 4}
	// Deprecated: Use SourceRepositoryURI
	OIDGitHubWorkflowRepository = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 5}
	// Deprecated: Use OIDSourceRepositoryRef
	OIDGitHubWorkflowRef = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 6}

	OIDOtherName = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 7}
	OIDIssuerV2  = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 8}

	// CI extensions
	OIDBuildSignerURI                      = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 9}
	OIDBuildSignerDigest                   = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 10}
	OIDRunnerEnvironment                   = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 11}
	OIDSourceRepositoryURI                 = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 12}
	OIDSourceRepositoryDigest              = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 13}
	OIDSourceRepositoryRef                 = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 14}
	OIDSourceRepositoryIdentifier          = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 15}
	OIDSourceRepositoryOwnerURI            = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 16}
	OIDSourceRepositoryOwnerIdentifier     = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 17}
	OIDBuildConfigURI                      = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 18}
	OIDBuildConfigDigest                   = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 19}
	OIDBuildTrigger                        = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 20}
	OIDRunInvocationURI                    = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 21}
	OIDSourceRepositoryVisibilityAtSigning = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 22}
)

// Extensions contains all custom x509 extensions defined by Fulcio
type Extensions struct {
	// NB: New extensions must be added here and documented
	// at docs/oidc-info.md

	// The OIDC issuer. Should match `iss` claim of ID token or, in the case of
	// a federated login like Dex it should match the issuer URL of the
	// upstream issuer. The issuer is not set the extensions are invalid and
	// will fail to render.
	Issuer string // OID 1.3.6.1.4.1.57264.1.8 and 1.3.6.1.4.1.57264.1.1 (Deprecated)

	// Deprecated
	// Triggering event of the Github Workflow. Matches the `event_name` claim of ID
	// tokens from Github Actions
	GithubWorkflowTrigger string `json:"GithubWorkflowTrigger,omitempty" yaml:"github-workflow-trigger,omitempty"` // OID 1.3.6.1.4.1.57264.1.2

	// Deprecated
	// SHA of git commit being built in Github Actions. Matches the `sha` claim of ID
	// tokens from Github Actions
	GithubWorkflowSHA string `json:"GithubWorkflowSHA,omitempty" yaml:"github-workflow-sha,omitempty"` // OID 1.3.6.1.4.1.57264.1.3

	// Deprecated
	// Name of Github Actions Workflow. Matches the `workflow` claim of the ID
	// tokens from Github Actions
	GithubWorkflowName string `json:"GithubWorkflowName,omitempty" yaml:"github-workflow-name,omitempty"` // OID 1.3.6.1.4.1.57264.1.4

	// Deprecated
	// Repository of the Github Actions Workflow. Matches the `repository` claim of the ID
	// tokens from Github Actions
	GithubWorkflowRepository string `json:"GithubWorkflowRepository,omitempty" yaml:"github-workflow-repository,omitempty"` // OID 1.3.6.1.4.1.57264.1.5

	// Deprecated
	// Git Ref of the Github Actions Workflow. Matches the `ref` claim of the ID tokens
	// from Github Actions
	GithubWorkflowRef string `json:"GithubWorkflowRef,omitempty" yaml:"github-workflow-ref,omitempty"` // 1.3.6.1.4.1.57264.1.6

	// Reference to specific build instructions that are responsible for signing.
	BuildSignerURI string `json:"BuildSignerURI,omitempty" yaml:"build-signer-uri,omitempty"` // 1.3.6.1.4.1.57264.1.9

	// Immutable reference to the specific version of the build instructions that is responsible for signing.
	BuildSignerDigest string `json:"BuildSignerDigest,omitempty" yaml:"build-signer-digest,omitempty"` // 1.3.6.1.4.1.57264.1.10

	// Specifies whether the build took place in platform-hosted cloud infrastructure or customer/self-hosted infrastructure.
	RunnerEnvironment string `json:"RunnerEnvironment,omitempty" yaml:"runner-environment,omitempty"` // 1.3.6.1.4.1.57264.1.11

	// Source repository URL that the build was based on.
	SourceRepositoryURI string `json:"SourceRepositoryURI,omitempty" yaml:"source-repository-uri,omitempty"` // 1.3.6.1.4.1.57264.1.12

	// Immutable reference to a specific version of the source code that the build was based upon.
	SourceRepositoryDigest string `json:"SourceRepositoryDigest,omitempty" yaml:"source-repository-digest,omitempty"` // 1.3.6.1.4.1.57264.1.13

	// Source Repository Ref that the build run was based upon.
	SourceRepositoryRef string `json:"SourceRepositoryRef,omitempty" yaml:"source-repository-ref,omitempty"` // 1.3.6.1.4.1.57264.1.14

	// Immutable identifier for the source repository the workflow was based upon.
	SourceRepositoryIdentifier string `json:"SourceRepositoryIdentifier,omitempty" yaml:"source-repository-identifier,omitempty"` // 1.3.6.1.4.1.57264.1.15

	// Source repository owner URL of the owner of the source repository that the build was based on.
	SourceRepositoryOwnerURI string `json:"SourceRepositoryOwnerURI,omitempty" yaml:"source-repository-owner-uri,omitempty"` // 1.3.6.1.4.1.57264.1.16

	// Immutable identifier for the owner of the source repository that the workflow was based upon.
	SourceRepositoryOwnerIdentifier string `json:"SourceRepositoryOwnerIdentifier,omitempty" yaml:"source-repository-owner-identifier,omitempty"` // 1.3.6.1.4.1.57264.1.17

	// Build Config URL to the top-level/initiating build instructions.
	BuildConfigURI string `json:"BuildConfigURI,omitempty" yaml:"build-config-uri,omitempty"` // 1.3.6.1.4.1.57264.1.18

	// Immutable reference to the specific version of the top-level/initiating build instructions.
	BuildConfigDigest string `json:"BuildConfigDigest,omitempty" yaml:"build-config-digest,omitempty"` // 1.3.6.1.4.1.57264.1.19

	// Event or action that initiated the build.
	BuildTrigger string `json:"BuildTrigger,omitempty" yaml:"build-trigger,omitempty"` // 1.3.6.1.4.1.57264.1.20

	// Run Invocation URL to uniquely identify the build execution.
	RunInvocationURI string `json:"RunInvocationURI,omitempty" yaml:"run-invocation-uri,omitempty"` // 1.3.6.1.4.1.57264.1.21

	// Source repository visibility at the time of signing the certificate.
	SourceRepositoryVisibilityAtSigning string `json:"SourceRepositoryVisibilityAtSigning,omitempty" yaml:"source-repository-visibility-at-signing,omitempty"` // 1.3.6.1.4.1.57264.1.22
}

func (e Extensions) Render() ([]pkix.Extension, error) {
	var exts []pkix.Extension

	// BEGIN: Deprecated
	if e.Issuer != "" {
		// deprecated issuer extension due to incorrect encoding
		exts = append(exts, pkix.Extension{
			Id:    OIDIssuer,
			Value: []byte(e.Issuer),
		})
	} else {
		return nil, errors.New("extensions must have a non-empty issuer url")
	}
	if e.GithubWorkflowTrigger != "" {
		exts = append(exts, pkix.Extension{
			Id:    OIDGitHubWorkflowTrigger,
			Value: []byte(e.GithubWorkflowTrigger),
		})
	}
	if e.GithubWorkflowSHA != "" {
		exts = append(exts, pkix.Extension{
			Id:    OIDGitHubWorkflowSHA,
			Value: []byte(e.GithubWorkflowSHA),
		})
	}
	if e.GithubWorkflowName != "" {
		exts = append(exts, pkix.Extension{
			Id:    OIDGitHubWorkflowName,
			Value: []byte(e.GithubWorkflowName),
		})
	}
	if e.GithubWorkflowRepository != "" {
		exts = append(exts, pkix.Extension{
			Id:    OIDGitHubWorkflowRepository,
			Value: []byte(e.GithubWorkflowRepository),
		})
	}
	if e.GithubWorkflowRef != "" {
		exts = append(exts, pkix.Extension{
			Id:    OIDGitHubWorkflowRef,
			Value: []byte(e.GithubWorkflowRef),
		})
	}
	// END: Deprecated

	// duplicate issuer with correct RFC 5280 encoding
	if e.Issuer != "" {
		// construct DER encoding of issuer string
		val, err := asn1.MarshalWithParams(e.Issuer, "utf8")
		if err != nil {
			return nil, err
		}
		exts = append(exts, pkix.Extension{
			Id:    OIDIssuerV2,
			Value: val,
		})
	} else {
		return nil, errors.New("extensions must have a non-empty issuer url")
	}

	if e.BuildSignerURI != "" {
		val, err := asn1.MarshalWithParams(e.BuildSignerURI, "utf8")
		if err != nil {
			return nil, err
		}
		exts = append(exts, pkix.Extension{
			Id:    OIDBuildSignerURI,
			Value: val,
		})
	}
	if e.BuildSignerDigest != "" {
		val, err := asn1.MarshalWithParams(e.BuildSignerDigest, "utf8")
		if err != nil {
			return nil, err
		}
		exts = append(exts, pkix.Extension{
			Id:    OIDBuildSignerDigest,
			Value: val,
		})
	}
	if e.RunnerEnvironment != "" {
		val, err := asn1.MarshalWithParams(e.RunnerEnvironment, "utf8")
		if err != nil {
			return nil, err
		}
		exts = append(exts, pkix.Extension{
			Id:    OIDRunnerEnvironment,
			Value: val,
		})
	}
	if e.SourceRepositoryURI != "" {
		val, err := asn1.MarshalWithParams(e.SourceRepositoryURI, "utf8")
		if err != nil {
			return nil, err
		}
		exts = append(exts, pkix.Extension{
			Id:    OIDSourceRepositoryURI,
			Value: val,
		})
	}
	if e.SourceRepositoryDigest != "" {
		val, err := asn1.MarshalWithParams(e.SourceRepositoryDigest, "utf8")
		if err != nil {
			return nil, err
		}
		exts = append(exts, pkix.Extension{
			Id:    OIDSourceRepositoryDigest,
			Value: val,
		})
	}
	if e.SourceRepositoryRef != "" {
		val, err := asn1.MarshalWithParams(e.SourceRepositoryRef, "utf8")
		if err != nil {
			return nil, err
		}
		exts = append(exts, pkix.Extension{
			Id:    OIDSourceRepositoryRef,
			Value: val,
		})
	}
	if e.SourceRepositoryIdentifier != "" {
		val, err := asn1.MarshalWithParams(e.SourceRepositoryIdentifier, "utf8")
		if err != nil {
			return nil, err
		}
		exts = append(exts, pkix.Extension{
			Id:    OIDSourceRepositoryIdentifier,
			Value: val,
		})
	}
	if e.SourceRepositoryOwnerURI != "" {
		val, err := asn1.MarshalWithParams(e.SourceRepositoryOwnerURI, "utf8")
		if err != nil {
			return nil, err
		}
		exts = append(exts, pkix.Extension{
			Id:    OIDSourceRepositoryOwnerURI,
			Value: val,
		})
	}
	if e.SourceRepositoryOwnerIdentifier != "" {
		val, err := asn1.MarshalWithParams(e.SourceRepositoryOwnerIdentifier, "utf8")
		if err != nil {
			return nil, err
		}
		exts = append(exts, pkix.Extension{
			Id:    OIDSourceRepositoryOwnerIdentifier,
			Value: val,
		})
	}
	if e.BuildConfigURI != "" {
		val, err := asn1.MarshalWithParams(e.BuildConfigURI, "utf8")
		if err != nil {
			return nil, err
		}
		exts = append(exts, pkix.Extension{
			Id:    OIDBuildConfigURI,
			Value: val,
		})
	}
	if e.BuildConfigDigest != "" {
		val, err := asn1.MarshalWithParams(e.BuildConfigDigest, "utf8")
		if err != nil {
			return nil, err
		}
		exts = append(exts, pkix.Extension{
			Id:    OIDBuildConfigDigest,
			Value: val,
		})
	}
	if e.BuildTrigger != "" {
		val, err := asn1.MarshalWithParams(e.BuildTrigger, "utf8")
		if err != nil {
			return nil, err
		}
		exts = append(exts, pkix.Extension{
			Id:    OIDBuildTrigger,
			Value: val,
		})
	}
	if e.RunInvocationURI != "" {
		val, err := asn1.MarshalWithParams(e.RunInvocationURI, "utf8")
		if err != nil {
			return nil, err
		}
		exts = append(exts, pkix.Extension{
			Id:    OIDRunInvocationURI,
			Value: val,
		})
	}
	if e.SourceRepositoryVisibilityAtSigning != "" {
		val, err := asn1.MarshalWithParams(e.SourceRepositoryVisibilityAtSigning, "utf8")
		if err != nil {
			return nil, err
		}
		exts = append(exts, pkix.Extension{
			Id:    OIDSourceRepositoryVisibilityAtSigning,
			Value: val,
		})
	}

	return exts, nil
}

func ParseExtensions(ext []pkix.Extension) (Extensions, error) {
	out := Extensions{}

	for _, e := range ext {
		switch {
		// BEGIN: Deprecated
		case e.Id.Equal(OIDIssuer):
			out.Issuer = string(e.Value)
		case e.Id.Equal(OIDGitHubWorkflowTrigger):
			out.GithubWorkflowTrigger = string(e.Value)
		case e.Id.Equal(OIDGitHubWorkflowSHA):
			out.GithubWorkflowSHA = string(e.Value)
		case e.Id.Equal(OIDGitHubWorkflowName):
			out.GithubWorkflowName = string(e.Value)
		case e.Id.Equal(OIDGitHubWorkflowRepository):
			out.GithubWorkflowRepository = string(e.Value)
		case e.Id.Equal(OIDGitHubWorkflowRef):
			out.GithubWorkflowRef = string(e.Value)
		// END: Deprecated
		case e.Id.Equal(OIDIssuerV2):
			if err := ParseDERString(e.Value, &out.Issuer); err != nil {
				return Extensions{}, err
			}
		case e.Id.Equal(OIDBuildSignerURI):
			if err := ParseDERString(e.Value, &out.BuildSignerURI); err != nil {
				return Extensions{}, err
			}
		case e.Id.Equal(OIDBuildSignerDigest):
			if err := ParseDERString(e.Value, &out.BuildSignerDigest); err != nil {
				return Extensions{}, err
			}
		case e.Id.Equal(OIDRunnerEnvironment):
			if err := ParseDERString(e.Value, &out.RunnerEnvironment); err != nil {
				return Extensions{}, err
			}
		case e.Id.Equal(OIDSourceRepositoryURI):
			if err := ParseDERString(e.Value, &out.SourceRepositoryURI); err != nil {
				return Extensions{}, err
			}
		case e.Id.Equal(OIDSourceRepositoryDigest):
			if err := ParseDERString(e.Value, &out.SourceRepositoryDigest); err != nil {
				return Extensions{}, err
			}
		case e.Id.Equal(OIDSourceRepositoryRef):
			if err := ParseDERString(e.Value, &out.SourceRepositoryRef); err != nil {
				return Extensions{}, err
			}
		case e.Id.Equal(OIDSourceRepositoryIdentifier):
			if err := ParseDERString(e.Value, &out.SourceRepositoryIdentifier); err != nil {
				return Extensions{}, err
			}
		case e.Id.Equal(OIDSourceRepositoryOwnerURI):
			if err := ParseDERString(e.Value, &out.SourceRepositoryOwnerURI); err != nil {
				return Extensions{}, err
			}
		case e.Id.Equal(OIDSourceRepositoryOwnerIdentifier):
			if err := ParseDERString(e.Value, &out.SourceRepositoryOwnerIdentifier); err != nil {
				return Extensions{}, err
			}
		case e.Id.Equal(OIDBuildConfigURI):
			if err := ParseDERString(e.Value, &out.BuildConfigURI); err != nil {
				return Extensions{}, err
			}
		case e.Id.Equal(OIDBuildConfigDigest):
			if err := ParseDERString(e.Value, &out.BuildConfigDigest); err != nil {
				return Extensions{}, err
			}
		case e.Id.Equal(OIDBuildTrigger):
			if err := ParseDERString(e.Value, &out.BuildTrigger); err != nil {
				return Extensions{}, err
			}
		case e.Id.Equal(OIDRunInvocationURI):
			if err := ParseDERString(e.Value, &out.RunInvocationURI); err != nil {
				return Extensions{}, err
			}
		case e.Id.Equal(OIDSourceRepositoryVisibilityAtSigning):
			if err := ParseDERString(e.Value, &out.SourceRepositoryVisibilityAtSigning); err != nil {
				return Extensions{}, err
			}
		}
	}

	// We only ever return nil, but leaving error in place so that we can add
	// more complex parsing of fields in a backwards compatible way if needed.
	return out, nil
}

// ParseDERString decodes a DER-encoded string and puts the value in parsedVal.
// Returns an error if the unmarshalling fails or if there are trailing bytes in the encoding.
func ParseDERString(val []byte, parsedVal *string) error {
	rest, err := asn1.Unmarshal(val, parsedVal)
	if err != nil {
		return fmt.Errorf("unexpected error unmarshalling DER-encoded string: %v", err)
	}
	if len(rest) != 0 {
		return errors.New("unexpected trailing bytes in DER-encoded string")
	}
	return nil
}
