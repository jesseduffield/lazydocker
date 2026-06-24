package gui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGotoBottomOriginY(t *testing.T) {
	type scenario struct {
		name             string
		totalLines       int
		viewHeight       int
		scrollPastBottom bool
		expected         int
	}

	scenarios := []scenario{
		{
			name:             "keeps the last page on screen when not scrolling past bottom",
			totalLines:       100,
			viewHeight:       20,
			scrollPastBottom: false,
			expected:         80,
		},
		{
			name:             "scrolls all content off the top when scrolling past bottom",
			totalLines:       100,
			viewHeight:       20,
			scrollPastBottom: true,
			expected:         100,
		},
		{
			name:             "never returns a negative origin when content fits the view",
			totalLines:       5,
			viewHeight:       20,
			scrollPastBottom: false,
			expected:         0,
		},
		{
			name:             "handles empty content",
			totalLines:       0,
			viewHeight:       20,
			scrollPastBottom: true,
			expected:         0,
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			assert.Equal(t, s.expected, gotoBottomOriginY(s.totalLines, s.viewHeight, s.scrollPastBottom))
		})
	}
}

func TestIsDoublePress(t *testing.T) {
	now := time.Unix(1000, 0)
	timeout := 250 * time.Millisecond

	type scenario struct {
		name     string
		previous time.Time
		now      time.Time
		expected bool
	}

	scenarios := []scenario{
		{
			name:     "no previous press is not a double press",
			previous: time.Time{},
			now:      now,
			expected: false,
		},
		{
			name:     "second press within the timeout is a double press",
			previous: now.Add(-100 * time.Millisecond),
			now:      now,
			expected: true,
		},
		{
			name:     "second press exactly on the timeout is a double press",
			previous: now.Add(-timeout),
			now:      now,
			expected: true,
		},
		{
			name:     "second press after the timeout is not a double press",
			previous: now.Add(-timeout - time.Millisecond),
			now:      now,
			expected: false,
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			assert.Equal(t, s.expected, isDoublePress(s.previous, s.now, timeout))
		})
	}
}
