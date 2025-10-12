package keybindings

import (
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/config"
)

// GetKey converts a string keybinding to the appropriate interface{} type
// that gocui expects (either rune or gocui.Key)
func GetKey(key string) interface{} {
	runeCount := utf8.RuneCountInString(key)

	if key == "<disabled>" {
		return nil // Disabled binding - don't register
	} else if runeCount > 1 {
		// Special key like "<c-c>", "<enter>", "<f1>"
		binding, ok := config.KeyByLabel[strings.ToLower(key)]
		if !ok {
			log.Fatalf("Unrecognized key %s for keybinding. For permitted values see https://github.com/jesseduffield/lazydocker/blob/master/docs/Config.md",
				strings.ToLower(key))
		}
		return binding // gocui.Key type
	} else if runeCount == 1 {
		// Single character like 'q', 'a', 'x'
		return []rune(key)[0] // rune type
	}
	return nil
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
