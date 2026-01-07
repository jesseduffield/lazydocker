//go:build !remote

package libpod

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/containers/buildah/pkg/parse"
	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/events"
	"github.com/containers/podman/v5/libpod/lock"
	"github.com/containers/podman/v5/libpod/plugin"
	"github.com/containers/podman/v5/libpod/shutdown"
	"github.com/containers/podman/v5/pkg/domain/entities"
	"github.com/containers/podman/v5/pkg/domain/entities/reports"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/containers/podman/v5/pkg/systemd"
	"github.com/containers/podman/v5/pkg/util"
	"github.com/docker/docker/pkg/namesgenerator"
	"github.com/hashicorp/go-multierror"
	jsoniter "github.com/json-iterator/go"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libimage"
	"go.podman.io/common/libnetwork/network"
	nettypes "go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/cgroups"
	"go.podman.io/common/pkg/config"
	artStore "go.podman.io/common/pkg/libartifact/store"
	"go.podman.io/common/pkg/secrets"
	systemdCommon "go.podman.io/common/pkg/systemd"
	"go.podman.io/image/v5/pkg/sysregistriesv2"
	is "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/lockfile"
	"go.podman.io/storage/pkg/unshare"
)

// Set up the JSON library for all of Libpod
var json = jsoniter.ConfigCompatibleWithStandardLibrary

// A RuntimeOption is a functional option which alters the Runtime created by
// NewRuntime
type RuntimeOption func(*Runtime) error

type storageSet struct {
	RunRootSet         bool
	GraphRootSet       bool
	StaticDirSet       bool
	VolumePathSet      bool
	GraphDriverNameSet bool
	TmpDirSet          bool
}

// Runtime is the core libpod runtime
type Runtime struct {
	config        *config.Config
	storageConfig storage.StoreOptions
	storageSet    storageSet

	state                  State
	store                  storage.Store
	storageService         *storageService
	imageContext           *types.SystemContext
	defaultOCIRuntime      OCIRuntime
	ociRuntimes            map[string]OCIRuntime
	runtimeFlags           []string
	network                nettypes.ContainerNetwork
	conmonPath             string
	libimageRuntime        *libimage.Runtime
	libimageEventsShutdown chan bool
	lockManager            lock.Manager

	// ArtifactStore returns the artifact store created from the runtime.
	ArtifactStore func() (*artStore.ArtifactStore, error)

	// Worker
	workerChannel chan func()
	workerGroup   sync.WaitGroup

	// syslog describes whenever logrus should log to the syslog as well.
	// Note that the syslog hook will be enabled early in cmd/podman/syslog_linux.go
	// This bool is just needed so that we can set it for netavark interface.
	syslog bool

	// doReset indicates that the runtime will perform a system reset.
	// A reset will remove all containers, pods, volumes, networks, etc.
	// A number of validation checks are relaxed, or replaced with logic to
	// remove as much of the runtime as possible if they fail. This ensures
	// that even a broken Libpod can still be removed via `system reset`.
	// This does not actually perform a `system reset`. That is done by
	// calling "Reset()" on the returned runtime.
	doReset bool
	// doRenumber indicates that the runtime will perform a system renumber.
	// A renumber will reassign lock numbers for all containers, pods, etc.
	// This will not perform the renumber itself, but will ignore some
	// errors related to lock initialization so a renumber can be performed
	// if something has gone wrong.
	doRenumber bool

	// valid indicates whether the runtime is ready to use.
	// valid is set to true when a runtime is returned from GetRuntime(),
	// and remains true until the runtime is shut down (rendering its
	// storage unusable). When valid is false, the runtime cannot be used.
	valid bool

	// mechanism to read and write even logs
	eventer events.Eventer

	// secretsManager manages secrets
	secretsManager *secrets.SecretsManager
}

// SetXdgDirs ensures the XDG_RUNTIME_DIR env and XDG_CONFIG_HOME variables are set.
// containers/image uses XDG_RUNTIME_DIR to locate the auth file, XDG_CONFIG_HOME is
// use for the containers.conf configuration file.
func SetXdgDirs() error {
	if !rootless.IsRootless() {
		return nil
	}

	// Set up XDG_RUNTIME_DIR
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")

	if runtimeDir == "" {
		var err error
		runtimeDir, err = util.GetRootlessRuntimeDir()
		if err != nil {
			return err
		}
	}
	if err := os.Setenv("XDG_RUNTIME_DIR", runtimeDir); err != nil {
		return fmt.Errorf("cannot set XDG_RUNTIME_DIR: %w", err)
	}

	if rootless.IsRootless() && os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		sessionAddr := filepath.Join(runtimeDir, "bus")
		if err := fileutils.Exists(sessionAddr); err == nil {
			os.Setenv("DBUS_SESSION_BUS_ADDRESS", fmt.Sprintf("unix:path=%s", sessionAddr))
		}
	}

	// Set up XDG_CONFIG_HOME
	if cfgHomeDir := os.Getenv("XDG_CONFIG_HOME"); cfgHomeDir == "" {
		cfgHomeDir, err := util.GetRootlessConfigHomeDir()
		if err != nil {
			return err
		}
		if err := os.Setenv("XDG_CONFIG_HOME", cfgHomeDir); err != nil {
			return fmt.Errorf("cannot set XDG_CONFIG_HOME: %w", err)
		}
	}
	return nil
}

// NewRuntime creates a new container runtime
// Options can be passed to override the default configuration for the runtime
func NewRuntime(ctx context.Context, options ...RuntimeOption) (*Runtime, error) {
	conf, err := config.Default()
	if err != nil {
		return nil, err
	}
	return newRuntimeFromConfig(ctx, conf, options...)
}

