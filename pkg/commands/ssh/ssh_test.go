package ssh

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
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
				assert.Equal(t, os.TempDir(), dir)
				assert.Equal(t, "lazydocker-sshtunnel-", pattern)

				return "/tmp/lazydocker-ssh-tunnel-12345", nil
			}

			findFreePort := func() (int, error) {
				return 12345, nil
			}

			var expectedDockerHost string
			var expectedNetwork string
			var expectedAddress string
			var expectedCmdArgs []string
			if runtime.GOOS == "windows" {
				expectedDockerHost = "tcp://localhost:12345"
				expectedNetwork = "tcp"
				expectedAddress = "localhost:12345"
				expectedCmdArgs = []string{"ssh", "-L", "localhost:12345:/var/run/docker.sock", "192.168.5.178", "-N"}
			} else {
				expectedDockerHost = "unix:///tmp/lazydocker-ssh-tunnel-12345/dockerhost.sock"
				expectedNetwork = "unix"
				expectedAddress = "/tmp/lazydocker-ssh-tunnel-12345/dockerhost.sock"
				expectedCmdArgs = []string{"ssh", "-L", "/tmp/lazydocker-ssh-tunnel-12345/dockerhost.sock:/var/run/docker.sock", "192.168.5.178", "-N"}
			}

			setenv := func(key, value string) error {
				assert.Equal(t, "DOCKER_HOST", key)
				assert.Equal(t, expectedDockerHost, value)
				return nil
			}

			startCmdCount := 0
			startCmd := func(cmd *exec.Cmd) error {
				assert.EqualValues(t, expectedCmdArgs, cmd.Args)

				startCmdCount++

				return nil
			}

			dialContextCount := 0
			dialContext := func(ctx context.Context, network string, address string) (io.Closer, error) {
				assert.Equal(t, expectedNetwork, network)
				assert.Equal(t, expectedAddress, address)

				dialContextCount++

				return noopCloser{}, nil
			}

			handler := &SSHHandler{
				oSCommand: &fakeCmdKiller{},

				dialContext:  dialContext,
				startCmd:     startCmd,
				tempDir:      tempDir,
				findFreePort: findFreePort,
				getenv:       getenv,
				setenv:       setenv,
			}

			_, err := handler.HandleSSHDockerHost()
			assert.NoError(t, err)

			assert.Equal(t, s.expectedDialContextCount, dialContextCount)
			assert.Equal(t, s.expectedStartCmdCount, startCmdCount)
		})
	}
}

func TestCreateDockerHostTunnelUnix(t *testing.T) {
	remoteHost := "192.168.5.178"
	socketDir := "/tmp/lazydocker-ssh-tunnel-12345"
	expectedNetwork := "unix"
	expectedAddress := socketDir + "/dockerhost.sock"
	expectedDockerHost := "unix://" + expectedAddress
	expectedCmdArgs := []string{"ssh", "-L", expectedAddress + ":/var/run/docker.sock", remoteHost, "-N"}

	tempDir := func(dir string, pattern string) (string, error) {
		return socketDir, nil
	}

	startCmd := func(cmd *exec.Cmd) error {
		assert.EqualValues(t, expectedCmdArgs, cmd.Args)
		return nil
	}

	dialContext := func(ctx context.Context, network string, address string) (io.Closer, error) {
		assert.Equal(t, expectedNetwork, network)
		assert.Equal(t, expectedAddress, address)
		return noopCloser{}, nil
	}

	handler := &SSHHandler{
		oSCommand:    &fakeCmdKiller{},
		dialContext:  dialContext,
		startCmd:     startCmd,
		tempDir:      tempDir,
		findFreePort: func() (int, error) { return 0, nil },
		getenv:       os.Getenv,
		setenv:       func(k, v string) error { return nil },
	}

	tunnel, err := handler.createDockerHostTunnelUnix(context.Background(), remoteHost)
	assert.NoError(t, err)
	assert.Equal(t, expectedDockerHost, tunnel.socketPath)
}

func TestCreateDockerHostTunnelTCP(t *testing.T) {
	remoteHost := "192.168.5.178"
	port := 54321
	expectedNetwork := "tcp"
	expectedAddress := fmt.Sprintf("localhost:%d", port)
	expectedDockerHost := "tcp://" + expectedAddress
	expectedCmdArgs := []string{"ssh", "-L", expectedAddress + ":/var/run/docker.sock", remoteHost, "-N"}

	startCmd := func(cmd *exec.Cmd) error {
		assert.EqualValues(t, expectedCmdArgs, cmd.Args)
		return nil
	}

	dialContext := func(ctx context.Context, network string, address string) (io.Closer, error) {
		assert.Equal(t, expectedNetwork, network)
		assert.Equal(t, expectedAddress, address)
		return noopCloser{}, nil
	}

	handler := &SSHHandler{
		oSCommand:    &fakeCmdKiller{},
		dialContext:  dialContext,
		startCmd:     startCmd,
		tempDir:      func(string, string) (string, error) { return "", nil },
		findFreePort: func() (int, error) { return port, nil },
		getenv:       os.Getenv,
		setenv:       func(k, v string) error { return nil },
	}

	tunnel, err := handler.createDockerHostTunnelTCP(context.Background(), remoteHost)
	assert.NoError(t, err)
	assert.Equal(t, expectedDockerHost, tunnel.socketPath)
}

func TestCreateDockerHostTunnelTCP_FindFreePortError(t *testing.T) {
	remoteHost := "192.168.5.178"
	handler := &SSHHandler{
		oSCommand:    &fakeCmdKiller{},
		findFreePort: func() (int, error) { return 0, fmt.Errorf("no ports available") },
	}

	_, err := handler.createDockerHostTunnelTCP(context.Background(), remoteHost)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "find free port for ssh tunnel")
}

func TestCreateDockerHostTunnelTCP_TunnelSSHError(t *testing.T) {
	remoteHost := "192.168.5.178"
	handler := &SSHHandler{
		oSCommand:    &fakeCmdKiller{},
		findFreePort: func() (int, error) { return 54321, nil },
		startCmd:     func(cmd *exec.Cmd) error { return fmt.Errorf("ssh not found") },
	}

	_, err := handler.createDockerHostTunnelTCP(context.Background(), remoteHost)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tunnel docker host over ssh")
}

type fakeCmdKiller struct{}

func (self *fakeCmdKiller) Kill(cmd *exec.Cmd) error {
	return nil
}

func (self *fakeCmdKiller) PrepareForChildren(cmd *exec.Cmd) {}
