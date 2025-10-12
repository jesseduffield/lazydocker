package config

import (
	"testing"

	"github.com/jesseduffield/gocui"
)

func TestIsValidKeybindingKey(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		valid bool
	}{
		// Single character keys (always valid)
		{"lowercase letter", "a", true},
		{"uppercase letter", "Q", true},
		{"digit", "1", true},
		{"special char", "!", true},
		{"space", " ", true},
		{"symbol", "+", true},
		{"bracket", "[", true},

		// Valid special keys
		{"escape", "<esc>", true},
		{"escape uppercase", "<ESC>", true},
		{"enter", "<enter>", true},
		{"tab", "<tab>", true},
		{"backtab", "<backtab>", true},
		{"ctrl-c", "<c-c>", true},
		{"ctrl-d", "<c-d>", true},
		{"f1", "<f1>", true},
		{"f12", "<f12>", true},
		{"pgup", "<pgup>", true},
		{"pgdown", "<pgdown>", true},
		{"arrow up", "<up>", true},
		{"arrow down", "<down>", true},
		{"arrow left", "<left>", true},
		{"arrow right", "<right>", true},
		{"home", "<home>", true},
		{"end", "<end>", true},
		{"delete", "<delete>", true},
		{"backspace", "<backspace>", true},
		{"insert", "<insert>", true},

		// Special case: disabled
		{"disabled", "<disabled>", true},

		// Invalid multi-char keys (not in mapping)
		{"invalid key", "<invalid>", false},
		{"unknown key", "<unknown-key>", false},
		{"random", "<foo>", false},
		{"backspace2 (not mapped)", "<backspace2>", false},
		{"ctrl-h alias (use backspace)", "<c-h>", false},
		{"ctrl-i alias (use tab)", "<c-i>", false},
		{"alt-a (not mapped)", "<a-a>", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidKeybindingKey(tt.key)
			if result != tt.valid {
				t.Errorf("IsValidKeybindingKey(%q) = %v, want %v", tt.key, result, tt.valid)
			}
		})
	}
}

func TestKeyByLabelMapping(t *testing.T) {
	// Test that common keys are in the mapping
	tests := []struct {
		label    string
		expected gocui.Key
	}{
		{"<esc>", gocui.KeyEsc},
		{"<enter>", gocui.KeyEnter},
		{"<tab>", gocui.KeyTab},
		{"<backtab>", gocui.KeyBacktab},
		{"<c-c>", gocui.KeyCtrlC},
		{"<c-d>", gocui.KeyCtrlD},
		{"<c-u>", gocui.KeyCtrlU},
		{"<f1>", gocui.KeyF1},
		{"<f12>", gocui.KeyF12},
		{"<up>", gocui.KeyArrowUp},
		{"<down>", gocui.KeyArrowDown},
		{"<left>", gocui.KeyArrowLeft},
		{"<right>", gocui.KeyArrowRight},
		{"<pgup>", gocui.KeyPgup},
		{"<pgdown>", gocui.KeyPgdn},
		{"<home>", gocui.KeyHome},
		{"<end>", gocui.KeyEnd},
		{"<delete>", gocui.KeyDelete},
		{"<backspace>", gocui.KeyBackspace},
		{"<insert>", gocui.KeyInsert},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			key, ok := KeyByLabel[tt.label]
			if !ok {
				t.Errorf("KeyByLabel[%q] not found in mapping", tt.label)
			}
			if key != tt.expected {
				t.Errorf("KeyByLabel[%q] = %v, want %v", tt.label, key, tt.expected)
			}
		})
	}
}

func TestLabelByKeyMapping(t *testing.T) {
	// Test that the reverse mapping works
	tests := []struct {
		key      gocui.Key
		expected string
	}{
		{gocui.KeyEsc, "<esc>"},
		{gocui.KeyEnter, "<enter>"},
		{gocui.KeyTab, "<tab>"},
		{gocui.KeyBacktab, "<backtab>"},
		{gocui.KeyCtrlC, "<c-c>"},
		{gocui.KeyArrowUp, "<up>"},
		{gocui.KeyArrowDown, "<down>"},
		{gocui.KeyF1, "<f1>"},
		{gocui.KeyPgup, "<pgup>"},
		{gocui.KeyPgdn, "<pgdown>"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			label, ok := LabelByKey[tt.key]
			if !ok {
				t.Errorf("LabelByKey[%v] not found in mapping", tt.key)
			}
			if label != tt.expected {
				t.Errorf("LabelByKey[%v] = %q, want %q", tt.key, label, tt.expected)
			}
		})
	}
}

func TestKeyByLabelIsCaseInsensitive(t *testing.T) {
	// Test that key lookups are case-insensitive
	tests := []string{
		"<esc>",
		"<ESC>",
		"<Esc>",
		"<enter>",
		"<ENTER>",
		"<Enter>",
		"<f1>",
		"<F1>",
	}

	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			// IsValidKeybindingKey should handle case-insensitivity
			if !IsValidKeybindingKey(tt) {
				t.Errorf("IsValidKeybindingKey(%q) should be true (case-insensitive)", tt)
			}
		})
	}
}

