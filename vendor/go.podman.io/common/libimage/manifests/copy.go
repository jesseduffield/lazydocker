package manifests

import (
	"go.podman.io/image/v5/signature"
)

// storageAllowedPolicyScopes overrides the policy for local storage
// to ensure that we can read images from it.
var storageAllowedPolicyScopes = signature.PolicyTransportScopes{
	"": []signature.PolicyRequirement{
		signature.NewPRInsecureAcceptAnything(),
	},
}
