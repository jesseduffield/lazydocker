package keybindings

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/config"
)

// GetKey converts a string keybinding to the appropriate interface{} type
// that gocui expects (either rune or gocui.Key)
func GetKey(key string) (interface{}, error) {
	runeCount := utf8.RuneCountInString(key)

	if key == "<disabled>" {
		return nil, nil // Disabled binding - not an error, intentional
	} else if key == "<default>" {
		// This should never happen as <default> should be resolved during config loading
		return nil, fmt.Errorf("<default> token was not resolved - this is a bug")
	} else if runeCount > 1 {
		// Special key like "<c-c>", "<enter>", "<f1>"
		binding, ok := config.KeyByLabel[strings.ToLower(key)]
		if !ok {
			return nil, fmt.Errorf(
				"unrecognized key '%s' for keybinding. "+
					"Valid special keys: <esc>, <enter>, <tab>, <c-[a-z]>, <f1>-<f12>, etc. "+
					"See: https://github.com/jesseduffield/lazydocker/blob/master/docs/keybindings/Config.md",
				key)
		}
		return binding, nil // gocui.Key type
	} else if runeCount == 1 {
		// Single character like 'q', 'a', 'x'
		return []rune(key)[0], nil // rune type
	}

	// Empty string case
	return nil, fmt.Errorf("empty string is not a valid keybinding")
}

// LabelFromKey converts a key interface{} back to a string label
func LabelFromKey(key interface{}) string {
	if key == nil {
		return ""
	}

	keyInt := 0

	switch key := key.(type) {
	case rune:
		keyInt = int(key)
	case gocui.Key:
		value, ok := config.LabelByKey[key]
		if ok {
			return value
		}
		keyInt = int(key)
	}

	return fmt.Sprintf("%c", keyInt)
}
