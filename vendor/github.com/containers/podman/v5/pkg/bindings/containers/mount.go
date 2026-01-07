package containers

import (
	"context"
	"net/http"

	"github.com/containers/podman/v5/pkg/bindings"
)

// Mount mounts an existing container to the filesystem. It returns the path
// of the mounted container in string format.
func Mount(ctx context.Context, nameOrID string, options *MountOptions) (string, error) {
	if options == nil {
		options = new(MountOptions)
	}
	_ = options
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return "", err
	}
	var (
		path string
	)
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/containers/%s/mount", nil, nil, nameOrID)
	if err != nil {
		return path, err
	}
	defer response.Body.Close()

	return path, response.Process(&path)
}

// Unmount unmounts a container from the filesystem.  The container must not be running
// or the unmount will fail.
func Unmount(ctx context.Context, nameOrID string, options *UnmountOptions) error {
	if options == nil {
		options = new(UnmountOptions)
	}
	_ = options
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/containers/%s/unmount", nil, nil, nameOrID)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	return response.Process(nil)
}

// GetMountedContainerPaths returns a map of mounted containers and their mount locations.
func GetMountedContainerPaths(ctx context.Context, options *MountedContainerPathsOptions) (map[string]string, error) {
	if options == nil {
		options = new(MountedContainerPathsOptions)
	}
	_ = options
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	mounts := make(map[string]string)
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/containers/showmounted", nil, nil)
	if err != nil {
		return mounts, err
	}
	defer response.Body.Close()

	return mounts, response.Process(&mounts)
}