func newRuntimeFromConfig(ctx context.Context, conf *config.Config, options ...RuntimeOption) (*Runtime, error) {
	runtime := new(Runtime)

	if conf.Engine.OCIRuntime == "" {
		conf.Engine.OCIRuntime = "runc"
		// If we're running on cgroups v2, default to using crun.
		if onCgroupsv2, _ := cgroups.IsCgroup2UnifiedMode(); onCgroupsv2 {
			conf.Engine.OCIRuntime = "crun"
		}
	}

	runtime.config = conf

	if err := SetXdgDirs(); err != nil {
		return nil, err
	}

	storeOpts, err := storage.DefaultStoreOptions()
	if err != nil {
		return nil, err
	}
	runtime.storageConfig = storeOpts

	// Overwrite config with user-given configuration options
	for _, opt := range options {
		if err := opt(runtime); err != nil {
			return nil, fmt.Errorf("configuring runtime: %w", err)
		}
	}

	if err := makeRuntime(ctx, runtime); err != nil {
		return nil, err
	}

	if err := shutdown.Register("libpod", func(_ os.Signal) error {
		if runtime.store != nil {
			_, _ = runtime.store.Shutdown(false)
		}
		return nil
	}); err != nil && !errors.Is(err, shutdown.ErrHandlerExists) {
		logrus.Errorf("Registering shutdown handler for libpod: %v", err)
	}

	if err := shutdown.Start(); err != nil {
		return nil, fmt.Errorf("starting shutdown signal handler: %w", err)
	}

	runtime.config.CheckCgroupsAndAdjustConfig()

	return runtime, nil
}

func getLockManager(runtime *Runtime) (lock.Manager, error) {
	var err error
	var manager lock.Manager

	switch runtime.config.Engine.LockType {
	case "file":
		lockPath := filepath.Join(runtime.config.Engine.TmpDir, "locks")
		manager, err = lock.OpenFileLockManager(lockPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				manager, err = lock.NewFileLockManager(lockPath)
				if err != nil {
					return nil, fmt.Errorf("failed to get new file lock manager: %w", err)
				}
			} else {
				return nil, err
			}
		}

	case "", "shm":
		lockPath := define.DefaultSHMLockPath
		if rootless.IsRootless() {
			lockPath = fmt.Sprintf("%s_%d", define.DefaultRootlessSHMLockPath, rootless.GetRootlessUID())
		}
		// Set up the lock manager
		manager, err = lock.OpenSHMLockManager(lockPath, runtime.config.Engine.NumLocks)
		if err != nil {
			switch {
			case errors.Is(err, os.ErrNotExist):
				manager, err = lock.NewSHMLockManager(lockPath, runtime.config.Engine.NumLocks)
				if err != nil {
					return nil, fmt.Errorf("failed to get new shm lock manager: %w", err)
				}
			case errors.Is(err, syscall.ERANGE) && runtime.doRenumber:
				logrus.Debugf("Number of locks does not match - removing old locks")

				// ERANGE indicates a lock numbering mismatch.
				// Since we're renumbering, this is not fatal.
				// Remove the earlier set of locks and recreate.
				if err := os.Remove(filepath.Join("/dev/shm", lockPath)); err != nil {
					return nil, fmt.Errorf("removing libpod locks file %s: %w", lockPath, err)
				}

				manager, err = lock.NewSHMLockManager(lockPath, runtime.config.Engine.NumLocks)
				if err != nil {
					return nil, err
				}
			default:
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("unknown lock type %s: %w", runtime.config.Engine.LockType, define.ErrInvalidArg)
	}
	return manager, nil
}

func getDBState(runtime *Runtime) (State, error) {
	// TODO - if we further break out the state implementation into
	// libpod/state, the config could take care of the code below.  It
	// would further allow to move the types and consts into a coherent
	// package.
	backend, err := config.ParseDBBackend(runtime.config.Engine.DBBackend)
	if err != nil {
		return nil, err
	}

	// get default boltdb path
	baseDir := runtime.config.Engine.StaticDir
	if runtime.storageConfig.TransientStore {
		baseDir = runtime.config.Engine.TmpDir
	}
	boltDBPath := filepath.Join(baseDir, "bolt_state.db")

	switch backend {
	case config.DBBackendDefault:
		// for backwards compatibility check if boltdb exists, if it does not we use sqlite
		if err := fileutils.Exists(boltDBPath); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// need to set DBBackend string so podman info will show the backend name correctly
				runtime.config.Engine.DBBackend = config.DBBackendSQLite.String()
				return NewSqliteState(runtime)
			}
			// Return error here some other problem with the boltdb file, rather than silently
			// switch to sqlite which would be hard to debug for the user return the error back
			// as this likely a real bug.
			return nil, err
		}
		runtime.config.Engine.DBBackend = config.DBBackendBoltDB.String()
		fallthrough
	case config.DBBackendBoltDB:
		return NewBoltState(boltDBPath, runtime)
	case config.DBBackendSQLite:
		return NewSqliteState(runtime)
	default:
		return nil, fmt.Errorf("unrecognized database backend passed (%q): %w", backend.String(), define.ErrInvalidArg)
	}
}

