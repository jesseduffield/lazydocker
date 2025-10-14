package keybindings

import (
	"strings"
	"testing"

	"github.com/jesseduffield/gocui"
)

func TestGetKeySingleChar(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected rune
	}{
		{"lowercase letter", "a", 'a'},
		{"uppercase letter", "Q", 'Q'},
		{"digit", "5", '5'},
		{"space", " ", ' '},
		{"symbol", "+", '+'},
		{"bracket", "[", '['},
		{"exclamation", "!", '!'},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GetKey(tt.input)
			if err != nil {
				t.Fatalf("GetKey(%q) unexpected error: %v", tt.input, err)
			}
			if result == nil {
				t.Fatalf("GetKey(%q) returned nil", tt.input)
			}

			// Should return a rune for single characters
			r, ok := result.(rune)
			if !ok {
				t.Fatalf("GetKey(%q) returned %T, want rune", tt.input, result)
			}

			if r != tt.expected {
				t.Errorf("GetKey(%q) = %c, want %c", tt.input, r, tt.expected)
			}
		})
	}
}

func TestGetKeySpecialKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected gocui.Key
	}{
		{"escape", "<esc>", gocui.KeyEsc},
		{"enter", "<enter>", gocui.KeyEnter},
		{"tab", "<tab>", gocui.KeyTab},
		{"backtab", "<backtab>", gocui.KeyBacktab},
		{"ctrl-c", "<c-c>", gocui.KeyCtrlC},
		{"ctrl-d", "<c-d>", gocui.KeyCtrlD},
		{"ctrl-u", "<c-u>", gocui.KeyCtrlU},
		{"f1", "<f1>", gocui.KeyF1},
		{"f12", "<f12>", gocui.KeyF12},
		{"up arrow", "<up>", gocui.KeyArrowUp},
		{"down arrow", "<down>", gocui.KeyArrowDown},
		{"left arrow", "<left>", gocui.KeyArrowLeft},
		{"right arrow", "<right>", gocui.KeyArrowRight},
		{"page up", "<pgup>", gocui.KeyPgup},
		{"page down", "<pgdown>", gocui.KeyPgdn},
		{"home", "<home>", gocui.KeyHome},
		{"end", "<end>", gocui.KeyEnd},
		{"delete", "<delete>", gocui.KeyDelete},
		{"backspace", "<backspace>", gocui.KeyBackspace},
		{"insert", "<insert>", gocui.KeyInsert},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GetKey(tt.input)
			if err != nil {
				t.Fatalf("GetKey(%q) unexpected error: %v", tt.input, err)
			}
			if result == nil {
				t.Fatalf("GetKey(%q) returned nil", tt.input)
			}

			// Should return gocui.Key for special keys
			key, ok := result.(gocui.Key)
			if !ok {
				t.Fatalf("GetKey(%q) returned %T, want gocui.Key", tt.input, result)
			}

			if key != tt.expected {
				t.Errorf("GetKey(%q) = %v, want %v", tt.input, key, tt.expected)
			}
		})
	}
}

func TestGetKeyCaseInsensitive(t *testing.T) {
	// Special keys should be case-insensitive
	tests := []struct {
		lowercase string
		uppercase string
		expected  gocui.Key
	}{
		{"<esc>", "<ESC>", gocui.KeyEsc},
		{"<enter>", "<ENTER>", gocui.KeyEnter},
		{"<f1>", "<F1>", gocui.KeyF1},
		{"<pgup>", "<PGUP>", gocui.KeyPgup},
		{"<c-c>", "<C-C>", gocui.KeyCtrlC},
	}

	for _, tt := range tests {
		t.Run(tt.lowercase, func(t *testing.T) {
			lowerResult, lowerErr := GetKey(tt.lowercase)
			upperResult, upperErr := GetKey(tt.uppercase)

			if lowerErr != nil {
				t.Fatalf("GetKey(%q) unexpected error: %v", tt.lowercase, lowerErr)
			}
			if upperErr != nil {
				t.Fatalf("GetKey(%q) unexpected error: %v", tt.uppercase, upperErr)
			}

			if lowerResult == nil || upperResult == nil {
				t.Fatalf("GetKey returned nil for %q or %q", tt.lowercase, tt.uppercase)
			}

			lowerKey, lowerOk := lowerResult.(gocui.Key)
			upperKey, upperOk := upperResult.(gocui.Key)

			if !lowerOk || !upperOk {
				t.Fatalf("GetKey did not return gocui.Key for both cases")
			}

			if lowerKey != upperKey {
				t.Errorf("Case-insensitive keys differ: %q=%v, %q=%v",
					tt.lowercase, lowerKey, tt.uppercase, upperKey)
			}

			if lowerKey != tt.expected {
				t.Errorf("GetKey(%q) = %v, want %v", tt.lowercase, lowerKey, tt.expected)
			}
		})
	}
}

