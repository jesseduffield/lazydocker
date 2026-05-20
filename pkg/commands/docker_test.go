package commands

import (
	"errors"
	"os"
	"testing"

	"github.com/docker/docker/client"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/stretchr/testify/assert"
)

// TestNewDockerClientVersionNegotiation verifies that newDockerClient allows
// API version negotiation even when DOCKER_API_VERSION is set.
//
// This is a regression test for https://github.com/jesseduffield/lazydocker/issues/715
// where users got "client version 1.25 is too old" errors because FromEnv()
// includes WithVersionFromEnv() which sets manualOverride=true, preventing
// API version negotiation.
func TestNewDockerClientVersionNegotiation(t *testing.T) {
	// Save original env var and restore after test
	originalAPIVersion := os.Getenv("DOCKER_API_VERSION")
	defer func() {
		if originalAPIVersion == "" {
			os.Unsetenv("DOCKER_API_VERSION")
		} else {
			os.Setenv("DOCKER_API_VERSION", originalAPIVersion)
		}
	}()

	// Set DOCKER_API_VERSION to an old version that would cause
	// "client version 1.25 is too old" errors if negotiation is disabled
	os.Setenv("DOCKER_API_VERSION", "1.25")

	t.Run("FromEnv locks version preventing negotiation", func(t *testing.T) {
		// This demonstrates the problematic behavior we're avoiding.
		// When using FromEnv with DOCKER_API_VERSION set, the client
		// version gets locked to 1.25 and negotiation is disabled.
		cli, err := client.NewClientWithOpts(
			client.FromEnv,
			client.WithAPIVersionNegotiation(),
		)
		assert.NoError(t, err)
		defer cli.Close()

		// Version is locked to the env var value
		assert.Equal(t, "1.25", cli.ClientVersion())
	})

	t.Run("newDockerClient allows version negotiation", func(t *testing.T) {
		// Test the actual production function.
		// Use DefaultDockerHost for cross-platform compatibility
		// (unix socket on Linux/macOS, named pipe on Windows).
		cli, err := newDockerClient(client.DefaultDockerHost)
		assert.NoError(t, err)
		defer cli.Close()

		// Version is NOT locked to the env var value (1.25).
		// Instead, it uses the library's default version and will negotiate
		// with the server on first request. This is the key difference that
		// fixes the "version too old" error.
		assert.NotEqual(t, "1.25", cli.ClientVersion(),
			"client version should not be locked to DOCKER_API_VERSION env var")
	})
}

// TestIsProjectScoped covers the predicate that drives whether the
// project/services panels appear and whether the containers panel filters by
// project. The "outside compose dir + -p" case is the regression we fixed
// after PR #776 silently disabled it.
func TestIsProjectScoped(t *testing.T) {
	cases := []struct {
		name                   string
		inDockerComposeProject bool
		projectName            string
		want                   bool
	}{
		{"inside compose dir, no -p", true, "", true},
		{"inside compose dir, with -p", true, "myproject", true},
		{"outside compose dir, no -p", false, "", false},
		{"outside compose dir, with -p", false, "myproject", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &DockerCommand{
				InDockerComposeProject: tc.inDockerComposeProject,
				Config:                 &config.AppConfig{ProjectName: tc.projectName},
			}
			assert.Equal(t, tc.want, c.IsProjectScoped())
		})
	}
}

