package types

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/sirupsen/logrus"
	cfg "go.podman.io/storage/pkg/config"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/homedir"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/unshare"
)

// TOML-friendly explicit tables used for conversions.
type TomlConfig struct {
	Storage struct {
		Driver              string            `toml:"driver,omitempty"`
		DriverPriority      []string          `toml:"driver_priority,omitempty"`
		RunRoot             string            `toml:"runroot,omitempty"`
		ImageStore          string            `toml:"imagestore,omitempty"`
		GraphRoot           string            `toml:"graphroot,omitempty"`
		RootlessStoragePath string            `toml:"rootless_storage_path,omitempty"`
		TransientStore      bool              `toml:"transient_store,omitempty"`
		Options             cfg.OptionsConfig `toml:"options,omitempty"`
	} `toml:"storage"`
}

const (
	overlayDriver  = "overlay"
	overlay2       = "overlay2"
	storageConfEnv = "CONTAINERS_STORAGE_CONF"
)

var (
	defaultStoreOptionsOnce    sync.Once
	loadDefaultStoreOptionsErr error
	once                       sync.Once
	storeOptions               StoreOptions
	storeError                 error
	defaultConfigFileSet       bool
	// defaultConfigFile path to the system wide storage.conf file
	defaultConfigFile = SystemConfigFile
	// DefaultStoreOptions is a reasonable default set of options.
	defaultStoreOptions StoreOptions
)

func loadDefaultStoreOptions() {
	defaultStoreOptions.GraphDriverName = ""

	setDefaults := func() {
		// reload could set values to empty for run and graph root if config does not contains anything
		if defaultStoreOptions.RunRoot == "" {
			defaultStoreOptions.RunRoot = defaultRunRoot
		}
		if defaultStoreOptions.GraphRoot == "" {
			defaultStoreOptions.GraphRoot = defaultGraphRoot
		}
	}
	setDefaults()

	if path, ok := os.LookupEnv(storageConfEnv); ok {
		defaultOverrideConfigFile = path
		if err := ReloadConfigurationFileIfNeeded(path, &defaultStoreOptions); err != nil {
			loadDefaultStoreOptionsErr = err
			return
		}
		setDefaults()
		return
	}

	if path, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok {
		homeConfigFile := filepath.Join(path, "containers", "storage.conf")
		if err := fileutils.Exists(homeConfigFile); err == nil {
			// user storage.conf in XDG_CONFIG_HOME if it exists
			defaultOverrideConfigFile = homeConfigFile
		} else {
			if !os.IsNotExist(err) {
				loadDefaultStoreOptionsErr = err
				return
			}
		}
	}

	err := fileutils.Exists(defaultOverrideConfigFile)
	if err == nil {
		// The DefaultConfigFile() function returns the path
		// of the used storage.conf file, by returning defaultConfigFile
		// If override exists containers/storage uses it by default.
		defaultConfigFile = defaultOverrideConfigFile
		if err := ReloadConfigurationFileIfNeeded(defaultOverrideConfigFile, &defaultStoreOptions); err != nil {
			loadDefaultStoreOptionsErr = err
			return
		}
		setDefaults()
		return
	}

	if !os.IsNotExist(err) {
		logrus.Warningf("Attempting to use %s, %v", defaultConfigFile, err)
	}
	if err := ReloadConfigurationFileIfNeeded(defaultConfigFile, &defaultStoreOptions); err != nil && !errors.Is(err, os.ErrNotExist) {
		loadDefaultStoreOptionsErr = err
		return
	}
	setDefaults()
}

// loadStoreOptions returns the default storage ops for containers
func loadStoreOptions() (StoreOptions, error) {
	storageConf, err := DefaultConfigFile()
	if err != nil {
		return defaultStoreOptions, err
	}
	return loadStoreOptionsFromConfFile(storageConf)
}

// usePerUserStorage returns whether the user private storage must be used.
// We cannot simply use the unshare.IsRootless() condition, because
// that checks only if the current process needs a user namespace to
// work and it would break cases where the process is already created
// in a user namespace (e.g. nested Podman/Buildah) and the desired
// behavior is to use system paths instead of user private paths.
func usePerUserStorage() bool {
	return unshare.IsRootless() && unshare.GetRootlessUID() != 0
}

