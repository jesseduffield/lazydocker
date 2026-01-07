//go:build !remote

package libpod

import (
	"context"
	"errors"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	istorage "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/idtools"
)

type storageService struct {
	store storage.Store
}

// getStorageService returns a storageService which can create container root
// filesystems from images
func getStorageService(store storage.Store) *storageService {
	return &storageService{store: store}
}

// ContainerInfo wraps a subset of information about a container: the locations
// of its nonvolatile and volatile per-container directories, along with a copy
// of the configuration blob from the image that was used to create the
// container, if the image had a configuration.
// It also returns the ProcessLabel and MountLabel selected for the container
type ContainerInfo struct {
	Dir          string
	RunDir       string
	Config       *v1.Image
	ProcessLabel string
	MountLabel   string
	UIDMap       []idtools.IDMap
	GIDMap       []idtools.IDMap
}

// RuntimeContainerMetadata is the structure that we encode as JSON and store
// in the metadata field of storage.Container objects.  It is used for
// specifying attributes containers when they are being created, and allows a
// container's MountLabel, and possibly other values, to be modified in one
// read/write cycle via calls to storageService.ContainerMetadata,
// RuntimeContainerMetadata.SetMountLabel, and
// storageService.SetContainerMetadata.
type RuntimeContainerMetadata struct {
	// The provided name and the ID of the image that was used to
	// instantiate the container.
	ImageName string `json:"image-name"` // Applicable to both PodSandboxes and Containers
	ImageID   string `json:"image-id"`   // Applicable to both PodSandboxes and Containers
	// The container's name, which for an infrastructure container is usually PodName + "-infra".
	ContainerName string `json:"name"`                 // Applicable to both PodSandboxes and Containers, mandatory
	CreatedAt     int64  `json:"created-at"`           // Applicable to both PodSandboxes and Containers
	MountLabel    string `json:"mountlabel,omitempty"` // Applicable to both PodSandboxes and Containers
}

// SetMountLabel updates the mount label held by a RuntimeContainerMetadata
// object.
func (metadata *RuntimeContainerMetadata) SetMountLabel(mountLabel string) {
	metadata.MountLabel = mountLabel
}

// CreateContainerStorage creates the storage end of things.  We already have the container spec created.
// imageID and imageName must both be either empty or non-empty.
// TO-DO We should be passing in an Image object in the future.
func (r *storageService) CreateContainerStorage(ctx context.Context, systemContext *types.SystemContext, imageName, imageID, containerName, containerID string, options storage.ContainerOptions) (_ ContainerInfo, retErr error) {
	var imageConfig *v1.Image
	if imageID != "" {
		if containerName == "" {
			return ContainerInfo{}, define.ErrEmptyID
		}
		// Check if we have the specified image.
		ref, err := istorage.Transport.NewStoreReference(r.store, nil, imageID)
		if err != nil {
			return ContainerInfo{}, err
		}
		// Pull out a copy of the image's configuration.
		image, err := ref.NewImage(ctx, systemContext)
		if err != nil {
			return ContainerInfo{}, err
		}
		defer image.Close()

		// Get OCI configuration of image
		imageConfig, err = image.OCIConfig(ctx)
		if err != nil {
			return ContainerInfo{}, err
		}
	}

	// Build metadata to store with the container.
	metadata := RuntimeContainerMetadata{
		ImageName:     imageName,
		ImageID:       imageID,
		ContainerName: containerName,
		CreatedAt:     time.Now().Unix(),
	}
	mdata, err := json.Marshal(&metadata)
	if err != nil {
		return ContainerInfo{}, err
	}

	// Build the container.
	names := []string{containerName}

	container, err := r.store.CreateContainer(containerID, names, imageID, "", string(mdata), &options)
	if err != nil {
		logrus.Debugf("Failed to create container %s(%s): %v", metadata.ContainerName, containerID, err)

		return ContainerInfo{}, err
	}
	logrus.Debugf("Created container %q", container.ID)

	// If anything fails after this point, we need to delete the incomplete
	// container before returning.
	defer func() {
		if retErr != nil {
			if err := r.store.DeleteContainer(container.ID); err != nil {
				logrus.Infof("Error deleting partially-created container %q: %v", container.ID, err)

				return
			}
			logrus.Infof("Deleted partially-created container %q", container.ID)
		}
	}()

	// Add a name to the container's layer so that it's easier to follow
	// what's going on if we're just looking at the storage-eye view of things.
	layerName := metadata.ContainerName + "-layer"
	names, err = r.store.Names(container.LayerID)
	if err != nil {
		return ContainerInfo{}, err
	}
	names = append(names, layerName)
	err = r.store.SetNames(container.LayerID, names)
	if err != nil {
		return ContainerInfo{}, err
	}

	// Find out where the container work directories are, so that we can return them.
	containerDir, err := r.store.ContainerDirectory(container.ID)
	if err != nil {
		return ContainerInfo{}, err
	}
	logrus.Debugf("Container %q has work directory %q", container.ID, containerDir)

	containerRunDir, err := r.store.ContainerRunDirectory(container.ID)
	if err != nil {
		return ContainerInfo{}, err
	}
	logrus.Debugf("Container %q has run directory %q", container.ID, containerRunDir)

	return ContainerInfo{
		UIDMap:       container.UIDMap,
		GIDMap:       container.GIDMap,
		Dir:          containerDir,
		RunDir:       containerRunDir,
		Config:       imageConfig,
		ProcessLabel: container.ProcessLabel(),
		MountLabel:   container.MountLabel(),
	}, nil
}

