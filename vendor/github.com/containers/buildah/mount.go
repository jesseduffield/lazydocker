package buildah

import "fmt"

// Mount mounts a container's root filesystem in a location which can be
// accessed from the host, and returns the location.
func (b *Builder) Mount(label string) (string, error) {
	mountpoint, err := b.store.Mount(b.ContainerID, label)
	if err != nil {
		return "", fmt.Errorf("mounting build container %q: %w", b.ContainerID, err)
	}
	b.MountPoint = mountpoint

	err = b.Save()
	if err != nil {
		return "", fmt.Errorf("saving updated state for build container %q: %w", b.ContainerID, err)
	}
	return mountpoint, nil
}

func (b *Builder) setMountPoint(mountPoint string) error {
	b.MountPoint = mountPoint
	if err := b.Save(); err != nil {
		return fmt.Errorf("saving updated state for build container %q: %w", b.ContainerID, err)
	}
	return nil
}

// Mounted returns whether the container is mounted or not
func (b *Builder) Mounted() (bool, error) {
	mountCnt, err := b.store.Mounted(b.ContainerID)
	if err != nil {
		return false, fmt.Errorf("determining if mounting build container %q is mounted: %w", b.ContainerID, err)
	}
	mounted := mountCnt > 0
	if mounted && b.MountPoint == "" {
		ctr, err := b.store.Container(b.ContainerID)
		if err != nil {
			return mountCnt > 0, fmt.Errorf("determining if mounting build container %q is mounted: %w", b.ContainerID, err)
		}
		layer, err := b.store.Layer(ctr.LayerID)
		if err != nil {
			return mountCnt > 0, fmt.Errorf("determining if mounting build container %q is mounted: %w", b.ContainerID, err)
		}
		return mounted, b.setMountPoint(layer.MountPoint)
	}
	if !mounted && b.MountPoint != "" {
		return mounted, b.setMountPoint("")
	}
	return mounted, nil
}