// loadStoreOptionsFromConfFile is an internal implementation detail of DefaultStoreOptions to allow testing.
// Everyone but the tests this is intended for should only call loadStoreOptions, never this function.
func loadStoreOptionsFromConfFile(storageConf string) (StoreOptions, error) {
	var (
		defaultRootlessRunRoot   string
		defaultRootlessGraphRoot string
		err                      error
	)

	defaultStoreOptionsOnce.Do(loadDefaultStoreOptions)
	if loadDefaultStoreOptionsErr != nil {
		return StoreOptions{}, loadDefaultStoreOptionsErr
	}
	storageOpts := defaultStoreOptions
	if usePerUserStorage() {
		storageOpts, err = getRootlessStorageOpts(storageOpts)
		if err != nil {
			return storageOpts, err
		}
	}
	err = fileutils.Exists(storageConf)
	if err != nil && !os.IsNotExist(err) {
		return storageOpts, err
	}
	if err == nil && !defaultConfigFileSet {
		defaultRootlessRunRoot = storageOpts.RunRoot
		defaultRootlessGraphRoot = storageOpts.GraphRoot
		storageOpts = StoreOptions{}
		reloadConfigurationFileIfNeeded(storageConf, &storageOpts)
		// If the file did not specify a graphroot or runroot,
		// set sane defaults so we don't try and use root-owned
		// directories
		if storageOpts.RunRoot == "" {
			storageOpts.RunRoot = defaultRootlessRunRoot
		}
		if storageOpts.GraphRoot == "" {
			if storageOpts.RootlessStoragePath != "" {
				storageOpts.GraphRoot = storageOpts.RootlessStoragePath
			} else {
				storageOpts.GraphRoot = defaultRootlessGraphRoot
			}
		}
	}
	if storageOpts.RunRoot == "" {
		return storageOpts, fmt.Errorf("runroot must be set")
	}
	rootlessUID := unshare.GetRootlessUID()
	runRoot, err := expandEnvPath(storageOpts.RunRoot, rootlessUID)
	if err != nil {
		return storageOpts, err
	}
	storageOpts.RunRoot = runRoot

	if storageOpts.GraphRoot == "" {
		return storageOpts, fmt.Errorf("graphroot must be set")
	}
	graphRoot, err := expandEnvPath(storageOpts.GraphRoot, rootlessUID)
	if err != nil {
		return storageOpts, err
	}
	storageOpts.GraphRoot = graphRoot

	if storageOpts.RootlessStoragePath != "" {
		storagePath, err := expandEnvPath(storageOpts.RootlessStoragePath, rootlessUID)
		if err != nil {
			return storageOpts, err
		}
		storageOpts.RootlessStoragePath = storagePath
	}

	if storageOpts.ImageStore != "" && storageOpts.ImageStore == storageOpts.GraphRoot {
		return storageOpts, fmt.Errorf("imagestore %s must either be not set or be a different than graphroot", storageOpts.ImageStore)
	}

	return storageOpts, nil
}

// UpdateOptions should be called iff container engine received a SIGHUP,
// otherwise use DefaultStoreOptions
func UpdateStoreOptions() (StoreOptions, error) {
	storeOptions, storeError = loadStoreOptions()
	return storeOptions, storeError
}

// DefaultStoreOptions returns the default storage ops for containers
func DefaultStoreOptions() (StoreOptions, error) {
	once.Do(func() {
		storeOptions, storeError = loadStoreOptions()
	})
	return storeOptions, storeError
}

