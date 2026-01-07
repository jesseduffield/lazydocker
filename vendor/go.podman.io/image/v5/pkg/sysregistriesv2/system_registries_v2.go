package sysregistriesv2

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/homedir"
	"go.podman.io/storage/pkg/regexp"
)

// systemRegistriesConfPath is the path to the system-wide registry
// configuration file and is used to add/subtract potential registries for
// obtaining images.  You can override this at build time with
// -ldflags '-X go.podman.io/image/v5/sysregistries.systemRegistriesConfPath=$your_path'
var systemRegistriesConfPath = builtinRegistriesConfPath

// systemRegistriesConfDirPath is the path to the system-wide registry
// configuration directory and is used to add/subtract potential registries for
// obtaining images.  You can override this at build time with
// -ldflags '-X go.podman.io/image/v5/sysregistries.systemRegistriesConfDirectoryPath=$your_path'
var systemRegistriesConfDirPath = builtinRegistriesConfDirPath

// AuthenticationFileHelper is a special key for credential helpers indicating
// the usage of consulting containers-auth.json files instead of a credential
// helper.
const AuthenticationFileHelper = "containers-auth.json"

const (
	// configuration values for "pull-from-mirror"
	// mirrors will be used for both digest pulls and tag pulls
	MirrorAll = "all"
	// mirrors will only be used for digest pulls
	MirrorByDigestOnly = "digest-only"
	// mirrors will only be used for tag pulls
	MirrorByTagOnly = "tag-only"
)

// Endpoint describes a remote location of a registry.
type Endpoint struct {
	// The endpoint's remote location. Can be empty iff Prefix contains
	// wildcard in the format: "*.example.com" for subdomain matching.
	// Please refer to FindRegistry / PullSourcesFromReference instead
	// of accessing/interpreting `Location` directly.
	Location string `toml:"location,omitempty"`
	// If true, certs verification will be skipped and HTTP (non-TLS)
	// connections will be allowed.
	Insecure bool `toml:"insecure,omitempty"`
	// PullFromMirror is used for adding restrictions to image pull through the mirror.
	// Set to "all", "digest-only", or "tag-only".
	// If "digest-only"， mirrors will only be used for digest pulls. Pulling images by
	// tag can potentially yield different images, depending on which endpoint
	// we pull from.  Restricting mirrors to pulls by digest avoids that issue.
	// If "tag-only", mirrors will only be used for tag pulls.  For a more up-to-date and expensive mirror
	// that it is less likely to be out of sync if tags move, it should not be unnecessarily
	// used for digest references.
	// Default is "all" (or left empty), mirrors will be used for both digest pulls and tag pulls unless the mirror-by-digest-only is set for the primary registry.
	// This can only be set in a registry's Mirror field, not in the registry's primary Endpoint.
	// This per-mirror setting is allowed only when mirror-by-digest-only is not configured for the primary registry.
	PullFromMirror string `toml:"pull-from-mirror,omitempty"`
}

// userRegistriesFile is the path to the per user registry configuration file.
var userRegistriesFile = filepath.FromSlash(".config/containers/registries.conf")

// userRegistriesDir is the path to the per user registry configuration file.
var userRegistriesDir = filepath.FromSlash(".config/containers/registries.conf.d")

// rewriteReference will substitute the provided reference `prefix` to the
// endpoints `location` from the `ref` and creates a new named reference from it.
// The function errors if the newly created reference is not parsable.
func (e *Endpoint) rewriteReference(ref reference.Named, prefix string) (reference.Named, error) {
	refString := ref.String()
	var newNamedRef string
	// refMatchingPrefix returns the length of the match. Everything that
	// follows the match gets appended to registries location.
	prefixLen := refMatchingPrefix(refString, prefix)
	if prefixLen == -1 {
		return nil, fmt.Errorf("invalid prefix '%v' for reference '%v'", prefix, refString)
	}
	// In the case of an empty `location` field, simply return the original
	// input ref as-is.
	//
	// FIXME: already validated in postProcessRegistries, so check can probably
	// be dropped.
	// https://github.com/containers/image/pull/1191#discussion_r610621608
	if e.Location == "" {
		if !strings.HasPrefix(prefix, "*.") {
			return nil, fmt.Errorf("invalid prefix '%v' for empty location, should be in the format: *.example.com", prefix)
		}
		return ref, nil
	}
	newNamedRef = e.Location + refString[prefixLen:]
	newParsedRef, err := reference.ParseNamed(newNamedRef)
	if err != nil {
		return nil, fmt.Errorf("rewriting reference: %w", err)
	}

	return newParsedRef, nil
}

// Registry represents a registry.
type Registry struct {
	// Prefix is used for matching images, and to translate one namespace to
	// another.  If `Prefix="example.com/bar"`, `location="example.com/foo/bar"`
	// and we pull from "example.com/bar/myimage:latest", the image will
	// effectively be pulled from "example.com/foo/bar/myimage:latest".
	// If no Prefix is specified, it defaults to the specified location.
	// Prefix can also be in the format: "*.example.com" for matching
	// subdomains. The wildcard should only be in the beginning and should also
	// not contain any namespaces or special characters: "/", "@" or ":".
	// Please refer to FindRegistry / PullSourcesFromReference instead
	// of accessing/interpreting `Prefix` directly.
	Prefix string `toml:"prefix"`
	// A registry is an Endpoint too
	Endpoint
	// The registry's mirrors.
	Mirrors []Endpoint `toml:"mirror,omitempty"`
	// If true, pulling from the registry will be blocked.
	Blocked bool `toml:"blocked,omitempty"`
	// If true, mirrors will only be used for digest pulls. Pulling images by
	// tag can potentially yield different images, depending on which endpoint
	// we pull from.  Restricting mirrors to pulls by digest avoids that issue.
	MirrorByDigestOnly bool `toml:"mirror-by-digest-only,omitempty"`
}

