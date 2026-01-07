package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	helperclient "github.com/docker/docker-credential-helpers/client"
	"github.com/docker/docker-credential-helpers/credentials"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/multierr"
	"go.podman.io/image/v5/internal/set"
	"go.podman.io/image/v5/pkg/sysregistriesv2"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/homedir"
	"go.podman.io/storage/pkg/ioutils"
)

type dockerAuthConfig struct {
	Auth          string `json:"auth,omitempty"`
	IdentityToken string `json:"identitytoken,omitempty"`
}

type dockerConfigFile struct {
	AuthConfigs map[string]dockerAuthConfig `json:"auths"`
	CredHelpers map[string]string           `json:"credHelpers,omitempty"`
}

var (
	defaultPerUIDPathFormat = filepath.FromSlash("/run/containers/%d/auth.json")
	xdgConfigHomePath       = filepath.FromSlash("containers/auth.json")
	xdgRuntimeDirPath       = filepath.FromSlash("containers/auth.json")
	dockerHomePath          = filepath.FromSlash(".docker/config.json")
	dockerLegacyHomePath    = ".dockercfg"
	nonLinuxAuthFilePath    = filepath.FromSlash(".config/containers/auth.json")

	// ErrNotLoggedIn is returned for users not logged into a registry
	// that they are trying to logout of
	ErrNotLoggedIn = errors.New("not logged in")
	// ErrNotSupported is returned for unsupported methods
	ErrNotSupported = errors.New("not supported")
)

// authPath combines a path to a file with container registry credentials,
// along with expected properties of that path (currently just whether it's
// legacy format or not).
type authPath struct {
	path         string
	legacyFormat bool
}

// newAuthPathDefault constructs an authPath in non-legacy format.
func newAuthPathDefault(path string) authPath {
	return authPath{path: path, legacyFormat: false}
}

// GetAllCredentials returns the registry credentials for all registries stored
// in any of the configured credential helpers.
func GetAllCredentials(sys *types.SystemContext) (map[string]types.DockerAuthConfig, error) {
	// To keep things simple, let's first extract all registries from all
	// possible sources, and then call `GetCredentials` on them.  That
	// prevents us from having to reverse engineer the logic in
	// `GetCredentials`.
	allKeys := set.New[string]()

	// To use GetCredentials, we must at least convert the URL forms into host names.
	// While we're at it, we’ll also canonicalize docker.io to the standard format.
	normalizedDockerIORegistry := normalizeRegistry("docker.io")

	helpers, err := sysregistriesv2.CredentialHelpers(sys)
	if err != nil {
		return nil, err
	}
	for _, helper := range helpers {
		switch helper {
		// Special-case the built-in helper for auth files.
		case sysregistriesv2.AuthenticationFileHelper:
			for _, path := range getAuthFilePaths(sys, homedir.Get()) {
				// parse returns an empty map in case the path doesn't exist.
				fileContents, err := path.parse()
				if err != nil {
					return nil, fmt.Errorf("reading JSON file %q: %w", path.path, err)
				}
				// Credential helpers in the auth file have a
				// direct mapping to a registry, so we can just
				// walk the map.
				allKeys.AddSeq(maps.Keys(fileContents.CredHelpers))
				for key := range fileContents.AuthConfigs {
					key := normalizeAuthFileKey(key, path.legacyFormat)
					if key == normalizedDockerIORegistry {
						key = "docker.io"
					}
					allKeys.Add(key)
				}
			}
		// External helpers.
		default:
			creds, err := listCredsInCredHelper(helper)
			if err != nil {
				logrus.Debugf("Error listing credentials stored in credential helper %s: %v", helper, err)
				if errors.Is(err, exec.ErrNotFound) {
					creds = nil // It's okay if the helper doesn't exist.
				} else {
					return nil, err
				}
			}
			allKeys.AddSeq(maps.Keys(creds))
		}
	}

	// Now use `GetCredentials` to the specific auth configs for each
	// previously listed registry.
	allCreds := make(map[string]types.DockerAuthConfig)
	for key := range allKeys.All() {
		creds, err := GetCredentials(sys, key)
		if err != nil {
			// Note: we rely on the logging in `GetCredentials`.
			return nil, err
		}
		if creds != (types.DockerAuthConfig{}) {
			allCreds[key] = creds
		}
	}

	return allCreds, nil
}

