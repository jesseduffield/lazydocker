//go:build !freebsd

package sysregistriesv2

// builtinRegistriesConfPath is the path to the registry configuration file.
// DO NOT change this, instead see systemRegistriesConfPath above.
const builtinRegistriesConfPath = "/etc/containers/registries.conf"

// builtinRegistriesConfDirPath is the path to the registry configuration directory.
// DO NOT change this, instead see systemRegistriesConfDirectoryPath above.
const builtinRegistriesConfDirPath = "/etc/containers/registries.conf.d"
