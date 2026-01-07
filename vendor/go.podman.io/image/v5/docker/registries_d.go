package docker

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/rootless"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/homedir"
	"gopkg.in/yaml.v3"
)

// systemRegistriesDirPath is the path to registries.d, used for locating lookaside Docker signature storage.
// You can override this at build time with
// -ldflags '-X go.podman.io/image/v5/docker.systemRegistriesDirPath=$your_path'
var systemRegistriesDirPath = builtinRegistriesDirPath

// builtinRegistriesDirPath is the path to registries.d.
// DO NOT change this, instead see systemRegistriesDirPath above.
const builtinRegistriesDirPath = etcDir + "/containers/registries.d"

// userRegistriesDirPath is the path to the per user registries.d.
var userRegistriesDir = filepath.FromSlash(".config/containers/registries.d")

// defaultUserDockerDir is the default lookaside directory for unprivileged user
var defaultUserDockerDir = filepath.FromSlash(".local/share/containers/sigstore")

// defaultDockerDir is the default lookaside directory for root
var defaultDockerDir = "/var/lib/containers/sigstore"

// registryConfiguration is one of the files in registriesDirPath configuring lookaside locations, or the result of merging them all.
// NOTE: Keep this in sync with docs/registries.d.md!
type registryConfiguration struct {
	DefaultDocker *registryNamespace `yaml:"default-docker"`
	// The key is a namespace, using fully-expanded Docker reference format or parent namespaces (per dockerReference.PolicyConfiguration*),
	Docker map[string]registryNamespace `yaml:"docker"`
}

// registryNamespace defines lookaside locations for a single namespace.
type registryNamespace struct {
	Lookaside              string `yaml:"lookaside"`         // For reading, and if LookasideStaging is not present, for writing.
	LookasideStaging       string `yaml:"lookaside-staging"` // For writing only.
	SigStore               string `yaml:"sigstore"`          // For compatibility, deprecated in favor of Lookaside.
	SigStoreStaging        string `yaml:"sigstore-staging"`  // For compatibility, deprecated in favor of LookasideStaging.
	UseSigstoreAttachments *bool  `yaml:"use-sigstore-attachments,omitempty"`
}

// lookasideStorageBase is an "opaque" type representing a lookaside Docker signature storage.
// Users outside of this file should use SignatureStorageBaseURL and lookasideStorageURL below.
type lookasideStorageBase *url.URL

// SignatureStorageBaseURL reads configuration to find an appropriate lookaside storage URL for ref, for write access if “write”.
// the usage of the BaseURL is defined under docker/distribution registries—separate storage of docs/signature-protocols.md
// Warning: This function only exposes configuration in registries.d;
// just because this function returns an URL does not mean that the URL will be used by c/image/docker (e.g. if the registry natively supports X-R-S-S).
func SignatureStorageBaseURL(sys *types.SystemContext, ref types.ImageReference, write bool) (*url.URL, error) {
	dr, ok := ref.(dockerReference)
	if !ok {
		return nil, errors.New("ref must be a dockerReference")
	}
	config, err := loadRegistryConfiguration(sys)
	if err != nil {
		return nil, err
	}

	return config.lookasideStorageBaseURL(dr, write)
}

// loadRegistryConfiguration returns a registryConfiguration appropriate for sys.
func loadRegistryConfiguration(sys *types.SystemContext) (*registryConfiguration, error) {
	dirPath := registriesDirPath(sys)
	logrus.Debugf(`Using registries.d directory %s`, dirPath)
	return loadAndMergeConfig(dirPath)
}

// registriesDirPath returns a path to registries.d
func registriesDirPath(sys *types.SystemContext) string {
	return registriesDirPathWithHomeDir(sys, homedir.Get())
}

// registriesDirPathWithHomeDir is an internal implementation detail of registriesDirPath,
// it exists only to allow testing it with an artificial home directory.
func registriesDirPathWithHomeDir(sys *types.SystemContext, homeDir string) string {
	if sys != nil && sys.RegistriesDirPath != "" {
		return sys.RegistriesDirPath
	}
	userRegistriesDirPath := filepath.Join(homeDir, userRegistriesDir)
	if err := fileutils.Exists(userRegistriesDirPath); err == nil {
		return userRegistriesDirPath
	}
	if sys != nil && sys.RootForImplicitAbsolutePaths != "" {
		return filepath.Join(sys.RootForImplicitAbsolutePaths, systemRegistriesDirPath)
	}

	return systemRegistriesDirPath
}

