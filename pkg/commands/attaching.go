package commands

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/jesseduffield/lazydocker/pkg/commands/streamer"
)

// AttachExecContainer attach container
func (c *DockerCommand) AttachExecContainer(id string, cmd []string) error {
	exec, err := c.createExec(id, cmd)
	if err != nil {
		return err
	}

	ctx := context.TODO()

	resp, err := c.Client.ContainerExecAttach(ctx, exec.ID, types.ExecStartCheck{Tty: true})
	if err != nil {
		return err
	}
	defer resp.Close()

	f := func(ctx context.Context, id string, options types.ResizeOptions) error {
		return c.Client.ContainerExecResize(ctx, id, options)
	}

	s := streamer.New(c.Log)
	if err := s.Stream(ctx, exec.ID, resp, streamer.ResizeContainer(f)); err != nil {
		return err
	}

	return nil
}

// createExec container exec create
func (c *DockerCommand) createExec(containerId string, cmd []string) (types.IDResponse, error) {
	return c.Client.ContainerExecCreate(context.TODO(), containerId, types.ExecConfig{
		Tty:          true,
		AttachStdin:  true,
		AttachStderr: true,
		AttachStdout: true,
		Cmd:          cmd,
	})
}