// PullSource consists of an Endpoint and a Reference. Note that the reference is
// rewritten according to the registries prefix and the Endpoint's location.
type PullSource struct {
	Endpoint  Endpoint
	Reference reference.Named
}

// PullSourcesFromReference returns a slice of PullSource's based on the passed
// reference.
func (r *Registry) PullSourcesFromReference(ref reference.Named) ([]PullSource, error) {
	var endpoints []Endpoint
	_, isDigested := ref.(reference.Canonical)
	if r.MirrorByDigestOnly {
		// Only use mirrors when the reference is a digested one.
		if isDigested {
			endpoints = append(endpoints, r.Mirrors...)
		}
	} else {
		for _, mirror := range r.Mirrors {
			// skip the mirror if per mirror setting exists but reference does not match the restriction
			switch mirror.PullFromMirror {
			case MirrorByDigestOnly:
				if !isDigested {
					continue
				}
			case MirrorByTagOnly:
				if isDigested {
					continue
				}
			}
			endpoints = append(endpoints, mirror)
		}
	}
	endpoints = append(endpoints, r.Endpoint)

	sources := []PullSource{}
	for _, ep := range endpoints {
		rewritten, err := ep.rewriteReference(ref, r.Prefix)
		if err != nil {
			return nil, err
		}
		sources = append(sources, PullSource{Endpoint: ep, Reference: rewritten})
	}

	return sources, nil
}

// V1TOMLregistries is for backwards compatibility to sysregistries v1
type V1TOMLregistries struct {
	Registries []string `toml:"registries"`
}

// V1TOMLConfig is for backwards compatibility to sysregistries v1
type V1TOMLConfig struct {
	Search   V1TOMLregistries `toml:"search"`
	Insecure V1TOMLregistries `toml:"insecure"`
	Block    V1TOMLregistries `toml:"block"`
}

// V1RegistriesConf is the sysregistries v1 configuration format.
type V1RegistriesConf struct {
	V1TOMLConfig `toml:"registries"`
}

// Nonempty returns true if config contains at least one configuration entry.
// Empty arrays are treated as missing entries.
func (config *V1RegistriesConf) Nonempty() bool {
	copy := *config // A shallow copy
	if copy.V1TOMLConfig.Search.Registries != nil && len(copy.V1TOMLConfig.Search.Registries) == 0 {
		copy.V1TOMLConfig.Search.Registries = nil
	}
	if copy.V1TOMLConfig.Insecure.Registries != nil && len(copy.V1TOMLConfig.Insecure.Registries) == 0 {
		copy.V1TOMLConfig.Insecure.Registries = nil
	}
	if copy.V1TOMLConfig.Block.Registries != nil && len(copy.V1TOMLConfig.Block.Registries) == 0 {
		copy.V1TOMLConfig.Block.Registries = nil
	}
	return copy.hasSetField()
}

// hasSetField returns true if config contains at least one configuration entry.
// This is useful because of a subtlety of the behavior of the TOML decoder, where a missing array field
// is not modified while unmarshaling (in our case remains to nil), while an [] is unmarshaled
// as a non-nil []string{}.
func (config *V1RegistriesConf) hasSetField() bool {
	return !reflect.DeepEqual(*config, V1RegistriesConf{})
}

// V2RegistriesConf is the sysregistries v2 configuration format.
type V2RegistriesConf struct {
	Registries []Registry `toml:"registry"`
	// An array of host[:port] (not prefix!) entries to use for resolving unqualified image references
	UnqualifiedSearchRegistries []string `toml:"unqualified-search-registries"`
	// An array of global credential helpers to use for authentication
	// (e.g., ["pass", "secretservice"]).  The helpers are consulted in the
	// specified order.  Note that "containers-auth.json" is a reserved
	// value for consulting auth files as specified in
	// containers-auth.json(5).
	//
	// If empty, CredentialHelpers defaults to  ["containers-auth.json"].
	CredentialHelpers []string `toml:"credential-helpers"`

	// ShortNameMode defines how short-name resolution should be handled by
	// _consumers_ of this package.  Depending on the mode, the user should
	// be prompted with a choice of using one of the unqualified-search
	// registries when referring to a short name.
	//
	// Valid modes are: * "prompt": prompt if stdout is a TTY, otherwise
	// use all unqualified-search registries * "enforcing": always prompt
	// and error if stdout is not a TTY * "disabled": do not prompt and
	// potentially use all unqualified-search registries
	ShortNameMode string `toml:"short-name-mode"`

	// AdditionalLayerStoreAuthHelper is a helper binary that receives
	// registry credentials pass them to Additional Layer Store for
	// registry authentication. These credentials are only collected when pulling (not pushing).
	AdditionalLayerStoreAuthHelper string `toml:"additional-layer-store-auth-helper"`

	shortNameAliasConf

	// If you add any field, make sure to update Nonempty() below.
}

