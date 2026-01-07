//go:build !remote

package libpod

import (
	"fmt"
	"os"
	"strconv"
	"syscall"

	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/containers/podman/v5/pkg/util"
)

func (r *Runtime) stopPauseProcess() error {
	if rootless.IsRootless() {
		pausePidPath, err := util.GetRootlessPauseProcessPidPath()
		if err != nil {
			return fmt.Errorf("could not get pause process pid file path: %w", err)
		}
		data, err := os.ReadFile(pausePidPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("cannot read pause process pid file: %w", err)
		}
		pausePid, err := strconv.Atoi(string(data))
		if err != nil {
			return fmt.Errorf("cannot parse pause pid file %s: %w", pausePidPath, err)
		}
		if err := os.Remove(pausePidPath); err != nil {
			return fmt.Errorf("cannot delete pause pid file %s: %w", pausePidPath, err)
		}
		if err := syscall.Kill(pausePid, syscall.SIGKILL); err != nil {
			return err
		}
	}
	return nil
}