func TestAllLabelByKeyHaveReverseMapping(t *testing.T) {
	// Verify that LabelByKey and KeyByLabel are proper inverses
	for key, label := range LabelByKey {
		reversedKey, ok := KeyByLabel[label]
		if !ok {
			t.Errorf("LabelByKey[%v] = %q, but KeyByLabel[%q] is missing", key, label, label)
		}
		if reversedKey != key {
			t.Errorf("LabelByKey[%v] = %q, but KeyByLabel[%q] = %v (expected %v)",
				key, label, label, reversedKey, key)
		}
	}
}

func TestAllKeyByLabelHaveReverseMapping(t *testing.T) {
	// Verify that KeyByLabel and LabelByKey are proper inverses
	for label, key := range KeyByLabel {
		reversedLabel, ok := LabelByKey[key]
		if !ok {
			t.Errorf("KeyByLabel[%q] = %v, but LabelByKey[%v] is missing", label, key, key)
		}
		if reversedLabel != label {
			t.Errorf("KeyByLabel[%q] = %v, but LabelByKey[%v] = %q (expected %q)",
				label, key, key, reversedLabel, label)
		}
	}
}

func TestControlKeys(t *testing.T) {
	// Test control key mappings that are actually in the keynames.go mapping
	// Note: Some control keys are mapped to their common names instead
	// (e.g., <backspace> instead of <c-h>, <tab> instead of <c-i>)
	controlKeys := []struct {
		label string
		key   gocui.Key
	}{
		{"<c-a>", gocui.KeyCtrlA},
		{"<c-b>", gocui.KeyCtrlB},
		{"<c-c>", gocui.KeyCtrlC},
		{"<c-d>", gocui.KeyCtrlD},
		{"<c-e>", gocui.KeyCtrlE},
		{"<c-f>", gocui.KeyCtrlF},
		{"<c-g>", gocui.KeyCtrlG},
		// Note: <c-h> is mapped as <backspace>, not <c-h>
		// Note: <c-i> is mapped as <tab>, not <c-i>
		{"<c-j>", gocui.KeyCtrlJ},
		{"<c-k>", gocui.KeyCtrlK},
		{"<c-l>", gocui.KeyCtrlL},
		{"<c-n>", gocui.KeyCtrlN},
		{"<c-o>", gocui.KeyCtrlO},
		{"<c-p>", gocui.KeyCtrlP},
		{"<c-q>", gocui.KeyCtrlQ},
		{"<c-r>", gocui.KeyCtrlR},
		{"<c-s>", gocui.KeyCtrlS},
		{"<c-t>", gocui.KeyCtrlT},
		{"<c-u>", gocui.KeyCtrlU},
		{"<c-v>", gocui.KeyCtrlV},
		{"<c-w>", gocui.KeyCtrlW},
		{"<c-x>", gocui.KeyCtrlX},
		{"<c-y>", gocui.KeyCtrlY},
		{"<c-z>", gocui.KeyCtrlZ},
		// Note: <c-~> is mapped as <c-space>, not <c-~>
		// Note: <c-[> is mapped as <esc>, not <c-[>
		// Note: <c-]> is mapped as <c-5>, not <c-]>
	}

	for _, tt := range controlKeys {
		t.Run(tt.label, func(t *testing.T) {
			key, ok := KeyByLabel[tt.label]
			if !ok {
				t.Errorf("KeyByLabel[%q] should exist", tt.label)
				return
			}
			if key != tt.key {
				t.Errorf("KeyByLabel[%q] = %v, want %v", tt.label, key, tt.key)
			}

			// Verify reverse mapping
			label, ok := LabelByKey[tt.key]
			if !ok {
				t.Errorf("LabelByKey[%v] should exist", tt.key)
				return
			}
			if label != tt.label {
				t.Errorf("LabelByKey[%v] = %q, want %q", tt.key, label, tt.label)
			}
		})
	}
}

func TestFunctionKeys(t *testing.T) {
	// Test all function key mappings
	for i := 1; i <= 12; i++ {
		t.Run("F"+string(rune('0'+i)), func(t *testing.T) {
			label := ""
			var expectedKey gocui.Key

			switch i {
			case 1:
				label = "<f1>"
				expectedKey = gocui.KeyF1
			case 2:
				label = "<f2>"
				expectedKey = gocui.KeyF2
			case 3:
				label = "<f3>"
				expectedKey = gocui.KeyF3
			case 4:
				label = "<f4>"
				expectedKey = gocui.KeyF4
			case 5:
				label = "<f5>"
				expectedKey = gocui.KeyF5
			case 6:
				label = "<f6>"
				expectedKey = gocui.KeyF6
			case 7:
				label = "<f7>"
				expectedKey = gocui.KeyF7
			case 8:
				label = "<f8>"
				expectedKey = gocui.KeyF8
			case 9:
				label = "<f9>"
				expectedKey = gocui.KeyF9
			case 10:
				label = "<f10>"
				expectedKey = gocui.KeyF10
			case 11:
				label = "<f11>"
				expectedKey = gocui.KeyF11
			case 12:
				label = "<f12>"
				expectedKey = gocui.KeyF12
			}

			key, ok := KeyByLabel[label]
			if !ok {
				t.Errorf("KeyByLabel[%q] should exist", label)
				return
			}
			if key != expectedKey {
				t.Errorf("KeyByLabel[%q] = %v, want %v", label, key, expectedKey)
			}
		})
	}
}