// getAuthFilePaths returns a slice of authPaths based on the system context
// in the order they should be searched. Note that some paths may not exist.
// The homeDir parameter should always be homedir.Get(), and is only intended to be overridden
// by tests.
func getAuthFilePaths(sys *types.SystemContext, homeDir string) []authPath {
	paths := []authPath{}
	pathToAuth, userSpecifiedPath, err := getPathToAuth(sys)
	if err == nil {
		paths = append(paths, pathToAuth)
	} else {
		// Error means that the path set for XDG_RUNTIME_DIR does not exist
		// but we don't want to completely fail in the case that the user is pulling a public image
		// Logging the error as a warning instead and moving on to pulling the image
		logrus.Warnf("%v: Trying to pull image in the event that it is a public image.", err)
	}
	if !userSpecifiedPath {
		xdgCfgHome := os.Getenv("XDG_CONFIG_HOME")
		if xdgCfgHome == "" {
			xdgCfgHome = filepath.Join(homeDir, ".config")
		}
		paths = append(paths, newAuthPathDefault(filepath.Join(xdgCfgHome, xdgConfigHomePath)))
		if dockerConfig := os.Getenv("DOCKER_CONFIG"); dockerConfig != "" {
			paths = append(paths, newAuthPathDefault(filepath.Join(dockerConfig, "config.json")))
		} else {
			paths = append(paths,
				newAuthPathDefault(filepath.Join(homeDir, dockerHomePath)),
			)
		}
		paths = append(paths,
			authPath{path: filepath.Join(homeDir, dockerLegacyHomePath), legacyFormat: true},
		)
	}
	return paths
}

// GetCredentials returns the registry credentials matching key, appropriate for
// sys and the users’ configuration.
// If an entry is not found, an empty struct is returned.
// A valid key is a repository, a namespace within a registry, or a registry hostname.
//
// GetCredentialsForRef should almost always be used in favor of this API.
func GetCredentials(sys *types.SystemContext, key string) (types.DockerAuthConfig, error) {
	return getCredentialsWithHomeDir(sys, key, homedir.Get())
}

// GetCredentialsForRef returns the registry credentials necessary for
// accessing ref on the registry ref points to,
// appropriate for sys and the users’ configuration.
// If an entry is not found, an empty struct is returned.
func GetCredentialsForRef(sys *types.SystemContext, ref reference.Named) (types.DockerAuthConfig, error) {
	return getCredentialsWithHomeDir(sys, ref.Name(), homedir.Get())
}

// getCredentialsWithHomeDir is an internal implementation detail of
// GetCredentialsForRef and GetCredentials. It exists only to allow testing it
// with an artificial home directory.
func getCredentialsWithHomeDir(sys *types.SystemContext, key, homeDir string) (types.DockerAuthConfig, error) {
	_, err := validateKey(key)
	if err != nil {
		return types.DockerAuthConfig{}, err
	}

	if sys != nil && sys.DockerAuthConfig != nil {
		logrus.Debugf("Returning credentials for %s from DockerAuthConfig", key)
		return *sys.DockerAuthConfig, nil
	}

	var registry string // We compute this once because it is used in several places.
	if firstSlash := strings.IndexRune(key, '/'); firstSlash != -1 {
		registry = key[:firstSlash]
	} else {
		registry = key
	}

	// Anonymous function to query credentials from auth files.
	getCredentialsFromAuthFiles := func() (types.DockerAuthConfig, string, error) {
		for _, path := range getAuthFilePaths(sys, homeDir) {
			creds, err := findCredentialsInFile(key, registry, path)
			if err != nil {
				return types.DockerAuthConfig{}, "", err
			}

			if creds != (types.DockerAuthConfig{}) {
				return creds, path.path, nil
			}
		}
		return types.DockerAuthConfig{}, "", nil
	}

	helpers, err := sysregistriesv2.CredentialHelpers(sys)
	if err != nil {
		return types.DockerAuthConfig{}, err
	}

	var multiErr []error
	for _, helper := range helpers {
		var (
			creds          types.DockerAuthConfig
			helperKey      string
			credHelperPath string
			err            error
		)
		switch helper {
		// Special-case the built-in helper for auth files.
		case sysregistriesv2.AuthenticationFileHelper:
			helperKey = key
			creds, credHelperPath, err = getCredentialsFromAuthFiles()
		// External helpers.
		default:
			// This intentionally uses "registry", not "key"; we don't support namespaced
			// credentials in helpers, but a "registry" is a valid parent of "key".
			helperKey = registry
			creds, err = getCredsFromCredHelper(helper, registry)
		}
		if err != nil {
			logrus.Debugf("Error looking up credentials for %s in credential helper %s: %v", helperKey, helper, err)
			multiErr = append(multiErr, err)
			continue
		}
		if creds != (types.DockerAuthConfig{}) {
			msg := fmt.Sprintf("Found credentials for %s in credential helper %s", helperKey, helper)
			if credHelperPath != "" {
				msg = fmt.Sprintf("%s in file %s", msg, credHelperPath)
			}
			logrus.Debug(msg)
			return creds, nil
		}
	}
	if multiErr != nil {
		return types.DockerAuthConfig{}, multierr.Format("errors looking up credentials:\n\t* ", "\nt* ", "\n", multiErr)
	}

	logrus.Debugf("No credentials for %s found", key)
	return types.DockerAuthConfig{}, nil
}

