// This defines the top-level policy evaluation API.
// To the extent possible, the interface of the functions provided
// here is intended to be completely unambiguous, and stable for users
// to rely on.

package signature

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/unparsedimage"
	"go.podman.io/image/v5/types"
)

// PolicyRequirementError is an explanatory text for rejecting a signature or an image.
type PolicyRequirementError string

func (err PolicyRequirementError) Error() string {
	return string(err)
}

// signatureAcceptanceResult is the principal value returned by isSignatureAuthorAccepted.
type signatureAcceptanceResult string

const (
	sarAccepted signatureAcceptanceResult = "sarAccepted"
	sarRejected signatureAcceptanceResult = "sarRejected"
	sarUnknown  signatureAcceptanceResult = "sarUnknown"
)

// PolicyRequirement is a rule which must be satisfied by at least one of the signatures of an image.
// The type is public, but its definition is private.
type PolicyRequirement interface {
	// FIXME: For speed, we should support creating per-context state (not stored in the PolicyRequirement), to cache
	// costly initialization like creating temporary GPG home directories and reading files.
	// Setup() (someState, error)
	// Then, the operations below would be done on the someState object, not directly on a PolicyRequirement.

	// isSignatureAuthorAccepted, given an image and a signature blob, returns:
	// - sarAccepted if the signature has been verified against the appropriate public key
	//   (where "appropriate public key" may depend on the contents of the signature);
	//   in that case a parsed Signature should be returned.
	// - sarRejected if the signature has not been verified;
	//   in that case error must be non-nil, and should be an PolicyRequirementError if evaluation
	//   succeeded but the result was rejection.
	// - sarUnknown if this PolicyRequirement does not deal with signatures.
	//   NOTE: sarUnknown should not be returned if this PolicyRequirement should make a decision but something failed.
	//   Returning sarUnknown and a non-nil error value is invalid.
	// WARNING: This makes the signature contents acceptable for further processing,
	// but it does not necessarily mean that the contents of the signature are
	// consistent with local policy.
	// For example:
	// - Do not use a true value to determine whether to run
	//   a container based on this image; use IsRunningImageAllowed instead.
	// - Just because a signature is accepted does not automatically mean the contents of the
	//   signature are authorized to run code as root, or to affect system or cluster configuration.
	isSignatureAuthorAccepted(ctx context.Context, image private.UnparsedImage, sig []byte) (signatureAcceptanceResult, *Signature, error)

	// isRunningImageAllowed returns true if the requirement allows running an image.
	// If it returns false, err must be non-nil, and should be an PolicyRequirementError if evaluation
	// succeeded but the result was rejection.
	// WARNING: This validates signatures and the manifest, but does not download or validate the
	// layers. Users must validate that the layers match their expected digests.
	isRunningImageAllowed(ctx context.Context, image private.UnparsedImage) (bool, error)
}

// PolicyReferenceMatch specifies a set of image identities accepted in PolicyRequirement.
// The type is public, but its implementation is private.
type PolicyReferenceMatch interface {
	// matchesDockerReference decides whether a specific image identity is accepted for an image
	// (or, usually, for the image's Reference().DockerReference()).  Note that
	// image.Reference().DockerReference() may be nil.
	matchesDockerReference(image private.UnparsedImage, signatureDockerReference string) bool
}

// PolicyContext encapsulates a policy and possible cached state
// for speeding up its evaluation.
type PolicyContext struct {
	Policy *Policy
	state  policyContextState // Internal consistency checking
}

// policyContextState is used internally to verify the users are not misusing a PolicyContext.
type policyContextState string

const (
	pcInitializing policyContextState = "Initializing"
	pcReady        policyContextState = "Ready"
	pcInUse        policyContextState = "InUse"
	pcDestroying   policyContextState = "Destroying"
	pcDestroyed    policyContextState = "Destroyed"
)

