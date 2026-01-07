//go:build !remote

package libpod

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	metadata "github.com/checkpoint-restore/checkpointctl/lib"
	"github.com/containers/buildah/copier"
	"github.com/containers/buildah/pkg/overlay"
	butil "github.com/containers/buildah/util"
	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/events"
	"github.com/containers/podman/v5/libpod/shutdown"
	"github.com/containers/podman/v5/pkg/ctime"
	"github.com/containers/podman/v5/pkg/domain/entities"
	envLib "github.com/containers/podman/v5/pkg/env"
	"github.com/containers/podman/v5/pkg/lookup"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/containers/podman/v5/pkg/selinux"
	"github.com/containers/podman/v5/pkg/systemd/notifyproxy"
	"github.com/containers/podman/v5/pkg/util"
	"github.com/coreos/go-systemd/v22/daemon"
	securejoin "github.com/cyphar/filepath-securejoin"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/opencontainers/selinux/go-selinux/label"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/etchosts"
	"go.podman.io/common/pkg/cgroups"
	"go.podman.io/common/pkg/chown"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/hooks"
	"go.podman.io/common/pkg/hooks/exec"
	"go.podman.io/common/pkg/timezone"
	cutil "go.podman.io/common/pkg/util"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/chrootarchive"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/idmap"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/lockfile"
	"go.podman.io/storage/pkg/mount"
	"golang.org/x/sys/unix"
)

const (
	// name of the directory holding the artifacts
	artifactsDir      = "artifacts"
	execDirPermission = 0755
	preCheckpointDir  = "pre-checkpoint"
)

// rootFsSize gets the size of the container, which can be divided notionally
// into two parts.  The first is the part of its size that can be directly
// attributed to its base image, if it has one.  The second is the set of
// changes that the container has had made relative to that base image.  Both
// parts include some ancillary data, and we count that, too.
func (c *Container) rootFsSize() (int64, error) {
	if c.config.Rootfs != "" {
		return 0, nil
	}
	if c.runtime.store == nil {
		return 0, nil
	}

	container, err := c.runtime.store.Container(c.ID())
	if err != nil {
		return 0, err
	}

	size := int64(0)
	if container.ImageID != "" {
		size, err = c.runtime.store.ImageSize(container.ImageID)
		if err != nil {
			return 0, err
		}
	}

	layerSize, err := c.runtime.store.ContainerSize(c.ID())

	return size + layerSize, err
}

// rwSize gets the combined size of the writeable layer and any ancillary data
// for a given container.
func (c *Container) rwSize() (int64, error) {
	if c.config.Rootfs != "" {
		size, err := util.SizeOfPath(c.config.Rootfs)
		return int64(size), err
	}

	layerSize, err := c.runtime.store.ContainerSize(c.ID())
	if err != nil {
		return 0, err
	}

	return layerSize, nil
}

// bundlePath returns the path to the container's root filesystem - where the OCI spec will be
// placed, amongst other things
func (c *Container) bundlePath() string {
	if c.runtime.storageConfig.TransientStore {
		return c.state.RunDir
	}
	return c.config.StaticDir
}

// ControlSocketPath returns the path to the container's control socket for things like tty
// resizing
func (c *Container) ControlSocketPath() string {
	return filepath.Join(c.bundlePath(), "ctl")
}

// CheckpointVolumesPath returns the path to the directory containing the checkpointed volumes
func (c *Container) CheckpointVolumesPath() string {
	return filepath.Join(c.bundlePath(), metadata.CheckpointVolumesDirectory)
}

// CheckpointPath returns the path to the directory containing the checkpoint
func (c *Container) CheckpointPath() string {
	return filepath.Join(c.bundlePath(), metadata.CheckpointDirectory)
}

// PreCheckPointPath returns the path to the directory containing the pre-checkpoint-images
func (c *Container) PreCheckPointPath() string {
	return filepath.Join(c.bundlePath(), preCheckpointDir)
}

// AttachSocketPath retrieves the path of the container's attach socket
func (c *Container) AttachSocketPath() (string, error) {
	return c.ociRuntime.AttachSocketPath(c)
}

// exitFilePath gets the path to the container's exit file
func (c *Container) exitFilePath() (string, error) {
	return c.ociRuntime.ExitFilePath(c)
}

func (c *Container) oomFilePath() (string, error) {
	return c.ociRuntime.OOMFilePath(c)
}

func (c *Container) persistDirPath() (string, error) {
	return c.ociRuntime.PersistDirectoryPath(c)
}

// Wait for the container's exit file to appear.
// When it does, update our state based on it.
func (c *Container) waitForExitFileAndSync() error {
	exitFile, err := c.exitFilePath()
	if err != nil {
		return err
	}

	chWait := make(chan error)
	defer close(chWait)

	_, err = cutil.WaitForFile(exitFile, chWait, time.Second*5)
	if err != nil {
		// Exit file did not appear
		// Reset our state
		c.state.ExitCode = -1
		c.state.FinishedTime = time.Now()
		c.state.State = define.ContainerStateStopped

		if err2 := c.save(); err2 != nil {
			logrus.Errorf("Saving container %s state: %v", c.ID(), err2)
		}

		return err
	}

	if err := c.checkExitFile(); err != nil {
		return err
	}

	return c.save()
}

// Handle the container exit file.
// The exit file is used to supply container exit time and exit code.
// This assumes the exit file already exists.
// Also check for an oom file to determine if the container was oom killed or not.
func (c *Container) handleExitFile(exitFile string, fi os.FileInfo) error {
	c.state.FinishedTime = ctime.Created(fi)
	statusCodeStr, err := os.ReadFile(exitFile)
	if err != nil {
		return fmt.Errorf("failed to read exit file for container %s: %w", c.ID(), err)
	}
	statusCode, err := strconv.Atoi(string(statusCodeStr))
	if err != nil {
		return fmt.Errorf("converting exit status code (%q, err) for container %s to int: %w",
			c.ID(), statusCodeStr, err)
	}
	c.state.ExitCode = int32(statusCode)

	oomFilePath, err := c.oomFilePath()
	if err != nil {
		return err
	}
	if err = fileutils.Exists(oomFilePath); err == nil {
		c.state.OOMKilled = true
	}

	c.state.Exited = true

	// Write an event for the container's death
	c.newContainerExitedEvent(c.state.ExitCode)

	return c.runtime.state.AddContainerExitCode(c.ID(), c.state.ExitCode)
}

func (c *Container) shouldRestart() bool {
	if c.config.HealthCheckOnFailureAction == define.HealthCheckOnFailureActionRestart {
		isUnhealthy, err := c.isUnhealthy()
		if err != nil {
			logrus.Errorf("Checking if container is unhealthy: %v", err)
		} else if isUnhealthy {
			return true
		}
	}

	// Explicitly stopped by user, do not restart again.
	if c.state.StoppedByUser {
		return false
	}

	// If we did not get a restart policy match, return false
	// Do the same if we're not a policy that restarts.
	if !c.state.RestartPolicyMatch ||
		c.config.RestartPolicy == define.RestartPolicyNo ||
		c.config.RestartPolicy == define.RestartPolicyNone {
		return false
	}

	// If we're RestartPolicyOnFailure, we need to check retries and exit
	// code.
	if c.config.RestartPolicy == define.RestartPolicyOnFailure {
		if c.state.ExitCode == 0 {
			return false
		}

		// If we don't have a max retries set, continue
		if c.config.RestartRetries > 0 {
			if c.state.RestartCount >= c.config.RestartRetries {
				return false
			}
		}
	}
	return true
}

// Handle container restart policy.
// This is called when a container has exited, and was not explicitly stopped by
// an API call to stop the container or pod it is in.
func (c *Container) handleRestartPolicy(ctx context.Context) (_ bool, retErr error) {
	if !c.shouldRestart() {
		return false, nil
	}
	logrus.Debugf("Restarting container %s due to restart policy %s", c.ID(), c.config.RestartPolicy)

	// Need to check if dependencies are alive.
	if err := c.checkDependenciesAndHandleError(); err != nil {
		return false, err
	}

	if c.config.HealthCheckConfig != nil {
		if err := c.removeTransientFiles(ctx,
			c.config.StartupHealthCheckConfig != nil && !c.state.StartupHCPassed,
			c.state.HCUnitName); err != nil {
			return false, err
		}
	}

	// Is the container running again?
	// If so, we don't have to do anything
	if c.ensureState(define.ContainerStateRunning, define.ContainerStatePaused) {
		return false, nil
	} else if c.state.State == define.ContainerStateUnknown {
		return false, fmt.Errorf("invalid container state encountered in restart attempt: %w", define.ErrInternal)
	}

	c.newContainerEvent(events.Restart)

	// Increment restart count
	c.state.RestartCount++
	logrus.Debugf("Container %s now on retry %d", c.ID(), c.state.RestartCount)
	if err := c.save(); err != nil {
		return false, err
	}

	defer func() {
		if retErr != nil {
			if err := c.cleanup(ctx); err != nil {
				logrus.Errorf("Cleaning up container %s: %v", c.ID(), err)
			}
		}
	}()

	// Always teardown the network, trying to reuse the netns has caused
	// a significant amount of bugs in this code here. It also never worked
	// for containers with user namespaces. So once and for all simplify this
	// by never reusing the netns. Originally this was done to have a faster
	// restart of containers but with netavark now we are much faster so it
	// shouldn't be that noticeable in practice. It also makes more sense to
	// reconfigure the netns as it is likely that the container exited due
	// some broken network state in which case reusing would just cause more
	// harm than good.
	if err := c.cleanupNetwork(); err != nil {
		return false, err
	}

	if err := c.prepare(); err != nil {
		return false, err
	}

	if c.state.State == define.ContainerStateStopped {
		// Reinitialize the container if we need to
		if err := c.reinit(ctx, true); err != nil {
			return false, err
		}
	} else if c.ensureState(define.ContainerStateConfigured, define.ContainerStateExited) {
		// Initialize the container
		if err := c.init(ctx, true); err != nil {
			return false, err
		}
	}
	if err := c.start(); err != nil {
		return false, err
	}
	return true, c.waitForHealthy(ctx)
}

// Ensure that the container is in a specific state or state.
// Returns true if the container is in one of the given states,
// or false otherwise.
func (c *Container) ensureState(states ...define.ContainerStatus) bool {
	return slices.Contains(states, c.state.State)
}

// Sync this container with on-disk state and runtime status
// Should only be called with container lock held
// This function should suffice to ensure a container's state is accurate and
// it is valid for use.
func (c *Container) syncContainer() error {
	if err := c.runtime.state.UpdateContainer(c); err != nil {
		return err
	}
	// If runtime knows about the container, update its status in runtime
	// And then save back to disk
	if c.ensureState(define.ContainerStateCreated, define.ContainerStateRunning, define.ContainerStateStopped, define.ContainerStateStopping, define.ContainerStatePaused) {
		oldState := c.state.State

		if err := c.checkExitFile(); err != nil {
			return err
		}

		// Only save back to DB if state changed
		if c.state.State != oldState {
			// Check for a restart policy match
			if c.config.RestartPolicy != define.RestartPolicyNone && c.config.RestartPolicy != define.RestartPolicyNo &&
				(oldState == define.ContainerStateRunning || oldState == define.ContainerStatePaused) &&
				(c.state.State == define.ContainerStateStopped || c.state.State == define.ContainerStateExited) &&
				!c.state.StoppedByUser {
				c.state.RestartPolicyMatch = true
			}

			if err := c.save(); err != nil {
				return err
			}
		}
	}

	if !c.valid {
		return fmt.Errorf("container %s is not valid: %w", c.ID(), define.ErrCtrRemoved)
	}

	return nil
}

func (c *Container) setupStorageMapping(dest, from *storage.IDMappingOptions) {
	*dest = *from
	// If we are creating a container inside a pod, we always want to inherit the
	// userns settings from the infra container. So clear the auto userns settings
	// so that we don't request storage for a new uid/gid map.
	if c.PodID() != "" && !c.IsInfra() {
		dest.AutoUserNs = false
	}
	if dest.AutoUserNs {
		overrides := c.getUserOverrides()
		dest.AutoUserNsOpts.PasswdFile = overrides.ContainerEtcPasswdPath
		dest.AutoUserNsOpts.GroupFile = overrides.ContainerEtcGroupPath
		if c.config.User != "" {
			initialSize := uint32(0)
			parts := strings.SplitSeq(c.config.User, ":")
			for p := range parts {
				s, err := strconv.ParseUint(p, 10, 32)
				if err == nil && uint32(s) > initialSize {
					initialSize = uint32(s)
				}
			}
			dest.AutoUserNsOpts.InitialSize = initialSize + 1
		}
	} else if c.config.Spec.Linux != nil {
		dest.UIDMap = nil
		for _, r := range c.config.Spec.Linux.UIDMappings {
			u := idtools.IDMap{
				ContainerID: int(r.ContainerID),
				HostID:      int(r.HostID),
				Size:        int(r.Size),
			}
			dest.UIDMap = append(dest.UIDMap, u)
		}
		dest.GIDMap = nil
		for _, r := range c.config.Spec.Linux.GIDMappings {
			g := idtools.IDMap{
				ContainerID: int(r.ContainerID),
				HostID:      int(r.HostID),
				Size:        int(r.Size),
			}
			dest.GIDMap = append(dest.GIDMap, g)
		}
		dest.HostUIDMapping = false
		dest.HostGIDMapping = false
	}
}

