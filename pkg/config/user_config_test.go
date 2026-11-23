package config

import (
	"testing"

	"github.com/jesseduffield/yaml"
)

func TestGetDefaultKeybindings(t *testing.T) {
	defaults := GetDefaultKeybindings()

	// Test Universal section has expected defaults
	if defaults.Universal.Quit != "q" {
		t.Errorf("Expected Universal.Quit to be 'q', got '%s'", defaults.Universal.Quit)
	}
	if defaults.Universal.QuitAlt != "<c-c>" {
		t.Errorf("Expected Universal.QuitAlt to be '<c-c>', got '%s'", defaults.Universal.QuitAlt)
	}
	if defaults.Universal.Return != "<esc>" {
		t.Errorf("Expected Universal.Return to be '<esc>', got '%s'", defaults.Universal.Return)
	}

	// Test Containers section
	if defaults.Containers.Remove != "d" {
		t.Errorf("Expected Containers.Remove to be 'd', got '%s'", defaults.Containers.Remove)
	}
	if defaults.Containers.Stop != "s" {
		t.Errorf("Expected Containers.Stop to be 's', got '%s'", defaults.Containers.Stop)
	}

	// Test Services section
	if defaults.Services.Up != "u" {
		t.Errorf("Expected Services.Up to be 'u', got '%s'", defaults.Services.Up)
	}
	if defaults.Services.Start != "S" {
		t.Errorf("Expected Services.Start to be 'S', got '%s'", defaults.Services.Start)
	}

	// Test Images section
	if defaults.Images.Remove != "d" {
		t.Errorf("Expected Images.Remove to be 'd', got '%s'", defaults.Images.Remove)
	}

	// Test Project section
	if defaults.Project.EditConfig != "e" {
		t.Errorf("Expected Project.EditConfig to be 'e', got '%s'", defaults.Project.EditConfig)
	}

	// Test Main section
	if defaults.Main.Return != "<esc>" {
		t.Errorf("Expected Main.Return to be '<esc>', got '%s'", defaults.Main.Return)
	}

	// Test Menu section
	if defaults.Menu.Select != " " {
		t.Errorf("Expected Menu.Select to be ' ' (space), got '%s'", defaults.Menu.Select)
	}

	// Test Filter section
	if defaults.Filter.Confirm != "<enter>" {
		t.Errorf("Expected Filter.Confirm to be '<enter>', got '%s'", defaults.Filter.Confirm)
	}
}

func TestKeybindingConfigYAMLUnmarshal(t *testing.T) {
	yamlContent := `
universal:
  quit: 'Q'
  prevMainTab: '-'
  nextMainTab: '='
containers:
  remove: 'D'
  stop: 'S'
services:
  up: 'U'
`

	var config KeybindingConfig
	err := yaml.Unmarshal([]byte(yamlContent), &config)
	if err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	// Test unmarshaled values
	if config.Universal.Quit != "Q" {
		t.Errorf("Expected Quit to be 'Q', got '%s'", config.Universal.Quit)
	}
	if config.Universal.PrevMainTab != "-" {
		t.Errorf("Expected PrevMainTab to be '-', got '%s'", config.Universal.PrevMainTab)
	}
	if config.Universal.NextMainTab != "=" {
		t.Errorf("Expected NextMainTab to be '=', got '%s'", config.Universal.NextMainTab)
	}
	if config.Containers.Remove != "D" {
		t.Errorf("Expected Containers.Remove to be 'D', got '%s'", config.Containers.Remove)
	}
	if config.Containers.Stop != "S" {
		t.Errorf("Expected Containers.Stop to be 'S', got '%s'", config.Containers.Stop)
	}
	if config.Services.Up != "U" {
		t.Errorf("Expected Services.Up to be 'U', got '%s'", config.Services.Up)
	}
}