// Nonempty returns true if config contains at least one configuration entry.
func (config *V2RegistriesConf) Nonempty() bool {
	copy := *config // A shallow copy
	if copy.Registries != nil && len(copy.Registries) == 0 {
		copy.Registries = nil
	}
	if copy.UnqualifiedSearchRegistries != nil && len(copy.UnqualifiedSearchRegistries) == 0 {
		copy.UnqualifiedSearchRegistries = nil
	}
	if copy.CredentialHelpers != nil && len(copy.CredentialHelpers) == 0 {
		copy.CredentialHelpers = nil
	}
	if !copy.shortNameAliasConf.nonempty() {
		copy.shortNameAliasConf = shortNameAliasConf{}
	}
	return copy.hasSetField()
}

// hasSetField returns true if config contains at least one configuration entry.
// This is useful because of a subtlety of the behavior of the TOML decoder, where a missing array field
// is not modified while unmarshaling (in our case remains to nil), while an [] is unmarshaled
// as a non-nil []string{}.
func (config *V2RegistriesConf) hasSetField() bool {
	return !reflect.DeepEqual(*config, V2RegistriesConf{})
}

// parsedConfig is the result of parsing, and possibly merging, configuration files;
// it is the boundary between the process of reading+ingesting the files, and
// later interpreting the configuration based on caller’s requests.
type parsedConfig struct {
	// NOTE: Update also parsedConfig.updateWithConfigurationFrom!

	// partialV2 must continue to exist to maintain the return value of TryUpdatingCache
	// for compatibility with existing callers.
	// We store the authoritative Registries and UnqualifiedSearchRegistries values there as well.
	partialV2 V2RegistriesConf
	// Absolute path to the configuration file that set the UnqualifiedSearchRegistries.
	unqualifiedSearchRegistriesOrigin string
	// Result of parsing of partialV2.ShortNameMode.
	// NOTE: May be ShortNameModeInvalid to represent ShortNameMode == "" in intermediate values;
	// the full configuration in configCache / getConfig() always contains a valid value.
	shortNameMode types.ShortNameMode
	aliasCache    *shortNameAliasCache
}

// InvalidRegistries represents an invalid registry configurations.  An example
// is when "registry.com" is defined multiple times in the configuration but
// with conflicting security settings.
type InvalidRegistries struct {
	s string
}

// Error returns the error string.
func (e *InvalidRegistries) Error() string {
	return e.s
}

// parseLocation parses the input string, performs some sanity checks and returns
// the sanitized input string.  An error is returned if the input string is
// empty or if contains an "http{s,}://" prefix.
func parseLocation(input string) (string, error) {
	trimmed := strings.TrimRight(input, "/")

	// FIXME: This check needs to exist but fails for empty Location field with
	// wildcarded prefix. Removal of this check "only" allows invalid input in,
	// and does not prevent correct operation.
	// https://github.com/containers/image/pull/1191#discussion_r610122617
	//
	//	if trimmed == "" {
	//		return "", &InvalidRegistries{s: "invalid location: cannot be empty"}
	//	}
	//

	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		msg := fmt.Sprintf("invalid location '%s': URI schemes are not supported", input)
		return "", &InvalidRegistries{s: msg}
	}

	return trimmed, nil
}

// ConvertToV2 returns a v2 config corresponding to a v1 one.
func (config *V1RegistriesConf) ConvertToV2() (*V2RegistriesConf, error) {
	regMap := make(map[string]*Registry)
	// The order of the registries is not really important, but make it deterministic (the same for the same config file)
	// to minimize behavior inconsistency and not contribute to difficult-to-reproduce situations.
	registryOrder := []string{}

	getRegistry := func(location string) (*Registry, error) { // Note: _pointer_ to a long-lived object
		var err error
		location, err = parseLocation(location)
		if err != nil {
			return nil, err
		}
		reg, exists := regMap[location]
		if !exists {
			reg = &Registry{
				Endpoint: Endpoint{Location: location},
				Mirrors:  []Endpoint{},
				Prefix:   location,
			}
			regMap[location] = reg
			registryOrder = append(registryOrder, location)
		}
		return reg, nil
	}

	for _, blocked := range config.V1TOMLConfig.Block.Registries {
		reg, err := getRegistry(blocked)
		if err != nil {
			return nil, err
		}
		reg.Blocked = true
	}
	for _, insecure := range config.V1TOMLConfig.Insecure.Registries {
		reg, err := getRegistry(insecure)
		if err != nil {
			return nil, err
		}
		reg.Insecure = true
	}

	res := &V2RegistriesConf{
		UnqualifiedSearchRegistries: config.V1TOMLConfig.Search.Registries,
	}
	for _, location := range registryOrder {
		reg := regMap[location]
		res.Registries = append(res.Registries, *reg)
	}
	return res, nil
}

// anchoredDomainRegexp is an internal implementation detail of postProcess, defining the valid values of elements of UnqualifiedSearchRegistries.
var anchoredDomainRegexp = regexp.Delayed("^" + reference.DomainRegexp.String() + "$")

