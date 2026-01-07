package rawversion

// RawVersion is the raw version string.
//
// This indirection is needed to prevent semver packages from bloating
// Quadlet's binary size.
const RawVersion = "5.7.1"