// StoreOptions is used for passing initialization options to GetStore(), for
// initializing a Store object and the underlying storage that it controls.
type StoreOptions struct {
	// RunRoot is the filesystem path under which we can store run-time
	// information, such as the locations of active mount points, that we
	// want to lose if the host is rebooted.
	RunRoot string `json:"runroot,omitempty"`
	// GraphRoot is the filesystem path under which we will store the
	// contents of layers, images, and containers.
	GraphRoot string `json:"root,omitempty"`
	// Image Store is the alternate location of image store if a location
	// separate from the container store is required.
	ImageStore string `json:"imagestore,omitempty"`
	// RootlessStoragePath is the storage path for rootless users
	// default $HOME/.local/share/containers/storage
	RootlessStoragePath string `toml:"rootless_storage_path"`
	// If the driver is not specified, the best suited driver will be picked
	// either from GraphDriverPriority, if specified, or from the platform
	// dependent priority list (in that order).
	GraphDriverName string `json:"driver,omitempty"`
	// GraphDriverPriority is a list of storage drivers that will be tried
	// to initialize the Store for a given RunRoot and GraphRoot unless a
	// GraphDriverName is set.
	// This list can be used to define a custom order in which the drivers
	// will be tried.
	GraphDriverPriority []string `json:"driver-priority,omitempty"`
	// GraphDriverOptions are driver-specific options.
	GraphDriverOptions []string `json:"driver-options,omitempty"`
	// UIDMap and GIDMap are used for setting up a container's root filesystem
	// for use inside of a user namespace where UID mapping is being used.
	UIDMap []idtools.IDMap `json:"uidmap,omitempty"`
	GIDMap []idtools.IDMap `json:"gidmap,omitempty"`
	// RootAutoNsUser is the user used to pick a subrange when automatically setting
	// a user namespace for the root user.
	RootAutoNsUser string `json:"root_auto_ns_user,omitempty"`
	// AutoNsMinSize is the minimum size for an automatic user namespace.
	AutoNsMinSize uint32 `json:"auto_userns_min_size,omitempty"`
	// AutoNsMaxSize is the maximum size for an automatic user namespace.
	AutoNsMaxSize uint32 `json:"auto_userns_max_size,omitempty"`
	// PullOptions specifies options to be handed to pull managers
	// This API is experimental and can be changed without bumping the major version number.
	PullOptions map[string]string `toml:"pull_options"`
	// DisableVolatile doesn't allow volatile mounts when it is set.
	DisableVolatile bool `json:"disable-volatile,omitempty"`
	// If transient, don't persist containers over boot (stores db in runroot)
	TransientStore bool `json:"transient_store,omitempty"`
}

// isRootlessDriver returns true if the given storage driver is valid for containers running as non root
func isRootlessDriver(driver string) bool {
	validDrivers := map[string]bool{
		"btrfs":    true,
		"overlay":  true,
		"overlay2": true,
		"vfs":      true,
	}
	return validDrivers[driver]
}

// getRootlessStorageOpts returns the storage opts for containers running as non root
func getRootlessStorageOpts(systemOpts StoreOptions) (StoreOptions, error) {
	var opts StoreOptions

	rootlessUID := unshare.GetRootlessUID()

	dataDir, err := homedir.GetDataHome()
	if err != nil {
		return opts, err
	}

	rootlessRuntime, err := homedir.GetRuntimeDir()
	if err != nil {
		return opts, err
	}

	opts.RunRoot = filepath.Join(rootlessRuntime, "containers")
	if err := os.MkdirAll(opts.RunRoot, 0o700); err != nil {
		return opts, fmt.Errorf("unable to make rootless runtime: %w", err)
	}

	opts.PullOptions = systemOpts.PullOptions
	if systemOpts.RootlessStoragePath != "" {
		opts.GraphRoot, err = expandEnvPath(systemOpts.RootlessStoragePath, rootlessUID)
		if err != nil {
			return opts, err
		}
	} else {
		opts.GraphRoot = filepath.Join(dataDir, "containers", "storage")
	}

	if driver := systemOpts.GraphDriverName; isRootlessDriver(driver) {
		opts.GraphDriverName = driver
	}
	if driver := os.Getenv("STORAGE_DRIVER"); driver != "" {
		opts.GraphDriverName = driver
	}
	if opts.GraphDriverName == overlay2 {
		logrus.Warnf("Switching default driver from overlay2 to the equivalent overlay driver")
		opts.GraphDriverName = overlayDriver
	}

	// If the configuration file was explicitly set, then copy all the options
	// present.
	if defaultConfigFileSet {
		opts.GraphDriverOptions = systemOpts.GraphDriverOptions
		opts.ImageStore = systemOpts.ImageStore
	} else if opts.GraphDriverName == overlayDriver {
		for _, o := range systemOpts.GraphDriverOptions {
			if strings.Contains(o, "ignore_chown_errors") {
				opts.GraphDriverOptions = append(opts.GraphDriverOptions, o)
				break
			}
		}
	}
	if opts.GraphDriverName == "" {
		if len(systemOpts.GraphDriverPriority) == 0 {
			dirEntries, err := os.ReadDir(opts.GraphRoot)
			if err == nil {
				for _, entry := range dirEntries {
					if name, ok := strings.CutSuffix(entry.Name(), "-images"); ok {
						opts.GraphDriverName = name
						break
					}
				}
			}

			if opts.GraphDriverName == "" {
				if canUseRootlessOverlay() {
					opts.GraphDriverName = overlayDriver
				} else {
					opts.GraphDriverName = "vfs"
				}
			}
		} else {
			opts.GraphDriverPriority = systemOpts.GraphDriverPriority
		}
	}

	if os.Getenv("STORAGE_OPTS") != "" {
		opts.GraphDriverOptions = slices.AppendSeq(opts.GraphDriverOptions, strings.SplitSeq(os.Getenv("STORAGE_OPTS"), ","))
	}

	return opts, nil
}