// GetAuthentication returns the registry credentials matching key, appropriate for
// sys and the users’ configuration.
// If an entry is not found, an empty struct is returned.
// A valid key is a repository, a namespace within a registry, or a registry hostname.
//
// Deprecated: This API only has support for username and password. To get the
// support for oauth2 in container registry authentication, we added the new
// GetCredentialsForRef and GetCredentials API. The new API should be used and this API is kept to
// maintain backward compatibility.
func GetAuthentication(sys *types.SystemContext, key string) (string, string, error) {
	return getAuthenticationWithHomeDir(sys, key, homedir.Get())
}

// getAuthenticationWithHomeDir is an internal implementation detail of GetAuthentication,
// it exists only to allow testing it with an artificial home directory.
func getAuthenticationWithHomeDir(sys *types.SystemContext, key, homeDir string) (string, string, error) {
	creds, err := getCredentialsWithHomeDir(sys, key, homeDir)
	if err != nil {
		return "", "", err
	}
	if creds.IdentityToken != "" {
		return "", "", fmt.Errorf("non-empty identity token found and this API doesn't support it: %w", ErrNotSupported)
	}
	return creds.Username, creds.Password, nil
}

// SetCredentials stores the username and password in a location
// appropriate for sys and the users’ configuration.
// A valid key is a repository, a namespace within a registry, or a registry hostname;
// using forms other than just a registry may fail depending on configuration.
// Returns a human-readable description of the location that was updated.
// NOTE: The return value is only intended to be read by humans; its form is not an API,
// it may change (or new forms can be added) any time.
func SetCredentials(sys *types.SystemContext, key, username, password string) (string, error) {
	helpers, jsonEditor, key, isNamespaced, err := prepareForEdit(sys, key, true)
	if err != nil {
		return "", err
	}

	// Make sure to collect all errors.
	var multiErr []error
	for _, helper := range helpers {
		var desc string
		var err error
		switch helper {
		// Special-case the built-in helpers for auth files.
		case sysregistriesv2.AuthenticationFileHelper:
			desc, err = jsonEditor(sys, func(fileContents *dockerConfigFile) (bool, string, error) {
				if ch, exists := fileContents.CredHelpers[key]; exists {
					if isNamespaced {
						return false, "", unsupportedNamespaceErr(ch)
					}
					desc, err := setCredsInCredHelper(ch, key, username, password)
					if err != nil {
						return false, "", err
					}
					return false, desc, nil
				}
				creds := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
				newCreds := dockerAuthConfig{Auth: creds}
				fileContents.AuthConfigs[key] = newCreds
				return true, "", nil
			})
		// External helpers.
		default:
			if isNamespaced {
				err = unsupportedNamespaceErr(helper)
			} else {
				desc, err = setCredsInCredHelper(helper, key, username, password)
			}
		}
		if err != nil {
			multiErr = append(multiErr, err)
			logrus.Debugf("Error storing credentials for %s in credential helper %s: %v", key, helper, err)
			continue
		}
		logrus.Debugf("Stored credentials for %s in credential helper %s", key, helper)
		return desc, nil
	}
	return "", multierr.Format("Errors storing credentials\n\t* ", "\n\t* ", "\n", multiErr)
}

func unsupportedNamespaceErr(helper string) error {
	return fmt.Errorf("namespaced key is not supported for credential helper %s", helper)
}

// SetAuthentication stores the username and password in the credential helper or file
// See the documentation of SetCredentials for format of "key"
func SetAuthentication(sys *types.SystemContext, key, username, password string) error {
	_, err := SetCredentials(sys, key, username, password)
	return err
}