// changeState changes pc.state, or fails if the state is unexpected
func (pc *PolicyContext) changeState(expected, new policyContextState) error {
	if pc.state != expected {
		return fmt.Errorf(`Invalid PolicyContext state, expected %q, found %q`, expected, pc.state)
	}
	pc.state = new
	return nil
}

// NewPolicyContext sets up and initializes a context for the specified policy.
// The policy must not be modified while the context exists. FIXME: make a deep copy?
// If this function succeeds, the caller should call PolicyContext.Destroy() when done.
func NewPolicyContext(policy *Policy) (*PolicyContext, error) {
	pc := &PolicyContext{Policy: policy, state: pcInitializing}
	// FIXME: initialize
	if err := pc.changeState(pcInitializing, pcReady); err != nil {
		// Huh?! This should never fail, we didn't give the pointer to anybody.
		// Just give up and leave unclean state around.
		return nil, err
	}
	return pc, nil
}

// Destroy should be called when the user of the context is done with it.
func (pc *PolicyContext) Destroy() error {
	if err := pc.changeState(pcReady, pcDestroying); err != nil {
		return err
	}
	// FIXME: destroy
	return pc.changeState(pcDestroying, pcDestroyed)
}

// policyIdentityLogName returns a string description of the image identity for policy purposes.
// ONLY use this for log messages, not for any decisions!
func policyIdentityLogName(ref types.ImageReference) string {
	return ref.Transport().Name() + ":" + ref.PolicyConfigurationIdentity()
}

// requirementsForImageRef selects the appropriate requirements for ref.
func (pc *PolicyContext) requirementsForImageRef(ref types.ImageReference) PolicyRequirements {
	// Do we have a PolicyTransportScopes for this transport?
	transportName := ref.Transport().Name()
	if transportScopes, ok := pc.Policy.Transports[transportName]; ok {
		// Look for a full match.
		identity := ref.PolicyConfigurationIdentity()
		if req, ok := transportScopes[identity]; ok {
			logrus.Debugf(` Using transport %q policy section %q`, transportName, identity)
			return req
		}

		// Look for a match of the possible parent namespaces.
		for _, name := range ref.PolicyConfigurationNamespaces() {
			if req, ok := transportScopes[name]; ok {
				logrus.Debugf(` Using transport %q specific policy section %q`, transportName, name)
				return req
			}
		}

		// Look for a default match for the transport.
		if req, ok := transportScopes[""]; ok {
			logrus.Debugf(` Using transport %q policy section ""`, transportName)
			return req
		}
	}

	logrus.Debugf(" Using default policy section")
	return pc.Policy.Default
}