// loadAndMergeConfig loads configuration files in dirPath
// FIXME: Probably rename to loadRegistryConfigurationForPath
func loadAndMergeConfig(dirPath string) (*registryConfiguration, error) {
	mergedConfig := registryConfiguration{Docker: map[string]registryNamespace{}}
	dockerDefaultMergedFrom := ""
	nsMergedFrom := map[string]string{}

	dir, err := os.Open(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &mergedConfig, nil
		}
		return nil, err
	}
	configNames, err := dir.Readdirnames(0)
	if err != nil {
		return nil, err
	}
	for _, configName := range configNames {
		if !strings.HasSuffix(configName, ".yaml") {
			continue
		}
		configPath := filepath.Join(dirPath, configName)
		configBytes, err := os.ReadFile(configPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// file must have been removed between the directory listing
				// and the open call, ignore that as it is a expected race
				continue
			}
			return nil, err
		}

		var config registryConfiguration
		err = yaml.Unmarshal(configBytes, &config)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", configPath, err)
		}

		if config.DefaultDocker != nil {
			if mergedConfig.DefaultDocker != nil {
				return nil, fmt.Errorf(`Error parsing signature storage configuration: "default-docker" defined both in %q and %q`,
					dockerDefaultMergedFrom, configPath)
			}
			mergedConfig.DefaultDocker = config.DefaultDocker
			dockerDefaultMergedFrom = configPath
		}

		for nsName, nsConfig := range config.Docker { // includes config.Docker == nil
			if _, ok := mergedConfig.Docker[nsName]; ok {
				return nil, fmt.Errorf(`Error parsing signature storage configuration: "docker" namespace %q defined both in %q and %q`,
					nsName, nsMergedFrom[nsName], configPath)
			}
			mergedConfig.Docker[nsName] = nsConfig
			nsMergedFrom[nsName] = configPath
		}
	}

	return &mergedConfig, nil
}

// lookasideStorageBaseURL returns an appropriate signature storage URL for ref, for write access if “write”.
// the usage of the BaseURL is defined under docker/distribution registries—separate storage of docs/signature-protocols.md
func (config *registryConfiguration) lookasideStorageBaseURL(dr dockerReference, write bool) (*url.URL, error) {
	topLevel := config.signatureTopLevel(dr, write)
	var baseURL *url.URL
	if topLevel != "" {
		u, err := url.Parse(topLevel)
		if err != nil {
			return nil, fmt.Errorf("Invalid signature storage URL %s: %w", topLevel, err)
		}
		baseURL = u
	} else {
		// returns default directory if no lookaside specified in configuration file
		baseURL = builtinDefaultLookasideStorageDir(rootless.GetRootlessEUID())
		logrus.Debugf(" No signature storage configuration found for %s, using built-in default %s", dr.PolicyConfigurationIdentity(), baseURL.Redacted())
	}
	// NOTE: Keep this in sync with docs/signature-protocols.md!
	// FIXME? Restrict to explicitly supported schemes?
	repo := reference.Path(dr.ref) // Note that this is without a tag or digest.
	if path.Clean(repo) != repo {  // Coverage: This should not be reachable because /./ and /../ components are not valid in docker references
		return nil, fmt.Errorf("Unexpected path elements in Docker reference %s for signature storage", dr.ref.String())
	}
	baseURL.Path = baseURL.Path + "/" + repo
	return baseURL, nil
}

// builtinDefaultLookasideStorageDir returns default signature storage URL as per euid
func builtinDefaultLookasideStorageDir(euid int) *url.URL {
	if euid != 0 {
		return &url.URL{Scheme: "file", Path: filepath.Join(homedir.Get(), defaultUserDockerDir)}
	}
	return &url.URL{Scheme: "file", Path: defaultDockerDir}
}