// RemoveAuthentication removes credentials for `key` from all possible
// sources such as credential helpers and auth files.
// A valid key is a repository, a namespace within a registry, or a registry hostname;
// using forms other than just a registry may fail depending on configuration.
func RemoveAuthentication(sys *types.SystemContext, key string) error {
	helpers, jsonEditor, key, isNamespaced, err := prepareForEdit(sys, key, true)
	if err != nil {
		return err
	}

	isLoggedIn := false

	removeFromCredHelper := func(helper string) error {
		if isNamespaced {
			logrus.Debugf("Not removing credentials because namespaced keys are not supported for the credential helper: %s", helper)
			return nil
		}
		err := deleteCredsFromCredHelper(helper, key)
		if err == nil {
			logrus.Debugf("Credentials for %q were deleted from credential helper %s", key, helper)
			isLoggedIn = true
			return nil
		}
		if credentials.IsErrCredentialsNotFoundMessage(err.Error()) {
			logrus.Debugf("Not logged in to %s with credential helper %s", key, helper)
			return nil
		}
		return fmt.Errorf("removing credentials for %s from credential helper %s: %w", key, helper, err)
	}

	var multiErr []error
	for _, helper := range helpers {
		var err error
		switch helper {
		// Special-case the built-in helper for auth files.
		case sysregistriesv2.AuthenticationFileHelper:
			_, err = jsonEditor(sys, func(fileContents *dockerConfigFile) (bool, string, error) {
				var helperErr error
				if innerHelper, exists := fileContents.CredHelpers[key]; exists {
					helperErr = removeFromCredHelper(innerHelper)
				}
				if _, ok := fileContents.AuthConfigs[key]; ok {
					isLoggedIn = true
					delete(fileContents.AuthConfigs, key)
				}
				return true, "", helperErr
			})
			if err != nil {
				multiErr = append(multiErr, err)
			}
		// External helpers.
		default:
			if err := removeFromCredHelper(helper); err != nil {
				multiErr = append(multiErr, err)
			}
		}
	}

	if multiErr != nil {
		return multierr.Format("errors removing credentials\n\t* ", "\n\t*", "\n", multiErr)
	}
	if !isLoggedIn {
		return ErrNotLoggedIn
	}

	return nil
}

// RemoveAllAuthentication deletes all the credentials stored in credential
// helpers and auth files.
func RemoveAllAuthentication(sys *types.SystemContext) error {
	helpers, jsonEditor, _, _, err := prepareForEdit(sys, "", false)
	if err != nil {
		return err
	}

	var multiErr []error
	for _, helper := range helpers {
		var err error
		switch helper {
		// Special-case the built-in helper for auth files.
		case sysregistriesv2.AuthenticationFileHelper:
			_, err = jsonEditor(sys, func(fileContents *dockerConfigFile) (bool, string, error) {
				for registry, helper := range fileContents.CredHelpers {
					// Helpers in auth files are expected
					// to exist, so no special treatment
					// for them.
					if err := deleteCredsFromCredHelper(helper, registry); err != nil {
						return false, "", err
					}
				}
				fileContents.CredHelpers = make(map[string]string)
				fileContents.AuthConfigs = make(map[string]dockerAuthConfig)
				return true, "", nil
			})
		// External helpers.
		default:
			var creds map[string]string
			creds, err = listCredsInCredHelper(helper)
			if err != nil {
				if errors.Is(err, exec.ErrNotFound) {
					// It's okay if the helper doesn't exist.
					continue
				} else {
					break
				}
			}
			for registry := range creds {
				err = deleteCredsFromCredHelper(helper, registry)
				if err != nil {
					break
				}
			}
		}
		if err != nil {
			logrus.Debugf("Error removing credentials from credential helper %s: %v", helper, err)
			multiErr = append(multiErr, err)
			continue
		}
		logrus.Debugf("All credentials removed from credential helper %s", helper)
	}

	if multiErr != nil {
		return multierr.Format("errors removing all credentials:\n\t* ", "\n\t* ", "\n", multiErr)
	}
	return nil
}

