//go:build freebsd

package signature

// builtinDefaultPolicyPath is the policy path used for DefaultPolicy().
// DO NOT change this, instead see systemDefaultPolicyPath above.
const builtinDefaultPolicyPath = "/usr/local/etc/containers/policy.json"