// Create container root filesystem for use
func (c *Container) setupStorage(ctx context.Context) error {
	if !c.valid {
		return fmt.Errorf("container %s is not valid: %w", c.ID(), define.ErrCtrRemoved)
	}

	if c.state.State != define.ContainerStateConfigured {
		return fmt.Errorf("container %s must be in Configured state to have storage set up: %w", c.ID(), define.ErrCtrStateInvalid)
	}

	// Need both an image ID and image name, plus a bool telling us whether to use the image configuration
	if c.config.Rootfs == "" && (c.config.RootfsImageID == "" || c.config.RootfsImageName == "") {
		return fmt.Errorf("must provide image ID and image name to use an image: %w", define.ErrInvalidArg)
	}
	options := storage.ContainerOptions{
		IDMappingOptions: storage.IDMappingOptions{
			HostUIDMapping: true,
			HostGIDMapping: true,
		},
		LabelOpts: c.config.LabelOpts,
	}

	options.StorageOpt = c.config.StorageOpts

	if c.restoreFromCheckpoint && c.config.ProcessLabel != "" && c.config.MountLabel != "" {
		// If restoring from a checkpoint, the root file-system needs
		// to be mounted with the same SELinux labels as it was mounted
		// previously. But only if both labels have been set. For
		// privileged containers or '--ipc host' only ProcessLabel will
		// be set and so we will skip it for cases like that.
		if options.Flags == nil {
			options.Flags = make(map[string]any)
		}
		options.Flags["ProcessLabel"] = c.config.ProcessLabel
		options.Flags["MountLabel"] = c.config.MountLabel
	}
	if c.config.Privileged {
		privOpt := func(opt string) bool {
			return slices.Contains([]string{"nodev", "nosuid", "noexec"}, opt)
		}

		defOptions, err := storage.GetMountOptions(c.runtime.store.GraphDriverName(), c.runtime.store.GraphOptions())
		if err != nil {
			return fmt.Errorf("getting default mount options: %w", err)
		}
		var newOptions []string
		for _, opt := range defOptions {
			if !privOpt(opt) {
				newOptions = append(newOptions, opt)
			}
		}
		options.MountOpts = newOptions
	}

	options.Volatile = c.config.Volatile

	c.setupStorageMapping(&options.IDMappingOptions, &c.config.IDMappings)

	// Unless the user has specified a name, use a randomly generated one.
	// Note that name conflicts may occur (see #11735), so we need to loop.
	generateName := c.config.Name == ""
	var containerInfo ContainerInfo
	var containerInfoErr error
	for {
		if generateName {
			name, err := c.runtime.generateName()
			if err != nil {
				return err
			}
			c.config.Name = name
		}
		containerInfo, containerInfoErr = c.runtime.storageService.CreateContainerStorage(ctx, c.runtime.imageContext, c.config.RootfsImageName, c.config.RootfsImageID, c.config.Name, c.config.ID, options)

		if !generateName || !errors.Is(containerInfoErr, storage.ErrDuplicateName) {
			break
		}
	}
	if containerInfoErr != nil {
		if errors.Is(containerInfoErr, storage.ErrDuplicateName) {
			if _, err := c.runtime.LookupContainer(c.config.Name); errors.Is(err, define.ErrNoSuchCtr) {
				return fmt.Errorf("creating container storage: %w by an external entity", containerInfoErr)
			}
		}
		return fmt.Errorf("creating container storage: %w", containerInfoErr)
	}

	c.config.IDMappings.UIDMap = containerInfo.UIDMap
	c.config.IDMappings.GIDMap = containerInfo.GIDMap

	processLabel, err := c.processLabel(containerInfo.ProcessLabel)
	if err != nil {
		return err
	}
	c.config.ProcessLabel = processLabel
	c.config.MountLabel = containerInfo.MountLabel
	c.config.StaticDir = containerInfo.Dir
	c.state.RunDir = containerInfo.RunDir

	// Set the default Entrypoint and Command
	if containerInfo.Config != nil {
		// Set CMD in the container to the default configuration only if ENTRYPOINT is not set by the user.
		if c.config.Entrypoint == nil && c.config.Command == nil {
			c.config.Command = containerInfo.Config.Config.Cmd
		}
		if c.config.Entrypoint == nil {
			c.config.Entrypoint = containerInfo.Config.Config.Entrypoint
		}
	}

	artifacts := filepath.Join(c.config.StaticDir, artifactsDir)
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		return fmt.Errorf("creating artifacts directory: %w", err)
	}

	return nil
}

func (c *Container) processLabel(processLabel string) (string, error) {
	if !c.Systemd() && !c.ociRuntime.SupportsKVM() {
		return processLabel, nil
	}
	ctrSpec, err := c.specFromState()
	if err != nil {
		return "", err
	}
	label, ok := ctrSpec.Annotations[define.InspectAnnotationLabel]
	if !ok || !strings.Contains(label, "type:") {
		switch {
		case c.ociRuntime.SupportsKVM():
			return selinux.KVMLabel(processLabel)
		case c.Systemd():
			return selinux.InitLabel(processLabel)
		}
	}
	return processLabel, nil
}

// Tear down a container's storage prior to removal
func (c *Container) teardownStorage() error {
	if c.ensureState(define.ContainerStateRunning, define.ContainerStatePaused) {
		return fmt.Errorf("cannot remove storage for container %s as it is running or paused: %w", c.ID(), define.ErrCtrStateInvalid)
	}

	artifacts := filepath.Join(c.config.StaticDir, artifactsDir)
	if err := os.RemoveAll(artifacts); err != nil {
		return fmt.Errorf("removing container %s artifacts %q: %w", c.ID(), artifacts, err)
	}

	if err := c.cleanupStorage(); err != nil {
		return fmt.Errorf("failed to clean up container %s storage: %w", c.ID(), err)
	}

	if err := c.runtime.storageService.DeleteContainer(c.ID()); err != nil {
		// If the container has already been removed, warn but do not
		// error - we wanted it gone, it is already gone.
		// Potentially another tool using containers/storage already
		// removed it?
		if errors.Is(err, storage.ErrNotAContainer) || errors.Is(err, storage.ErrContainerUnknown) {
			logrus.Infof("Storage for container %s already removed", c.ID())
			return nil
		}

		return fmt.Errorf("removing container %s root filesystem: %w", c.ID(), err)
	}

	return nil
}

// Reset resets state fields to default values.
// It is performed before a refresh and clears the state after a reboot.
// It does not save the results - assumes the database will do that for us.
func resetContainerState(state *ContainerState) {
	state.PID = 0
	state.ConmonPID = 0
	state.Mountpoint = ""
	state.Mounted = false
	// Reset state.
	// Almost all states are reset to either Configured or Exited,
	// except ContainerStateRemoving which is preserved.
	switch state.State {
	case define.ContainerStateStopped, define.ContainerStateExited, define.ContainerStateStopping, define.ContainerStateRunning, define.ContainerStatePaused:
		// All containers that ran at any point during the last boot
		// must be placed in the Exited state.
		state.State = define.ContainerStateExited
	case define.ContainerStateConfigured, define.ContainerStateCreated:
		state.State = define.ContainerStateConfigured
	case define.ContainerStateUnknown:
		// Something really strange must have happened to get us here.
		// Reset to configured, maybe the reboot cleared things up?
		state.State = define.ContainerStateConfigured
	}
	state.ExecSessions = make(map[string]*ExecSession)
	state.LegacyExecSessions = nil
	state.BindMounts = make(map[string]string)
	state.StoppedByUser = false
	state.RestartPolicyMatch = false
	state.RestartCount = 0
	state.Checkpointed = false
	state.Restored = false
	state.CheckpointedTime = time.Time{}
	state.RestoredTime = time.Time{}
	state.CheckpointPath = ""
	state.CheckpointLog = ""
	state.RestoreLog = ""
	state.StartupHCPassed = false
	state.StartupHCSuccessCount = 0
	state.StartupHCFailureCount = 0
	state.HCUnitName = ""
	state.NetNS = ""
	state.NetworkStatus = nil
}

// Refresh refreshes the container's state after a restart.
// Refresh cannot perform any operations that would lock another container.
// We cannot guarantee any other container has a valid lock at the time it is
// running.
func (c *Container) refresh() error {
	// Don't need a full sync, but we do need to update from the database to
	// pick up potentially-missing container state
	if err := c.runtime.state.UpdateContainer(c); err != nil {
		return err
	}

	if !c.valid {
		return fmt.Errorf("container %s is not valid - may have been removed: %w", c.ID(), define.ErrCtrRemoved)
	}

	// We need to get the container's temporary directory from c/storage
	// It was lost in the reboot and must be recreated
	dir, err := c.runtime.storageService.GetRunDir(c.ID())
	if err != nil {
		return fmt.Errorf("retrieving temporary directory for container %s: %w", c.ID(), err)
	}
	c.state.RunDir = dir

	if len(c.config.IDMappings.UIDMap) != 0 || len(c.config.IDMappings.GIDMap) != 0 {
		info, err := os.Stat(c.runtime.config.Engine.TmpDir)
		if err != nil {
			return err
		}
		if err := os.Chmod(c.runtime.config.Engine.TmpDir, info.Mode()|0111); err != nil {
			return err
		}
		root := filepath.Join(c.runtime.config.Engine.TmpDir, "containers-root", c.ID())
		if err := os.MkdirAll(root, 0o755); err != nil {
			return fmt.Errorf("creating userNS tmpdir for container %s: %w", c.ID(), err)
		}
		if err := idtools.SafeChown(root, c.RootUID(), c.RootGID()); err != nil {
			return err
		}
	}

	// We need to pick up a new lock
	lock, err := c.runtime.lockManager.AllocateAndRetrieveLock(c.config.LockID)
	if err != nil {
		return fmt.Errorf("acquiring lock %d for container %s: %w", c.config.LockID, c.ID(), err)
	}
	c.lock = lock

	c.state.NetworkStatus = nil

	// Rewrite the config if necessary.
	// Podman 4.0 uses a new port format in the config.
	// getContainerConfigFromDB() already converted the old ports to the new one
	// but it did not write the config to the db back for performance reasons.
	// If a rewrite must happen the config.rewrite field is set to true.
	if c.config.rewrite {
		// SafeRewriteContainerConfig must be used with care. Make sure to not change config fields by accident.
		if err := c.runtime.state.SafeRewriteContainerConfig(c, "", "", c.config); err != nil {
			return fmt.Errorf("failed to rewrite the config for container %s: %w", c.config.ID, err)
		}
		c.config.rewrite = false
	}

	if err := c.save(); err != nil {
		return fmt.Errorf("refreshing state for container %s: %w", c.ID(), err)
	}

	// Remove ctl and attach files, which may persist across reboot
	if err := c.removeConmonFiles(); err != nil {
		return err
	}

	return nil
}

// Remove conmon attach socket and terminal resize FIFO
// This is necessary for restarting containers
func (c *Container) removeConmonFiles() error {
	// Files are allowed to not exist, so ignore ENOENT
	attachFile, err := c.AttachSocketPath()
	if err != nil {
		return fmt.Errorf("failed to get attach socket path for container %s: %w", c.ID(), err)
	}

	if err := os.Remove(attachFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing container %s attach file: %w", c.ID(), err)
	}

	ctlFile := filepath.Join(c.bundlePath(), "ctl")
	if err := os.Remove(ctlFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing container %s ctl file: %w", c.ID(), err)
	}

	winszFile := filepath.Join(c.bundlePath(), "winsz")
	if err := os.Remove(winszFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing container %s winsz file: %w", c.ID(), err)
	}

	// Remove the exit file so we don't leak memory in tmpfs
	exitFile, err := c.exitFilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(exitFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing container %s exit file: %w", c.ID(), err)
	}

	// Remove the persist directory
	persistDir, err := c.persistDirPath()
	if err != nil {
		return err
	}
	if persistDir != "" {
		if err := os.RemoveAll(persistDir); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("removing container %s persist directory: %w", c.ID(), err)
		}
	}

	return nil
}

func (c *Container) export(out io.Writer) error {
	mountPoint := c.state.Mountpoint
	if !c.state.Mounted {
		containerMount, err := c.runtime.store.Mount(c.ID(), c.config.MountLabel)
		if err != nil {
			return fmt.Errorf("mounting container %q: %w", c.ID(), err)
		}
		mountPoint = containerMount
		defer func() {
			if _, err := c.runtime.store.Unmount(c.ID(), false); err != nil {
				logrus.Errorf("Unmounting container %q: %v", c.ID(), err)
			}
		}()
	}

	input, err := chrootarchive.Tar(mountPoint, nil, mountPoint)
	if err != nil {
		return fmt.Errorf("reading container directory %q: %w", c.ID(), err)
	}

	_, err = io.Copy(out, input)
	return err
}