// Make a new runtime based on the given configuration
// Sets up containers/storage, state store, OCI runtime
func makeRuntime(ctx context.Context, runtime *Runtime) (retErr error) {
	// Find a working conmon binary
	cPath, err := runtime.config.FindConmon()
	if err != nil {
		return err
	}
	runtime.conmonPath = cPath

	if runtime.config.Engine.StaticDir == "" {
		runtime.config.Engine.StaticDir = filepath.Join(runtime.storageConfig.GraphRoot, "libpod")
		runtime.storageSet.StaticDirSet = true
	}

	if runtime.config.Engine.VolumePath == "" {
		runtime.config.Engine.VolumePath = filepath.Join(runtime.storageConfig.GraphRoot, "volumes")
		runtime.storageSet.VolumePathSet = true
	}

	// Make the static files directory if it does not exist
	if err := os.MkdirAll(runtime.config.Engine.StaticDir, 0o700); err != nil {
		// The directory is allowed to exist
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("creating runtime static files directory %q: %w", runtime.config.Engine.StaticDir, err)
		}
	}

	// Create the TmpDir if needed
	if err := os.MkdirAll(runtime.config.Engine.TmpDir, 0o751); err != nil {
		return fmt.Errorf("creating runtime temporary files directory: %w", err)
	}

	// Create the volume path if needed.
	// This is not strictly necessary at this point, but the path not
	// existing can cause troubles with DB path validation on OSTree based
	// systems. Ref: https://github.com/containers/podman/issues/23515
	if err := os.MkdirAll(runtime.config.Engine.VolumePath, 0o700); err != nil {
		return fmt.Errorf("creating runtime volume path directory: %w", err)
	}

	// Set up the state.
	runtime.state, err = getDBState(runtime)
	if err != nil {
		return err
	}

	// Grab config from the database so we can reset some defaults
	dbConfig, err := runtime.state.GetDBConfig()
	if err != nil {
		if runtime.doReset {
			// We can at least delete the DB and the static files
			// directory.
			// Can't safely touch anything else because we aren't
			// sure of other directories.
			if err := runtime.state.Close(); err != nil {
				logrus.Errorf("Closing database connection: %v", err)
			} else {
				if err := os.RemoveAll(runtime.config.Engine.StaticDir); err != nil {
					logrus.Errorf("Removing static files directory %v: %v", runtime.config.Engine.StaticDir, err)
				}
			}
		}

		return fmt.Errorf("retrieving runtime configuration from database: %w", err)
	}

	runtime.mergeDBConfig(dbConfig)

	checkCgroups2UnifiedMode(runtime)

	logrus.Debugf("Using graph driver %s", runtime.storageConfig.GraphDriverName)
	logrus.Debugf("Using graph root %s", runtime.storageConfig.GraphRoot)
	logrus.Debugf("Using run root %s", runtime.storageConfig.RunRoot)
	logrus.Debugf("Using static dir %s", runtime.config.Engine.StaticDir)
	logrus.Debugf("Using tmp dir %s", runtime.config.Engine.TmpDir)
	logrus.Debugf("Using volume path %s", runtime.config.Engine.VolumePath)
	logrus.Debugf("Using transient store: %v", runtime.storageConfig.TransientStore)

	// Validate our config against the database, now that we've set our
	// final storage configuration
	if err := runtime.state.ValidateDBConfig(runtime); err != nil {
		// If we are performing a storage reset: continue on with a
		// warning. Otherwise we can't `system reset` after a change to
		// the core paths.
		if !runtime.doReset {
			return err
		}
		logrus.Errorf("Runtime paths differ from those stored in database, storage reset may not remove all files")
	}

	if runtime.config.Engine.Namespace != "" {
		return fmt.Errorf("namespaces are not supported by this version of Libpod, please unset the `namespace` field in containers.conf: %w", define.ErrNotImplemented)
	}

	needsUserns := os.Geteuid() != 0
	if !needsUserns {
		hasCapSysAdmin, err := unshare.HasCapSysAdmin()
		if err != nil {
			return err
		}
		needsUserns = !hasCapSysAdmin
	}
	// Set up containers/storage
	var store storage.Store
	if needsUserns {
		logrus.Debug("Not configuring container store")
	} else if err := runtime.configureStore(); err != nil {
		// Make a best-effort attempt to clean up if performing a
		// storage reset.
		if runtime.doReset {
			if err := runtime.removeAllDirs(); err != nil {
				logrus.Errorf("Removing libpod directories: %v", err)
			}
		}

		return fmt.Errorf("configure storage: %w", err)
	}
	defer func() {
		if retErr != nil && store != nil {
			// Don't forcibly shut down
			// We could be opening a store in use by another libpod
			if _, err := store.Shutdown(false); err != nil {
				logrus.Errorf("Removing store for partially-created runtime: %s", err)
			}
		}
	}()

	// Set up the eventer
	eventer, err := runtime.newEventer()
	if err != nil {
		return err
	}
	runtime.eventer = eventer

	// Set up containers/image
	if runtime.imageContext == nil {
		runtime.imageContext = &types.SystemContext{
			BigFilesTemporaryDir: parse.GetTempDir(),
		}
	}
	runtime.imageContext.SignaturePolicyPath = runtime.config.Engine.SignaturePolicyPath

	// Get us at least one working OCI runtime.
	runtime.ociRuntimes = make(map[string]OCIRuntime)

	// Initialize remaining OCI runtimes
	for name, paths := range runtime.config.Engine.OCIRuntimes {
		ociRuntime, err := newConmonOCIRuntime(name, paths, runtime.conmonPath, runtime.runtimeFlags, runtime.config)
		if err != nil {
			// Don't fatally error.
			// This will allow us to ship configs including optional
			// runtimes that might not be installed (crun, kata).
			// Only an infof so default configs don't spec errors.
			logrus.Debugf("Configured OCI runtime %s initialization failed: %v", name, err)
			continue
		}

		runtime.ociRuntimes[name] = ociRuntime
	}

	// Do we have a default OCI runtime?
	if runtime.config.Engine.OCIRuntime != "" {
		// If the string starts with / it's a path to a runtime
		// executable.
		if strings.HasPrefix(runtime.config.Engine.OCIRuntime, "/") {
			ociRuntime, err := newConmonOCIRuntime(runtime.config.Engine.OCIRuntime, []string{runtime.config.Engine.OCIRuntime}, runtime.conmonPath, runtime.runtimeFlags, runtime.config)
			if err != nil {
				return err
			}

			runtime.ociRuntimes[runtime.config.Engine.OCIRuntime] = ociRuntime
			runtime.defaultOCIRuntime = ociRuntime
		} else {
			ociRuntime, ok := runtime.ociRuntimes[runtime.config.Engine.OCIRuntime]
			if !ok {
				return fmt.Errorf("default OCI runtime %q not found: %w", runtime.config.Engine.OCIRuntime, define.ErrInvalidArg)
			}
			runtime.defaultOCIRuntime = ociRuntime
		}
	}
	logrus.Debugf("Using OCI runtime %q", runtime.defaultOCIRuntime.Path())

	// Do we have at least one valid OCI runtime?
	if len(runtime.ociRuntimes) == 0 {
		return fmt.Errorf("no OCI runtime has been configured: %w", define.ErrInvalidArg)
	}

	// Do we have a default runtime?
	if runtime.defaultOCIRuntime == nil {
		return fmt.Errorf("no default OCI runtime was configured: %w", define.ErrInvalidArg)
	}

	// the store is only set up when we are in the userns so we do the same for the network interface
	if !needsUserns {
		netBackend, netInterface, err := network.NetworkBackend(runtime.store, runtime.config, runtime.syslog)
		if err != nil {
			return err
		}
		runtime.config.Network.NetworkBackend = string(netBackend)
		runtime.network = netInterface

		// Using sync once value to only init the store exactly once and only when it will be actually be used.
		runtime.ArtifactStore = sync.OnceValues(func() (*artStore.ArtifactStore, error) {
			return artStore.NewArtifactStore(filepath.Join(runtime.storageConfig.GraphRoot, "artifacts"), runtime.SystemContext())
		})
	}

	// We now need to see if the system has restarted
	// We check for the presence of a file in our tmp directory to verify this
	// This check must be locked to prevent races
	runtimeAliveFile := filepath.Join(runtime.config.Engine.TmpDir, "alive")
	aliveLock, err := runtime.getRuntimeAliveLock()
	if err != nil {
		return fmt.Errorf("acquiring runtime init lock: %w", err)
	}
	// Acquire the lock and hold it until we return
	// This ensures that no two processes will be in runtime.refresh at once
	aliveLock.Lock()
	doRefresh := false
	unLockFunc := aliveLock.Unlock
	defer func() {
		if unLockFunc != nil {
			unLockFunc()
		}
	}()

	err = fileutils.Exists(runtimeAliveFile)
	if err != nil {
		// If we need to refresh, then it is safe to assume there are
		// no containers running.  Create immediately a namespace, as
		// we will need to access the storage.
		if needsUserns {
			// warn users if mode is rootless and cgroup manager is systemd
			// and no valid systemd session is present
			// warn only whenever new namespace is created
			if runtime.config.Engine.CgroupManager == config.SystemdCgroupsManager {
				unified, _ := cgroups.IsCgroup2UnifiedMode()
				if unified && rootless.IsRootless() && !systemd.IsSystemdSessionValid(rootless.GetRootlessUID()) {
					logrus.Debug("Invalid systemd user session for current user")
				}
			}
			unLockFunc()
			unLockFunc = nil
			pausePid, err := util.GetRootlessPauseProcessPidPath()
			if err != nil {
				return fmt.Errorf("could not get pause process pid file path: %w", err)
			}

			// create the path in case it does not already exists
			// https://github.com/containers/podman/issues/8539
			if err := os.MkdirAll(filepath.Dir(pausePid), 0o700); err != nil {
				return fmt.Errorf("could not create pause process pid file directory: %w", err)
			}

			became, ret, err := rootless.BecomeRootInUserNS(pausePid)
			if err != nil {
				return err
			}
			if became {
				// Check if the pause process was created.  If it was created, then
				// move it to its own systemd scope.
				systemdCommon.MovePauseProcessToScope(pausePid)

				// gocritic complains because defer is not run on os.Exit()
				// However this is fine because the lock is released anyway when the process exits
				//nolint:gocritic
				os.Exit(ret)
			}
		}
		// If the file doesn't exist, we need to refresh the state
		// This will trigger on first use as well, but refreshing an
		// empty state only creates a single file
		// As such, it's not really a performance concern
		if errors.Is(err, os.ErrNotExist) {
			doRefresh = true
		} else {
			return fmt.Errorf("reading runtime status file %s: %w", runtimeAliveFile, err)
		}
	}

	runtime.lockManager, err = getLockManager(runtime)
	if err != nil {
		return err
	}

	// Mark the runtime as valid - ready to be used, cannot be modified
	// further.
	// Need to do this *before* refresh as we can remove containers there.
	// Should not be a big deal as we don't return it to users until after
	// refresh runs.
	runtime.valid = true

	// Setup the worker channel early to start accepting jobs from refresh,
	// but do not start to execute the jobs right away. The runtime is not
	// ready at this point.
	runtime.setupWorkerQueue()

	// If we need to refresh the state, do it now - things are guaranteed to
	// be set up by now.
	if doRefresh {
		// Ensure we have a store before refresh occurs
		if runtime.store == nil {
			if err := runtime.configureStore(); err != nil {
				return fmt.Errorf("configure storage: %w", err)
			}
		}

		if err2 := runtime.refresh(ctx, runtimeAliveFile); err2 != nil {
			return err2
		}
	}

	// Check current boot ID - will be written to the alive file.
	if err := runtime.checkBootID(runtimeAliveFile); err != nil {
		return err
	}

	runtime.startWorker()

	return nil
}

