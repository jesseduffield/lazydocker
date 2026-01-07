package buildah

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/containers/buildah/copier"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/selinux/go-selinux"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/pkg/sysregistriesv2"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/reexec"
)

// InitReexec is a wrapper for reexec.Init().  It should be called at
// the start of main(), and if it returns true, main() should return
// immediately.
func InitReexec() bool {
	return reexec.Init()
}

func copyHistory(history []v1.History) []v1.History {
	if len(history) == 0 {
		return nil
	}
	h := make([]v1.History, 0, len(history))
	for _, entry := range history {
		created := entry.Created
		if created != nil {
			timestamp := *created
			created = &timestamp
		}
		h = append(h, v1.History{
			Created:    created,
			CreatedBy:  entry.CreatedBy,
			Author:     entry.Author,
			Comment:    entry.Comment,
			EmptyLayer: entry.EmptyLayer,
		})
	}
	return h
}

func convertStorageIDMaps(UIDMap, GIDMap []idtools.IDMap) ([]rspec.LinuxIDMapping, []rspec.LinuxIDMapping) {
	uidmap := make([]rspec.LinuxIDMapping, 0, len(UIDMap))
	gidmap := make([]rspec.LinuxIDMapping, 0, len(GIDMap))
	for _, m := range UIDMap {
		uidmap = append(uidmap, rspec.LinuxIDMapping{
			HostID:      uint32(m.HostID),
			ContainerID: uint32(m.ContainerID),
			Size:        uint32(m.Size),
		})
	}
	for _, m := range GIDMap {
		gidmap = append(gidmap, rspec.LinuxIDMapping{
			HostID:      uint32(m.HostID),
			ContainerID: uint32(m.ContainerID),
			Size:        uint32(m.Size),
		})
	}
	return uidmap, gidmap
}

func convertRuntimeIDMaps(UIDMap, GIDMap []rspec.LinuxIDMapping) ([]idtools.IDMap, []idtools.IDMap) {
	uidmap := make([]idtools.IDMap, 0, len(UIDMap))
	gidmap := make([]idtools.IDMap, 0, len(GIDMap))
	for _, m := range UIDMap {
		uidmap = append(uidmap, idtools.IDMap{
			HostID:      int(m.HostID),
			ContainerID: int(m.ContainerID),
			Size:        int(m.Size),
		})
	}
	for _, m := range GIDMap {
		gidmap = append(gidmap, idtools.IDMap{
			HostID:      int(m.HostID),
			ContainerID: int(m.ContainerID),
			Size:        int(m.Size),
		})
	}
	return uidmap, gidmap
}

// isRegistryBlocked checks if the named registry is marked as blocked
func isRegistryBlocked(registry string, sc *types.SystemContext) (bool, error) {
	reginfo, err := sysregistriesv2.FindRegistry(sc, registry)
	if err != nil {
		return false, fmt.Errorf("unable to parse the registries configuration (%s): %w", sysregistriesv2.ConfigPath(sc), err)
	}
	if reginfo != nil {
		if reginfo.Blocked {
			logrus.Debugf("registry %q is marked as blocked in registries configuration %q", registry, sysregistriesv2.ConfigPath(sc))
		} else {
			logrus.Debugf("registry %q is not marked as blocked in registries configuration %q", registry, sysregistriesv2.ConfigPath(sc))
		}
		return reginfo.Blocked, nil
	}
	logrus.Debugf("registry %q is not listed in registries configuration %q, assuming it's not blocked", registry, sysregistriesv2.ConfigPath(sc))
	return false, nil
}

// isReferenceSomething checks if the registry part of a reference is insecure or blocked
func isReferenceSomething(ref types.ImageReference, sc *types.SystemContext, what func(string, *types.SystemContext) (bool, error)) (bool, error) {
	if ref != nil {
		if named := ref.DockerReference(); named != nil {
			if domain := reference.Domain(named); domain != "" {
				return what(domain, sc)
			}
		}
	}
	return false, nil
}

// isReferenceBlocked checks if the registry part of a reference is blocked
func isReferenceBlocked(ref types.ImageReference, sc *types.SystemContext) (bool, error) {
	if ref != nil && ref.Transport() != nil {
		switch ref.Transport().Name() {
		case "docker":
			return isReferenceSomething(ref, sc, isRegistryBlocked)
		}
	}
	return false, nil
}

// ReserveSELinuxLabels reads containers storage and reserves SELinux contexts
// which are already being used by buildah containers.
func ReserveSELinuxLabels(store storage.Store, id string) error {
	if selinuxGetEnabled() {
		containers, err := store.Containers()
		if err != nil {
			return fmt.Errorf("getting list of containers: %w", err)
		}

		for _, c := range containers {
			if id == c.ID {
				continue
			}
			b, err := OpenBuilder(store, c.ID)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					// Ignore not exist errors since containers probably created by other tool
					// TODO, we need to read other containers json data to reserve their SELinux labels
					continue
				}
				return err
			}
			// Prevent different containers from using same MCS label
			selinux.ReserveLabel(b.ProcessLabel)
		}
	}
	return nil
}

// IsContainer identifies if the specified container id is a buildah container
// in the specified store.
func IsContainer(id string, store storage.Store) (bool, error) {
	cdir, err := store.ContainerDirectory(id)
	if err != nil {
		return false, err
	}
	// Assuming that if the stateFile exists, that this is a Buildah
	// container.
	if _, err = os.Stat(filepath.Join(cdir, stateFile)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Copy content from the directory "src" to the directory "dest", ensuring that
// content from outside of "root" (which is a parent of "src" or "src" itself)
// isn't read.
func extractWithTar(root, src, dest string) error {
	var getErr, putErr error
	var wg sync.WaitGroup

	pipeReader, pipeWriter := io.Pipe()

	wg.Add(1)
	go func() {
		getErr = copier.Get(root, src, copier.GetOptions{}, []string{"."}, pipeWriter)
		pipeWriter.Close()
		wg.Done()
	}()
	wg.Add(1)
	go func() {
		putErr = copier.Put(dest, dest, copier.PutOptions{}, pipeReader)
		pipeReader.Close()
		wg.Done()
	}()
	wg.Wait()

	if getErr != nil {
		return fmt.Errorf("reading %q: %w", src, getErr)
	}
	if putErr != nil {
		return fmt.Errorf("copying contents of %q to %q: %w", src, dest, putErr)
	}
	return nil
}