// Get path of artifact with a given name for this container
func (c *Container) getArtifactPath(name string) string {
	return filepath.Join(c.config.StaticDir, artifactsDir, name)
}

// save container state to the database
func (c *Container) save() error {
	if err := c.runtime.state.SaveContainer(c); err != nil {
		return fmt.Errorf("saving container %s state: %w", c.ID(), err)
	}
	return nil
}

// Checks the container is in the right state, then initializes the container in preparation to start the container.
// If recursive is true, each of the container's dependencies will be started.
// Otherwise, this function will return with error if there are dependencies of this container that aren't running.
func (c *Container) prepareToStart(ctx context.Context, recursive bool) (retErr error) {
	// Container must be created or stopped to be started
	if !c.ensureState(define.ContainerStateConfigured, define.ContainerStateCreated, define.ContainerStateStopped, define.ContainerStateExited) {
		// Special case: Let the caller know that is is already running,
		// the caller can then decide to ignore/handle the error the way it needs.
		if c.state.State == define.ContainerStateRunning {
			return fmt.Errorf("container %s: %w", c.ID(), define.ErrCtrStateRunning)
		}
		return fmt.Errorf("container %s must be in Created or Stopped state to be started: %w", c.ID(), define.ErrCtrStateInvalid)
	}

	if !recursive {
		if err := c.checkDependenciesAndHandleError(); err != nil {
			return err
		}
	} else {
		if err := c.startDependencies(ctx); err != nil {
			return err
		}
	}

	defer func() {
		if retErr != nil {
			if err := c.cleanup(ctx); err != nil {
				logrus.Errorf("Cleaning up container %s: %v", c.ID(), err)
			}
		}
	}()

	if err := c.prepare(); err != nil {
		return err
	}

	if c.state.State == define.ContainerStateStopped {
		// Reinitialize the container if we need to
		if err := c.reinit(ctx, false); err != nil {
			return err
		}
	} else if c.ensureState(define.ContainerStateConfigured, define.ContainerStateExited) {
		// Or initialize it if necessary
		if err := c.init(ctx, false); err != nil {
			return err
		}
	}
	return nil
}

// checks dependencies are running and prints a helpful message
func (c *Container) checkDependenciesAndHandleError() error {
	notRunning, err := c.checkDependenciesRunning()
	if err != nil {
		return fmt.Errorf("checking dependencies for container %s: %w", c.ID(), err)
	}
	if len(notRunning) > 0 {
		depString := strings.Join(notRunning, ",")
		return fmt.Errorf("some dependencies of container %s are not started: %s: %w", c.ID(), depString, define.ErrCtrStateInvalid)
	}

	return nil
}

// Recursively start all dependencies of a container so the container can be started.
func (c *Container) startDependencies(ctx context.Context) error {
	depCtrIDs := c.Dependencies()
	if len(depCtrIDs) == 0 {
		return nil
	}

	depVisitedCtrs := make(map[string]*Container)
	if err := c.getAllDependencies(depVisitedCtrs); err != nil {
		return fmt.Errorf("starting dependency for container %s: %w", c.ID(), err)
	}

	// Because of how Go handles passing slices through functions, a slice cannot grow between function calls
	// without clunky syntax. Circumnavigate this by translating the map to a slice for buildContainerGraph
	depCtrs := make([]*Container, 0)
	for _, ctr := range depVisitedCtrs {
		depCtrs = append(depCtrs, ctr)
	}

	// Build a dependency graph of containers
	graph, err := BuildContainerGraph(depCtrs)
	if err != nil {
		return fmt.Errorf("generating dependency graph for container %s: %w", c.ID(), err)
	}

	// If there are no containers without dependencies, we can't start
	// Error out
	if len(graph.noDepNodes) == 0 {
		// we have no dependencies that need starting, go ahead and return
		if len(graph.nodes) == 0 {
			return nil
		}
		return fmt.Errorf("all dependencies have dependencies of %s: %w", c.ID(), define.ErrNoSuchCtr)
	}

	ctrErrors := make(map[string]error)
	ctrsVisited := make(map[string]bool)

	// Traverse the graph beginning at nodes with no dependencies
	for _, node := range graph.noDepNodes {
		startNode(ctx, node, false, ctrErrors, ctrsVisited, false)
	}

	if len(ctrErrors) > 0 {
		logrus.Errorf("Starting some container dependencies")
		for _, e := range ctrErrors {
			logrus.Errorf("%q", e)
		}
		return fmt.Errorf("starting some containers: %w", define.ErrInternal)
	}
	return nil
}

// getAllDependencies is a precursor to starting dependencies.
// To start a container with all of its dependencies, we need to recursively find all dependencies
// a container has, as well as each of those containers' dependencies, and so on
// To do so, keep track of containers already visited (so there aren't redundant state lookups),
// and recursively search until we have reached the leafs of every dependency node.
// Since we need to start all dependencies for our original container to successfully start, we propagate any errors
// in looking up dependencies.
// Note: this function is currently meant as a robust solution to a narrow problem: start an infra-container when
// a container in the pod is run. It has not been tested for performance past one level, so expansion of recursive start
// must be tested first.
func (c *Container) getAllDependencies(visited map[string]*Container) error {
	depIDs := c.Dependencies()
	if len(depIDs) == 0 {
		return nil
	}
	for _, depID := range depIDs {
		if _, ok := visited[depID]; !ok {
			dep, err := c.runtime.state.Container(depID)
			if err != nil {
				return err
			}
			visited[depID] = dep
			if err := dep.getAllDependencies(visited); err != nil {
				return err
			}
		}
	}
	return nil
}

// Check if a container's dependencies are running
// Returns a []string containing the IDs of dependencies that are not running
func (c *Container) checkDependenciesRunning() ([]string, error) {
	deps := c.Dependencies()
	notRunning := []string{}

	// We were not passed a set of dependency containers
	// Make it ourselves
	depCtrs := make(map[string]*Container, len(deps))
	for _, dep := range deps {
		// Get the dependency container
		depCtr, err := c.runtime.state.Container(dep)
		if err != nil {
			return nil, fmt.Errorf("retrieving dependency %s of container %s from state: %w", dep, c.ID(), err)
		}

		// Check the status
		state, err := depCtr.State()
		if err != nil {
			return nil, fmt.Errorf("retrieving state of dependency %s of container %s: %w", dep, c.ID(), err)
		}
		if state != define.ContainerStateRunning && !depCtr.config.IsInfra {
			notRunning = append(notRunning, dep)
		}
		depCtrs[dep] = depCtr
	}

	return notRunning, nil
}

func (c *Container) completeNetworkSetup() error {
	netDisabled, err := c.NetworkDisabled()
	if err != nil {
		return err
	}
	if netDisabled {
		// with net=none we still want to set up /etc/hosts
		return c.addHosts()
	}
	if c.config.NetNsCtr != "" {
		return nil
	}
	if c.config.PostConfigureNetNS {
		if err := c.syncContainer(); err != nil {
			return err
		}
		if err := c.runtime.setupNetNS(c); err != nil {
			return err
		}
		if err := c.save(); err != nil {
			return err
		}
	}
	// add /etc/hosts entries
	if err := c.addHosts(); err != nil {
		return err
	}

	return c.addResolvConf()
}

// Initialize a container, creating it in the runtime
func (c *Container) init(ctx context.Context, retainRetries bool) error {
	// Unconditionally remove conmon temporary files.
	// We've been running into far too many issues where they block startup.
	if err := c.removeConmonFiles(); err != nil {
		return err
	}

	// Generate the OCI newSpec
	newSpec, cleanupFunc, err := c.generateSpec(ctx)
	if err != nil {
		return err
	}
	defer cleanupFunc()

	// Make sure the workdir exists while initializing container
	if err := c.resolveWorkDir(); err != nil {
		return err
	}

	// Save the OCI newSpec to disk
	if err := c.saveSpec(newSpec); err != nil {
		return err
	}

	for _, v := range c.config.NamedVolumes {
		if err := c.fixVolumePermissions(v); err != nil {
			return err
		}
	}

	// To ensure that we don't lose track of Conmon if hit by a SIGTERM
	// in the middle of setting up the container, inhibit shutdown signals
	// until after we save Conmon's PID to the state.
	// TODO: This can likely be removed once conmon-rs support merges.
	shutdown.Inhibit()
	defer shutdown.Uninhibit()

	// If the container is part of a pod, make sure the pod cgroup is created before the container
	// so the limits can be applied.
	if c.PodID() != "" {
		pod, err := c.runtime.LookupPod(c.PodID())
		if err != nil {
			return err
		}

		if _, err := c.runtime.platformMakePod(pod, &pod.config.ResourceLimits); err != nil {
			return err
		}
	}

	// With the spec complete, do an OCI create
	if _, err = c.ociRuntime.CreateContainer(c, nil); err != nil {
		return err
	}

	logrus.Debugf("Created container %s in OCI runtime", c.ID())

	// Remove any exec sessions leftover from a potential prior run.
	if len(c.state.ExecSessions) > 0 {
		if err := c.runtime.state.RemoveContainerExecSessions(c); err != nil {
			logrus.Errorf("Removing container %s exec sessions from DB: %v", c.ID(), err)
		}
		c.state.ExecSessions = make(map[string]*ExecSession)
	}

	c.state.Checkpointed = false
	c.state.Restored = false
	c.state.CheckpointedTime = time.Time{}
	c.state.RestoredTime = time.Time{}
	c.state.CheckpointPath = ""
	c.state.CheckpointLog = ""
	c.state.RestoreLog = ""
	c.state.ExitCode = 0
	c.state.Exited = false
	// Reset any previous errors as we try to init it again, either it works and we don't
	// want to keep an old error around or a new error will be written anyway.
	c.state.Error = ""
	c.state.State = define.ContainerStateCreated
	c.state.StoppedByUser = false
	c.state.RestartPolicyMatch = false
	c.state.StartupHCFailureCount = 0
	c.state.StartupHCSuccessCount = 0
	c.state.StartupHCPassed = false

	if !retainRetries {
		c.state.RestartCount = 0
	}

	// bugzilla.redhat.com/show_bug.cgi?id=2144754:
	// In case of a restart, make sure to remove the healthcheck log to
	// have a clean state.
	err = c.writeHealthCheckLog(define.HealthCheckResults{Status: define.HealthCheckReset})
	if err != nil {
		return err
	}

	if err := c.save(); err != nil {
		return err
	}

	if c.config.HealthCheckConfig != nil {
		timer := c.config.HealthCheckConfig.Interval.String()
		if c.config.StartupHealthCheckConfig != nil {
			timer = c.config.StartupHealthCheckConfig.Interval.String()
		}
		if err := c.createTimer(timer, c.config.StartupHealthCheckConfig != nil); err != nil {
			return fmt.Errorf("create healthcheck: %w", err)
		}
	}

	defer c.newContainerEvent(events.Init)
	return c.completeNetworkSetup()
}

// Clean up a container in the OCI runtime.
// Deletes the container in the runtime, and resets its state to Exited.
// The container can be restarted cleanly after this.
func (c *Container) cleanupRuntime(ctx context.Context) error {
	// If the container is not ContainerStateStopped or
	// ContainerStateCreated, do nothing.
	if !c.ensureState(define.ContainerStateStopped, define.ContainerStateCreated) {
		return nil
	}

	// We may be doing this redundantly for some call paths but we need to
	// make sure the exit code is being read at this point.
	if err := c.checkExitFile(); err != nil {
		return err
	}

	// If necessary, delete attach and ctl files
	if err := c.removeConmonFiles(); err != nil {
		return err
	}

	if err := c.delete(ctx); err != nil {
		return err
	}

	// If we were Stopped, we are now Exited, as we've removed ourself
	// from the runtime.
	// If we were Created, we are now Configured.
	switch c.state.State {
	case define.ContainerStateStopped:
		c.state.State = define.ContainerStateExited
	case define.ContainerStateCreated:
		c.state.State = define.ContainerStateConfigured
	}

	if c.valid {
		if err := c.save(); err != nil {
			return err
		}
	}

	logrus.Debugf("Successfully cleaned up container %s", c.ID())

	return nil
}

// Reinitialize a container.
// Deletes and recreates a container in the runtime.
// Should only be done on ContainerStateStopped containers.
// Not necessary for ContainerStateExited - the container has already been
// removed from the runtime, so init() can proceed freely.
func (c *Container) reinit(ctx context.Context, retainRetries bool) error {
	logrus.Debugf("Recreating container %s in OCI runtime", c.ID())

	if err := c.cleanupRuntime(ctx); err != nil {
		return err
	}

	// Initialize the container again
	return c.init(ctx, retainRetries)
}