// prepareForEdit processes sys and key (if keyRelevant) to return:
// - a list of credential helpers
// - a function which can be used to edit the JSON file
// - the key value to actually use in credential helpers / JSON
// - a boolean which is true if key is namespaced (and should not be used with credential helpers).
func prepareForEdit(sys *types.SystemContext, key string, keyRelevant bool) ([]string, func(*types.SystemContext, func(*dockerConfigFile) (bool, string, error)) (string, error), string, bool, error) {
	var isNamespaced bool
	if keyRelevant {
		ns, err := validateKey(key)
		if err != nil {
			return nil, nil, "", false, err
		}
		isNamespaced = ns
	}

	if sys != nil && sys.DockerCompatAuthFilePath != "" {
		if sys.AuthFilePath != "" {
			return nil, nil, "", false, errors.New("AuthFilePath and DockerCompatAuthFilePath can not be set simultaneously")
		}
		if keyRelevant {
			if isNamespaced {
				return nil, nil, "", false, fmt.Errorf("Credentials cannot be recorded in Docker-compatible format with namespaced key %q", key)
			}
			if key == "docker.io" {
				key = "https://index.docker.io/v1/"
			}
		}

		// Do not use helpers defined in sysregistriesv2 because Docker isn’t aware of them.
		return []string{sysregistriesv2.AuthenticationFileHelper}, modifyDockerConfigJSON, key, false, nil
	}

	helpers, err := sysregistriesv2.CredentialHelpers(sys)
	if err != nil {
		return nil, nil, "", false, err
	}

	return helpers, modifyJSON, key, isNamespaced, nil
}

func listCredsInCredHelper(credHelper string) (map[string]string, error) {
	helperName := fmt.Sprintf("docker-credential-%s", credHelper)
	p := helperclient.NewShellProgramFunc(helperName)
	return helperclient.List(p)
}

// getPathToAuth gets the path of the auth.json file used for reading and writing credentials,
// and a boolean indicating whether the return value came from an explicit user choice (i.e. not defaults)
func getPathToAuth(sys *types.SystemContext) (authPath, bool, error) {
	return getPathToAuthWithOS(sys, runtime.GOOS)
}

// getPathToAuthWithOS is an internal implementation detail of getPathToAuth,
// it exists only to allow testing it with an artificial runtime.GOOS.
func getPathToAuthWithOS(sys *types.SystemContext, goOS string) (authPath, bool, error) {
	if sys != nil {
		if sys.AuthFilePath != "" && sys.DockerCompatAuthFilePath != "" {
			return authPath{}, false, errors.New("AuthFilePath and DockerCompatAuthFilePath can not be set simultaneously")
		}
		if sys.AuthFilePath != "" {
			return newAuthPathDefault(sys.AuthFilePath), true, nil
		}
		// When reading, we can process auth.json and Docker’s config.json with the same code.
		// When writing, prepareForEdit chooses an appropriate jsonEditor implementation.
		if sys.DockerCompatAuthFilePath != "" {
			return newAuthPathDefault(sys.DockerCompatAuthFilePath), true, nil
		}
		if sys.LegacyFormatAuthFilePath != "" {
			return authPath{path: sys.LegacyFormatAuthFilePath, legacyFormat: true}, true, nil
		}
		// Note: RootForImplicitAbsolutePaths should not affect paths starting with $HOME
		if sys.RootForImplicitAbsolutePaths != "" && goOS == "linux" {
			return newAuthPathDefault(filepath.Join(sys.RootForImplicitAbsolutePaths, fmt.Sprintf(defaultPerUIDPathFormat, os.Getuid()))), false, nil
		}
	}
	if goOS != "linux" {
		return newAuthPathDefault(filepath.Join(homedir.Get(), nonLinuxAuthFilePath)), false, nil
	}

	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir != "" {
		// This function does not in general need to separately check that the returned path exists; that’s racy, and callers will fail accessing the file anyway.
		// We are checking for fs.ErrNotExist here only to give the user better guidance what to do in this special case.
		err := fileutils.Exists(runtimeDir)
		if errors.Is(err, fs.ErrNotExist) {
			// This means the user set the XDG_RUNTIME_DIR variable and either forgot to create the directory
			// or made a typo while setting the environment variable,
			// so return an error referring to $XDG_RUNTIME_DIR instead of xdgRuntimeDirPath inside.
			return authPath{}, false, fmt.Errorf("%q directory set by $XDG_RUNTIME_DIR does not exist. Either create the directory or unset $XDG_RUNTIME_DIR.: %w", runtimeDir, err)
		} // else ignore err and let the caller fail accessing xdgRuntimeDirPath.
		return newAuthPathDefault(filepath.Join(runtimeDir, xdgRuntimeDirPath)), false, nil
	}
	return newAuthPathDefault(fmt.Sprintf(defaultPerUIDPathFormat, os.Getuid())), false, nil
}