func TestGetKeyDisabled(t *testing.T) {
	result, err := GetKey("<disabled>")
	if err != nil {
		t.Fatalf("GetKey('<disabled>') unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("GetKey('<disabled>') = %v, want nil", result)
	}
}

func TestGetKeyEmptyString(t *testing.T) {
	result, err := GetKey("")
	if err == nil {
		t.Fatalf("GetKey('') expected error, got nil")
	}
	if !strings.Contains(err.Error(), "empty string") {
		t.Errorf("GetKey('') error = %v, want substring %q", err, "empty string")
	}
	if result != nil {
		t.Errorf("GetKey('') = %v, want nil", result)
	}
}

func TestGetKeyErrors(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "invalid special key",
			input:       "<invalid>",
			expectError: true,
			errorMsg:    "unrecognized key",
		},
		{
			name:        "unknown key",
			input:       "<foo-bar>",
			expectError: true,
			errorMsg:    "unrecognized key",
		},
		{
			name:        "empty string",
			input:       "",
			expectError: true,
			errorMsg:    "empty string",
		},
		{
			name:        "valid single char",
			input:       "a",
			expectError: false,
		},
		{
			name:        "valid special key",
			input:       "<esc>",
			expectError: false,
		},
		{
			name:        "disabled",
			input:       "<disabled>",
			expectError: false,
		},
		{
			name:        "default token",
			input:       "<default>",
			expectError: true,
			errorMsg:    "was not resolved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GetKey(tt.input)

			if tt.expectError {
				if err == nil {
					t.Fatalf("GetKey(%q) expected error, got nil", tt.input)
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("GetKey(%q) error = %v, want substring %q",
						tt.input, err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Fatalf("GetKey(%q) unexpected error: %v", tt.input, err)
				}
				if tt.input != "<disabled>" && result == nil {
					t.Errorf("GetKey(%q) returned nil without error", tt.input)
				}
			}
		})
	}
}