// Initialize (if necessary) and start a container
// Performs all necessary steps to start a container that is not running
// Does not lock or check validity, requires to run on the same thread that holds the lock for the container.
func (c *Container) initAndStart(ctx context.Context) (retErr error) {
	// If we are ContainerState{Unknown,Removing}, throw an error.
	switch c.state.State {
	case define.ContainerStateUnknown:
		return fmt.Errorf("container %s is in an unknown state: %w", c.ID(), define.ErrCtrStateInvalid)
	case define.ContainerStateRemoving:
		return fmt.Errorf("cannot start container %s as it is being removed: %w", c.ID(), define.ErrCtrStateInvalid)
	}

	// If we are running, do nothing
	if c.state.State == define.ContainerStateRunning {
		return nil
	}
	// If we are paused, throw an error
	if c.state.State == define.ContainerStatePaused {
		return fmt.Errorf("cannot start paused container %s: %w", c.ID(), define.ErrCtrStateInvalid)
	}

	defer func() {
		if retErr != nil {
			if err := c.cleanup(ctx); err != nil {
				logrus.Errorf("Cleaning up container %s: %v", c.ID(), err)
			}
		}
	}()

	if err := c.prepare(); err != nil {
		return err
	}

	// If we are ContainerStateStopped we need to remove from runtime
	// And reset to ContainerStateConfigured
	if c.state.State == define.ContainerStateStopped {
		logrus.Debugf("Recreating container %s in OCI runtime", c.ID())

		if err := c.reinit(ctx, false); err != nil {
			return err
		}
	} else if c.ensureState(define.ContainerStateConfigured, define.ContainerStateExited) {
		if err := c.init(ctx, false); err != nil {
			return err
		}
	}

	// Now start the container
	if err := c.start(); err != nil {
		return err
	}
	return c.waitForHealthy(ctx)
}

// Internal function to start a container without taking the pod lock.
// Please note that this DOES take the container lock.
// Intended to be used in pod-related functions.
func (c *Container) startNoPodLock(ctx context.Context, recursive bool) (finalErr error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		// defer's are executed LIFO so we are locked here
		// as long as we call this after the defer unlock()
		defer func() {
			if finalErr != nil {
				if err := saveContainerError(c, finalErr); err != nil {
					logrus.Debug(err)
				}
			}
		}()

		if err := c.syncContainer(); err != nil {
			return err
		}
	}

	if err := c.prepareToStart(ctx, recursive); err != nil {
		return err
	}

	// Start the container
	if err := c.start(); err != nil {
		return err
	}
	return c.waitForHealthy(ctx)
}

// Internal, non-locking function to start a container
func (c *Container) start() error {
	if c.config.Spec.Process != nil {
		logrus.Debugf("Starting container %s with command %v", c.ID(), c.config.Spec.Process.Args)
	}

	if err := c.ociRuntime.StartContainer(c); err != nil {
		return err
	}
	logrus.Debugf("Started container %s", c.ID())

	c.state.State = define.ContainerStateRunning

	// Unless being ignored, set the MAINPID to conmon.
	if c.config.SdNotifyMode != define.SdNotifyModeIgnore {
		payload := fmt.Sprintf("MAINPID=%d", c.state.ConmonPID)
		if c.config.SdNotifyMode == define.SdNotifyModeConmon {
			// Also send the READY message for the "conmon" policy.
			payload += "\n"
			payload += daemon.SdNotifyReady
		}
		if err := notifyproxy.SendMessage(c.config.SdNotifySocket, payload); err != nil {
			logrus.Errorf("Notifying systemd of Conmon PID: %s", err.Error())
		} else {
			logrus.Debugf("Notify sent successfully")
		}
	}

	// Check if healthcheck is not nil and --no-healthcheck option is not set.
	// If --no-healthcheck is set Test will be always set to `[NONE]` so no need
	// to update status in such case.
	if c.config.HealthCheckConfig != nil && (len(c.config.HealthCheckConfig.Test) != 1 || c.config.HealthCheckConfig.Test[0] != "NONE") {
		if err := c.updateHealthStatus(define.HealthCheckStarting); err != nil {
			return fmt.Errorf("update healthcheck status: %w", err)
		}
		if err := c.startTimer(c.config.StartupHealthCheckConfig != nil); err != nil {
			return fmt.Errorf("start healthcheck: %w", err)
		}
	}

	c.newContainerEvent(events.Start)

	return c.save()
}

// waitForHealthy, when sdNotifyMode == SdNotifyModeHealthy, waits up to the DefaultWaitInterval
// for the container to get into the healthy state and reports the status to the notify socket.
// The function unlocks the container lock, so it must be called from the same thread that locks
// the container.
func (c *Container) waitForHealthy(ctx context.Context) error {
	if c.config.SdNotifyMode != define.SdNotifyModeHealthy {
		return nil
	}

	// Wait for the container to turn healthy before sending the READY
	// message.  This implies that we need to unlock and re-lock the
	// container.
	if !c.batched {
		c.lock.Unlock()
		defer c.lock.Lock()
	}

	if _, err := c.WaitForConditionWithInterval(ctx, DefaultWaitInterval, define.HealthCheckHealthy); err != nil {
		if errors.Is(err, define.ErrNoSuchCtr) {
			return nil
		}
		return err
	}

	if err := notifyproxy.SendMessage(c.config.SdNotifySocket, daemon.SdNotifyReady); err != nil {
		logrus.Errorf("Sending READY message after turning healthy: %s", err.Error())
	} else {
		logrus.Debugf("Notify sent successfully")
	}
	return nil
}

// Whether a container should use `all` when stopping
func (c *Container) stopWithAll() (bool, error) {
	// If the container is running in a PID Namespace, then killing the
	// primary pid is enough to kill the container.  If it is not running in
	// a pid namespace then the OCI Runtime needs to kill ALL processes in
	// the container's cgroup in order to make sure the container is stopped.
	all := !c.hasNamespace(spec.PIDNamespace)
	// We can't use --all if Cgroups aren't present.
	// Rootless containers with Cgroups v1 and NoCgroups are both cases
	// where this can happen.
	if all {
		if c.config.NoCgroups {
			all = false
		} else if rootless.IsRootless() {
			// Only do this check if we need to
			unified, err := cgroups.IsCgroup2UnifiedMode()
			if err != nil {
				return false, err
			}
			if !unified {
				all = false
			}
		}
	}

	return all, nil
}

// Internal, non-locking function to stop container
func (c *Container) stop(timeout uint) error {
	logrus.Debugf("Stopping ctr %s (timeout %d)", c.ID(), timeout)

	all, err := c.stopWithAll()
	if err != nil {
		return err
	}

	// OK, the following code looks a bit weird but we have to make sure we can stop
	// containers with the restart policy always, to do this we have to set
	// StoppedByUser even when there is nothing to stop right now. This is due to the
	// cleanup process waiting on the container lock and then afterwards restarts it.
	// shouldRestart() then checks for StoppedByUser and does not restart it.
	// https://github.com/containers/podman/issues/18259
	var cannotStopErr error
	if c.ensureState(define.ContainerStateStopped, define.ContainerStateExited) {
		cannotStopErr = define.ErrCtrStopped
	} else if !c.ensureState(define.ContainerStateCreated, define.ContainerStateRunning, define.ContainerStateStopping) {
		cannotStopErr = fmt.Errorf("can only stop created or running containers. %s is in state %s: %w", c.ID(), c.state.State.String(), define.ErrCtrStateInvalid)
	}

	c.state.StoppedByUser = true
	if cannotStopErr == nil {
		// Set the container state to "stopping" and unlock the container
		// before handing it over to conmon to unblock other commands.  #8501
		// demonstrates nicely that a high stop timeout will block even simple
		// commands such as `podman ps` from progressing if the container lock
		// is held when busy-waiting for the container to be stopped.
		c.state.State = define.ContainerStateStopping
	}
	if err := c.save(); err != nil {
		rErr := fmt.Errorf("saving container %s state before stopping: %w", c.ID(), err)
		if cannotStopErr == nil {
			return rErr
		}
		// we return below with cannotStopErr
		logrus.Error(rErr)
	}
	if cannotStopErr != nil {
		return cannotStopErr
	}
	if !c.batched {
		c.lock.Unlock()
	}

	stopErr := c.ociRuntime.StopContainer(c, timeout, all)

	if !c.batched {
		c.lock.Lock()
		if err := c.syncContainer(); err != nil {
			if errors.Is(err, define.ErrNoSuchCtr) || errors.Is(err, define.ErrCtrRemoved) {
				// If the container has already been removed (e.g., via
				// the cleanup process), set the container state to "stopped".
				c.state.State = define.ContainerStateStopped
				return stopErr
			}

			if stopErr != nil {
				logrus.Errorf("Syncing container %s status: %v", c.ID(), err)
				return stopErr
			}
			return err
		}
	}

	// We have to check stopErr *after* we lock again - otherwise, we have a
	// change of panicking on a double-unlock. Ref: GH Issue 9615
	if stopErr != nil {
		return stopErr
	}

	// Since we're now subject to a race condition with other processes who
	// may have altered the state (and other data), let's check if the
	// state has changed.  If so, we should return immediately and leave
	// breadcrumbs for debugging if needed.
	if c.state.State != define.ContainerStateStopping {
		logrus.Debugf(
			"Container %q state changed from %q to %q while waiting for it to be stopped: discontinuing stop procedure as another process interfered",
			c.ID(), define.ContainerStateStopping, c.state.State,
		)
		return nil
	}

	c.newContainerEvent(events.Stop)
	return c.waitForConmonToExitAndSave()
}

func (c *Container) waitForConmonToExitAndSave() error {
	conmonAlive, err := c.ociRuntime.CheckConmonRunning(c)
	if err != nil {
		return err
	}
	if !conmonAlive {
		if err := c.checkExitFile(); err != nil {
			return err
		}

		// If we are still ContainerStateStopping, conmon exited without
		// creating an exit file. Let's try and handle that here.
		if c.state.State == define.ContainerStateStopping {
			// Is container PID1 still alive?
			if err := unix.Kill(c.state.PID, 0); err == nil {
				// We have a runaway container, unmanaged by
				// Conmon. Invoke OCI runtime stop.
				// Use 0 timeout for immediate SIGKILL as things
				// have gone seriously wrong.
				// Ignore the error from stopWithAll, it's just
				// a cgroup check - more important that we
				// continue.
				// If we wanted to be really fancy here, we
				// could open a pidfd on container PID1 before
				// this to get the real exit code... But I'm not
				// that dedicated.
				all, _ := c.stopWithAll()
				if err := c.ociRuntime.StopContainer(c, 0, all); err != nil {
					logrus.Errorf("Error stopping container %s after Conmon exited prematurely: %v", c.ID(), err)
				}
			}

			// Conmon is dead. Handle it.
			c.state.State = define.ContainerStateStopped
			c.state.PID = 0
			c.state.ConmonPID = 0
			c.state.FinishedTime = time.Now()
			c.state.ExitCode = -1
			c.state.Exited = true

			c.state.Error = "conmon died without writing exit file, container exit code could not be retrieved"

			c.newContainerExitedEvent(c.state.ExitCode)

			if err := c.save(); err != nil {
				logrus.Errorf("Error saving container %s state after Conmon exited prematurely: %v", c.ID(), err)
			}

			if err := c.runtime.state.AddContainerExitCode(c.ID(), c.state.ExitCode); err != nil {
				logrus.Errorf("Error saving container %s exit code after Conmon exited prematurely: %v", c.ID(), err)
			}

			// No Conmon alive to trigger cleanup, and the calls in
			// regular Podman are conditional on no errors.
			// Need to clean up manually.
			if err := c.cleanup(context.Background()); err != nil {
				logrus.Errorf("Error cleaning up container %s after Conmon exited prematurely: %v", c.ID(), err)
			}

			return fmt.Errorf("container %s conmon exited prematurely, exit code could not be retrieved: %w", c.ID(), define.ErrConmonDead)
		}

		return c.save()
	}

	if err := c.save(); err != nil {
		return fmt.Errorf("saving container %s state after stopping: %w", c.ID(), err)
	}

	// Wait until we have an exit file, and sync once we do
	if err := c.waitForExitFileAndSync(); err != nil {
		return err
	}

	return nil
}

// Internal, non-locking function to pause a container
func (c *Container) pause() error {
	if c.config.NoCgroups {
		return fmt.Errorf("cannot pause without using Cgroups: %w", define.ErrNoCgroups)
	}

	if rootless.IsRootless() {
		cgroupv2, err := cgroups.IsCgroup2UnifiedMode()
		if err != nil {
			return fmt.Errorf("failed to determine cgroupversion: %w", err)
		}
		if !cgroupv2 {
			return fmt.Errorf("can not pause containers on rootless containers with cgroup V1: %w", define.ErrNoCgroups)
		}
	}

	if c.state.HCUnitName != "" {
		if err := c.removeTransientFiles(context.Background(),
			c.config.StartupHealthCheckConfig != nil && !c.state.StartupHCPassed,
			c.state.HCUnitName); err != nil {
			return fmt.Errorf("failed to remove HealthCheck timer: %v", err)
		}
	}

	if err := c.ociRuntime.PauseContainer(c); err != nil {
		// TODO when using docker-py there is some sort of race/incompatibility here
		return err
	}

	logrus.Debugf("Paused container %s", c.ID())

	c.state.State = define.ContainerStatePaused
	c.state.HCUnitName = ""

	return c.save()
}