// parse unmarshals the credentials stored in the auth.json file and returns it
// or returns an empty dockerConfigFile data structure if auth.json does not exist
// if the file exists and is empty, this function returns an error.
func (path authPath) parse() (dockerConfigFile, error) {
	var fileContents dockerConfigFile

	raw, err := os.ReadFile(path.path)
	if err != nil {
		if os.IsNotExist(err) {
			fileContents.AuthConfigs = map[string]dockerAuthConfig{}
			return fileContents, nil
		}
		return dockerConfigFile{}, err
	}

	if path.legacyFormat {
		if err = json.Unmarshal(raw, &fileContents.AuthConfigs); err != nil {
			return dockerConfigFile{}, fmt.Errorf("unmarshaling JSON at %q: %w", path.path, err)
		}
		return fileContents, nil
	}

	if err = json.Unmarshal(raw, &fileContents); err != nil {
		return dockerConfigFile{}, fmt.Errorf("unmarshaling JSON at %q: %w", path.path, err)
	}

	if fileContents.AuthConfigs == nil {
		fileContents.AuthConfigs = map[string]dockerAuthConfig{}
	}
	if fileContents.CredHelpers == nil {
		fileContents.CredHelpers = make(map[string]string)
	}

	return fileContents, nil
}

// modifyJSON finds an auth.json file, calls editor on the contents, and
// writes it back if editor returns true.
// Returns a human-readable description of the file, to be returned by SetCredentials.
//
// The editor may also return a human-readable description of the updated location; if it is "",
// the file itself is used.
func modifyJSON(sys *types.SystemContext, editor func(fileContents *dockerConfigFile) (bool, string, error)) (string, error) {
	path, _, err := getPathToAuth(sys)
	if err != nil {
		return "", err
	}
	if path.legacyFormat {
		return "", fmt.Errorf("writes to %s using legacy format are not supported", path.path)
	}

	dir := filepath.Dir(path.path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}

	fileContents, err := path.parse()
	if err != nil {
		return "", fmt.Errorf("reading JSON file %q: %w", path.path, err)
	}

	updated, description, err := editor(&fileContents)
	if err != nil {
		return "", fmt.Errorf("updating %q: %w", path.path, err)
	}
	if updated {
		newData, err := json.MarshalIndent(fileContents, "", "\t")
		if err != nil {
			return "", fmt.Errorf("marshaling JSON %q: %w", path.path, err)
		}

		if err = ioutils.AtomicWriteFile(path.path, newData, 0600); err != nil {
			return "", fmt.Errorf("writing to file %q: %w", path.path, err)
		}
	}

	if description == "" {
		description = path.path
	}
	return description, nil
}

// modifyDockerConfigJSON finds a docker config.json file, calls editor on the contents, and
// writes it back if editor returns true.
// Returns a human-readable description of the file, to be returned by SetCredentials.
//
// The editor may also return a human-readable description of the updated location; if it is "",
// the file itself is used.
func modifyDockerConfigJSON(sys *types.SystemContext, editor func(fileContents *dockerConfigFile) (bool, string, error)) (string, error) {
	if sys == nil || sys.DockerCompatAuthFilePath == "" {
		return "", errors.New("internal error: modifyDockerConfigJSON called with DockerCompatAuthFilePath not set")
	}
	path := sys.DockerCompatAuthFilePath

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}

	// Try hard not to clobber fields we don’t understand, even fields which may be added in future Docker versions.
	var rawContents map[string]json.RawMessage
	originalBytes, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(originalBytes, &rawContents); err != nil {
			return "", fmt.Errorf("unmarshaling JSON at %q: %w", path, err)
		}
	case errors.Is(err, fs.ErrNotExist):
		rawContents = map[string]json.RawMessage{}
	default: // err != nil
		return "", err
	}

	syntheticContents := dockerConfigFile{
		AuthConfigs: map[string]dockerAuthConfig{},
		CredHelpers: map[string]string{},
	}
	// json.Unmarshal also falls back to case-insensitive field matching; this code does not do that. Presumably
	// config.json is mostly maintained by machines doing `docker login`, so the files should, hopefully, not contain field names with
	// unexpected case.
	if rawAuths, ok := rawContents["auths"]; ok {
		// This conversion will lose fields we don’t know about; when updating an entry, we can’t tell whether an unknown field
		// should be preserved or discarded (because it is made obsolete/unwanted with the new credentials).
		// It might make sense to track which entries of "auths" we actually modified, and to not touch any others.
		if err := json.Unmarshal(rawAuths, &syntheticContents.AuthConfigs); err != nil {
			return "", fmt.Errorf(`unmarshaling "auths" in JSON at %q: %w`, path, err)
		}
	}
	if rawCH, ok := rawContents["credHelpers"]; ok {
		if err := json.Unmarshal(rawCH, &syntheticContents.CredHelpers); err != nil {
			return "", fmt.Errorf(`unmarshaling "credHelpers" in JSON at %q: %w`, path, err)

		}
	}

	updated, description, err := editor(&syntheticContents)
	if err != nil {
		return "", fmt.Errorf("updating %q: %w", path, err)
	}
	if updated {
		rawAuths, err := json.MarshalIndent(syntheticContents.AuthConfigs, "", "\t")
		if err != nil {
			return "", fmt.Errorf("marshaling JSON %q: %w", path, err)
		}
		rawContents["auths"] = rawAuths
		// We never modify syntheticContents.CredHelpers, so we don’t need to update it.
		newData, err := json.MarshalIndent(rawContents, "", "\t")
		if err != nil {
			return "", fmt.Errorf("marshaling JSON %q: %w", path, err)
		}

		if err = ioutils.AtomicWriteFile(path, newData, 0600); err != nil {
			return "", fmt.Errorf("writing to file %q: %w", path, err)
		}
	}

	if description == "" {
		description = path
	}
	return description, nil
}