// postProcess checks the consistency of all the configuration, looks for conflicts,
// and normalizes the configuration (e.g., sets the Prefix to Location if not set).
func (config *V2RegistriesConf) postProcessRegistries() error {
	regMap := make(map[string][]*Registry)

	for i := range config.Registries {
		reg := &config.Registries[i]
		// make sure Location and Prefix are valid
		var err error
		reg.Location, err = parseLocation(reg.Location)
		if err != nil {
			return err
		}

		if reg.Prefix == "" {
			if reg.Location == "" {
				return &InvalidRegistries{s: "invalid condition: both location and prefix are unset"}
			}
			reg.Prefix = reg.Location
		} else {
			reg.Prefix, err = parseLocation(reg.Prefix)
			if err != nil {
				return err
			}
			// FIXME: allow config authors to always use Prefix.
			// https://github.com/containers/image/pull/1191#discussion_r610622495
			if !strings.HasPrefix(reg.Prefix, "*.") && reg.Location == "" {
				return &InvalidRegistries{s: "invalid condition: location is unset and prefix is not in the format: *.example.com"}
			}
		}

		// validate the mirror usage settings does not apply to primary registry
		if reg.PullFromMirror != "" {
			return fmt.Errorf("pull-from-mirror must not be set for a non-mirror registry %q", reg.Prefix)
		}
		// make sure mirrors are valid
		for j := range reg.Mirrors {
			mir := &reg.Mirrors[j]
			mir.Location, err = parseLocation(mir.Location)
			if err != nil {
				return err
			}

			//FIXME: unqualifiedSearchRegistries now also accepts empty values
			//and shouldn't
			// https://github.com/containers/image/pull/1191#discussion_r610623216
			if mir.Location == "" {
				return &InvalidRegistries{s: "invalid condition: mirror location is unset"}
			}

			if reg.MirrorByDigestOnly && mir.PullFromMirror != "" {
				return &InvalidRegistries{s: fmt.Sprintf("cannot set mirror usage mirror-by-digest-only for the registry (%q) and pull-from-mirror for per-mirror (%q) at the same time", reg.Prefix, mir.Location)}
			}
			if mir.PullFromMirror != "" && mir.PullFromMirror != MirrorAll &&
				mir.PullFromMirror != MirrorByDigestOnly && mir.PullFromMirror != MirrorByTagOnly {
				return &InvalidRegistries{s: fmt.Sprintf("unsupported pull-from-mirror value %q for mirror %q", mir.PullFromMirror, mir.Location)}
			}
		}
		if reg.Location == "" {
			regMap[reg.Prefix] = append(regMap[reg.Prefix], reg)
		} else {
			regMap[reg.Location] = append(regMap[reg.Location], reg)
		}
	}

	// Given a registry can be mentioned multiple times (e.g., to have
	// multiple prefixes backed by different mirrors), we need to make sure
	// there are no conflicts among them.
	//
	// Note: we need to iterate over the registries array to ensure a
	// deterministic behavior which is not guaranteed by maps.
	for _, reg := range config.Registries {
		var others []*Registry
		var ok bool
		if reg.Location == "" {
			others, ok = regMap[reg.Prefix]
		} else {
			others, ok = regMap[reg.Location]
		}
		if !ok {
			return fmt.Errorf("Internal error in V2RegistriesConf.PostProcess: entry in regMap is missing")
		}
		for _, other := range others {
			if reg.Insecure != other.Insecure {
				msg := fmt.Sprintf("registry '%s' is defined multiple times with conflicting 'insecure' setting", reg.Location)
				return &InvalidRegistries{s: msg}
			}

			if reg.Blocked != other.Blocked {
				msg := fmt.Sprintf("registry '%s' is defined multiple times with conflicting 'blocked' setting", reg.Location)
				return &InvalidRegistries{s: msg}
			}
		}
	}

	for i := range config.UnqualifiedSearchRegistries {
		registry, err := parseLocation(config.UnqualifiedSearchRegistries[i])
		if err != nil {
			return err
		}
		if !anchoredDomainRegexp.MatchString(registry) {
			return &InvalidRegistries{fmt.Sprintf("Invalid unqualified-search-registries entry %#v", registry)}
		}
		config.UnqualifiedSearchRegistries[i] = registry
	}

	// Registries are ordered and the first longest prefix always wins,
	// rendering later items with the same prefix non-existent. We cannot error
	// out anymore as this might break existing users, so let's just ignore them
	// to guarantee that the same prefix exists only once.
	//
	// As a side effect of parsedConfig.updateWithConfigurationFrom, the Registries slice
	// is always sorted. To be consistent in situations where it is not called (no drop-ins),
	// sort it here as well.
	prefixes := []string{}
	uniqueRegistries := make(map[string]Registry)
	for i := range config.Registries {
		// TODO: should we warn if we see the same prefix being used multiple times?
		prefix := config.Registries[i].Prefix
		if _, exists := uniqueRegistries[prefix]; !exists {
			uniqueRegistries[prefix] = config.Registries[i]
			prefixes = append(prefixes, prefix)
		}
	}
	sort.Strings(prefixes)
	config.Registries = []Registry{}
	for _, prefix := range prefixes {
		config.Registries = append(config.Registries, uniqueRegistries[prefix])
	}

	return nil
}