// Internal, non-locking function to unpause a container
func (c *Container) unpause() error {
	if c.config.NoCgroups {
		return fmt.Errorf("cannot unpause without using Cgroups: %w", define.ErrNoCgroups)
	}

	if err := c.ociRuntime.UnpauseContainer(c); err != nil {
		// TODO when using docker-py there is some sort of race/incompatibility here
		return err
	}

	isStartupHealthCheck := c.config.StartupHealthCheckConfig != nil && !c.state.StartupHCPassed
	isHealthCheckEnabled := c.config.HealthCheckConfig != nil &&
		(len(c.config.HealthCheckConfig.Test) != 1 || c.config.HealthCheckConfig.Test[0] != "NONE")
	if isHealthCheckEnabled || isStartupHealthCheck {
		timer := c.config.HealthCheckConfig.Interval.String()
		if isStartupHealthCheck {
			timer = c.config.StartupHealthCheckConfig.Interval.String()
		}
		if err := c.createTimer(timer, isStartupHealthCheck); err != nil {
			return fmt.Errorf("create healthcheck: %w", err)
		}
	}

	if isHealthCheckEnabled {
		if err := c.updateHealthStatus(define.HealthCheckReset); err != nil {
			return err
		}
		if err := c.startTimer(isStartupHealthCheck); err != nil {
			return err
		}
	}

	logrus.Debugf("Unpaused container %s", c.ID())

	c.state.State = define.ContainerStateRunning

	return c.save()
}

// Internal, non-locking function to restart a container
// It requires to run on the same thread that holds the lock.
func (c *Container) restartWithTimeout(ctx context.Context, timeout uint) (retErr error) {
	if !c.ensureState(define.ContainerStateConfigured, define.ContainerStateCreated, define.ContainerStateRunning, define.ContainerStateStopped, define.ContainerStateExited) {
		return fmt.Errorf("unable to restart a container in a paused or unknown state: %w", define.ErrCtrStateInvalid)
	}

	c.newContainerEvent(events.Restart)

	if c.state.State == define.ContainerStateRunning {
		if err := c.stop(timeout); err != nil {
			return err
		}

		if c.config.HealthCheckConfig != nil {
			if err := c.removeTransientFiles(context.Background(),
				c.config.StartupHealthCheckConfig != nil && !c.state.StartupHCPassed,
				c.state.HCUnitName); err != nil {
				logrus.Error(err.Error())
			}
		}
		// Ensure we tear down the container network so it will be
		// recreated - otherwise, behavior of restart differs from stop
		// and start
		if err := c.cleanupNetwork(); err != nil {
			return err
		}
	}
	defer func() {
		if retErr != nil {
			if err := c.cleanup(ctx); err != nil {
				logrus.Errorf("Cleaning up container %s: %v", c.ID(), err)
			}
		}
	}()
	if err := c.prepare(); err != nil {
		return err
	}

	switch c.state.State {
	case define.ContainerStateStopped:
		// Reinitialize the container if we need to.
		if err := c.reinit(ctx, false); err != nil {
			return err
		}
	case define.ContainerStateConfigured, define.ContainerStateExited:
		// Initialize the container.
		if err := c.init(ctx, false); err != nil {
			return err
		}
	}
	if err := c.start(); err != nil {
		return err
	}
	return c.waitForHealthy(ctx)
}

// mountStorage sets up the container's root filesystem
// It mounts the image and any other requested mounts
// TODO: Add ability to override mount label so we can use this for Mount() too
// TODO: Can we use this for export? Copying SHM into the export might not be
// good
func (c *Container) mountStorage() (_ string, deferredErr error) {
	var err error
	// Container already mounted, nothing to do
	if c.state.Mounted {
		mounted := true
		if c.ensureState(define.ContainerStateExited) {
			mounted, _ = mount.Mounted(c.state.Mountpoint)
		}
		if mounted {
			return c.state.Mountpoint, nil
		}
	}

	if !c.config.NoShm {
		mounted, err := mount.Mounted(c.config.ShmDir)
		if err != nil {
			return "", fmt.Errorf("unable to determine if %q is mounted: %w", c.config.ShmDir, err)
		}

		if !mounted && !MountExists(c.config.Spec.Mounts, "/dev/shm") {
			shmOptions := fmt.Sprintf("mode=1777,size=%d", c.config.ShmSize)
			if err := c.mountSHM(shmOptions); err != nil {
				return "", err
			}
			if err := idtools.SafeChown(c.config.ShmDir, c.RootUID(), c.RootGID()); err != nil {
				return "", fmt.Errorf("failed to chown %s: %w", c.config.ShmDir, err)
			}
			defer func() {
				if deferredErr != nil {
					if err := c.unmountSHM(c.config.ShmDir); err != nil {
						logrus.Errorf("Unmounting SHM for container %s after mount error: %v", c.ID(), err)
					}
				}
			}()
		}
	}

	// We need to mount the container before volumes - to ensure the copyup
	// works properly.
	mountPoint := c.config.Rootfs

	if c.config.RootfsMapping != nil {
		uidMappings, gidMappings, err := parseIDMapMountOption(c.config.IDMappings, *c.config.RootfsMapping)
		if err != nil {
			return "", err
		}

		pid, cleanupFunc, err := idmap.CreateUsernsProcess(util.RuntimeSpecToIDtools(uidMappings), util.RuntimeSpecToIDtools(gidMappings))
		if err != nil {
			return "", err
		}
		defer cleanupFunc()

		if err := idmap.CreateIDMappedMount(c.config.Rootfs, c.config.Rootfs, pid); err != nil {
			return "", fmt.Errorf("failed to create idmapped mount: %w", err)
		}
		defer func() {
			if deferredErr != nil {
				if err := unix.Unmount(c.config.Rootfs, 0); err != nil {
					logrus.Errorf("Unmounting idmapped rootfs for container %s after mount error: %v", c.ID(), err)
				}
			}
		}()
	}

	// Check if overlay has to be created on top of Rootfs
	if c.config.RootfsOverlay {
		overlayDest := c.runtime.GraphRoot()
		contentDir, err := overlay.GenerateStructure(overlayDest, c.ID(), "rootfs", c.RootUID(), c.RootGID())
		if err != nil {
			return "", fmt.Errorf("rootfs-overlay: failed to create TempDir in the %s directory: %w", overlayDest, err)
		}

		// Recreate the rootfs for infra container. It can be missing after system reboot if it's stored on tmpfs.
		if c.IsDefaultInfra() || c.IsService() {
			err := c.createInitRootfs()
			if err != nil {
				return "", err
			}
		}

		overlayMount, err := overlay.Mount(contentDir, c.config.Rootfs, overlayDest, c.RootUID(), c.RootGID(), c.runtime.store.GraphOptions())
		if err != nil {
			return "", fmt.Errorf("rootfs-overlay: creating overlay failed %q: %w", c.config.Rootfs, err)
		}

		// Seems fuse-overlayfs is not present
		// fallback to native overlay
		if overlayMount.Type == "overlay" {
			overlayMount.Options = append(overlayMount.Options, "nodev")
			mountOpts := label.FormatMountLabel(strings.Join(overlayMount.Options, ","), c.MountLabel())
			err = mount.Mount("overlay", overlayMount.Source, overlayMount.Type, mountOpts)
			if err != nil {
				return "", fmt.Errorf("rootfs-overlay: creating overlay failed %q from native overlay: %w", c.config.Rootfs, err)
			}
		}

		mountPoint = overlayMount.Source
		execUser, err := lookup.GetUserGroupInfo(mountPoint, c.config.User, nil)
		if err != nil {
			return "", err
		}
		hostUID, hostGID, err := butil.GetHostIDs(util.IDtoolsToRuntimeSpec(c.config.IDMappings.UIDMap), util.IDtoolsToRuntimeSpec(c.config.IDMappings.GIDMap), uint32(execUser.Uid), uint32(execUser.Gid))
		if err != nil {
			return "", fmt.Errorf("unable to get host UID and host GID: %w", err)
		}

		//note: this should not be recursive, if using external rootfs users should be responsible on configuring ownership.
		if err := chown.ChangeHostPathOwnership(mountPoint, false, int(hostUID), int(hostGID)); err != nil {
			return "", err
		}
	}

	if mountPoint == "" {
		mountPoint, err = c.mount()
		if err != nil {
			return "", err
		}
		defer func() {
			if deferredErr != nil {
				if err := c.unmount(false); err != nil {
					logrus.Errorf("Unmounting container %s after mount error: %v", c.ID(), err)
				}
			}
		}()
	}

	rootUID, rootGID := c.RootUID(), c.RootGID()

	dirfd, err := openDirectory(mountPoint)
	if err != nil {
		return "", fmt.Errorf("open mount point: %w", err)
	}
	defer unix.Close(dirfd)

	err = unix.Mkdirat(dirfd, "etc", 0o755)
	if err != nil && !os.IsExist(err) {
		return "", fmt.Errorf("create /etc: %w", err)
	}
	// If the etc directory was created, chown it to root in the container
	if err == nil && (rootUID != 0 || rootGID != 0) {
		err = unix.Fchownat(dirfd, "etc", rootUID, rootGID, unix.AT_SYMLINK_NOFOLLOW)
		if err != nil {
			return "", fmt.Errorf("chown /etc: %w", err)
		}
	}

	etcInTheContainerPath, err := securejoin.SecureJoin(mountPoint, "etc")
	if err != nil {
		return "", fmt.Errorf("resolve /etc in the container: %w", err)
	}

	etcInTheContainerFd, err := openDirectory(etcInTheContainerPath)
	if err != nil {
		return "", fmt.Errorf("open /etc in the container: %w", err)
	}
	defer unix.Close(etcInTheContainerFd)

	if err := c.makePlatformMtabLink(etcInTheContainerFd, rootUID, rootGID); err != nil {
		return "", err
	}

	tz := c.Timezone()
	localTimePath, err := timezone.ConfigureContainerTimeZone(tz, c.state.RunDir, mountPoint, etcInTheContainerPath, c.ID())
	if err != nil {
		return "", fmt.Errorf("configuring timezone for container %s: %w", c.ID(), err)
	}
	if localTimePath != "" {
		if err := c.relabel(localTimePath, c.config.MountLabel, false); err != nil {
			return "", err
		}
		if c.state.BindMounts == nil {
			c.state.BindMounts = make(map[string]string)
		}
		c.state.BindMounts["/etc/localtime"] = localTimePath
	}

	// Request a mount of all named volumes
	for _, v := range c.config.NamedVolumes {
		vol, err := c.mountNamedVolume(v, mountPoint)
		if err != nil {
			return "", err
		}
		defer func() {
			if deferredErr == nil {
				return
			}
			vol.lock.Lock()
			if err := vol.unmount(false); err != nil {
				logrus.Errorf("Unmounting volume %s after error mounting container %s: %v", vol.Name(), c.ID(), err)
			}
			vol.lock.Unlock()
		}()
	}

	return mountPoint, nil
}

