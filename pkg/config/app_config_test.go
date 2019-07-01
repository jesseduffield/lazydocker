package config

import (
	"testing"
)

func TestDockerComposeCommandNoFiles(t *testing.T) {
	composeFiles := []string{}
	conf, err := NewAppConfig("name", "version", "commit", "date", "buildSource", false, composeFiles)

	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	actual := conf.UserConfig.CommandTemplates.DockerCompose
	expected := "docker-compose"
	if actual != expected {
		t.Fatalf("Expected %s but got %s", expected, actual)
	}
}

func TestDockerComposeCommandSingleFile(t *testing.T) {
	composeFiles := []string{"one.yml"}
	conf, err := NewAppConfig("name", "version", "commit", "date", "buildSource", false, composeFiles)

	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	actual := conf.UserConfig.CommandTemplates.DockerCompose
	expected := "docker-compose -f one.yml"
	if actual != expected {
		t.Fatalf("Expected %s but got %s", expected, actual)
	}
}

func TestDockerComposeCommandMultipleFiles(t *testing.T) {
	composeFiles := []string{"one.yml", "two.yml", "three.yml"}
	conf, err := NewAppConfig("name", "version", "commit", "date", "buildSource", false, composeFiles)

	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	actual := conf.UserConfig.CommandTemplates.DockerCompose
	expected := "docker-compose -f one.yml -f two.yml -f three.yml"
	if actual != expected {
		t.Fatalf("Expected %s but got %s", expected, actual)
	}
}