func getCredsFromCredHelper(credHelper, registry string) (types.DockerAuthConfig, error) {
	helperName := fmt.Sprintf("docker-credential-%s", credHelper)
	p := helperclient.NewShellProgramFunc(helperName)
	creds, err := helperclient.Get(p, registry)
	if err != nil {
		if credentials.IsErrCredentialsNotFoundMessage(err.Error()) {
			logrus.Debugf("Not logged in to %s with credential helper %s", registry, credHelper)
			err = nil
		}
		return types.DockerAuthConfig{}, err
	}

	switch creds.Username {
	case "<token>":
		return types.DockerAuthConfig{
			IdentityToken: creds.Secret,
		}, nil
	default:
		return types.DockerAuthConfig{
			Username: creds.Username,
			Password: creds.Secret,
		}, nil
	}
}

// setCredsInCredHelper stores (username, password) for registry in credHelper.
// Returns a human-readable description of the destination, to be returned by SetCredentials.
func setCredsInCredHelper(credHelper, registry, username, password string) (string, error) {
	helperName := fmt.Sprintf("docker-credential-%s", credHelper)
	p := helperclient.NewShellProgramFunc(helperName)
	creds := &credentials.Credentials{
		ServerURL: registry,
		Username:  username,
		Secret:    password,
	}
	if err := helperclient.Store(p, creds); err != nil {
		return "", err
	}
	return fmt.Sprintf("credential helper: %s", credHelper), nil
}

func deleteCredsFromCredHelper(credHelper, registry string) error {
	helperName := fmt.Sprintf("docker-credential-%s", credHelper)
	p := helperclient.NewShellProgramFunc(helperName)
	return helperclient.Erase(p, registry)
}

// findCredentialsInFile looks for credentials matching "key"
// (which is "registry" or a namespace in "registry") in "path".
func findCredentialsInFile(key, registry string, path authPath) (types.DockerAuthConfig, error) {
	fileContents, err := path.parse()
	if err != nil {
		return types.DockerAuthConfig{}, fmt.Errorf("reading JSON file %q: %w", path.path, err)
	}

	// First try cred helpers. They should always be normalized.
	// This intentionally uses "registry", not "key"; we don't support namespaced
	// credentials in helpers.
	if ch, exists := fileContents.CredHelpers[registry]; exists {
		logrus.Debugf("Looking up in credential helper %s based on credHelpers entry in %s", ch, path.path)
		return getCredsFromCredHelper(ch, registry)
	}

	// Support sub-registry namespaces in auth.
	// (This is not a feature of ~/.docker/config.json; we support it even for
	// those files as an extension.)
	//
	// Repo or namespace keys are only supported as exact matches. For registry
	// keys we prefer exact matches as well.
	for key := range authKeyLookupOrder(key, registry, path.legacyFormat) {
		if val, exists := fileContents.AuthConfigs[key]; exists {
			return decodeDockerAuth(path.path, key, val)
		}
	}

	// bad luck; let's normalize the entries first
	// This primarily happens for legacyFormat, which for a time used API URLs
	// (http[s:]//…/v1/) as keys.
	// Secondarily, (docker login) accepted URLs with no normalization for
	// several years, and matched registry hostnames against that, so support
	// those entries even in non-legacyFormat ~/.docker/config.json.
	// The docker.io registry still uses the /v1/ key with a special host name,
	// so account for that as well.
	registry = normalizeRegistry(registry)
	for k, v := range fileContents.AuthConfigs {
		if normalizeAuthFileKey(k, path.legacyFormat) == registry {
			return decodeDockerAuth(path.path, k, v)
		}
	}

	// Only log this if we found nothing; getCredentialsWithHomeDir logs the
	// source of found data.
	logrus.Debugf("No credentials matching %s found in %s", key, path.path)
	return types.DockerAuthConfig{}, nil
}