// Mount a single named volume into the container.
// If necessary, copy up image contents into the volume.
// Does not verify that the name volume given is actually present in container
// config.
// Returns the volume that was mounted.
func (c *Container) mountNamedVolume(v *ContainerNamedVolume, mountpoint string) (*Volume, error) {
	logrus.Debugf("Going to mount named volume %s", v.Name)
	vol, err := c.runtime.state.Volume(v.Name)
	if err != nil {
		return nil, fmt.Errorf("retrieving named volume %s for container %s: %w", v.Name, c.ID(), err)
	}

	if vol.config.LockID == c.config.LockID {
		return nil, fmt.Errorf("container %s and volume %s share lock ID %d: %w", c.ID(), vol.Name(), c.config.LockID, define.ErrWillDeadlock)
	}
	vol.lock.Lock()
	defer vol.lock.Unlock()
	if vol.needsMount() {
		if err := vol.mount(); err != nil {
			return nil, fmt.Errorf("mounting volume %s for container %s: %w", vol.Name(), c.ID(), err)
		}
	}
	// The volume may need a copy-up. Check the state.
	if err := vol.update(); err != nil {
		return nil, err
	}
	_, hasNoCopy := vol.config.Options["nocopy"]
	if vol.state.NeedsCopyUp && !slices.Contains(v.Options, "nocopy") && !hasNoCopy {
		logrus.Debugf("Copying up contents from container %s to volume %s", c.ID(), vol.Name())

		srcDir, err := securejoin.SecureJoin(mountpoint, v.Dest)
		if err != nil {
			return nil, fmt.Errorf("calculating destination path to copy up container %s volume %s: %w", c.ID(), vol.Name(), err)
		}
		// Do a manual stat on the source directory to verify existence.
		// Skip the rest if it exists.
		srcStat, err := os.Lstat(srcDir)
		if err != nil {
			if os.IsNotExist(err) {
				// Source does not exist, don't bother copying
				// up.
				return vol, nil
			}
			return nil, fmt.Errorf("identifying source directory for copy up into volume %s: %w", vol.Name(), err)
		}
		// If it's not a directory we're mounting over it.
		if !srcStat.IsDir() {
			return vol, nil
		}
		// Read contents, do not bother continuing if it's empty. Fixes
		// a bizarre issue where something copier.Get will ENOENT on
		// empty directories and sometimes it will not.
		// RHBZ#1928643
		srcContents, err := os.ReadDir(srcDir)
		if err != nil {
			return nil, fmt.Errorf("reading contents of source directory for copy up into volume %s: %w", vol.Name(), err)
		}
		if len(srcContents) == 0 {
			return vol, nil
		}

		// If the volume is not empty, we should not copy up.
		volMount := vol.mountPoint()
		contents, err := os.ReadDir(volMount)
		if err != nil {
			return nil, fmt.Errorf("listing contents of volume %s mountpoint when copying up from container %s: %w", vol.Name(), c.ID(), err)
		}
		if len(contents) > 0 {
			// The volume is not empty. It was likely modified
			// outside of Podman. For safety, let's not copy up into
			// it. Fixes CVE-2020-1726.
			return vol, nil
		}

		// Set NeedsCopyUp to false since we are about to do first copy
		// Do not copy second time.
		vol.state.NeedsCopyUp = false
		vol.state.CopiedUp = true
		if err := vol.save(); err != nil {
			return nil, err
		}

		// Buildah Copier accepts a reader, so we'll need a pipe.
		reader, writer := io.Pipe()
		defer reader.Close()

		errChan := make(chan error, 1)

		logrus.Infof("About to copy up into volume %s", vol.Name())

		// Copy, container side: get a tar archive of what needs to be
		// streamed into the volume.
		go func() {
			defer writer.Close()
			getOptions := copier.GetOptions{
				KeepDirectoryNames: false,
			}
			// If the volume is idmapped, we need to "undo" the idmapping
			if slices.Contains(v.Options, "idmap") {
				getOptions.UIDMap = c.config.IDMappings.UIDMap
				getOptions.GIDMap = c.config.IDMappings.GIDMap
			}
			errChan <- copier.Get(srcDir, "", getOptions, []string{"/."}, writer)
		}()

		// Copy, volume side: stream what we've written to the pipe, into
		// the volume.
		copyOpts := copier.PutOptions{}
		if err := copier.Put(volMount, "", copyOpts, reader); err != nil {
			// consume the reader otherwise the goroutine will block
			_, _ = io.Copy(io.Discard, reader)
			err2 := <-errChan
			if err2 != nil {
				logrus.Errorf("Streaming contents of container %s directory for volume copy-up: %v", c.ID(), err2)
			}
			return nil, fmt.Errorf("copying up to volume %s: %w", vol.Name(), err)
		}

		if err := <-errChan; err != nil {
			return nil, fmt.Errorf("streaming container content for copy up into volume %s: %w", vol.Name(), err)
		}
	}
	return vol, nil
}

// cleanupStorage unmounts and cleans up the container's root filesystem
func (c *Container) cleanupStorage() error {
	if !c.state.Mounted {
		// Already unmounted, do nothing
		logrus.Debugf("Container %s storage is already unmounted, skipping...", c.ID())
		return nil
	}

	var cleanupErr error
	reportErrorf := func(msg string, args ...any) {
		err := fmt.Errorf(msg, args...) // Always use fmt.Errorf instead of just logrus.Errorf(…) because the format string probably contains %w
		if cleanupErr == nil {
			cleanupErr = err
		} else {
			logrus.Errorf("%s", err.Error())
		}
	}

	markUnmounted := func() {
		c.state.Mountpoint = ""
		c.state.Mounted = false

		if c.valid {
			if err := c.save(); err != nil {
				reportErrorf("unmounting container %s: %w", c.ID(), err)
			}
		}
	}

	// umount rootfs overlay if it was created
	if c.config.RootfsOverlay {
		overlayBasePath := filepath.Dir(c.state.Mountpoint)
		if err := overlay.Unmount(overlayBasePath); err != nil {
			reportErrorf("failed to clean up overlay mounts for %s: %w", c.ID(), err)
		}
	}
	if c.config.RootfsMapping != nil {
		if err := unix.Unmount(c.config.Rootfs, 0); err != nil && err != unix.EINVAL {
			reportErrorf("unmounting idmapped rootfs for container %s after mount error: %w", c.ID(), err)
		}
	}

	for _, containerMount := range c.config.Mounts {
		if err := c.unmountSHM(containerMount); err != nil {
			reportErrorf("unmounting container %s: %w", c.ID(), err)
		}
	}

	if err := c.cleanupOverlayMounts(); err != nil {
		// If the container can't remove content report the error
		reportErrorf("failed to clean up overlay mounts for %s: %w", c.ID(), err)
	}

	if c.config.Rootfs != "" {
		markUnmounted()
		return cleanupErr
	}

	if err := c.unmount(false); err != nil {
		// If the container has already been removed, warn but don't
		// error
		// We still want to be able to kick the container out of the
		// state
		switch {
		case errors.Is(err, storage.ErrLayerNotMounted):
			logrus.Infof("Storage for container %s is not mounted: %v", c.ID(), err)
		case errors.Is(err, storage.ErrNotAContainer) || errors.Is(err, storage.ErrContainerUnknown):
			logrus.Warnf("Storage for container %s has been removed: %v", c.ID(), err)
		default:
			reportErrorf("cleaning up container %s storage: %w", c.ID(), err)
		}
	}

	// Request an unmount of all named volumes
	for _, v := range c.config.NamedVolumes {
		vol, err := c.runtime.state.Volume(v.Name)
		if err != nil {
			reportErrorf("retrieving named volume %s for container %s: %w", v.Name, c.ID(), err)

			// We need to try and unmount every volume, so continue
			// if they fail.
			continue
		}

		if vol.needsMount() {
			vol.lock.Lock()
			if err := vol.unmount(false); err != nil {
				reportErrorf("unmounting volume %s for container %s: %w", vol.Name(), c.ID(), err)
			}
			vol.lock.Unlock()
		}
	}

	markUnmounted()
	return cleanupErr
}

// fullCleanup performs all cleanup tasks, including handling restart policy.
func (c *Container) fullCleanup(ctx context.Context, onlyStopped bool) error {
	// Check if state is good
	if !c.ensureState(define.ContainerStateConfigured, define.ContainerStateCreated, define.ContainerStateStopped, define.ContainerStateStopping, define.ContainerStateExited) {
		return fmt.Errorf("container %s is running or paused, refusing to clean up: %w", c.ID(), define.ErrCtrStateInvalid)
	}
	if onlyStopped && !c.ensureState(define.ContainerStateStopped) {
		return fmt.Errorf("container %s is not stopped and only cleanup for a stopped container was requested: %w", c.ID(), define.ErrCtrStateInvalid)
	}

	// if the container was not created in the oci runtime or was already cleaned up, then do nothing
	if c.ensureState(define.ContainerStateConfigured, define.ContainerStateExited) {
		return nil
	}

	// Handle restart policy.
	// Returns a bool indicating whether we actually restarted.
	// If we did, don't proceed to cleanup - just exit.
	didRestart, err := c.handleRestartPolicy(ctx)
	if err != nil {
		return err
	}
	if didRestart {
		return nil
	}

	// If we didn't restart, we perform a normal cleanup

	// make sure all the container processes are terminated if we are running without a pid namespace.
	hasPidNs := false
	if c.config.Spec.Linux != nil {
		for _, i := range c.config.Spec.Linux.Namespaces {
			if i.Type == spec.PIDNamespace {
				hasPidNs = true
				break
			}
		}
	}
	if !hasPidNs {
		// do not fail on errors
		_ = c.ociRuntime.KillContainer(c, uint(unix.SIGKILL), true)
	}

	// Check for running exec sessions
	sessions, err := c.getActiveExecSessions()
	if err != nil {
		return err
	}
	if len(sessions) > 0 {
		return fmt.Errorf("container %s has active exec sessions, refusing to clean up: %w", c.ID(), define.ErrCtrStateInvalid)
	}

	defer c.newContainerEvent(events.Cleanup)
	return c.cleanup(ctx)
}

// Unmount the container and free its resources
func (c *Container) cleanup(ctx context.Context) error {
	var lastError error

	logrus.Debugf("Cleaning up container %s", c.ID())

	// Ensure we are not killed half way through cleanup
	// which can leave us in a bad state.
	shutdown.Inhibit()
	defer shutdown.Uninhibit()

	// Remove healthcheck unit/timer file if it execs
	if c.config.HealthCheckConfig != nil {
		if err := c.removeTransientFiles(ctx,
			c.config.StartupHealthCheckConfig != nil && !c.state.StartupHCPassed,
			c.state.HCUnitName); err != nil {
			logrus.Errorf("Removing timer for container %s healthcheck: %v", c.ID(), err)
		}
	}

	// Clean up network namespace, if present
	if err := c.cleanupNetwork(); err != nil {
		lastError = fmt.Errorf("removing container %s network: %w", c.ID(), err)
	}

	// cleanup host entry if it is shared
	if c.config.NetNsCtr != "" {
		if hoststFile, ok := c.state.BindMounts[config.DefaultHostsFile]; ok {
			if err := fileutils.Exists(hoststFile); err == nil {
				// we cannot use the dependency container lock due ABBA deadlocks
				if lock, err := lockfile.GetLockFile(hoststFile); err == nil {
					lock.Lock()
					// make sure to ignore ENOENT error in case the netns container was cleaned up before this one
					if err := etchosts.Remove(hoststFile, getLocalhostHostEntry(c)); err != nil && !errors.Is(err, os.ErrNotExist) {
						// this error is not fatal we still want to do proper cleanup
						logrus.Errorf("failed to remove hosts entry from the netns containers /etc/hosts: %v", err)
					}
					lock.Unlock()
				}
			}
		}
	}

	// Remove the container from the runtime, if necessary.
	// Do this *before* unmounting storage - some runtimes (e.g. Kata)
	// apparently object to having storage removed while the container still
	// exists.
	if err := c.cleanupRuntime(ctx); err != nil {
		if lastError != nil {
			logrus.Errorf("Removing container %s from OCI runtime: %v", c.ID(), err)
		} else {
			lastError = err
		}
	}

	// Unmount storage
	if err := c.cleanupStorage(); err != nil {
		if lastError != nil {
			logrus.Errorf("Unmounting container %s storage: %v", c.ID(), err)
		} else {
			lastError = fmt.Errorf("unmounting container %s storage: %w", c.ID(), err)
		}
	}

	// Unmount image volumes
	for _, v := range c.config.ImageVolumes {
		img, _, err := c.runtime.LibimageRuntime().LookupImage(v.Source, nil)
		if err != nil {
			if lastError == nil {
				lastError = err
				continue
			}
			logrus.Errorf("Unmounting image volume %q:%q :%v", v.Source, v.Dest, err)
		}
		if err := img.Unmount(false); err != nil {
			if lastError == nil {
				lastError = err
				continue
			}
			logrus.Errorf("Unmounting image volume %q:%q :%v", v.Source, v.Dest, err)
		}
	}

	if err := c.stopPodIfNeeded(context.Background()); err != nil {
		if lastError == nil {
			lastError = err
		} else {
			logrus.Errorf("Stopping pod of container %s: %v", c.ID(), err)
		}
	}

	// Prune the exit codes of other container during clean up.
	// Since Podman is no daemon, we have to clean them up somewhere.
	// Cleanup seems like a good place as it's not performance
	// critical.
	if err := c.runtime.state.PruneContainerExitCodes(); err != nil {
		if lastError == nil {
			lastError = err
		} else {
			logrus.Errorf("Pruning container exit codes: %v", err)
		}
	}

	return lastError
}

// If the container is part of a pod where only the infra container remains
// running, attempt to stop the pod.
func (c *Container) stopPodIfNeeded(ctx context.Context) error {
	if c.config.Pod == "" {
		return nil
	}

	// Never try to stop the pod when a init container stopped
	if c.IsInitCtr() {
		return nil
	}

	pod, err := c.runtime.state.Pod(c.config.Pod)
	if err != nil {
		return fmt.Errorf("container %s is in pod %s, but pod cannot be retrieved: %w", c.ID(), c.config.Pod, err)
	}

	switch pod.config.ExitPolicy {
	case config.PodExitPolicyContinue:
		return nil

	case config.PodExitPolicyStop:
		// Use the runtime's work queue to stop the pod. This resolves
		// a number of scenarios where we'd otherwise run into
		// deadlocks.  For instance, during `pod stop`, the pod has
		// already been locked.
		// The work queue is a simple means without having to worry about
		// future changes that may introduce more deadlock scenarios.
		c.runtime.queueWork(func() {
			if err := pod.stopIfOnlyInfraRemains(ctx, c.ID()); err != nil {
				if !errors.Is(err, define.ErrNoSuchPod) {
					logrus.Errorf("Checking if infra needs to be stopped: %v", err)
				}
			}
		})
	}
	return nil
}