func TestLabelFromKeySingleChar(t *testing.T) {
	tests := []struct {
		name     string
		input    rune
		expected string
	}{
		{"lowercase letter", 'a', "a"},
		{"uppercase letter", 'Q', "Q"},
		{"digit", '5', "5"},
		{"space", ' ', " "},
		{"symbol", '+', "+"},
		{"bracket", '[', "["},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := LabelFromKey(tt.input)
			if result != tt.expected {
				t.Errorf("LabelFromKey(%c) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLabelFromKeySpecialKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    gocui.Key
		expected string
	}{
		{"escape", gocui.KeyEsc, "<esc>"},
		{"enter", gocui.KeyEnter, "<enter>"},
		{"tab", gocui.KeyTab, "<tab>"},
		{"ctrl-c", gocui.KeyCtrlC, "<c-c>"},
		{"f1", gocui.KeyF1, "<f1>"},
		{"up arrow", gocui.KeyArrowUp, "<up>"},
		{"down arrow", gocui.KeyArrowDown, "<down>"},
		{"page up", gocui.KeyPgup, "<pgup>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := LabelFromKey(tt.input)
			if result != tt.expected {
				t.Errorf("LabelFromKey(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLabelFromKeyNil(t *testing.T) {
	result := LabelFromKey(nil)
	if result != "" {
		t.Errorf("LabelFromKey(nil) = %q, want empty string", result)
	}
}

func TestGetKeyRoundTrip(t *testing.T) {
	// Test that GetKey -> LabelFromKey round-trips correctly
	tests := []string{
		"a",
		"Q",
		"5",
		"[",
		"<esc>",
		"<enter>",
		"<c-c>",
		"<f1>",
		"<up>",
		"<pgup>",
	}

	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			key, err := GetKey(tt)
			if err != nil {
				t.Fatalf("GetKey(%q) unexpected error: %v", tt, err)
			}
			if key == nil {
				// Skip disabled keys
				if tt != "<disabled>" {
					t.Fatalf("GetKey(%q) returned nil", tt)
				}
				return
			}

			label := LabelFromKey(key)

			// For special keys, should match exactly
			if len(tt) > 1 {
				if label != tt {
					t.Errorf("Round-trip failed: %q -> %v -> %q", tt, key, label)
				}
			} else {
				// For single chars, just verify it's the same character
				if label != tt {
					t.Errorf("Round-trip failed: %q -> %v -> %q", tt, key, label)
				}
			}
		})
	}
}

func TestAllControlKeys(t *testing.T) {
	// Test control key combinations that are actually mapped
	// Note: Some control keys use their common names instead (e.g., <tab> for <c-i>)
	controlKeys := map[string]gocui.Key{
		"<c-a>": gocui.KeyCtrlA,
		"<c-b>": gocui.KeyCtrlB,
		"<c-c>": gocui.KeyCtrlC,
		"<c-d>": gocui.KeyCtrlD,
		"<c-e>": gocui.KeyCtrlE,
		"<c-f>": gocui.KeyCtrlF,
		"<c-g>": gocui.KeyCtrlG,
		// Note: <c-h> is mapped as <backspace>
		// Note: <c-i> is mapped as <tab>
		"<c-j>": gocui.KeyCtrlJ,
		"<c-k>": gocui.KeyCtrlK,
		"<c-l>": gocui.KeyCtrlL,
		"<c-n>": gocui.KeyCtrlN,
		"<c-o>": gocui.KeyCtrlO,
		"<c-p>": gocui.KeyCtrlP,
		"<c-q>": gocui.KeyCtrlQ,
		"<c-r>": gocui.KeyCtrlR,
		"<c-s>": gocui.KeyCtrlS,
		"<c-t>": gocui.KeyCtrlT,
		"<c-u>": gocui.KeyCtrlU,
		"<c-v>": gocui.KeyCtrlV,
		"<c-w>": gocui.KeyCtrlW,
		"<c-x>": gocui.KeyCtrlX,
		"<c-y>": gocui.KeyCtrlY,
		"<c-z>": gocui.KeyCtrlZ,
	}

	for label, expectedKey := range controlKeys {
		t.Run(label, func(t *testing.T) {
			result, err := GetKey(label)
			if err != nil {
				t.Fatalf("GetKey(%q) unexpected error: %v", label, err)
			}
			if result == nil {
				t.Fatalf("GetKey(%q) returned nil", label)
			}

			key, ok := result.(gocui.Key)
			if !ok {
				t.Fatalf("GetKey(%q) returned %T, want gocui.Key", label, result)
			}

			if key != expectedKey {
				t.Errorf("GetKey(%q) = %v, want %v", label, key, expectedKey)
			}

			// Verify reverse mapping
			reverseLabel := LabelFromKey(key)
			if reverseLabel != label {
				t.Errorf("LabelFromKey(%v) = %q, want %q", key, reverseLabel, label)
			}
		})
	}
}

func TestAllFunctionKeys(t *testing.T) {
	// Test all function keys
	functionKeys := map[string]gocui.Key{
		"<f1>":  gocui.KeyF1,
		"<f2>":  gocui.KeyF2,
		"<f3>":  gocui.KeyF3,
		"<f4>":  gocui.KeyF4,
		"<f5>":  gocui.KeyF5,
		"<f6>":  gocui.KeyF6,
		"<f7>":  gocui.KeyF7,
		"<f8>":  gocui.KeyF8,
		"<f9>":  gocui.KeyF9,
		"<f10>": gocui.KeyF10,
		"<f11>": gocui.KeyF11,
		"<f12>": gocui.KeyF12,
	}

	for label, expectedKey := range functionKeys {
		t.Run(label, func(t *testing.T) {
			result, err := GetKey(label)
			if err != nil {
				t.Fatalf("GetKey(%q) unexpected error: %v", label, err)
			}
			if result == nil {
				t.Fatalf("GetKey(%q) returned nil", label)
			}

			key, ok := result.(gocui.Key)
			if !ok {
				t.Fatalf("GetKey(%q) returned %T, want gocui.Key", label, result)
			}

			if key != expectedKey {
				t.Errorf("GetKey(%q) = %v, want %v", label, key, expectedKey)
			}

			// Verify reverse mapping
			reverseLabel := LabelFromKey(key)
			if reverseLabel != label {
				t.Errorf("LabelFromKey(%v) = %q, want %q", key, reverseLabel, label)
			}
		})
	}
}

func TestGetKeyReturnsCorrectTypes(t *testing.T) {
	// Verify that GetKey returns the correct Go types

	// Single char -> rune
	singleCharResult, err := GetKey("a")
	if err != nil {
		t.Fatalf("GetKey('a') unexpected error: %v", err)
	}
	if _, ok := singleCharResult.(rune); !ok {
		t.Errorf("GetKey('a') should return rune, got %T", singleCharResult)
	}

	// Special key -> gocui.Key
	specialKeyResult, err := GetKey("<esc>")
	if err != nil {
		t.Fatalf("GetKey('<esc>') unexpected error: %v", err)
	}
	if _, ok := specialKeyResult.(gocui.Key); !ok {
		t.Errorf("GetKey('<esc>') should return gocui.Key, got %T", specialKeyResult)
	}

	// Disabled -> nil
	disabledResult, err := GetKey("<disabled>")
	if err != nil {
		t.Fatalf("GetKey('<disabled>') unexpected error: %v", err)
	}
	if disabledResult != nil {
		t.Errorf("GetKey('<disabled>') should return nil, got %v", disabledResult)
	}

	// Empty -> error
	emptyResult, err := GetKey("")
	if err == nil {
		t.Fatalf("GetKey('') expected error, got nil")
	}
	if emptyResult != nil {
		t.Errorf("GetKey('') should return nil, got %v", emptyResult)
	}
}