var prevReloadConfig = struct {
	storeOptions *StoreOptions
	mod          time.Time
	mutex        sync.Mutex
	configFile   string
}{}

// SetDefaultConfigFilePath sets the default configuration to the specified path
func SetDefaultConfigFilePath(path string) error {
	defaultConfigFile = path
	defaultConfigFileSet = true
	return ReloadConfigurationFileIfNeeded(defaultConfigFile, &defaultStoreOptions)
}

func ReloadConfigurationFileIfNeeded(configFile string, storeOptions *StoreOptions) error {
	prevReloadConfig.mutex.Lock()
	defer prevReloadConfig.mutex.Unlock()

	fi, err := os.Stat(configFile)
	if err != nil {
		return err
	}

	mtime := fi.ModTime()
	if prevReloadConfig.storeOptions != nil && mtime.Equal(prevReloadConfig.mod) && prevReloadConfig.configFile == configFile {
		*storeOptions = *prevReloadConfig.storeOptions
		return nil
	}

	if err := ReloadConfigurationFile(configFile, storeOptions); err != nil {
		return err
	}

	cOptions := *storeOptions
	prevReloadConfig.storeOptions = &cOptions
	prevReloadConfig.mod = mtime
	prevReloadConfig.configFile = configFile
	return nil
}