func TestKeybindingConfigYAMLMerge(t *testing.T) {
	// Start with defaults
	defaults := GetDefaultKeybindings()

	// Partial override YAML
	yamlContent := `
universal:
  quit: 'X'
containers:
  remove: 'R'
`

	// Unmarshal into defaults (simulates YAML merge behavior)
	err := yaml.Unmarshal([]byte(yamlContent), &defaults)
	if err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	// Test that overridden values changed
	if defaults.Universal.Quit != "X" {
		t.Errorf("Expected Quit to be overridden to 'X', got '%s'", defaults.Universal.Quit)
	}
	if defaults.Containers.Remove != "R" {
		t.Errorf("Expected Containers.Remove to be overridden to 'R', got '%s'", defaults.Containers.Remove)
	}

	// Test that non-overridden values remain as defaults
	if defaults.Universal.QuitAlt != "<c-c>" {
		t.Errorf("Expected QuitAlt to remain '<c-c>', got '%s'", defaults.Universal.QuitAlt)
	}
	if defaults.Containers.Stop != "s" {
		t.Errorf("Expected Containers.Stop to remain 's', got '%s'", defaults.Containers.Stop)
	}
	if defaults.Services.Up != "u" {
		t.Errorf("Expected Services.Up to remain 'u', got '%s'", defaults.Services.Up)
	}
}

func TestKeybindingConfigSpecialKeys(t *testing.T) {
	yamlContent := `
universal:
  quit: '<f1>'
  quitAlt: '<c-c>'
  return: '<esc>'
  scrollUpMain: '<pgup>'
  scrollDownMain: '<pgdown>'
  prevItem: '<up>'
  nextItem: '<down>'
  prevPanel: '<left>'
  nextPanel: '<right>'
  togglePanel: '<tab>'
  enterMain: '<enter>'
`

	var config KeybindingConfig
	err := yaml.Unmarshal([]byte(yamlContent), &config)
	if err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	// Verify special keys are preserved as strings
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"F1", config.Universal.Quit, "<f1>"},
		{"Ctrl-C", config.Universal.QuitAlt, "<c-c>"},
		{"Escape", config.Universal.Return, "<esc>"},
		{"PageUp", config.Universal.ScrollUpMain, "<pgup>"},
		{"PageDown", config.Universal.ScrollDownMain, "<pgdown>"},
		{"Up Arrow", config.Universal.PrevItem, "<up>"},
		{"Down Arrow", config.Universal.NextItem, "<down>"},
		{"Left Arrow", config.Universal.PrevPanel, "<left>"},
		{"Right Arrow", config.Universal.NextPanel, "<right>"},
		{"Tab", config.Universal.TogglePanel, "<tab>"},
		{"Enter", config.Universal.EnterMain, "<enter>"},
	}

	for _, tt := range tests {
		if tt.got != tt.expected {
			t.Errorf("%s: expected '%s', got '%s'", tt.name, tt.expected, tt.got)
		}
	}
}

func TestKeybindingConfigDisabled(t *testing.T) {
	yamlContent := `
universal:
  quit: '<disabled>'
containers:
  remove: '<disabled>'
`

	var config KeybindingConfig
	err := yaml.Unmarshal([]byte(yamlContent), &config)
	if err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	if config.Universal.Quit != "<disabled>" {
		t.Errorf("Expected Quit to be '<disabled>', got '%s'", config.Universal.Quit)
	}
	if config.Containers.Remove != "<disabled>" {
		t.Errorf("Expected Containers.Remove to be '<disabled>', got '%s'", config.Containers.Remove)
	}
}

func TestKeybindingConfigAllSections(t *testing.T) {
	// Ensure all sections are present in the config struct
	config := GetDefaultKeybindings()

	// This test verifies the struct has all expected sections
	if config.Universal.Quit == "" {
		t.Error("Universal section missing Quit field")
	}
	if config.Containers.Remove == "" {
		t.Error("Containers section missing Remove field")
	}
	if config.Services.Up == "" {
		t.Error("Services section missing Up field")
	}
	if config.Images.Remove == "" {
		t.Error("Images section missing Remove field")
	}
	if config.Volumes.Remove == "" {
		t.Error("Volumes section missing Remove field")
	}
	if config.Networks.Remove == "" {
		t.Error("Networks section missing Remove field")
	}
	if config.Project.EditConfig == "" {
		t.Error("Project section missing EditConfig field")
	}
	if config.Main.Return == "" {
		t.Error("Main section missing Return field")
	}
	if config.Menu.Close == "" {
		t.Error("Menu section missing Close field")
	}
	if config.Filter.Confirm == "" {
		t.Error("Filter section missing Confirm field")
	}
}
