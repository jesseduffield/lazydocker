package config

import (
	"errors"
	"fmt"
	"os"

	dockerclient "github.com/fsouza/go-dockerclient"
	"github.com/openshift/imagebuilder"
)

// configOnlyExecutor implements the Executor interface that an
// imagebuilder.Builder expects to be able to call to do some heavy lifting,
// but it just refuses to do the work of ADD, COPY, or RUN.  It also doesn't
// care if the working directory exists in a container, because it's really
// only concerned with letting the Builder's RunConfig get updated by changes
// from a Dockerfile.  Try anything more than that and it'll return an error.
type configOnlyExecutor struct{}

func (g *configOnlyExecutor) Preserve(_ string) error {
	return errors.New("ADD/COPY/RUN not supported as changes")
}

func (g *configOnlyExecutor) EnsureContainerPath(_ string) error {
	return nil
}

func (g *configOnlyExecutor) EnsureContainerPathAs(_, _ string, _ *os.FileMode) error {
	return nil
}

func (g *configOnlyExecutor) Copy(_ []string, copies ...imagebuilder.Copy) error {
	if len(copies) == 0 {
		return nil
	}
	return errors.New("ADD/COPY not supported as changes")
}

func (g *configOnlyExecutor) Run(_ imagebuilder.Run, _ dockerclient.Config) error {
	return errors.New("RUN not supported as changes")
}

func (g *configOnlyExecutor) UnrecognizedInstruction(step *imagebuilder.Step) error {
	return fmt.Errorf("did not understand change instruction %q", step.Original)
}