// ConfigPath returns the path to the system-wide registry configuration file.
// Deprecated: This API implies configuration is read from files, and that there is only one.
// Please use ConfigurationSourceDescription to obtain a string usable for error messages.
func ConfigPath(ctx *types.SystemContext) string {
	return newConfigWrapper(ctx).configPath
}

// ConfigDirPath returns the path to the directory for drop-in
// registry configuration files.
// Deprecated: This API implies configuration is read from directories, and that there is only one.
// Please use ConfigurationSourceDescription to obtain a string usable for error messages.
func ConfigDirPath(ctx *types.SystemContext) string {
	configWrapper := newConfigWrapper(ctx)
	if configWrapper.userConfigDirPath != "" {
		return configWrapper.userConfigDirPath
	}
	return configWrapper.configDirPath
}

// configWrapper is used to store the paths from ConfigPath and ConfigDirPath
// and acts as a key to the internal cache.
type configWrapper struct {
	// path to the registries.conf file
	configPath string
	// path to system-wide registries.conf.d directory, or "" if not used
	configDirPath string
	// path to user specified registries.conf.d directory, or "" if not used
	userConfigDirPath string
}

// newConfigWrapper returns a configWrapper for the specified SystemContext.
func newConfigWrapper(ctx *types.SystemContext) configWrapper {
	return newConfigWrapperWithHomeDir(ctx, homedir.Get())
}

// newConfigWrapperWithHomeDir is an internal implementation detail of newConfigWrapper,
// it exists only to allow testing it with an artificial home directory.
func newConfigWrapperWithHomeDir(ctx *types.SystemContext, homeDir string) configWrapper {
	var wrapper configWrapper
	userRegistriesFilePath := filepath.Join(homeDir, userRegistriesFile)
	userRegistriesDirPath := filepath.Join(homeDir, userRegistriesDir)

	// decide configPath using per-user path or system file
	if ctx != nil && ctx.SystemRegistriesConfPath != "" {
		wrapper.configPath = ctx.SystemRegistriesConfPath
	} else if err := fileutils.Exists(userRegistriesFilePath); err == nil {
		// per-user registries.conf exists, not reading system dir
		// return config dirs from ctx or per-user one
		wrapper.configPath = userRegistriesFilePath
		if ctx != nil && ctx.SystemRegistriesConfDirPath != "" {
			wrapper.configDirPath = ctx.SystemRegistriesConfDirPath
		} else {
			wrapper.userConfigDirPath = userRegistriesDirPath
		}

		return wrapper
	} else if ctx != nil && ctx.RootForImplicitAbsolutePaths != "" {
		wrapper.configPath = filepath.Join(ctx.RootForImplicitAbsolutePaths, systemRegistriesConfPath)
	} else {
		wrapper.configPath = systemRegistriesConfPath
	}

	// potentially use both system and per-user dirs if not using per-user config file
	if ctx != nil && ctx.SystemRegistriesConfDirPath != "" {
		// dir explicitly chosen: use only that one
		wrapper.configDirPath = ctx.SystemRegistriesConfDirPath
	} else if ctx != nil && ctx.RootForImplicitAbsolutePaths != "" {
		wrapper.configDirPath = filepath.Join(ctx.RootForImplicitAbsolutePaths, systemRegistriesConfDirPath)
		wrapper.userConfigDirPath = userRegistriesDirPath
	} else {
		wrapper.configDirPath = systemRegistriesConfDirPath
		wrapper.userConfigDirPath = userRegistriesDirPath
	}

	return wrapper
}

// ConfigurationSourceDescription returns a string containers paths of registries.conf and registries.conf.d
func ConfigurationSourceDescription(ctx *types.SystemContext) string {
	wrapper := newConfigWrapper(ctx)
	configSources := []string{wrapper.configPath}
	if wrapper.configDirPath != "" {
		configSources = append(configSources, wrapper.configDirPath)
	}
	if wrapper.userConfigDirPath != "" {
		configSources = append(configSources, wrapper.userConfigDirPath)
	}
	return strings.Join(configSources, ", ")
}

// configMutex is used to synchronize concurrent accesses to configCache.
var configMutex = sync.Mutex{}

// configCache caches already loaded configs with config paths as keys and is
// used to avoid redundantly parsing configs. Concurrent accesses to the cache
// are synchronized via configMutex.
var configCache = make(map[configWrapper]*parsedConfig)

// InvalidateCache invalidates the registry cache.  This function is meant to be
// used for long-running processes that need to reload potential changes made to
// the cached registry config files.
func InvalidateCache() {
	configMutex.Lock()
	defer configMutex.Unlock()
	configCache = make(map[configWrapper]*parsedConfig)
}

// getConfig returns the config object corresponding to ctx, loading it if it is not yet cached.
func getConfig(ctx *types.SystemContext) (*parsedConfig, error) {
	wrapper := newConfigWrapper(ctx)
	configMutex.Lock()
	if config, inCache := configCache[wrapper]; inCache {
		configMutex.Unlock()
		return config, nil
	}
	configMutex.Unlock()

	return tryUpdatingCache(ctx, wrapper)
}

