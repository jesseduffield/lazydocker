package sysregistriesv2

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/multierr"
	"go.podman.io/image/v5/internal/rootless"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/homedir"
	"go.podman.io/storage/pkg/lockfile"
)

// defaultShortNameMode is the default mode of registries.conf files if the
// corresponding field is left empty.
const defaultShortNameMode = types.ShortNameModePermissive

// userShortNamesFile is the user-specific config file to store aliases.
var userShortNamesFile = filepath.FromSlash("containers/short-name-aliases.conf")

// shortNameAliasesConfPath returns the path to the machine-generated
// short-name-aliases.conf file.
func shortNameAliasesConfPath(ctx *types.SystemContext) (string, error) {
	if ctx != nil && len(ctx.UserShortNameAliasConfPath) > 0 {
		return ctx.UserShortNameAliasConfPath, nil
	}

	if rootless.GetRootlessEUID() == 0 {
		// Root user or in a non-conforming user NS
		return filepath.Join("/var/cache", userShortNamesFile), nil
	}

	// Rootless user
	cacheRoot, err := homedir.GetCacheHome()
	if err != nil {
		return "", err
	}

	return filepath.Join(cacheRoot, userShortNamesFile), nil
}

// shortNameAliasConf is a subset of the `V2RegistriesConf` format.  It's used in the
// software-maintained `userShortNamesFile`.
type shortNameAliasConf struct {
	// A map for aliasing short names to their fully-qualified image
	// reference counter parts.
	// Note that Aliases is niled after being loaded from a file.
	Aliases map[string]string `toml:"aliases"`

	// If you add any field, make sure to update nonempty() below.
}

// nonempty returns true if config contains at least one configuration entry.
func (c *shortNameAliasConf) nonempty() bool {
	copy := *c // A shallow copy
	if copy.Aliases != nil && len(copy.Aliases) == 0 {
		copy.Aliases = nil
	}
	return !reflect.DeepEqual(copy, shortNameAliasConf{})
}

// alias combines the parsed value of an alias with the config file it has been
// specified in.  The config file is crucial for an improved user experience
// such that users are able to resolve potential pull errors.
type alias struct {
	// The parsed value of an alias.  May be nil if set to "" in a config.
	value reference.Named
	// The config file the alias originates from.
	configOrigin string
}

// shortNameAliasCache is the result of parsing shortNameAliasConf,
// pre-processed for faster usage.
type shortNameAliasCache struct {
	// Note that an alias value may be nil iff it's set as an empty string
	// in the config.
	namedAliases map[string]alias
}

// ResolveShortNameAlias performs an alias resolution of the specified name.
// The user-specific short-name-aliases.conf has precedence over aliases in the
// assembled registries.conf.  It returns the possibly resolved alias or nil, a
// human-readable description of the config where the alias is specified, and
// an error. The origin of the config file is crucial for an improved user
// experience such that users are able to resolve potential pull errors.
// Almost all callers should use pkg/shortnames instead.
//
// Note that it’s the caller’s responsibility to pass only a repository
// (reference.IsNameOnly) as the short name.
func ResolveShortNameAlias(ctx *types.SystemContext, name string) (reference.Named, string, error) {
	if err := validateShortName(name); err != nil {
		return nil, "", err
	}
	confPath, lock, err := shortNameAliasesConfPathAndLock(ctx)
	if err != nil {
		return nil, "", err
	}

	// Acquire the lock as a reader to allow for multiple routines in the
	// same process space to read simultaneously.
	lock.RLock()
	defer lock.Unlock()

	_, aliasCache, err := loadShortNameAliasConf(confPath)
	if err != nil {
		return nil, "", err
	}

	// First look up the short-name-aliases.conf.  Note that a value may be
	// nil iff it's set as an empty string in the config.
	alias, resolved := aliasCache.namedAliases[name]
	if resolved {
		return alias.value, alias.configOrigin, nil
	}

	config, err := getConfig(ctx)
	if err != nil {
		return nil, "", err
	}
	alias, resolved = config.aliasCache.namedAliases[name]
	if resolved {
		return alias.value, alias.configOrigin, nil
	}
	return nil, "", nil
}

