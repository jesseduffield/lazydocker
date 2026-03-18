package config

import (
	"os"
	"testing"

	"github.com/jesseduffield/yaml"
)

func TestWritingToConfigFile(t *testing.T) {
	emptyComposeFiles := []string{}
	conf, err := NewAppConfig("name", "version", "commit", "date", "buildSource", false, emptyComposeFiles, "projectDir", "")
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

	testFn(t, conf, true)
	testFn(t, conf, false)
}
