//go:build freebsd

package sysregistriesv2

// builtinRegistriesConfPath is the path to the registry configuration file.
// DO NOT change this, instead see systemRegistriesConfPath above.
const builtinRegistriesConfPath = "/usr/local/etc/containers/registries.conf"

// builtinRegistriesConfDirPath is the path to the registry configuration directory.
// DO NOT change this, instead see systemRegistriesConfDirectoryPath above.
const builtinRegistriesConfDirPath = "/usr/local/etc/containers/registries.conf.d"