// config.signatureTopLevel returns an URL string configured in config for ref, for write access if “write”.
// (the top level of the storage, namespaced by repo.FullName etc.), or "" if nothing has been configured.
func (config *registryConfiguration) signatureTopLevel(ref dockerReference, write bool) string {
	if config.Docker != nil {
		// Look for a full match.
		identity := ref.PolicyConfigurationIdentity()
		if ns, ok := config.Docker[identity]; ok {
			logrus.Debugf(` Lookaside configuration: using "docker" namespace %s`, identity)
			if ret := ns.signatureTopLevel(write); ret != "" {
				return ret
			}
		}

		// Look for a match of the possible parent namespaces.
		for _, name := range ref.PolicyConfigurationNamespaces() {
			if ns, ok := config.Docker[name]; ok {
				logrus.Debugf(` Lookaside configuration: using "docker" namespace %s`, name)
				if ret := ns.signatureTopLevel(write); ret != "" {
					return ret
				}
			}
		}
	}
	// Look for a default location
	if config.DefaultDocker != nil {
		logrus.Debugf(` Lookaside configuration: using "default-docker" configuration`)
		if ret := config.DefaultDocker.signatureTopLevel(write); ret != "" {
			return ret
		}
	}
	return ""
}

// config.useSigstoreAttachments returns whether we should look for and write sigstore attachments.
// for ref.
func (config *registryConfiguration) useSigstoreAttachments(ref dockerReference) bool {
	if config.Docker != nil {
		// Look for a full match.
		identity := ref.PolicyConfigurationIdentity()
		if ns, ok := config.Docker[identity]; ok {
			logrus.Debugf(` Sigstore attachments: using "docker" namespace %s`, identity)
			if ns.UseSigstoreAttachments != nil {
				return *ns.UseSigstoreAttachments
			}
		}

		// Look for a match of the possible parent namespaces.
		for _, name := range ref.PolicyConfigurationNamespaces() {
			if ns, ok := config.Docker[name]; ok {
				logrus.Debugf(` Sigstore attachments: using "docker" namespace %s`, name)
				if ns.UseSigstoreAttachments != nil {
					return *ns.UseSigstoreAttachments
				}
			}
		}
	}
	// Look for a default location
	if config.DefaultDocker != nil {
		logrus.Debugf(` Sigstore attachments: using "default-docker" configuration`)
		if config.DefaultDocker.UseSigstoreAttachments != nil {
			return *config.DefaultDocker.UseSigstoreAttachments
		}
	}
	return false
}

// ns.signatureTopLevel returns an URL string configured in ns for ref, for write access if “write”.
// or "" if nothing has been configured.
func (ns registryNamespace) signatureTopLevel(write bool) string {
	if write {
		if ns.LookasideStaging != "" {
			logrus.Debugf(`  Using "lookaside-staging" %s`, ns.LookasideStaging)
			return ns.LookasideStaging
		}
		if ns.SigStoreStaging != "" {
			logrus.Debugf(`  Using "sigstore-staging" %s`, ns.SigStoreStaging)
			return ns.SigStoreStaging
		}
	}
	if ns.Lookaside != "" {
		logrus.Debugf(`  Using "lookaside" %s`, ns.Lookaside)
		return ns.Lookaside
	}
	if ns.SigStore != "" {
		logrus.Debugf(`  Using "sigstore" %s`, ns.SigStore)
		return ns.SigStore
	}
	return ""
}

// lookasideStorageURL returns an URL usable for accessing signature index in base with known manifestDigest.
// base is not nil from the caller
// NOTE: Keep this in sync with docs/signature-protocols.md!
func lookasideStorageURL(base lookasideStorageBase, manifestDigest digest.Digest, index int) (*url.URL, error) {
	if err := manifestDigest.Validate(); err != nil { // digest.Digest.Encoded() panics on failure, and could possibly result in a path with ../, so validate explicitly.
		return nil, err
	}
	sigURL := *base
	sigURL.Path = fmt.Sprintf("%s@%s=%s/signature-%d", sigURL.Path, manifestDigest.Algorithm(), manifestDigest.Encoded(), index+1)
	return &sigURL, nil
}
