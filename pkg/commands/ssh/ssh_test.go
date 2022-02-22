package ssh

import (
	"context"
	"io"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSSHHandlerHandleSSHDockerHost(t *testing.T) {
	type scenario struct {
		testName                 string
		envVarValue              string
		expectedDialContextCount int
		expectedStartCmdCount    int
	}

	scenarios := []scenario{
		{
			testName:                 "No env var set",
			envVarValue:              "",
			expectedDialContextCount: 0,
			expectedStartCmdCount:    0,
		},
		{
			testName:                 "Env var set with https scheme",
			envVarValue:              "https://myhost.com",
			expectedStartCmdCount:    0,
			expectedDialContextCount: 0,
		},
		{
			testName:                 "Env var set with ssh scheme",
			envVarValue:              "ssh://myhost@192.168.5.178",
			expectedStartCmdCount:    1,
			expectedDialContextCount: 1,
		},
	}

	for _, s := range scenarios {
		s := s
		t.Run(s.testName, func(t *testing.T) {
			getenv := func(key string) string {
				if key != "DOCKER_HOST" {
					t.Errorf("Expected key to be DOCKER_HOST, got %s", key)
				}

				return s.envVarValue
			}

			tempDir := func(dir string, pattern string) (string, error) {
				assert.Equal(t, "/tmp", dir)
				assert.Equal(t, "lazydocker-sshtunnel-", pattern)

				return "/tmp/lazydocker-ssh-tunnel-12345", nil
			}

			setenv := func(key, value string) error {
				assert.Equal(t, "DOCKER_HOST", key)
				assert.Equal(t, "unix:///tmp/lazydocker-ssh-tunnel-12345/dockerhost.sock", value)
				return nil
			}

			startCmdCount := 0
			startCmd := func(cmd *exec.Cmd) error {
				assert.EqualValues(t, []string{"ssh", "-L", "/tmp/lazydocker-ssh-tunnel-12345/dockerhost.sock:/var/run/docker.sock", "192.168.5.178", "-N"}, cmd.Args)

				startCmdCount++

				return nil
			}

			dialContextCount := 0
			dialContext := func(ctx context.Context, network string, address string) (io.Closer, error) {
				assert.Equal(t, "unix", network)
				assert.Equal(t, "/tmp/lazydocker-ssh-tunnel-12345/dockerhost.sock", address)

				dialContextCount++

				return noopCloser{}, nil
			}

			handler := &SSHHandler{
				oSCommand: &fakeCmdKiller{},

				dialContext: dialContext,
				startCmd:    startCmd,
				tempDir:     tempDir,
				getenv:      getenv,
				setenv:      setenv,
			}

			_, err := handler.HandleSSHDockerHost()
			assert.NoError(t, err)

			assert.Equal(t, s.expectedDialContextCount, dialContextCount)
			assert.Equal(t, s.expectedStartCmdCount, startCmdCount)
		})
	}
}

type fakeCmdKiller struct{}

func (self *fakeCmdKiller) Kill(cmd *exec.Cmd) error {
	return nil
}

func (self *fakeCmdKiller) PrepareForChildren(cmd *exec.Cmd) {}
