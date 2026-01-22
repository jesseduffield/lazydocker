package config

import (
	"fmt"
	"log"
	"reflect"
)

// Validate validates the user config
func (config *UserConfig) Validate() error {
	if err := validateKeybindings(config.Keybinding); err != nil {
		return err
	}

	// Note: We don't validate for duplicate keys across different contexts
	// because it's intentional to reuse keys (e.g., 'd' for remove in containers, images, volumes).
	// This follows lazygit's approach. Within a context, duplicate keys use last-wins behavior
	// which is standard YAML behavior.

	return nil
}

// validateKeybindingsRecurse recursively validates keybinding config structs
func validateKeybindingsRecurse(path string, node interface{}) error {
	value := reflect.ValueOf(node)
	if value.Kind() == reflect.Struct {
		for _, field := range reflect.VisibleFields(reflect.TypeOf(node)) {
			var newPath string
			if len(path) == 0 {
				newPath = field.Name
			} else {
				newPath = fmt.Sprintf("%s.%s", path, field.Name)
			}
			if err := validateKeybindingsRecurse(newPath,
				value.FieldByName(field.Name).Interface()); err != nil {
				return err
			}
		}
	} else if value.Kind() == reflect.String {
		key := node.(string)
		if !IsValidKeybindingKey(key) {
			return fmt.Errorf("Unrecognized key '%s' for keybinding '%s'. For permitted values see https://github.com/jesseduffield/lazydocker/blob/master/docs/Config.md",
				key, path)
		}
	} else if value.Kind() != reflect.Invalid {
		log.Fatalf("Unexpected type for property '%s': %s", path, value.Kind())
	}
	return nil
}

// validateKeybindings validates the keybinding configuration
func validateKeybindings(keybindingConfig KeybindingConfig) error {
	if err := validateKeybindingsRecurse("", keybindingConfig); err != nil {
		return err
	}
	return nil
}