// GetSignaturesWithAcceptedAuthor returns those signatures from an image
// for which the policy accepts the author (and which have been successfully
// verified).
// NOTE: This may legitimately return an empty list and no error, if the image
// has no signatures or only invalid signatures.
// WARNING: This makes the signature contents acceptable for further processing,
// but it does not necessarily mean that the contents of the signature are
// consistent with local policy.
// For example:
//   - Do not use a an existence of an accepted signature to determine whether to run
//     a container based on this image; use IsRunningImageAllowed instead.
//   - Just because a signature is accepted does not automatically mean the contents of the
//     signature are authorized to run code as root, or to affect system or cluster configuration.
func (pc *PolicyContext) GetSignaturesWithAcceptedAuthor(ctx context.Context, publicImage types.UnparsedImage) (sigs []*Signature, finalErr error) {
	if err := pc.changeState(pcReady, pcInUse); err != nil {
		return nil, err
	}
	defer func() {
		if err := pc.changeState(pcInUse, pcReady); err != nil {
			sigs = nil
			finalErr = err
		}
	}()

	image := unparsedimage.FromPublic(publicImage)

	logrus.Debugf("GetSignaturesWithAcceptedAuthor for image %s", policyIdentityLogName(image.Reference()))
	reqs := pc.requirementsForImageRef(image.Reference())

	// FIXME: Use image.UntrustedSignatures, use that to improve error messages (needs tests!)
	unverifiedSignatures, err := image.Signatures(ctx)
	if err != nil {
		return nil, err
	}

	res := make([]*Signature, 0, len(unverifiedSignatures))
	for sigNumber, sig := range unverifiedSignatures {
		var acceptedSig *Signature // non-nil if accepted
		rejected := false
		// FIXME? Say more about the contents of the signature, i.e. parse it even before verification?!
		logrus.Debugf("Evaluating signature %d:", sigNumber)
	interpretingReqs:
		for reqNumber, req := range reqs {
			// FIXME: Log the requirement itself? For now, we use just the number.
			// FIXME: supply state
			switch res, as, err := req.isSignatureAuthorAccepted(ctx, image, sig); res {
			case sarAccepted:
				if as == nil { // Coverage: this should never happen
					logrus.Debugf(" Requirement %d: internal inconsistency: sarAccepted but no parsed contents", reqNumber)
					rejected = true
					break interpretingReqs
				}
				logrus.Debugf(" Requirement %d: signature accepted", reqNumber)
				if acceptedSig == nil {
					acceptedSig = as
				} else if *as != *acceptedSig { // Coverage: this should never happen
					// Huh?! Two ways of verifying the same signature blob resulted in two different parses of its already accepted contents?
					logrus.Debugf(" Requirement %d: internal inconsistency: sarAccepted but different parsed contents", reqNumber)
					rejected = true
					acceptedSig = nil
					break interpretingReqs
				}
			case sarRejected:
				logrus.Debugf(" Requirement %d: signature rejected: %s", reqNumber, err.Error())
				rejected = true
				break interpretingReqs
			case sarUnknown:
				if err != nil { // Coverage: this should never happen
					logrus.Debugf(" Requirement %d: internal inconsistency: sarUnknown but an error message %s", reqNumber, err.Error())
					rejected = true
					break interpretingReqs
				}
				logrus.Debugf(" Requirement %d: signature state unknown, continuing", reqNumber)
			default: // Coverage: this should never happen
				logrus.Debugf(" Requirement %d: internal inconsistency: unknown result %#v", reqNumber, string(res))
				rejected = true
				break interpretingReqs
			}
		}
		// This also handles the (invalid) case of empty reqs, by rejecting the signature.
		if acceptedSig != nil && !rejected {
			logrus.Debugf(" Overall: OK, signature accepted")
			res = append(res, acceptedSig)
		} else {
			logrus.Debugf(" Overall: Signature not accepted")
		}
	}
	return res, nil
}

// IsRunningImageAllowed returns true iff the policy allows running the image.
// If it returns false, err must be non-nil, and should be an PolicyRequirementError if evaluation
// succeeded but the result was rejection.
// WARNING: This validates signatures and the manifest, but does not download or validate the
// layers. Users must validate that the layers match their expected digests.
func (pc *PolicyContext) IsRunningImageAllowed(ctx context.Context, publicImage types.UnparsedImage) (res bool, finalErr error) {
	if err := pc.changeState(pcReady, pcInUse); err != nil {
		return false, err
	}
	defer func() {
		if err := pc.changeState(pcInUse, pcReady); err != nil {
			res = false
			finalErr = err
		}
	}()

	image := unparsedimage.FromPublic(publicImage)

	logrus.Debugf("IsRunningImageAllowed for image %s", policyIdentityLogName(image.Reference()))
	reqs := pc.requirementsForImageRef(image.Reference())

	if len(reqs) == 0 {
		return false, PolicyRequirementError("List of verification policy requirements must not be empty")
	}

	for reqNumber, req := range reqs {
		// FIXME: supply state
		allowed, err := req.isRunningImageAllowed(ctx, image)
		if !allowed {
			logrus.Debugf("Requirement %d: denied, done", reqNumber)
			return false, err
		}
		logrus.Debugf(" Requirement %d: allowed", reqNumber)
	}
	// We have tested that len(reqs) != 0, so at least one req must have explicitly allowed this image.
	logrus.Debugf("Overall: allowed")
	return true, nil
}