// TestNewCommandObjectProfileInjection verifies that NewCommandObject appends
// `-p <LocalProjectName>` and `--profile <name>` correctly for the various
// ways a caller can identify a profile, and that the existing project/service
// `-p` injection isn't disturbed by the new code paths.
func TestNewCommandObjectProfileInjection(t *testing.T) {
	cases := []struct {
		name             string
		localProjectName string
		obj              CommandObject
		want             string
	}{
		{
			name: "no project, no profile",
			obj:  CommandObject{},
			want: "docker compose",
		},
		{
			name:             "profile via Profile field with LocalProjectName",
			localProjectName: "myapp",
			obj:              CommandObject{Profile: "dev"},
			want:             "docker compose -p myapp --profile dev",
		},
		{
			name:             "profile via Project pseudo-project with LocalProjectName",
			localProjectName: "myapp",
			obj:              CommandObject{Project: &Project{Name: "dev", IsProfile: true}},
			want:             "docker compose -p myapp --profile dev",
		},
		{
			name: "non-profile Project still gets -p with its own name",
			obj:  CommandObject{Project: &Project{Name: "other"}},
			want: "docker compose -p other",
		},
		{
			name: "Service supplies -p; profile only adds --profile",
			obj: CommandObject{
				Service: &Service{ProjectName: "svcproj"},
				Profile: "dev",
			},
			want: "docker compose -p svcproj --profile dev",
		},
		{
			name:             "profile without LocalProjectName emits --profile only",
			localProjectName: "",
			obj:              CommandObject{Profile: "dev"},
			want:             "docker compose --profile dev",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestDockerCommand(nil)
			c.LocalProjectName = tc.localProjectName
			got := c.NewCommandObject(tc.obj).DockerCompose
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestGetProfilesCaching exercises the per-session cache: the underlying
// command is invoked exactly once even across repeated calls, output is
// split/sorted/deduped, and the !InDockerComposeProject path short-circuits.
func TestGetProfilesCaching(t *testing.T) {
	t.Run("caches across calls, dedupes and sorts", func(t *testing.T) {
		calls := 0
		c := newTestDockerCommand(func(cmd string) (string, error) {
			calls++
			assert.Equal(t, "docker compose config --profiles", cmd)
			return "regulator\nobligations\nregulator\n", nil
		})
		got1, err := c.GetProfiles()
		assert.NoError(t, err)
		assert.Equal(t, []string{"obligations", "regulator"}, got1)
		got2, err := c.GetProfiles()
		assert.NoError(t, err)
		assert.Equal(t, got1, got2)
		assert.Equal(t, 1, calls)
	})
	t.Run("short-circuits when not in compose project", func(t *testing.T) {
		calls := 0
		c := newTestDockerCommand(func(cmd string) (string, error) {
			calls++
			return "", nil
		})
		c.InDockerComposeProject = false
		got, err := c.GetProfiles()
		assert.NoError(t, err)
		assert.Nil(t, got)
		assert.Equal(t, 0, calls)
	})
}

// TestGetProfileServicesCaching ensures the per-profile cache invokes the
// underlying command once per distinct profile, not once per call.
func TestGetProfileServicesCaching(t *testing.T) {
	calls := 0
	c := newTestDockerCommand(func(cmd string) (string, error) {
		calls++
		switch cmd {
		case "docker compose --profile a config --services":
			return "svc-a1\nsvc-a2\n", nil
		case "docker compose --profile b config --services":
			return "svc-b\n", nil
		}
		t.Fatalf("unexpected command: %q", cmd)
		return "", nil
	})
	a1, err := c.GetProfileServices("a")
	assert.NoError(t, err)
	assert.Equal(t, []string{"svc-a1", "svc-a2"}, a1)
	a2, err := c.GetProfileServices("a")
	assert.NoError(t, err)
	assert.Equal(t, a1, a2)
	b1, err := c.GetProfileServices("b")
	assert.NoError(t, err)
	assert.Equal(t, []string{"svc-b"}, b1)
	assert.Equal(t, 2, calls, "one call per distinct profile, cached thereafter")
}

// TestGetProfilesSoftFailsOnError verifies that errors from the compose
// shellout become a logged warning + empty cache rather than propagating
// to the UI, and that a subsequent call doesn't retry.
func TestGetProfilesSoftFailsOnError(t *testing.T) {
	calls := 0
	c := newTestDockerCommand(func(cmd string) (string, error) {
		calls++
		return "", errors.New("compose not installed")
	})
	got, err := c.GetProfiles()
	assert.NoError(t, err, "errors are soft-failed; UI sees empty list")
	assert.Nil(t, got)
	got2, err := c.GetProfiles()
	assert.NoError(t, err)
	assert.Equal(t, got, got2)
	assert.Equal(t, 1, calls, "error result is cached; no retry")
}