// TmpDir gets the current Libpod temporary files directory.
func (r *Runtime) TmpDir() (string, error) {
	if !r.valid {
		return "", define.ErrRuntimeStopped
	}

	return r.config.Engine.TmpDir, nil
}

// GetConfig returns the configuration used by the runtime.
// Note that the returned value is not a copy and must hence
// only be used in a reading fashion.
func (r *Runtime) GetConfigNoCopy() (*config.Config, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}
	return r.config, nil
}

// GetConfig returns a copy of the configuration used by the runtime.
// Please use GetConfigNoCopy() in case you only want to read from
// but not write to the returned config.
func (r *Runtime) GetConfig() (*config.Config, error) {
	rtConfig, err := r.GetConfigNoCopy()
	if err != nil {
		return nil, err
	}

	config := new(config.Config)

	// Copy so the caller won't be able to modify the actual config
	if err := JSONDeepCopy(rtConfig, config); err != nil {
		return nil, fmt.Errorf("copying config: %w", err)
	}

	return config, nil
}

// libimageEventsMap translates a libimage event type to a libpod event status.
var libimageEventsMap = map[libimage.EventType]events.Status{
	libimage.EventTypeImagePull:      events.Pull,
	libimage.EventTypeImagePullError: events.PullError,
	libimage.EventTypeImagePush:      events.Push,
	libimage.EventTypeImageRemove:    events.Remove,
	libimage.EventTypeImageLoad:      events.LoadFromArchive,
	libimage.EventTypeImageSave:      events.Save,
	libimage.EventTypeImageTag:       events.Tag,
	libimage.EventTypeImageUntag:     events.Untag,
	libimage.EventTypeImageMount:     events.Mount,
	libimage.EventTypeImageUnmount:   events.Unmount,
}