// editShortNameAlias loads the aliases.conf file and changes it. If value is
// set, it adds the name-value pair as a new alias. Otherwise, it will remove
// name from the config.
func editShortNameAlias(ctx *types.SystemContext, name string, value *string) (retErr error) {
	if err := validateShortName(name); err != nil {
		return err
	}
	if value != nil {
		if _, err := parseShortNameValue(*value); err != nil {
			return err
		}
	}

	confPath, lock, err := shortNameAliasesConfPathAndLock(ctx)
	if err != nil {
		return err
	}

	// Acquire the lock as a writer to prevent data corruption.
	lock.Lock()
	defer lock.Unlock()

	// Load the short-name-alias.conf, add the specified name-value pair,
	// and write it back to the file.
	conf, _, err := loadShortNameAliasConf(confPath)
	if err != nil {
		return err
	}

	if conf.Aliases == nil { // Ensure we have a map to update.
		conf.Aliases = make(map[string]string)
	}
	if value != nil {
		conf.Aliases[name] = *value
	} else {
		// If the name does not exist, throw an error.
		if _, exists := conf.Aliases[name]; !exists {
			return fmt.Errorf("short-name alias %q not found in %q: please check registries.conf files", name, confPath)
		}

		delete(conf.Aliases, name)
	}

	f, err := os.OpenFile(confPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	// since we are writing to this file, make sure we handle err on Close()
	defer func() {
		closeErr := f.Close()
		if retErr == nil {
			retErr = closeErr
		}
	}()

	encoder := toml.NewEncoder(f)
	return encoder.Encode(conf)
}

// AddShortNameAlias adds the specified name-value pair as a new alias to the
// user-specific aliases.conf.  It may override an existing alias for `name`.
//
// Note that it’s the caller’s responsibility to pass only a repository
// (reference.IsNameOnly) as the short name.
func AddShortNameAlias(ctx *types.SystemContext, name string, value string) error {
	return editShortNameAlias(ctx, name, &value)
}

// RemoveShortNameAlias clears the alias for the specified name.  It throws an
// error in case name does not exist in the machine-generated
// short-name-alias.conf.  In such case, the alias must be specified in one of
// the registries.conf files, which is the users' responsibility.
//
// Note that it’s the caller’s responsibility to pass only a repository
// (reference.IsNameOnly) as the short name.
func RemoveShortNameAlias(ctx *types.SystemContext, name string) error {
	return editShortNameAlias(ctx, name, nil)
}

// parseShortNameValue parses the specified alias into a reference.Named.  The alias is
// expected to not be tagged or carry a digest and *must* include a
// domain/registry.
//
// Note that the returned reference is always normalized.
func parseShortNameValue(alias string) (reference.Named, error) {
	ref, err := reference.Parse(alias)
	if err != nil {
		return nil, fmt.Errorf("parsing alias %q: %w", alias, err)
	}

	if _, ok := ref.(reference.Digested); ok {
		return nil, fmt.Errorf("invalid alias %q: must not contain digest", alias)
	}

	if _, ok := ref.(reference.Tagged); ok {
		return nil, fmt.Errorf("invalid alias %q: must not contain tag", alias)
	}

	named, ok := ref.(reference.Named)
	if !ok {
		return nil, fmt.Errorf("invalid alias %q: must contain registry and repository", alias)
	}

	registry := reference.Domain(named)
	if !strings.ContainsAny(registry, ".:") && registry != "localhost" {
		return nil, fmt.Errorf("invalid alias %q: must contain registry and repository", alias)
	}

	// A final parse to make sure that docker.io references are correctly
	// normalized (e.g., docker.io/alpine to docker.io/library/alpine.
	named, err = reference.ParseNormalizedNamed(alias)
	return named, err
}

// validateShortName parses the specified `name` of an alias (i.e., the left-hand
// side) and checks if it's a short name and does not include a tag or digest.
func validateShortName(name string) error {
	repo, err := reference.Parse(name)
	if err != nil {
		return fmt.Errorf("cannot parse short name: %q: %w", name, err)
	}

	if _, ok := repo.(reference.Digested); ok {
		return fmt.Errorf("invalid short name %q: must not contain digest", name)
	}

	if _, ok := repo.(reference.Tagged); ok {
		return fmt.Errorf("invalid short name %q: must not contain tag", name)
	}

	named, ok := repo.(reference.Named)
	if !ok {
		return fmt.Errorf("invalid short name %q: no name", name)
	}

	registry := reference.Domain(named)
	if strings.ContainsAny(registry, ".:") || registry == "localhost" {
		return fmt.Errorf("invalid short name %q: must not contain registry", name)
	}
	return nil
}

// newShortNameAliasCache parses shortNameAliasConf and returns the corresponding internal
// representation.
func newShortNameAliasCache(path string, conf *shortNameAliasConf) (*shortNameAliasCache, error) {
	res := shortNameAliasCache{
		namedAliases: make(map[string]alias),
	}
	errs := []error{}
	for name, value := range conf.Aliases {
		if err := validateShortName(name); err != nil {
			errs = append(errs, err)
		}

		// Empty right-hand side values in config files allow to reset
		// an alias in a previously loaded config. This way, drop-in
		// config files from registries.conf.d can reset potentially
		// malconfigured aliases.
		if value == "" {
			res.namedAliases[name] = alias{nil, path}
			continue
		}

		named, err := parseShortNameValue(value)
		if err != nil {
			// We want to report *all* malformed entries to avoid a
			// whack-a-mole for the user.
			errs = append(errs, err)
		} else {
			res.namedAliases[name] = alias{named, path}
		}
	}
	if len(errs) > 0 {
		return nil, multierr.Format("", "\n", "", errs)
	}
	return &res, nil
}

// updateWithConfigurationFrom updates c with configuration from updates.
// In case of conflict, updates is preferred.
func (c *shortNameAliasCache) updateWithConfigurationFrom(updates *shortNameAliasCache) {
	maps.Copy(c.namedAliases, updates.namedAliases)
}

func loadShortNameAliasConf(confPath string) (*shortNameAliasConf, *shortNameAliasCache, error) {
	conf := shortNameAliasConf{}

	meta, err := toml.DecodeFile(confPath, &conf)
	if err != nil && !os.IsNotExist(err) {
		// It's okay if the config doesn't exist.  Other errors are not.
		return nil, nil, fmt.Errorf("loading short-name aliases config file %q: %w", confPath, err)
	}
	if keys := meta.Undecoded(); len(keys) > 0 {
		logrus.Debugf("Failed to decode keys %q from %q", keys, confPath)
	}

	// Even if we don’t always need the cache, doing so validates the machine-generated config.  The
	// file could still be corrupted by another process or user.
	cache, err := newShortNameAliasCache(confPath, &conf)
	if err != nil {
		return nil, nil, fmt.Errorf("loading short-name aliases config file %q: %w", confPath, err)
	}

	return &conf, cache, nil
}

func shortNameAliasesConfPathAndLock(ctx *types.SystemContext) (string, *lockfile.LockFile, error) {
	shortNameAliasesConfPath, err := shortNameAliasesConfPath(ctx)
	if err != nil {
		return "", nil, err
	}
	// Make sure the path to file exists.
	if err := os.MkdirAll(filepath.Dir(shortNameAliasesConfPath), 0700); err != nil {
		return "", nil, err
	}

	lockPath := shortNameAliasesConfPath + ".lock"
	locker, err := lockfile.GetLockFile(lockPath)
	return shortNameAliasesConfPath, locker, err
}