// ReloadConfigurationFile parses the specified configuration file and overrides
// the configuration in storeOptions.
func ReloadConfigurationFile(configFile string, storeOptions *StoreOptions) error {
	config := new(TomlConfig)

	meta, err := toml.DecodeFile(configFile, &config)
	if err == nil {
		keys := meta.Undecoded()
		if len(keys) > 0 {
			logrus.Warningf("Failed to decode the keys %q from %q", keys, configFile)
		}
	} else {
		if !os.IsNotExist(err) {
			logrus.Warningf("Failed to read %s %v\n", configFile, err.Error())
			return err
		}
	}

	// Clear storeOptions of previous settings
	*storeOptions = StoreOptions{}
	if config.Storage.Driver != "" {
		storeOptions.GraphDriverName = config.Storage.Driver
	}
	if os.Getenv("STORAGE_DRIVER") != "" {
		config.Storage.Driver = os.Getenv("STORAGE_DRIVER")
		storeOptions.GraphDriverName = config.Storage.Driver
	}
	if storeOptions.GraphDriverName == overlay2 {
		logrus.Warnf("Switching default driver from overlay2 to the equivalent overlay driver")
		storeOptions.GraphDriverName = overlayDriver
	}
	storeOptions.GraphDriverPriority = config.Storage.DriverPriority
	if storeOptions.GraphDriverName == "" && len(storeOptions.GraphDriverPriority) == 0 {
		logrus.Warnf("The storage 'driver' option should be set in %s. A driver was picked automatically.", configFile)
	}
	if config.Storage.RunRoot != "" {
		storeOptions.RunRoot = config.Storage.RunRoot
	}
	if config.Storage.GraphRoot != "" {
		storeOptions.GraphRoot = config.Storage.GraphRoot
	}
	if config.Storage.ImageStore != "" {
		storeOptions.ImageStore = config.Storage.ImageStore
	}
	if config.Storage.RootlessStoragePath != "" {
		storeOptions.RootlessStoragePath = config.Storage.RootlessStoragePath
	}
	for _, s := range config.Storage.Options.AdditionalImageStores {
		storeOptions.GraphDriverOptions = append(storeOptions.GraphDriverOptions, fmt.Sprintf("%s.imagestore=%s", config.Storage.Driver, s))
	}
	for _, s := range config.Storage.Options.AdditionalLayerStores {
		storeOptions.GraphDriverOptions = append(storeOptions.GraphDriverOptions, fmt.Sprintf("%s.additionallayerstore=%s", config.Storage.Driver, s))
	}
	if config.Storage.Options.Size != "" {
		storeOptions.GraphDriverOptions = append(storeOptions.GraphDriverOptions, fmt.Sprintf("%s.size=%s", config.Storage.Driver, config.Storage.Options.Size))
	}
	if config.Storage.Options.MountProgram != "" {
		storeOptions.GraphDriverOptions = append(storeOptions.GraphDriverOptions, fmt.Sprintf("%s.mount_program=%s", config.Storage.Driver, config.Storage.Options.MountProgram))
	}
	if config.Storage.Options.SkipMountHome != "" {
		storeOptions.GraphDriverOptions = append(storeOptions.GraphDriverOptions, fmt.Sprintf("%s.skip_mount_home=%s", config.Storage.Driver, config.Storage.Options.SkipMountHome))
	}
	if config.Storage.Options.IgnoreChownErrors != "" {
		storeOptions.GraphDriverOptions = append(storeOptions.GraphDriverOptions, fmt.Sprintf("%s.ignore_chown_errors=%s", config.Storage.Driver, config.Storage.Options.IgnoreChownErrors))
	}
	if config.Storage.Options.ForceMask != 0 {
		storeOptions.GraphDriverOptions = append(storeOptions.GraphDriverOptions, fmt.Sprintf("%s.force_mask=%o", config.Storage.Driver, config.Storage.Options.ForceMask))
	}
	if config.Storage.Options.MountOpt != "" {
		storeOptions.GraphDriverOptions = append(storeOptions.GraphDriverOptions, fmt.Sprintf("%s.mountopt=%s", config.Storage.Driver, config.Storage.Options.MountOpt))
	}
	storeOptions.RootAutoNsUser = config.Storage.Options.RootAutoUsernsUser
	if config.Storage.Options.AutoUsernsMinSize > 0 {
		storeOptions.AutoNsMinSize = config.Storage.Options.AutoUsernsMinSize
	}
	if config.Storage.Options.AutoUsernsMaxSize > 0 {
		storeOptions.AutoNsMaxSize = config.Storage.Options.AutoUsernsMaxSize
	}
	if config.Storage.Options.PullOptions != nil {
		storeOptions.PullOptions = config.Storage.Options.PullOptions
	}

	storeOptions.DisableVolatile = config.Storage.Options.DisableVolatile
	storeOptions.TransientStore = config.Storage.TransientStore

	storeOptions.GraphDriverOptions = append(storeOptions.GraphDriverOptions, cfg.GetGraphDriverOptions(storeOptions.GraphDriverName, config.Storage.Options)...)

	if opts, ok := os.LookupEnv("STORAGE_OPTS"); ok {
		storeOptions.GraphDriverOptions = strings.Split(opts, ",")
	}
	if len(storeOptions.GraphDriverOptions) == 1 && storeOptions.GraphDriverOptions[0] == "" {
		storeOptions.GraphDriverOptions = nil
	}
	return nil
}

func Options() (StoreOptions, error) {
	defaultStoreOptionsOnce.Do(loadDefaultStoreOptions)
	return defaultStoreOptions, loadDefaultStoreOptionsErr
}

// Save overwrites the tomlConfig in storage.conf with the given conf
func Save(conf TomlConfig) error {
	configFile, err := DefaultConfigFile()
	if err != nil {
		return err
	}

	if err = os.Remove(configFile); !os.IsNotExist(err) && err != nil {
		return err
	}

	f, err := os.Create(configFile)
	if err != nil {
		return err
	}

	return toml.NewEncoder(f).Encode(conf)
}

// StorageConfig is used to retrieve the storage.conf toml in order to overwrite it
func StorageConfig() (*TomlConfig, error) {
	config := new(TomlConfig)

	configFile, err := DefaultConfigFile()
	if err != nil {
		return nil, err
	}

	_, err = toml.DecodeFile(configFile, &config)
	if err != nil {
		return nil, err
	}

	return config, nil
}