// libimageEvents spawns a goroutine which will listen for events on
// the libimage.Runtime.  The goroutine will be cleaned up implicitly
// when the main() exists.
func (r *Runtime) libimageEvents() {
	r.libimageEventsShutdown = make(chan bool)

	toLibpodEventStatus := func(e *libimage.Event) events.Status {
		status, found := libimageEventsMap[e.Type]
		if !found {
			return "Unknown"
		}
		return status
	}

	eventChannel := r.libimageRuntime.EventChannel()
	go func() {
		sawShutdown := false
		for {
			// Make sure to read and write all events before
			// shutting down.
			for len(eventChannel) > 0 {
				libimageEvent := <-eventChannel
				e := events.Event{
					ID:     libimageEvent.ID,
					Name:   libimageEvent.Name,
					Status: toLibpodEventStatus(libimageEvent),
					Time:   libimageEvent.Time,
					Type:   events.Image,
				}
				if libimageEvent.Error != nil {
					e.Error = libimageEvent.Error.Error()
				}
				if err := r.eventer.Write(e); err != nil {
					logrus.Errorf("Unable to write image event: %q", err)
				}
			}

			if sawShutdown {
				close(r.libimageEventsShutdown)
				return
			}

			select {
			case <-r.libimageEventsShutdown:
				sawShutdown = true
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()
}

// DeferredShutdown shuts down the runtime without exposing any
// errors. This is only meant to be used when the runtime is being
// shutdown within a defer statement; else use Shutdown
func (r *Runtime) DeferredShutdown(force bool) {
	_ = r.Shutdown(force)
}

// Shutdown shuts down the runtime and associated containers and storage
// If force is true, containers and mounted storage will be shut down before
// cleaning up; if force is false, an error will be returned if there are
// still containers running or mounted
func (r *Runtime) Shutdown(force bool) error {
	if !r.valid {
		return nil
	}

	if r.workerChannel != nil {
		r.workerGroup.Wait()
		close(r.workerChannel)
	}

	r.valid = false

	// Shutdown all containers if --force is given
	if force {
		ctrs, err := r.state.AllContainers(false)
		if err != nil {
			logrus.Errorf("Retrieving containers from database: %v", err)
		} else {
			for _, ctr := range ctrs {
				if err := ctr.StopWithTimeout(r.config.Engine.StopTimeout); err != nil {
					logrus.Errorf("Stopping container %s: %v", ctr.ID(), err)
				}
			}
		}
	}

	var lastError error
	// If no store was requested, it can be nil and there is no need to
	// attempt to shut it down
	if r.store != nil {
		// Wait for the events to be written.
		if r.libimageEventsShutdown != nil {
			// Tell loop to shutdown
			r.libimageEventsShutdown <- true
			// Wait for close to signal shutdown
			<-r.libimageEventsShutdown
		}

		// Note that the libimage runtime shuts down the store.
		if err := r.libimageRuntime.Shutdown(force); err != nil {
			lastError = fmt.Errorf("shutting down container storage: %w", err)
		}
	}
	if err := r.state.Close(); err != nil {
		if lastError != nil {
			logrus.Error(lastError)
		}
		lastError = err
	}

	return lastError
}

// Reconfigures the runtime after a reboot
// Refreshes the state, recreating temporary files
// Does not check validity as the runtime is not valid until after this has run
func (r *Runtime) refresh(ctx context.Context, alivePath string) error {
	logrus.Debugf("Podman detected system restart - performing state refresh")

	// Clear state of database if not running in container
	if !graphRootMounted() {
		// First clear the state in the database
		if err := r.state.Refresh(); err != nil {
			return err
		}
	}

	// Next refresh the state of all containers to recreate dirs and
	// namespaces, and all the pods to recreate cgroups.
	// Containers, pods, and volumes must also reacquire their locks.
	ctrs, err := r.state.AllContainers(false)
	if err != nil {
		return fmt.Errorf("retrieving all containers from state: %w", err)
	}
	pods, err := r.state.AllPods()
	if err != nil {
		return fmt.Errorf("retrieving all pods from state: %w", err)
	}
	vols, err := r.state.AllVolumes()
	if err != nil {
		return fmt.Errorf("retrieving all volumes from state: %w", err)
	}
	// No locks are taken during pod, volume, and container refresh.
	// Furthermore, the pod/volume/container refresh() functions are not
	// allowed to take locks themselves.
	// We cannot assume that any pod/volume/container has a valid lock until
	// after this function has returned.
	// The runtime alive lock should suffice to provide mutual exclusion
	// until this has run.
	for _, ctr := range ctrs {
		if err := ctr.refresh(); err != nil {
			logrus.Errorf("Refreshing container %s: %v", ctr.ID(), err)
		}
		// This is the only place it's safe to use ctr.state.State unlocked
		// We're holding the alive lock, guaranteed to be the only Libpod on the system right now.
		if (ctr.AutoRemove() && ctr.state.State == define.ContainerStateExited) || ctr.state.State == define.ContainerStateRemoving {
			opts := ctrRmOpts{
				// Don't force-remove, we're supposed to be fresh off a reboot
				// If we have to force something is seriously wrong
				Force:        false,
				RemoveVolume: true,
			}
			// This container should have autoremoved before the
			// reboot but did not.
			// Get rid of it.
			if _, _, err := r.removeContainer(ctx, ctr, opts); err != nil {
				logrus.Errorf("Unable to remove container %s which should have autoremoved: %v", ctr.ID(), err)
			}
		}
	}
	for _, pod := range pods {
		if err := pod.refresh(); err != nil {
			logrus.Errorf("Refreshing pod %s: %v", pod.ID(), err)
		}
	}
	for _, vol := range vols {
		if err := vol.refresh(); err != nil {
			logrus.Errorf("Refreshing volume %s: %v", vol.Name(), err)
		}
	}

	// Create a file indicating the runtime is alive and ready
	file, err := os.OpenFile(alivePath, os.O_RDONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("creating runtime status file: %w", err)
	}
	defer file.Close()

	r.NewSystemEvent(events.Refresh)

	return nil
}

// Info returns the store and host information
func (r *Runtime) Info() (*define.Info, error) {
	return r.info()
}

// generateName generates a unique name for a container or pod.
func (r *Runtime) generateName() (string, error) {
	for {
		name := namesgenerator.GetRandomName(0)
		// Make sure container with this name does not exist
		if _, err := r.state.LookupContainer(name); err == nil {
			continue
		} else if !errors.Is(err, define.ErrNoSuchCtr) {
			return "", err
		}
		// Make sure pod with this name does not exist
		if _, err := r.state.LookupPod(name); err == nil {
			continue
		} else if !errors.Is(err, define.ErrNoSuchPod) {
			return "", err
		}
		return name, nil
	}
	// The code should never reach here.
}

// Configure store and image runtime
func (r *Runtime) configureStore() error {
	store, err := storage.GetStore(r.storageConfig)
	if err != nil {
		return err
	}

	r.store = store
	is.Transport.SetStore(store)

	// Set up a storage service for creating container root filesystems from
	// images
	r.storageService = getStorageService(r.store)

	runtimeOptions := &libimage.RuntimeOptions{
		SystemContext: r.imageContext,
	}
	libimageRuntime, err := libimage.RuntimeFromStore(store, runtimeOptions)
	if err != nil {
		return err
	}
	r.libimageRuntime = libimageRuntime
	// Run the libimage events routine.
	r.libimageEvents()

	return nil
}

// LibimageRuntime ... to allow for a step-by-step migration to libimage.
func (r *Runtime) LibimageRuntime() *libimage.Runtime {
	return r.libimageRuntime
}

// SystemContext returns the imagecontext
func (r *Runtime) SystemContext() *types.SystemContext {
	// Return the context from the libimage runtime.  libimage is sensitive
	// to a number of env vars.
	return r.libimageRuntime.SystemContext()
}

// GetOCIRuntimePath retrieves the path of the default OCI runtime.
func (r *Runtime) GetOCIRuntimePath() string {
	return r.defaultOCIRuntime.Path()
}

// DefaultOCIRuntime return copy of Default OCI Runtime
func (r *Runtime) DefaultOCIRuntime() OCIRuntime {
	return r.defaultOCIRuntime
}

// StorageConfig retrieves the storage options for the container runtime
func (r *Runtime) StorageConfig() storage.StoreOptions {
	return r.storageConfig
}

func (r *Runtime) GarbageCollect() error {
	return r.store.GarbageCollect()
}

// RunRoot retrieves the current c/storage temporary directory in use by Libpod.
func (r *Runtime) RunRoot() string {
	if r.store == nil {
		return ""
	}
	return r.store.RunRoot()
}

// GraphRoot retrieves the current c/storage directory in use by Libpod.
func (r *Runtime) GraphRoot() string {
	if r.store == nil {
		return ""
	}
	return r.store.GraphRoot()
}

// GetPodName retrieves the pod name associated with a given full ID.
// If the given ID does not correspond to any existing Pod or Container,
// ErrNoSuchPod is returned.
func (r *Runtime) GetPodName(id string) (string, error) {
	if !r.valid {
		return "", define.ErrRuntimeStopped
	}

	return r.state.GetPodName(id)
}

// DBConfig is a set of Libpod runtime configuration settings that are saved in
// a State when it is first created, and can subsequently be retrieved.
type DBConfig struct {
	LibpodRoot  string
	LibpodTmp   string
	StorageRoot string
	StorageTmp  string
	GraphDriver string
	VolumePath  string
}

// mergeDBConfig merges the configuration from the database.
func (r *Runtime) mergeDBConfig(dbConfig *DBConfig) {
	c := &r.config.Engine
	if !r.storageSet.RunRootSet && dbConfig.StorageTmp != "" {
		if r.storageConfig.RunRoot != dbConfig.StorageTmp &&
			r.storageConfig.RunRoot != "" {
			logrus.Debugf("Overriding run root %q with %q from database",
				r.storageConfig.RunRoot, dbConfig.StorageTmp)
		}
		r.storageConfig.RunRoot = dbConfig.StorageTmp
	}

	if !r.storageSet.GraphRootSet && dbConfig.StorageRoot != "" {
		if r.storageConfig.GraphRoot != dbConfig.StorageRoot &&
			r.storageConfig.GraphRoot != "" {
			logrus.Debugf("Overriding graph root %q with %q from database",
				r.storageConfig.GraphRoot, dbConfig.StorageRoot)
		}
		r.storageConfig.GraphRoot = dbConfig.StorageRoot
	}

	if !r.storageSet.GraphDriverNameSet && dbConfig.GraphDriver != "" {
		if r.storageConfig.GraphDriverName != dbConfig.GraphDriver &&
			r.storageConfig.GraphDriverName != "" {
			logrus.Errorf("User-selected graph driver %q overwritten by graph driver %q from database - delete libpod local files (%q) to resolve.  May prevent use of images created by other tools",
				r.storageConfig.GraphDriverName, dbConfig.GraphDriver, r.storageConfig.GraphRoot)
		}
		r.storageConfig.GraphDriverName = dbConfig.GraphDriver
	}

	if !r.storageSet.StaticDirSet && dbConfig.LibpodRoot != "" {
		if c.StaticDir != dbConfig.LibpodRoot && c.StaticDir != "" {
			logrus.Debugf("Overriding static dir %q with %q from database", c.StaticDir, dbConfig.LibpodRoot)
		}
		c.StaticDir = dbConfig.LibpodRoot
	}

	if !r.storageSet.TmpDirSet && dbConfig.LibpodTmp != "" {
		if c.TmpDir != dbConfig.LibpodTmp && c.TmpDir != "" {
			logrus.Debugf("Overriding tmp dir %q with %q from database", c.TmpDir, dbConfig.LibpodTmp)
		}
		c.TmpDir = dbConfig.LibpodTmp
	}

	if !r.storageSet.VolumePathSet && dbConfig.VolumePath != "" {
		if c.VolumePath != dbConfig.VolumePath && c.VolumePath != "" {
			logrus.Debugf("Overriding volume path %q with %q from database", c.VolumePath, dbConfig.VolumePath)
		}
		c.VolumePath = dbConfig.VolumePath
	}
}

func (r *Runtime) EnableLabeling() bool {
	return r.config.Containers.EnableLabeling
}

// Reload reloads the configurations files
func (r *Runtime) Reload() error {
	if err := r.reloadContainersConf(); err != nil {
		return err
	}
	if err := r.reloadStorageConf(); err != nil {
		return err
	}
	// Invalidate the registries.conf cache. The next invocation will
	// reload all data.
	sysregistriesv2.InvalidateCache()
	return nil
}

// reloadContainersConf reloads the containers.conf
func (r *Runtime) reloadContainersConf() error {
	config, err := config.Reload()
	if err != nil {
		return err
	}
	r.config = config
	logrus.Infof("Applied new containers configuration: %v", config)
	return nil
}

// reloadStorageConf reloads the storage.conf
func (r *Runtime) reloadStorageConf() error {
	configFile, err := storage.DefaultConfigFile()
	if err != nil {
		return err
	}
	storage.ReloadConfigurationFile(configFile, &r.storageConfig)
	logrus.Infof("Applied new storage configuration: %v", r.storageConfig)
	return nil
}

// getVolumePlugin gets a specific volume plugin.
func (r *Runtime) getVolumePlugin(volConfig *VolumeConfig) (*plugin.VolumePlugin, error) {
	// There is no plugin for local.
	name := volConfig.Driver
	timeout := volConfig.Timeout
	if name == define.VolumeDriverLocal || name == "" {
		return nil, nil
	}

	pluginPath, ok := r.config.Engine.VolumePlugins[name]
	if !ok {
		if name == define.VolumeDriverImage {
			return nil, nil
		}
		return nil, fmt.Errorf("no volume plugin with name %s available: %w", name, define.ErrMissingPlugin)
	}

	return plugin.GetVolumePlugin(name, pluginPath, timeout, r.config)
}

// GetSecretsStorageDir returns the directory that the secrets manager should take
func (r *Runtime) GetSecretsStorageDir() string {
	return filepath.Join(r.store.GraphRoot(), "secrets")
}

// SecretsManager returns the directory that the secrets manager should take
func (r *Runtime) SecretsManager() (*secrets.SecretsManager, error) {
	if r.secretsManager == nil {
		manager, err := secrets.NewManager(r.GetSecretsStorageDir())
		if err != nil {
			return nil, err
		}
		r.secretsManager = manager
	}
	return r.secretsManager, nil
}

func graphRootMounted() bool {
	f, err := os.OpenFile("/run/.containerenv", os.O_RDONLY, os.ModePerm)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if scanner.Text() == "graphRootMounted=1" {
			return true
		}
	}
	return false
}

func (r *Runtime) graphRootMountedFlag(mounts []spec.Mount) string {
	root := r.store.GraphRoot()
	for _, val := range mounts {
		if strings.HasPrefix(root, val.Source) {
			return "graphRootMounted=1"
		}
	}
	return ""
}

// Returns a copy of the runtime alive lock
func (r *Runtime) getRuntimeAliveLock() (*lockfile.LockFile, error) {
	return lockfile.GetLockFile(filepath.Join(r.config.Engine.TmpDir, "alive.lck"))
}

// Network returns the network interface which is used by the runtime
func (r *Runtime) Network() nettypes.ContainerNetwork {
	return r.network
}

// GetDefaultNetworkName returns the network interface which is used by the runtime
func (r *Runtime) GetDefaultNetworkName() string {
	return r.config.Network.DefaultNetwork
}

// RemoteURI returns the API server URI
func (r *Runtime) RemoteURI() string {
	return r.config.Engine.RemoteURI
}

// SetRemoteURI records the API server URI
func (r *Runtime) SetRemoteURI(uri string) {
	r.config.Engine.RemoteURI = uri
}

// Get information on potential lock conflicts.
// Returns a map of lock number to object(s) using the lock, formatted as
// "container <id>" or "volume <id>" or "pod <id>", and an array of locks that
// are currently being held, formatted as []uint32.
// If the map returned is not empty, you should immediately renumber locks on
// the runtime, because you have a deadlock waiting to happen.
func (r *Runtime) LockConflicts() (map[uint32][]string, []uint32, error) {
	// Make an internal map to store what lock is associated with what
	locksInUse := make(map[uint32][]string)

	ctrs, err := r.state.AllContainers(false)
	if err != nil {
		return nil, nil, err
	}
	for _, ctr := range ctrs {
		lockNum := ctr.lock.ID()
		ctrString := fmt.Sprintf("container %s", ctr.ID())
		locksInUse[lockNum] = append(locksInUse[lockNum], ctrString)
	}

	pods, err := r.state.AllPods()
	if err != nil {
		return nil, nil, err
	}
	for _, pod := range pods {
		lockNum := pod.lock.ID()
		podString := fmt.Sprintf("pod %s", pod.ID())
		locksInUse[lockNum] = append(locksInUse[lockNum], podString)
	}

	volumes, err := r.state.AllVolumes()
	if err != nil {
		return nil, nil, err
	}
	for _, vol := range volumes {
		lockNum := vol.lock.ID()
		volString := fmt.Sprintf("volume %s", vol.Name())
		locksInUse[lockNum] = append(locksInUse[lockNum], volString)
	}

	// Now go through and find any entries with >1 item associated
	toReturn := make(map[uint32][]string)
	for lockNum, objects := range locksInUse {
		// If debug logging is requested, just spit out *every* lock in
		// use.
		logrus.Debugf("Lock number %d is in use by %v", lockNum, objects)

		if len(objects) > 1 {
			toReturn[lockNum] = objects
		}
	}

	locksHeld, err := r.lockManager.LocksHeld()
	if err != nil {
		if errors.Is(err, define.ErrNotImplemented) {
			logrus.Warnf("Could not retrieve currently taken locks as the lock backend does not support this operation")
			return toReturn, []uint32{}, nil
		}

		return nil, nil, err
	}

	return toReturn, locksHeld, nil
}

// PruneBuildContainers removes any build containers that were created during the build,
// but were not removed because the build was unexpectedly terminated.
//
// Note: This is not safe operation and should be executed only when no builds are in progress. It can interfere with builds in progress.
func (r *Runtime) PruneBuildContainers() ([]*reports.PruneReport, error) {
	stageContainersPruneReports := []*reports.PruneReport{}

	containers, err := r.store.Containers()
	if err != nil {
		return stageContainersPruneReports, err
	}
	for _, container := range containers {
		path, err := r.store.ContainerDirectory(container.ID)
		if err != nil {
			return stageContainersPruneReports, err
		}
		if err := fileutils.Exists(filepath.Join(path, "buildah.json")); err != nil {
			continue
		}

		report := &reports.PruneReport{
			Id: container.ID,
		}
		size, err := r.store.ContainerSize(container.ID)
		if err != nil {
			report.Err = err
		}
		report.Size = uint64(size)

		if err := r.store.DeleteContainer(container.ID); err != nil {
			report.Err = errors.Join(report.Err, err)
		}
		stageContainersPruneReports = append(stageContainersPruneReports, report)
	}
	return stageContainersPruneReports, nil
}

// SystemCheck checks our storage for consistency, and depending on the options
// specified, will attempt to remove anything which fails consistency checks.
func (r *Runtime) SystemCheck(_ context.Context, options entities.SystemCheckOptions) (entities.SystemCheckReport, error) {
	what := storage.CheckEverything()
	if options.Quick {
		// Turn off checking layer digests and layer contents to do quick check.
		// This is not a complete check like storage.CheckEverything(), and may fail detecting
		// whether a file is missing from the image or its content has changed.
		// In some cases it's desirable to trade check thoroughness for speed.
		what = &storage.CheckOptions{
			LayerDigests:   false,
			LayerMountable: true,
			LayerContents:  false,
			LayerData:      true,
			ImageData:      true,
			ContainerData:  true,
		}
	}
	if options.UnreferencedLayerMaximumAge != nil {
		tmp := *options.UnreferencedLayerMaximumAge
		what.LayerUnreferencedMaximumAge = &tmp
	}
	storageReport, err := r.store.Check(what)
	if err != nil {
		return entities.SystemCheckReport{}, err
	}
	if len(storageReport.Containers) == 0 &&
		len(storageReport.Layers) == 0 &&
		len(storageReport.ROLayers) == 0 &&
		len(storageReport.Images) == 0 &&
		len(storageReport.ROImages) == 0 {
		// no errors detected
		return entities.SystemCheckReport{}, nil
	}
	mapErrorSlicesToStringSlices := func(m map[string][]error) map[string][]string {
		if len(m) == 0 {
			return nil
		}
		mapped := make(map[string][]string, len(m))
		for k, errs := range m {
			strs := make([]string, len(errs))
			for i, e := range errs {
				strs[i] = e.Error()
			}
			mapped[k] = strs
		}
		return mapped
	}

	report := entities.SystemCheckReport{
		Errors:     true,
		Layers:     mapErrorSlicesToStringSlices(storageReport.Layers),
		ROLayers:   mapErrorSlicesToStringSlices(storageReport.ROLayers),
		Images:     mapErrorSlicesToStringSlices(storageReport.Images),
		ROImages:   mapErrorSlicesToStringSlices(storageReport.ROImages),
		Containers: mapErrorSlicesToStringSlices(storageReport.Containers),
	}
	if !options.Repair && report.Errors {
		// errors detected, no corrective measures to be taken
		return report, err
	}

	// get a list of images that we knew of before we tried to clean up any
	// that were damaged
	imagesBefore, err := r.store.Images()
	if err != nil {
		return report, fmt.Errorf("getting a list of images before attempting repairs: %w", err)
	}

	repairOptions := storage.RepairOptions{
		RemoveContainers: options.RepairLossy,
	}
	var containers []*Container
	if repairOptions.RemoveContainers {
		// build a list of the containers that we claim as ours that we
		// expect to be removing in a bit
		for containerID := range storageReport.Containers {
			ctr, lookupErr := r.state.LookupContainer(containerID)
			if lookupErr != nil {
				// we're about to remove it, so it's okay that
				// it isn't even one of ours
				continue
			}
			containers = append(containers, ctr)
		}
	}

	// run the cleanup
	merr := multierror.Append(nil, r.store.Repair(storageReport, &repairOptions)...)

	if repairOptions.RemoveContainers {
		// get the list of containers that storage will still admit to knowing about
		containersAfter, err := r.store.Containers()
		if err != nil {
			merr = multierror.Append(merr, fmt.Errorf("getting a list of containers after attempting repairs: %w", err))
		}
		for _, ctr := range containers {
			// if one of our containers that we tried to remove is
			// still on disk, report an error
			if slices.IndexFunc(containersAfter, func(containerAfter storage.Container) bool {
				return containerAfter.ID == ctr.ID()
			}) != -1 {
				merr = multierror.Append(merr, fmt.Errorf("clearing storage for container %s: %w", ctr.ID(), err))
				continue
			}
			// remove the container from our database
			if removeErr := r.state.RemoveContainer(ctr); removeErr != nil {
				merr = multierror.Append(merr, fmt.Errorf("updating state database to reflect removal of container %s: %w", ctr.ID(), removeErr))
				continue
			}
			if report.RemovedContainers == nil {
				report.RemovedContainers = make(map[string]string)
			}
			report.RemovedContainers[ctr.ID()] = ctr.config.Name
		}
	}

	// get a list of images that are still around after we clean up any
	// that were damaged
	imagesAfter, err := r.store.Images()
	if err != nil {
		merr = multierror.Append(merr, fmt.Errorf("getting a list of images after attempting repairs: %w", err))
	}
	for _, imageBefore := range imagesBefore {
		if slices.IndexFunc(imagesAfter, func(imageAfter storage.Image) bool {
			return imageAfter.ID == imageBefore.ID
		}) == -1 {
			if report.RemovedImages == nil {
				report.RemovedImages = make(map[string][]string)
			}
			report.RemovedImages[imageBefore.ID] = slices.Clone(imageBefore.Names)
		}
	}

	if merr != nil {
		err = merr.ErrorOrNil()
	}

	return report, err
}

func (r *Runtime) GetContainerExitCode(id string) (int32, error) {
	return r.state.GetContainerExitCode(id)
}
