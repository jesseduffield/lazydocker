package config

import (
	"os"
	"testing"

	"github.com/jesseduffield/yaml"
)

func TestDockerComposeCommandNoFiles(t *testing.T) {
	composeFiles := []string{}
	conf, err := NewAppConfig("name", "version", "commit", "date", "buildSource", false, composeFiles, "projectDir", "docker")
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	actual := conf.UserConfig.CommandTemplates.DockerCompose
	expected := "docker compose"
	if actual != expected {
		t.Fatalf("Expected %s but got %s", expected, actual)
	}
}

func TestDockerComposeCommandSingleFile(t *testing.T) {
	composeFiles := []string{"one.yml"}
	conf, err := NewAppConfig("name", "version", "commit", "date", "buildSource", false, composeFiles, "projectDir", "docker")
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	actual := conf.UserConfig.CommandTemplates.DockerCompose
	expected := "docker compose -f one.yml"
	if actual != expected {
		t.Fatalf("Expected %s but got %s", expected, actual)
	}
}

func TestDockerComposeCommandMultipleFiles(t *testing.T) {
	composeFiles := []string{"one.yml", "two.yml", "three.yml"}
	conf, err := NewAppConfig("name", "version", "commit", "date", "buildSource", false, composeFiles, "projectDir", "docker")
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	actual := conf.UserConfig.CommandTemplates.DockerCompose
	expected := "docker compose -f one.yml -f two.yml -f three.yml"
	if actual != expected {
		t.Fatalf("Expected %s but got %s", expected, actual)
	}
}

func TestWritingToConfigFile(t *testing.T) {
	// init the AppConfig
	emptyComposeFiles := []string{}
	conf, err := NewAppConfig("name", "version", "commit", "date", "buildSource", false, emptyComposeFiles, "projectDir", "docker")
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	testFn := func(t *testing.T, ac *AppConfig, newValue bool) {
		t.Helper()
		updateFn := func(uc *UserConfig) error {
			uc.ConfirmOnQuit = newValue
			return nil
		}

		err = ac.WriteToUserConfig(updateFn)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		file, err := os.OpenFile(ac.ConfigFilename(), os.O_RDONLY, 0o660)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		sampleUC := UserConfig{}
		err = yaml.NewDecoder(file).Decode(&sampleUC)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		err = file.Close()
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		if sampleUC.ConfirmOnQuit != newValue {
			t.Fatalf("Got %v, Expected %v\n", sampleUC.ConfirmOnQuit, newValue)
		}
	}

	// insert value into an empty file
	testFn(t, conf, true)

	// modifying an existing file that already has 'ConfirmOnQuit'
	testFn(t, conf, false)
}

// Test runtime parameter validation and configuration
func TestRuntimeValidation(t *testing.T) {
	composeFiles := []string{}

	// Test valid docker runtime
	conf, err := NewAppConfig("name", "version", "commit", "date", "buildSource", false, composeFiles, "projectDir", "docker")
	if err != nil {
		t.Fatalf("Unexpected error for docker runtime: %s", err)
	}
	if conf.Runtime != "docker" {
		t.Fatalf("Expected runtime 'docker' but got '%s'", conf.Runtime)
	}

	// Test valid apple runtime
	conf, err = NewAppConfig("name", "version", "commit", "date", "buildSource", false, composeFiles, "projectDir", "apple")
	if err != nil {
		t.Fatalf("Unexpected error for apple runtime: %s", err)
	}
	if conf.Runtime != "apple" {
		t.Fatalf("Expected runtime 'apple' but got '%s'", conf.Runtime)
	}

	// Test invalid runtime
	_, err = NewAppConfig("name", "version", "commit", "date", "buildSource", false, composeFiles, "projectDir", "invalid")
	if err == nil {
		t.Fatalf("Expected error for invalid runtime but got none")
	}
	expectedError := "unsupported runtime 'invalid'. Supported runtimes: docker, apple"
	if err.Error() != expectedError {
		t.Fatalf("Expected error '%s' but got '%s'", expectedError, err.Error())
	}
}

func TestRuntimeConfigurationDefaults(t *testing.T) {
	composeFiles := []string{}

	// Test docker runtime gets correct defaults
	conf, err := NewAppConfig("name", "version", "commit", "date", "buildSource", false, composeFiles, "projectDir", "docker")
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	// Docker runtime should have normal docker-compose command
	expected := "docker compose"
	actual := conf.UserConfig.CommandTemplates.DockerCompose
	if actual != expected {
		t.Fatalf("Expected DockerCompose command '%s' but got '%s'", expected, actual)
	}
}

func TestRuntimeFieldInAppConfig(t *testing.T) {
	composeFiles := []string{}

	testCases := []struct {
		runtime  string
		expected string
	}{
		{"docker", "docker"},
		{"apple", "apple"},
	}

	for _, tc := range testCases {
		conf, err := NewAppConfig("name", "version", "commit", "date", "buildSource", false, composeFiles, "projectDir", tc.runtime)
		if err != nil {
			t.Fatalf("Unexpected error for runtime '%s': %s", tc.runtime, err)
		}

		if conf.Runtime != tc.expected {
			t.Fatalf("Expected Runtime field '%s' but got '%s'", tc.expected, conf.Runtime)
		}
	}
}