// dropInConfigs returns a slice of drop-in-configs from the registries.conf.d
// directory.
func dropInConfigs(wrapper configWrapper) ([]string, error) {
	var (
		configs  []string
		dirPaths []string
	)
	if wrapper.configDirPath != "" {
		dirPaths = append(dirPaths, wrapper.configDirPath)
	}
	if wrapper.userConfigDirPath != "" {
		dirPaths = append(dirPaths, wrapper.userConfigDirPath)
	}
	for _, dirPath := range dirPaths {
		err := filepath.WalkDir(dirPath,
			// WalkFunc to read additional configs
			func(path string, d fs.DirEntry, err error) error {
				switch {
				case err != nil:
					// return error (could be a permission problem)
					return err
				case d == nil:
					// this should only happen when err != nil but let's be sure
					return nil
				case d.IsDir():
					if path != dirPath {
						// make sure to not recurse into sub-directories
						return filepath.SkipDir
					}
					// ignore directories
					return nil
				default:
					// only add *.conf files
					if strings.HasSuffix(path, ".conf") {
						configs = append(configs, path)
					}
					return nil
				}
			},
		)

		if err != nil && !os.IsNotExist(err) {
			// Ignore IsNotExist errors: most systems won't have a registries.conf.d
			// directory.
			return nil, fmt.Errorf("reading registries.conf.d: %w", err)
		}
	}

	return configs, nil
}

// TryUpdatingCache loads the configuration from the provided `SystemContext`
// without using the internal cache. On success, the loaded configuration will
// be added into the internal registry cache.
// It returns the resulting configuration; this is DEPRECATED and may not correctly
// reflect any future data handled by this package.
func TryUpdatingCache(ctx *types.SystemContext) (*V2RegistriesConf, error) {
	config, err := tryUpdatingCache(ctx, newConfigWrapper(ctx))
	if err != nil {
		return nil, err
	}
	return &config.partialV2, err
}

// tryUpdatingCache implements TryUpdatingCache with an additional configWrapper
// argument to avoid redundantly calculating the config paths.
func tryUpdatingCache(ctx *types.SystemContext, wrapper configWrapper) (*parsedConfig, error) {
	configMutex.Lock()
	defer configMutex.Unlock()

	// load the config
	config, err := loadConfigFile(wrapper.configPath, false)
	if err != nil {
		// Continue with an empty []Registry if we use the default config, which
		// implies that the config path of the SystemContext isn't set.
		//
		// Note: if ctx.SystemRegistriesConfPath points to the default config,
		// we will still return an error.
		if os.IsNotExist(err) && (ctx == nil || ctx.SystemRegistriesConfPath == "") {
			config = &parsedConfig{}
			config.partialV2 = V2RegistriesConf{Registries: []Registry{}}
			config.aliasCache, err = newShortNameAliasCache("", &shortNameAliasConf{})
			if err != nil {
				return nil, err // Should never happen
			}
		} else {
			return nil, fmt.Errorf("loading registries configuration %q: %w", wrapper.configPath, err)
		}
	}

	// Load the configs from the conf directory path.
	dinConfigs, err := dropInConfigs(wrapper)
	if err != nil {
		return nil, err
	}
	for _, path := range dinConfigs {
		// Enforce v2 format for drop-in-configs.
		dropIn, err := loadConfigFile(path, true)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// file must have been removed between the directory listing
				// and the open call, ignore that as it is a expected race
				continue
			}
			return nil, fmt.Errorf("loading drop-in registries configuration %q: %w", path, err)
		}
		config.updateWithConfigurationFrom(dropIn)
	}

	if config.shortNameMode == types.ShortNameModeInvalid {
		config.shortNameMode = defaultShortNameMode
	}

	if len(config.partialV2.CredentialHelpers) == 0 {
		config.partialV2.CredentialHelpers = []string{AuthenticationFileHelper}
	}

	// populate the cache
	configCache[wrapper] = config
	return config, nil
}

// GetRegistries has been deprecated. Use FindRegistry instead.
//
// GetRegistries loads and returns the registries specified in the config.
// Note the parsed content of registry config files is cached.  For reloading,
// use `InvalidateCache` and re-call `GetRegistries`.
func GetRegistries(ctx *types.SystemContext) ([]Registry, error) {
	config, err := getConfig(ctx)
	if err != nil {
		return nil, err
	}
	return config.partialV2.Registries, nil
}

// UnqualifiedSearchRegistries returns a list of host[:port] entries to try
// for unqualified image search, in the returned order)
func UnqualifiedSearchRegistries(ctx *types.SystemContext) ([]string, error) {
	registries, _, err := UnqualifiedSearchRegistriesWithOrigin(ctx)
	return registries, err
}

// UnqualifiedSearchRegistriesWithOrigin returns a list of host[:port] entries
// to try for unqualified image search, in the returned order.  It also returns
// a human-readable description of where these entries are specified (e.g., a
// registries.conf file).
func UnqualifiedSearchRegistriesWithOrigin(ctx *types.SystemContext) ([]string, string, error) {
	config, err := getConfig(ctx)
	if err != nil {
		return nil, "", err
	}
	return config.partialV2.UnqualifiedSearchRegistries, config.unqualifiedSearchRegistriesOrigin, nil
}

