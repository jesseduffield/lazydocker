//go:build linux && cgo && !exclude_disk_quota

package overlay

import (
	"path"

	"go.podman.io/storage/pkg/directory"
)

// ReadWriteDiskUsage returns the disk usage of the writable directory for the ID.
// For Overlay, it attempts to check the XFS quota for size, and falls back to
// finding the size of the "diff" directory.
func (d *Driver) ReadWriteDiskUsage(id string) (*directory.DiskUsage, error) {
	usage := &directory.DiskUsage{}
	if d.quotaCtl != nil {
		err := d.quotaCtl.GetDiskUsage(d.dir(id), usage)
		return usage, err
	}
	return directory.Usage(path.Join(d.dir(id), "diff"))
}
