package gui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/jesseduffield/gocui"
	"github.com/stretchr/testify/assert"
)

func TestTranslateKeybindingsToKorean(t *testing.T) {
	inputBindings := []*Binding{
		{
			ViewName: "services",
			Key:      's',
			Modifier: gocui.ModNone,
			Handler:  nil,
		},
		{
			ViewName: "services",
			Key:      'S',
			Modifier: gocui.ModNone,
			Handler:  nil,
		},
		{
			ViewName: "services",
			Key:      'l',
			Modifier: gocui.ModNone,
			Handler:  nil,
		},
		{
			ViewName: "global",
			Key:      gocui.KeyCtrlC,
			Modifier: gocui.ModNone,
			Handler:  nil,
		},
	}

	result := translateKeybindingsToKorean(inputBindings)

	// The original bindings must remain intact, and new ones should be appended
	assert.Len(t, result, len(inputBindings)+3)

	// Verify original bindings are still present and unaltered
	for i, b := range inputBindings {
		assert.Equal(t, b.Key, result[i].Key)
		assert.Equal(t, b.Modifier, result[i].Modifier)
	}

	// Verify translated Korean bindings are appended correctly
	// 's' -> 'ㄴ' (gocui.ModNone)
	// 'S' -> 'ㄴ' (gocui.Modifier(tcell.ModShift))
	// 'l' -> 'ㅣ' (gocui.ModNone)
	
	// We expect translated bindings to be appended:
	foundS := false
	foundShiftS := false
	foundL := false

	for _, b := range result[len(inputBindings):] {
		if b.Key == 'ㄴ' && b.Modifier == gocui.ModNone {
			foundS = true
		}
		if b.Key == 'ㄴ' && b.Modifier == gocui.Modifier(tcell.ModShift) {
			foundShiftS = true
		}
		if b.Key == 'ㅣ' && b.Modifier == gocui.ModNone {
			foundL = true
		}
	}

	assert.True(t, foundS, "Expected to find translated Korean binding for 's'")
	assert.True(t, foundShiftS, "Expected to find translated Korean binding for 'S'")
	assert.True(t, foundL, "Expected to find translated Korean binding for 'l'")
}
