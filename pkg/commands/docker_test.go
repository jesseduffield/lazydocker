package commands

import (
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

// TestParseComposeConfigJSON verifies we pull the resolved project name and
// service list out of `docker compose config --format json` output.
// Regression context: https://github.com/jesseduffield/lazydocker/issues/842
func TestParseComposeConfigJSON(t *testing.T) {
	output := `{"name":"matomo","services":{"db":{"image":"mariadb"},"app":{"image":"matomo"}},"networks":{"default":{}},"volumes":{}}`
	name, services, err := parseComposeConfigJSON(output)
	assert.NoError(t, err)
	assert.Equal(t, "matomo", name)
	assert.Equal(t, []string{"app", "db"}, services)
}

func TestParseComposeConfigJSONInvalid(t *testing.T) {
	_, _, err := parseComposeConfigJSON("not json at all")
	assert.Error(t, err)
}

func TestParseComposeConfigJSONEmptyServices(t *testing.T) {
	name, services, err := parseComposeConfigJSON(`{"name":"lonely","services":{}}`)
	assert.NoError(t, err)
	assert.Equal(t, "lonely", name)
	assert.Empty(t, services)
}

// TestAssignContainersToServicesPicksOwnProject is a regression test for
// https://github.com/jesseduffield/lazydocker/issues/842: two compose projects
// running a service with the same name must not get each other's containers.
func TestAssignContainersToServicesPicksOwnProject(t *testing.T) {
	c := &DockerCommand{LocalProjectName: "matomo"}

	services := []*Service{
		{Name: "app", ProjectName: "matomo"},
	}
	foreign := &Container{ID: "foreign-id", ServiceName: "app", ProjectName: "env2"}
	local := &Container{ID: "local-id", ServiceName: "app", ProjectName: "matomo"}
	// foreign container first in daemon list order, like the real bug
	containers := []*Container{foreign, local}

	c.assignContainersToServices(containers, services)

	assert.NotNil(t, services[0].Container)
	assert.Equal(t, "local-id", services[0].Container.ID)
}
