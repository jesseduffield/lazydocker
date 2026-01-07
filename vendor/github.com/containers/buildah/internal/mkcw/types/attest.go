package types

// RegistrationRequest is the body of the request which we use for registering
// this confidential workload with the attestation server.
// https://github.com/virtee/reference-kbs/blob/10b2a4c0f8caf78a077210b172863bbae54f66aa/src/main.rs#L83
type RegistrationRequest struct {
	WorkloadID        string `json:"workload_id"`
	LaunchMeasurement string `json:"launch_measurement"`
	Passphrase        string `json:"passphrase"`
	TeeConfig         string `json:"tee_config"` // JSON-encoded teeConfig? or specific to the type of TEE?
}

// TeeConfig contains information about a trusted execution environment.
type TeeConfig struct {
	Flags TeeConfigFlags `json:"flags"` // runtime requirement bits
	MinFW TeeConfigMinFW `json:"minfw"` // minimum platform firmware version
}

// TeeConfigFlags is a bit field containing policy flags specific to the environment.
// https://github.com/virtee/sev/blob/d3e40917fd8531c69f47c2498e9667fe8a5303aa/src/launch/sev.rs#L172
// https://github.com/virtee/sev/blob/d3e40917fd8531c69f47c2498e9667fe8a5303aa/src/launch/snp.rs#L114
type TeeConfigFlags struct {
	Bits TeeConfigFlagBits `json:"bits"`
}

// TeeConfigFlagBits are bits representing run-time expectations.
type TeeConfigFlagBits int

//nolint:revive,staticcheck // Don't warn about bad naming.
const (
	SEV_CONFIG_NO_DEBUG        TeeConfigFlagBits = 0b00000001 // no debugging of guests
	SEV_CONFIG_NO_KEY_SHARING  TeeConfigFlagBits = 0b00000010 // no sharing keys between guests
	SEV_CONFIG_ENCRYPTED_STATE TeeConfigFlagBits = 0b00000100 // requires SEV-ES
	SEV_CONFIG_NO_SEND         TeeConfigFlagBits = 0b00001000 // no transferring the guest to another platform
	SEV_CONFIG_DOMAIN          TeeConfigFlagBits = 0b00010000 // no transferring the guest out of the domain (?)
	SEV_CONFIG_SEV             TeeConfigFlagBits = 0b00100000 // no transferring the guest to non-SEV platforms
	SNP_CONFIG_SMT             TeeConfigFlagBits = 0b00000001 // SMT is enabled on the host machine
	SNP_CONFIG_MANDATORY       TeeConfigFlagBits = 0b00000010 // reserved bit which should always be set
	SNP_CONFIG_MIGRATE_MA      TeeConfigFlagBits = 0b00000100 // allowed to use a migration agent
	SNP_CONFIG_DEBUG           TeeConfigFlagBits = 0b00001000 // allow debugging
)

// TeeConfigFlagMinFW corresponds to a minimum version of the kernel+initrd
// combination that should be booted.
type TeeConfigMinFW struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
}