func (r *storageService) DeleteContainer(idOrName string) error {
	if idOrName == "" {
		return define.ErrEmptyID
	}
	container, err := r.store.Container(idOrName)
	if err != nil {
		return err
	}
	err = r.store.DeleteContainer(container.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotAContainer) || errors.Is(err, storage.ErrContainerUnknown) {
			logrus.Infof("Storage for container %s already removed", container.ID)
		} else {
			logrus.Debugf("Failed to delete container %q: %v", container.ID, err)
			return err
		}
	}
	return nil
}

func (r *storageService) SetContainerMetadata(idOrName string, metadata RuntimeContainerMetadata) error {
	mdata, err := json.Marshal(&metadata)
	if err != nil {
		logrus.Debugf("Failed to encode metadata for %q: %v", idOrName, err)
		return err
	}
	return r.store.SetMetadata(idOrName, string(mdata))
}

func (r *storageService) GetContainerMetadata(idOrName string) (RuntimeContainerMetadata, error) {
	metadata := RuntimeContainerMetadata{}
	mdata, err := r.store.Metadata(idOrName)
	if err != nil {
		return metadata, err
	}
	if err = json.Unmarshal([]byte(mdata), &metadata); err != nil {
		return metadata, err
	}
	return metadata, nil
}

func (r *storageService) MountContainerImage(idOrName string) (string, error) {
	container, err := r.store.Container(idOrName)
	if err != nil {
		if errors.Is(err, storage.ErrContainerUnknown) {
			return "", define.ErrNoSuchCtr
		}
		return "", err
	}
	metadata := RuntimeContainerMetadata{}
	if err = json.Unmarshal([]byte(container.Metadata), &metadata); err != nil {
		return "", err
	}
	mountPoint, err := r.store.Mount(container.ID, metadata.MountLabel)
	if err != nil {
		logrus.Debugf("Failed to mount container %q: %v", container.ID, err)
		return "", err
	}
	logrus.Debugf("Mounted container %q at %q", container.ID, mountPoint)
	return mountPoint, nil
}

func (r *storageService) UnmountContainerImage(idOrName string, force bool) (bool, error) {
	if idOrName == "" {
		return false, define.ErrEmptyID
	}
	container, err := r.store.Container(idOrName)
	if err != nil {
		return false, err
	}

	if !force {
		mounted, err := r.store.Mounted(container.ID)
		if err != nil {
			return false, err
		}
		if mounted == 0 {
			return false, storage.ErrLayerNotMounted
		}
	}
	mounted, err := r.store.Unmount(container.ID, force)
	if err != nil {
		logrus.Debugf("Failed to unmount container %q: %v", container.ID, err)
		return false, err
	}
	logrus.Debugf("Unmounted container %q", container.ID)
	return mounted, nil
}

func (r *storageService) MountedContainerImage(idOrName string) (int, error) {
	if idOrName == "" {
		return 0, define.ErrEmptyID
	}
	container, err := r.store.Container(idOrName)
	if err != nil {
		return 0, err
	}
	mounted, err := r.store.Mounted(container.ID)
	if err != nil {
		return 0, err
	}
	return mounted, nil
}

func (r *storageService) GetMountpoint(id string) (string, error) {
	container, err := r.store.Container(id)
	if err != nil {
		if errors.Is(err, storage.ErrContainerUnknown) {
			return "", define.ErrNoSuchCtr
		}
		return "", err
	}
	layer, err := r.store.Layer(container.LayerID)
	if err != nil {
		return "", err
	}

	return layer.MountPoint, nil
}

func (r *storageService) GetWorkDir(id string) (string, error) {
	container, err := r.store.Container(id)
	if err != nil {
		if errors.Is(err, storage.ErrContainerUnknown) {
			return "", define.ErrNoSuchCtr
		}
		return "", err
	}
	return r.store.ContainerDirectory(container.ID)
}

func (r *storageService) GetRunDir(id string) (string, error) {
	container, err := r.store.Container(id)
	if err != nil {
		if errors.Is(err, storage.ErrContainerUnknown) {
			return "", define.ErrNoSuchCtr
		}
		return "", err
	}
	return r.store.ContainerRunDirectory(container.ID)
}