// parseShortNameMode translates the string into well-typed
// types.ShortNameMode.
func parseShortNameMode(mode string) (types.ShortNameMode, error) {
	switch mode {
	case "disabled":
		return types.ShortNameModeDisabled, nil
	case "enforcing":
		return types.ShortNameModeEnforcing, nil
	case "permissive":
		return types.ShortNameModePermissive, nil
	default:
		return types.ShortNameModeInvalid, fmt.Errorf("invalid short-name mode: %q", mode)
	}
}

// GetShortNameMode returns the configured types.ShortNameMode.
func GetShortNameMode(ctx *types.SystemContext) (types.ShortNameMode, error) {
	if ctx != nil && ctx.ShortNameMode != nil {
		return *ctx.ShortNameMode, nil
	}
	config, err := getConfig(ctx)
	if err != nil {
		return -1, err
	}
	return config.shortNameMode, err
}

// CredentialHelpers returns the global top-level credential helpers.
func CredentialHelpers(sys *types.SystemContext) ([]string, error) {
	config, err := getConfig(sys)
	if err != nil {
		return nil, err
	}
	return config.partialV2.CredentialHelpers, nil
}

// AdditionalLayerStoreAuthHelper returns the helper for passing registry
// credentials to Additional Layer Store.
func AdditionalLayerStoreAuthHelper(sys *types.SystemContext) (string, error) {
	config, err := getConfig(sys)
	if err != nil {
		return "", err
	}
	return config.partialV2.AdditionalLayerStoreAuthHelper, nil
}

// refMatchingSubdomainPrefix returns the length of ref
// iff ref, which is a registry, repository namespace, repository or image reference (as formatted by
// reference.Domain(), reference.Named.Name() or reference.Reference.String()
// — note that this requires the name to start with an explicit hostname!),
// matches a Registry.Prefix value containing wildcarded subdomains in the
// format: *.example.com. Wildcards are only accepted at the beginning, so
// other formats like example.*.com will not work. Wildcarded prefixes also
// cannot contain port numbers or namespaces in them.
func refMatchingSubdomainPrefix(ref, prefix string) int {
	index := strings.Index(ref, prefix[1:])
	if index == -1 {
		return -1
	}
	if strings.Contains(ref[:index], "/") {
		return -1
	}
	index += len(prefix[1:])
	if index == len(ref) {
		return index
	}
	switch ref[index] {
	case ':', '/', '@':
		return index
	default:
		return -1
	}
}

// refMatchingPrefix returns the length of the prefix iff ref,
// which is a registry, repository namespace, repository or image reference (as formatted by
// reference.Domain(), reference.Named.Name() or reference.Reference.String()
// — note that this requires the name to start with an explicit hostname!),
// matches a Registry.Prefix value.
// (This is split from the caller primarily to make testing easier.)
func refMatchingPrefix(ref, prefix string) int {
	switch {
	case strings.HasPrefix(prefix, "*."):
		return refMatchingSubdomainPrefix(ref, prefix)
	case len(ref) < len(prefix):
		return -1
	case len(ref) == len(prefix):
		if ref == prefix {
			return len(prefix)
		}
		return -1
	case len(ref) > len(prefix):
		if !strings.HasPrefix(ref, prefix) {
			return -1
		}
		c := ref[len(prefix)]
		// This allows "example.com:5000" to match "example.com",
		// which is unintended; that will get fixed eventually, DON'T RELY
		// ON THE CURRENT BEHAVIOR.
		if c == ':' || c == '/' || c == '@' {
			return len(prefix)
		}
		return -1
	default:
		panic("Internal error: impossible comparison outcome")
	}
}

// FindRegistry returns the Registry with the longest prefix for ref,
// which is a registry, repository namespace repository or image reference (as formatted by
// reference.Domain(), reference.Named.Name() or reference.Reference.String()
// — note that this requires the name to start with an explicit hostname!).
// If no Registry prefixes the image, nil is returned.
func FindRegistry(ctx *types.SystemContext, ref string) (*Registry, error) {
	config, err := getConfig(ctx)
	if err != nil {
		return nil, err
	}

	return findRegistryWithParsedConfig(config, ref)
}

// findRegistryWithParsedConfig implements `FindRegistry` with a pre-loaded
// parseConfig.
func findRegistryWithParsedConfig(config *parsedConfig, ref string) (*Registry, error) {
	reg := Registry{}
	prefixLen := 0
	for _, r := range config.partialV2.Registries {
		if refMatchingPrefix(ref, r.Prefix) != -1 {
			length := len(r.Prefix)
			if length > prefixLen {
				reg = r
				prefixLen = length
			}
		}
	}
	if prefixLen != 0 {
		return &reg, nil
	}
	return nil, nil
}