// delete deletes the container and runs any configured poststop
// hooks.
func (c *Container) delete(ctx context.Context) error {
	if err := c.ociRuntime.DeleteContainer(c); err != nil {
		return fmt.Errorf("removing container %s from runtime: %w", c.ID(), err)
	}

	if err := c.postDeleteHooks(ctx); err != nil {
		return fmt.Errorf("container %s poststop hooks: %w", c.ID(), err)
	}

	return nil
}

// postDeleteHooks runs the poststop hooks (if any) as specified by
// the OCI Runtime Specification (which requires them to run
// post-delete, despite the stage name).
func (c *Container) postDeleteHooks(ctx context.Context) error {
	if c.state.ExtensionStageHooks != nil {
		extensionHooks, ok := c.state.ExtensionStageHooks["poststop"]
		if ok {
			state, err := json.Marshal(spec.State{
				Version:     spec.Version,
				ID:          c.ID(),
				Status:      "stopped",
				Bundle:      c.bundlePath(),
				Annotations: c.config.Spec.Annotations,
			})
			if err != nil {
				return err
			}
			for i, hook := range extensionHooks {
				logrus.Debugf("container %s: invoke poststop hook %d, path %s", c.ID(), i, hook.Path)
				var stderr, stdout bytes.Buffer
				hookErr, err := exec.RunWithOptions(
					ctx,
					exec.RunOptions{
						Hook:            &hook,
						Dir:             c.bundlePath(),
						State:           state,
						Stdout:          &stdout,
						Stderr:          &stderr,
						PostKillTimeout: exec.DefaultPostKillTimeout,
					},
				)
				if err != nil {
					logrus.Warnf("Container %s: poststop hook %d: %v", c.ID(), i, err)
					if hookErr != err {
						logrus.Debugf("container %s: poststop hook %d (hook error): %v", c.ID(), i, hookErr)
					}
					stdoutString := stdout.String()
					if stdoutString != "" {
						logrus.Debugf("container %s: poststop hook %d: stdout:\n%s", c.ID(), i, stdoutString)
					}
					stderrString := stderr.String()
					if stderrString != "" {
						logrus.Debugf("container %s: poststop hook %d: stderr:\n%s", c.ID(), i, stderrString)
					}
				}
			}
		}
	}

	return nil
}

// writeStringToRundir writes the given string to a file with the given name in
// the container's temporary files directory. The file will be chown'd to the
// container's root user and have an appropriate SELinux label set.
// If a file with the same name already exists, it will be deleted and recreated
// with the new contents.
// Returns the full path to the new file.
func (c *Container) writeStringToRundir(destFile, contents string) (string, error) {
	destFileName := filepath.Join(c.state.RunDir, destFile)

	if err := os.Remove(destFileName); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("removing %s for container %s: %w", destFile, c.ID(), err)
	}

	if err := writeStringToPath(destFileName, contents, c.config.MountLabel, c.RootUID(), c.RootGID()); err != nil {
		return "", err
	}

	return destFileName, nil
}

// writeStringToStaticDir writes the given string to a file with the given name
// in the container's permanent files directory. The file will be chown'd to the
// container's root user and have an appropriate SELinux label set.
// Unlike writeStringToRundir, will *not* delete and re-create if the file
// already exists (will instead error).
// Returns the full path to the new file.
func (c *Container) writeStringToStaticDir(filename, contents string) (string, error) {
	destFileName := filepath.Join(c.config.StaticDir, filename)

	if err := writeStringToPath(destFileName, contents, c.config.MountLabel, c.RootUID(), c.RootGID()); err != nil {
		return "", err
	}

	return destFileName, nil
}

// saveSpec saves the OCI spec to disk, replacing any existing specs for the container
func (c *Container) saveSpec(spec *spec.Spec) error {
	// If the OCI spec already exists, we need to replace it
	// Cannot guarantee some things, e.g. network namespaces, have the same
	// paths
	jsonPath := filepath.Join(c.bundlePath(), "config.json")
	if err := fileutils.Exists(jsonPath); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("doing stat on container %s spec: %w", c.ID(), err)
		}
		// The spec does not exist, we're fine
	} else {
		// The spec exists, need to remove it
		if err := os.Remove(jsonPath); err != nil {
			return fmt.Errorf("replacing runtime spec for container %s: %w", c.ID(), err)
		}
	}

	fileJSON, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("exporting runtime spec for container %s to JSON: %w", c.ID(), err)
	}
	if err := os.WriteFile(jsonPath, fileJSON, 0o644); err != nil {
		return fmt.Errorf("writing runtime spec JSON for container %s to disk: %w", c.ID(), err)
	}

	logrus.Debugf("Created OCI spec for container %s at %s", c.ID(), jsonPath)

	c.state.ConfigPath = jsonPath

	return nil
}

// Warning: precreate hooks may alter 'config' in place.
func (c *Container) setupOCIHooks(ctx context.Context, config *spec.Spec) (map[string][]spec.Hook, error) {
	allHooks := make(map[string][]spec.Hook)
	if len(c.runtime.config.Engine.HooksDir.Get()) == 0 {
		if rootless.IsRootless() {
			return nil, nil
		}
		for _, hDir := range []string{hooks.DefaultDir, hooks.OverrideDir} {
			manager, err := hooks.New(ctx, []string{hDir}, []string{"precreate", "poststop"})
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, err
			}
			ociHooks, err := manager.Hooks(config, c.config.Spec.Annotations, len(c.config.UserVolumes) > 0)
			if err != nil {
				return nil, err
			}
			if len(ociHooks) > 0 || config.Hooks != nil {
				logrus.Warnf("Implicit hook directories are deprecated; set --ociHooks-dir=%q explicitly to continue to load ociHooks from this directory", hDir)
			}
			maps.Copy(allHooks, ociHooks)
		}
	} else {
		manager, err := hooks.New(ctx, c.runtime.config.Engine.HooksDir.Get(), []string{"precreate", "poststop"})
		if err != nil {
			return nil, err
		}

		allHooks, err = manager.Hooks(config, c.config.Spec.Annotations, len(c.config.UserVolumes) > 0)
		if err != nil {
			return nil, err
		}
	}

	hookErr, err := exec.RuntimeConfigFilterWithOptions(
		ctx,
		exec.RuntimeConfigFilterOptions{
			Hooks:           allHooks["precreate"],
			Dir:             c.bundlePath(),
			Config:          config,
			PostKillTimeout: exec.DefaultPostKillTimeout,
		},
	)
	if err != nil {
		logrus.Warnf("Container %s: precreate hook: %v", c.ID(), err)
		if hookErr != nil && hookErr != err {
			logrus.Debugf("container %s: precreate hook (hook error): %v", c.ID(), hookErr)
		}
		return nil, err
	}

	return allHooks, nil
}

// getRootPathForOCI returns the root path to use for the OCI runtime.
// If the current user is mapped in the container user namespace, then it returns
// the container's mountpoint directly from the storage.
// Otherwise, it returns an intermediate mountpoint that is accessible to anyone.
func (c *Container) getRootPathForOCI() (string, error) {
	if hasCurrentUserMapped(c) || c.config.RootfsMapping != nil {
		return c.state.Mountpoint, nil
	}
	return c.getIntermediateMountpointUser()
}

var (
	intermediateMountPoint     string
	intermediateMountPointErr  error
	intermediateMountPointSync sync.Mutex
)

// getIntermediateMountpointUser returns a path that is accessible to everyone.  It must be on TMPDIR since
// the runroot/tmpdir used by libpod are accessible only to the owner.
// To avoid TOCTOU issues, the path must be owned by the current user's UID and GID.
// The path can be used by different containers, so a mount must be created only in a private mount namespace.
func (c *Container) recreateIntermediateMountpointUser() (string, error) {
	uid := os.Geteuid()
	gid := os.Getegid()
	for i := 0; ; i++ {
		tmpDir := os.Getenv("TMPDIR")
		if tmpDir == "" {
			tmpDir = "/tmp"
		}
		dir := filepath.Join(tmpDir, fmt.Sprintf("intermediate-mountpoint-%d.%d", rootless.GetRootlessUID(), i))
		err := os.Mkdir(dir, 0o755)
		if err != nil {
			if !errors.Is(err, os.ErrExist) {
				return "", err
			}
			st, err2 := os.Stat(dir)
			if err2 != nil {
				return "", err
			}
			sys := st.Sys().(*syscall.Stat_t)
			if !st.IsDir() || sys.Uid != uint32(uid) || sys.Gid != uint32(gid) {
				continue
			}
		}
		return dir, nil
	}
}

// getIntermediateMountpointUser returns a path that is accessible to everyone.
// To avoid TOCTOU issues, the path must be owned by the current user's UID and GID.
// The path can be used by different containers, so a mount must be created only in a private mount namespace.
func (c *Container) getIntermediateMountpointUser() (string, error) {
	intermediateMountPointSync.Lock()
	defer intermediateMountPointSync.Unlock()

	if intermediateMountPoint == "" || fileutils.Exists(intermediateMountPoint) != nil {
		return c.recreateIntermediateMountpointUser()
	}

	// update the timestamp to prevent systemd-tmpfiles from removing it
	now := time.Now()
	if err := os.Chtimes(intermediateMountPoint, now, now); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return c.recreateIntermediateMountpointUser()
		}
	}
	return intermediateMountPoint, intermediateMountPointErr
}

// mount mounts the container's root filesystem
func (c *Container) mount() (string, error) {
	if c.state.State == define.ContainerStateRemoving {
		return "", fmt.Errorf("cannot mount container %s as it is being removed: %w", c.ID(), define.ErrCtrStateInvalid)
	}

	mountPoint, err := c.runtime.storageService.MountContainerImage(c.ID())
	if err != nil {
		return "", fmt.Errorf("mounting storage for container %s: %w", c.ID(), err)
	}
	mountPoint, err = filepath.EvalSymlinks(mountPoint)
	if err != nil {
		return "", fmt.Errorf("resolving storage path for container %s: %w", c.ID(), err)
	}
	if err := idtools.SafeChown(mountPoint, c.RootUID(), c.RootGID()); err != nil {
		return "", fmt.Errorf("cannot chown %s to %d:%d: %w", mountPoint, c.RootUID(), c.RootGID(), err)
	}
	return mountPoint, nil
}

// unmount unmounts the container's root filesystem
func (c *Container) unmount(force bool) error {
	// Also unmount storage
	if _, err := c.runtime.storageService.UnmountContainerImage(c.ID(), force); err != nil {
		return fmt.Errorf("unmounting container %s root filesystem: %w", c.ID(), err)
	}

	return nil
}

// checkReadyForRemoval checks whether the given container is ready to be
// removed.
// These checks are only used if force-remove is not specified.
// If it is, we'll remove the container anyways.
// Returns nil if safe to remove, or an error describing why it's unsafe if not.
func (c *Container) checkReadyForRemoval() error {
	if c.state.State == define.ContainerStateUnknown {
		return fmt.Errorf("container %s is in invalid state: %w", c.ID(), define.ErrCtrStateInvalid)
	}

	if c.ensureState(define.ContainerStateRunning, define.ContainerStatePaused, define.ContainerStateStopping) && !c.IsInfra() {
		return fmt.Errorf("cannot remove container %s as it is %s - running or paused containers cannot be removed without force: %w", c.ID(), c.state.State.String(), define.ErrCtrStateInvalid)
	}

	// Check exec sessions
	sessions, err := c.getActiveExecSessions()
	if err != nil {
		return err
	}
	if len(sessions) != 0 {
		return fmt.Errorf("cannot remove container %s as it has active exec sessions: %w", c.ID(), define.ErrCtrStateInvalid)
	}

	return nil
}

// canWithPrevious return the stat of the preCheckPoint dir
func (c *Container) canWithPrevious() error {
	return fileutils.Exists(c.PreCheckPointPath())
}