// authKeyLookupOrder returns a sequence for lookup keys matching (key or registry)
// in file with legacyFormat, in order from the best match to worst.
// For example, in a non-legacy file,
// when given a repository key "quay.io/repo/ns/image", it returns
// - quay.io/repo/ns/image
// - quay.io/repo/ns
// - quay.io/repo
// - quay.io
func authKeyLookupOrder(key, registry string, legacyFormat bool) iter.Seq[string] {
	return func(yield func(string) bool) {
		if legacyFormat {
			_ = yield(registry) // We stop in any case
			return
		}

		for {
			if !yield(key) {
				return
			}

			lastSlash := strings.LastIndex(key, "/")
			if lastSlash == -1 {
				break
			}
			key = key[:lastSlash]
		}
	}
}

// decodeDockerAuth decodes the username and password from conf,
// which is entry key in path.
func decodeDockerAuth(path, key string, conf dockerAuthConfig) (types.DockerAuthConfig, error) {
	decoded, err := base64.StdEncoding.DecodeString(conf.Auth)
	if err != nil {
		return types.DockerAuthConfig{}, err
	}

	user, passwordPart, valid := strings.Cut(string(decoded), ":")
	if !valid {
		// if it's invalid just skip, as docker does
		if len(decoded) > 0 { // Docker writes "auths": { "$host": {} } entries if a credential helper is used, don’t warn about those
			logrus.Warnf(`Error parsing the "auth" field of a credential entry %q in %q, missing semicolon`, key, path) // Don’t include the text of decoded, because that might put secrets into a log.
		} else {
			logrus.Debugf("Found an empty credential entry %q in %q (an unhandled credential helper marker?), moving on", key, path)
		}
		return types.DockerAuthConfig{}, nil
	}

	password := strings.Trim(passwordPart, "\x00")
	return types.DockerAuthConfig{
		Username:      user,
		Password:      password,
		IdentityToken: conf.IdentityToken,
	}, nil
}

// normalizeAuthFileKey takes a key, converts it to a host name and normalizes
// the resulting registry.
func normalizeAuthFileKey(key string, legacyFormat bool) string {
	stripped := strings.TrimPrefix(key, "http://")
	stripped = strings.TrimPrefix(stripped, "https://")

	if legacyFormat || stripped != key {
		stripped, _, _ = strings.Cut(stripped, "/")
	}

	return normalizeRegistry(stripped)
}

// normalizeRegistry converts the provided registry if a known docker.io host
// is provided.
func normalizeRegistry(registry string) string {
	switch registry {
	case "registry-1.docker.io", "docker.io":
		return "index.docker.io"
	}
	return registry
}

// validateKey verifies that the input key does not have a prefix that is not
// allowed and returns an indicator if the key is namespaced.
func validateKey(key string) (bool, error) {
	if strings.HasPrefix(key, "http://") || strings.HasPrefix(key, "https://") {
		return false, fmt.Errorf("key %s contains http[s]:// prefix", key)
	}

	// Ideally this should only accept explicitly valid keys, compare
	// validateIdentityRemappingPrefix. For now, just reject values that look
	// like tagged or digested values.
	if strings.ContainsRune(key, '@') {
		return false, fmt.Errorf(`key %s contains a '@' character`, key)
	}

	firstSlash := strings.IndexRune(key, '/')
	isNamespaced := firstSlash != -1
	// Reject host/repo:tag, but allow localhost:5000 and localhost:5000/foo.
	if isNamespaced && strings.ContainsRune(key[firstSlash+1:], ':') {
		return false, fmt.Errorf(`key %s contains a ':' character after host[:port]`, key)
	}
	// check if the provided key contains one or more subpaths.
	return isNamespaced, nil
}