// loadConfigFile loads and unmarshals a single config file.
// Use forceV2 if the config must in the v2 format.
func loadConfigFile(path string, forceV2 bool) (*parsedConfig, error) {
	logrus.Debugf("Loading registries configuration %q", path)

	// tomlConfig allows us to unmarshal either V1 or V2 simultaneously.
	type tomlConfig struct {
		V2RegistriesConf
		V1RegistriesConf // for backwards compatibility with sysregistries v1
	}

	// Load the tomlConfig. Note that `DecodeFile` will overwrite set fields.
	var combinedTOML tomlConfig
	meta, err := toml.DecodeFile(path, &combinedTOML)
	if err != nil {
		return nil, err
	}
	if keys := meta.Undecoded(); len(keys) > 0 {
		logrus.Debugf("Failed to decode keys %q from %q", keys, path)
	}

	if combinedTOML.V1RegistriesConf.hasSetField() {
		// Enforce the v2 format if requested.
		if forceV2 {
			return nil, &InvalidRegistries{s: "registry must be in v2 format but is in v1"}
		}

		// Convert a v1 config into a v2 config.
		if combinedTOML.V2RegistriesConf.hasSetField() {
			return nil, &InvalidRegistries{s: fmt.Sprintf("mixing sysregistry v1/v2 is not supported: %#v", combinedTOML)}
		}
		converted, err := combinedTOML.V1RegistriesConf.ConvertToV2()
		if err != nil {
			return nil, err
		}
		combinedTOML.V1RegistriesConf = V1RegistriesConf{}
		combinedTOML.V2RegistriesConf = *converted
	}

	res := parsedConfig{partialV2: combinedTOML.V2RegistriesConf}

	// Post process registries, set the correct prefixes, sanity checks, etc.
	if err := res.partialV2.postProcessRegistries(); err != nil {
		return nil, err
	}

	res.unqualifiedSearchRegistriesOrigin = path

	if len(res.partialV2.ShortNameMode) > 0 {
		mode, err := parseShortNameMode(res.partialV2.ShortNameMode)
		if err != nil {
			return nil, err
		}
		res.shortNameMode = mode
	} else {
		res.shortNameMode = types.ShortNameModeInvalid
	}

	// Valid wildcarded prefixes must be in the format: *.example.com
	// FIXME: Move to postProcessRegistries
	// https://github.com/containers/image/pull/1191#discussion_r610623829
	for i := range res.partialV2.Registries {
		prefix := res.partialV2.Registries[i].Prefix
		if strings.HasPrefix(prefix, "*.") && strings.ContainsAny(prefix, "/@:") {
			msg := fmt.Sprintf("Wildcarded prefix should be in the format: *.example.com. Current prefix %q is incorrectly formatted", prefix)
			return nil, &InvalidRegistries{s: msg}
		}
	}

	// Parse and validate short-name aliases.
	cache, err := newShortNameAliasCache(path, &res.partialV2.shortNameAliasConf)
	if err != nil {
		return nil, fmt.Errorf("validating short-name aliases: %w", err)
	}
	res.aliasCache = cache
	// Clear conf.partialV2.shortNameAliasConf to make it available for garbage collection and
	// reduce memory consumption.  We're consulting aliasCache for lookups.
	res.partialV2.shortNameAliasConf = shortNameAliasConf{}

	return &res, nil
}

// updateWithConfigurationFrom updates c with configuration from updates.
//
// Fields present in updates will typically replace already set fields in c.
// The [[registry]] and alias tables are merged.
func (c *parsedConfig) updateWithConfigurationFrom(updates *parsedConfig) {
	// == Merge Registries:
	registryMap := make(map[string]Registry)
	for i := range c.partialV2.Registries {
		registryMap[c.partialV2.Registries[i].Prefix] = c.partialV2.Registries[i]
	}
	// Merge the freshly loaded registries.
	for i := range updates.partialV2.Registries {
		registryMap[updates.partialV2.Registries[i].Prefix] = updates.partialV2.Registries[i]
	}

	// Go maps have a non-deterministic order when iterating the keys, so
	// we sort the keys to enforce some order in Registries slice.
	// Some consumers of c/image (e.g., CRI-O) log the configuration
	// and a non-deterministic order could easily cause confusion.
	prefixes := slices.Sorted(maps.Keys(registryMap))

	c.partialV2.Registries = []Registry{}
	for _, prefix := range prefixes {
		c.partialV2.Registries = append(c.partialV2.Registries, registryMap[prefix])
	}

	// == Merge UnqualifiedSearchRegistries:
	// This depends on an subtlety of the behavior of the TOML decoder, where a missing array field
	// is not modified while unmarshaling (in our case remains to nil), while an [] is unmarshaled
	// as a non-nil []string{}.
	if updates.partialV2.UnqualifiedSearchRegistries != nil {
		c.partialV2.UnqualifiedSearchRegistries = updates.partialV2.UnqualifiedSearchRegistries
		c.unqualifiedSearchRegistriesOrigin = updates.unqualifiedSearchRegistriesOrigin
	}

	// == Merge credential helpers:
	if updates.partialV2.CredentialHelpers != nil {
		c.partialV2.CredentialHelpers = updates.partialV2.CredentialHelpers
	}

	// == Merge shortNameMode:
	// We don’t maintain c.partialV2.ShortNameMode.
	if updates.shortNameMode != types.ShortNameModeInvalid {
		c.shortNameMode = updates.shortNameMode
	}

	// == Merge AdditionalLayerStoreAuthHelper:
	if updates.partialV2.AdditionalLayerStoreAuthHelper != "" {
		c.partialV2.AdditionalLayerStoreAuthHelper = updates.partialV2.AdditionalLayerStoreAuthHelper
	}

	// == Merge aliasCache:
	// We don’t maintain (in fact we actively clear) c.partialV2.shortNameAliasConf.
	c.aliasCache.updateWithConfigurationFrom(updates.aliasCache)
}