// prepareCheckpointExport writes the config and spec to
// JSON files for later export
func (c *Container) prepareCheckpointExport() error {
	networks, err := c.networks()
	if err != nil {
		return err
	}
	// make sure to exclude the short ID alias since the container gets a new ID on restore
	for net, opts := range networks {
		newAliases := make([]string, 0, len(opts.Aliases))
		for _, alias := range opts.Aliases {
			if alias != c.config.ID[:12] {
				newAliases = append(newAliases, alias)
			}
		}
		opts.Aliases = newAliases
		networks[net] = opts
	}

	// add the networks from the db to the config so that the exported checkpoint still stores all current networks
	c.config.Networks = networks
	// save live config
	if _, err := metadata.WriteJSONFile(c.config, c.bundlePath(), metadata.ConfigDumpFile); err != nil {
		return err
	}

	// save spec
	jsonPath := filepath.Join(c.bundlePath(), "config.json")
	g, err := generate.NewFromFile(jsonPath)
	if err != nil {
		logrus.Debugf("generating spec for container %q failed with %v", c.ID(), err)
		return err
	}
	if _, err := metadata.WriteJSONFile(g.Config, c.bundlePath(), metadata.SpecDumpFile); err != nil {
		return err
	}

	return nil
}

// SortUserVolumes sorts the volumes specified for a container
// between named and normal volumes
func (c *Container) SortUserVolumes(ctrSpec *spec.Spec) ([]*ContainerNamedVolume, []spec.Mount) {
	namedUserVolumes := []*ContainerNamedVolume{}
	userMounts := []spec.Mount{}

	// We need to parse all named volumes and mounts into maps, so we don't
	// end up with repeated lookups for each user volume.
	// Map destination to struct, as destination is what is stored in
	// UserVolumes.
	namedVolumes := make(map[string]*ContainerNamedVolume)
	mounts := make(map[string]spec.Mount)
	for _, namedVol := range c.config.NamedVolumes {
		namedVolumes[namedVol.Dest] = namedVol
	}
	for _, mount := range ctrSpec.Mounts {
		mounts[mount.Destination] = mount
	}

	for _, vol := range c.config.UserVolumes {
		if volume, ok := namedVolumes[vol]; ok {
			namedUserVolumes = append(namedUserVolumes, volume)
		} else if mount, ok := mounts[vol]; ok {
			userMounts = append(userMounts, mount)
		} else {
			logrus.Warnf("Could not find mount at destination %q when parsing user volumes for container %s", vol, c.ID())
		}
	}
	return namedUserVolumes, userMounts
}

// Check for an exit file, and handle one if present
func (c *Container) checkExitFile() error {
	// If the container's not running, nothing to do.
	if !c.ensureState(define.ContainerStateRunning, define.ContainerStatePaused, define.ContainerStateStopping) {
		return nil
	}

	exitFile, err := c.exitFilePath()
	if err != nil {
		return err
	}

	// Check for the exit file
	info, err := os.Stat(exitFile)
	if err != nil {
		if os.IsNotExist(err) {
			// Container is still running, no error
			return nil
		}

		return fmt.Errorf("running stat on container %s exit file: %w", c.ID(), err)
	}

	// Alright, it exists. Transition to Stopped state.
	c.state.State = define.ContainerStateStopped
	c.state.PID = 0
	c.state.ConmonPID = 0

	// Read the exit file to get our stopped time and exit code.
	return c.handleExitFile(exitFile, info)
}

func (c *Container) hasNamespace(namespace spec.LinuxNamespaceType) bool {
	if c.config.Spec == nil || c.config.Spec.Linux == nil {
		return false
	}
	for _, n := range c.config.Spec.Linux.Namespaces {
		if n.Type == namespace {
			return true
		}
	}
	return false
}

// extractSecretToCtrStorage copies a secret's data from the secrets manager to the container's static dir
func (c *Container) extractSecretToCtrStorage(secr *ContainerSecret) error {
	manager, err := c.runtime.SecretsManager()
	if err != nil {
		return err
	}
	_, data, err := manager.LookupSecretData(secr.Name)
	if err != nil {
		return err
	}
	secretFile := filepath.Join(c.config.SecretsPath, secr.Name)

	hostUID, hostGID, err := butil.GetHostIDs(util.IDtoolsToRuntimeSpec(c.config.IDMappings.UIDMap), util.IDtoolsToRuntimeSpec(c.config.IDMappings.GIDMap), secr.UID, secr.GID)
	if err != nil {
		return fmt.Errorf("unable to extract secret: %w", err)
	}
	err = os.WriteFile(secretFile, data, 0o644)
	if err != nil {
		return fmt.Errorf("unable to create %s: %w", secretFile, err)
	}
	if err := idtools.SafeLchown(secretFile, int(hostUID), int(hostGID)); err != nil {
		return err
	}
	if err := os.Chmod(secretFile, os.FileMode(secr.Mode)); err != nil {
		return err
	}
	if err := c.relabel(secretFile, c.config.MountLabel, false); err != nil {
		return err
	}
	return nil
}

// Update a container's resources or restart policy after creation.
// At least one of resources or restartPolicy must not be nil.
func (c *Container) update(updateOptions *entities.ContainerUpdateOptions) error {
	if updateOptions.Resources == nil && updateOptions.RestartPolicy == nil {
		return fmt.Errorf("must provide at least one of resources and restartPolicy to update a container: %w", define.ErrInvalidArg)
	}
	if updateOptions.RestartRetries != nil && updateOptions.RestartPolicy == nil {
		return fmt.Errorf("must provide restart policy if updating restart retries: %w", define.ErrInvalidArg)
	}

	oldResources := new(spec.LinuxResources)
	if c.config.Spec.Linux.Resources != nil {
		if err := JSONDeepCopy(c.config.Spec.Linux.Resources, oldResources); err != nil {
			return err
		}
	}
	oldRestart := c.config.RestartPolicy
	oldRetries := c.config.RestartRetries

	if updateOptions.RestartPolicy != nil {
		if err := define.ValidateRestartPolicy(*updateOptions.RestartPolicy); err != nil {
			return err
		}

		if updateOptions.RestartRetries != nil {
			if *updateOptions.RestartPolicy != define.RestartPolicyOnFailure {
				return fmt.Errorf("cannot set restart policy retries unless policy is on-failure: %w", define.ErrInvalidArg)
			}
		}

		c.config.RestartPolicy = *updateOptions.RestartPolicy
		if updateOptions.RestartRetries != nil {
			c.config.RestartRetries = *updateOptions.RestartRetries
		} else {
			c.config.RestartRetries = 0
		}
	}

	if updateOptions.Resources != nil {
		if c.config.Spec.Linux == nil {
			c.config.Spec.Linux = new(spec.Linux)
		}

		resourcesToUpdate, err := json.Marshal(updateOptions.Resources)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(resourcesToUpdate, c.config.Spec.Linux.Resources); err != nil {
			return err
		}
		updateOptions.Resources = c.config.Spec.Linux.Resources
	}

	if len(updateOptions.Env) != 0 {
		c.config.Spec.Process.Env = envLib.Slice(envLib.Join(envLib.Map(c.config.Spec.Process.Env), envLib.Map(updateOptions.Env)))
	}

	if len(updateOptions.UnsetEnv) != 0 {
		envMap := envLib.Map(c.config.Spec.Process.Env)
		for _, e := range updateOptions.UnsetEnv {
			delete(envMap, e)
		}
		c.config.Spec.Process.Env = envLib.Slice(envMap)
	}

	if err := c.runtime.state.SafeRewriteContainerConfig(c, "", "", c.config); err != nil {
		// Assume DB write failed, revert to old resources block
		c.config.Spec.Linux.Resources = oldResources
		c.config.RestartPolicy = oldRestart
		c.config.RestartRetries = oldRetries
		return err
	}

	if c.ensureState(define.ContainerStateCreated, define.ContainerStateRunning, define.ContainerStatePaused) &&
		(updateOptions.Resources != nil || updateOptions.Env != nil || updateOptions.UnsetEnv != nil) {
		// So `podman inspect` on running containers sources its OCI spec from disk.
		// To keep inspect accurate we need to update the on-disk OCI spec.
		onDiskSpec, err := c.specFromState()
		if err != nil {
			return fmt.Errorf("retrieving on-disk OCI spec to update: %w", err)
		}
		if updateOptions.Resources != nil {
			if onDiskSpec.Linux == nil {
				onDiskSpec.Linux = new(spec.Linux)
			}
			onDiskSpec.Linux.Resources = updateOptions.Resources
		}
		if len(updateOptions.Env) != 0 || len(updateOptions.UnsetEnv) != 0 {
			onDiskSpec.Process.Env = c.config.Spec.Process.Env
		}
		if err := c.saveSpec(onDiskSpec); err != nil {
			logrus.Errorf("Unable to update container %s OCI spec - `podman inspect` may not be accurate until container is restarted: %v", c.ID(), err)
		}

		if err := c.ociRuntime.UpdateContainer(c, updateOptions.Resources); err != nil {
			return err
		}
	}

	logrus.Debugf("updated container %s", c.ID())
	return nil
}

func (c *Container) resetHealthCheckTimers(noHealthCheck bool, changedTimer bool, wasEnabledHealthCheck bool, isStartup bool) error {
	if !c.ensureState(define.ContainerStateCreated, define.ContainerStateRunning, define.ContainerStatePaused) {
		return nil
	}
	if noHealthCheck {
		if err := c.removeTransientFiles(context.Background(),
			c.config.StartupHealthCheckConfig != nil && !c.state.StartupHCPassed,
			c.state.HCUnitName); err != nil {
			return err
		}
		return nil
	}

	if !changedTimer {
		return nil
	}

	if !isStartup {
		if c.state.StartupHCPassed || c.config.StartupHealthCheckConfig == nil {
			if err := c.recreateHealthCheckTimer(context.Background(), false, false); err != nil {
				return err
			}
		}
		return nil
	}

	if !c.state.StartupHCPassed {
		c.state.StartupHCPassed = !wasEnabledHealthCheck
		c.state.StartupHCSuccessCount = 0
		c.state.StartupHCFailureCount = 0
		if err := c.save(); err != nil {
			return err
		}
		if wasEnabledHealthCheck {
			if err := c.recreateHealthCheckTimer(context.Background(), true, true); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

func (c *Container) updateHealthCheck(newHealthCheckConfig IHealthCheckConfig, currentHealthCheckConfig IHealthCheckConfig) error {
	oldHealthCheckConfig := currentHealthCheckConfig
	if !oldHealthCheckConfig.IsNil() {
		if err := JSONDeepCopy(currentHealthCheckConfig, oldHealthCheckConfig); err != nil {
			return err
		}
	}

	newHealthCheckConfig.SetTo(c.config)

	if err := c.runtime.state.SafeRewriteContainerConfig(c, "", "", c.config); err != nil {
		// Assume DB write failed, revert to old resources block
		oldHealthCheckConfig.SetTo(c.config)
		return err
	}

	oldInterval := time.Duration(0)
	if !oldHealthCheckConfig.IsNil() {
		oldInterval = oldHealthCheckConfig.GetInterval()
	}

	changedTimer := false
	if !newHealthCheckConfig.IsNil() {
		changedTimer = newHealthCheckConfig.IsTimeChanged(oldInterval)
	}

	noHealthCheck := c.config.HealthCheckConfig != nil && slices.Contains(c.config.HealthCheckConfig.Test, "NONE")

	if err := c.resetHealthCheckTimers(noHealthCheck, changedTimer, !oldHealthCheckConfig.IsNil(), newHealthCheckConfig.IsStartup()); err != nil {
		return err
	}

	checkType := "HealthCheck"
	if newHealthCheckConfig.IsStartup() {
		checkType = "Startup HealthCheck"
	}
	logrus.Debugf("%s configuration updated for container %s", checkType, c.ID())
	return nil
}

func (c *Container) updateGlobalHealthCheckConfiguration(globalOptions define.GlobalHealthCheckOptions) error {
	oldHealthCheckOnFailureAction := c.config.HealthCheckOnFailureAction
	oldHealthLogDestination := c.config.HealthLogDestination
	oldHealthMaxLogCount := c.config.HealthMaxLogCount
	oldHealthMaxLogSize := c.config.HealthMaxLogSize

	if globalOptions.HealthCheckOnFailureAction != nil {
		c.config.HealthCheckOnFailureAction = *globalOptions.HealthCheckOnFailureAction
	}

	if globalOptions.HealthMaxLogCount != nil {
		c.config.HealthMaxLogCount = globalOptions.HealthMaxLogCount
	}

	if globalOptions.HealthMaxLogSize != nil {
		c.config.HealthMaxLogSize = globalOptions.HealthMaxLogSize
	}

	if globalOptions.HealthLogDestination != nil {
		dest, err := define.GetValidHealthCheckDestination(*globalOptions.HealthLogDestination)
		if err != nil {
			return err
		}
		c.config.HealthLogDestination = &dest
	}

	if err := c.runtime.state.SafeRewriteContainerConfig(c, "", "", c.config); err != nil {
		// Assume DB write failed, revert to old resources block
		c.config.HealthCheckOnFailureAction = oldHealthCheckOnFailureAction
		c.config.HealthLogDestination = oldHealthLogDestination
		c.config.HealthMaxLogCount = oldHealthMaxLogCount
		c.config.HealthMaxLogSize = oldHealthMaxLogSize
		return err
	}

	logrus.Debugf("Global HealthCheck configuration updated for container %s", c.ID())
	return nil
}
