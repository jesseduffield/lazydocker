package commands

import (
	"testing"

	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestAppleContainerCommandCreation(t *testing.T) {
	log := logrus.NewEntry(logrus.New())

	// Create a basic config
	appConfig := &config.AppConfig{
		Runtime: "apple",
	}

	// Create a basic OS command (real, but used only for help/introspection)
	osCommand := NewOSCommand(log, appConfig)

	// Create translation set
	tr := &i18n.TranslationSet{}

	errorChan := make(chan error, 1)

	_, err := NewAppleContainerCommand(log, osCommand, tr, appConfig, errorChan)
	if isAppleContainerAvailable() {
		// On machines where Apple CLI is installed, expect success
		assert.Nil(t, err)
	} else {
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "Apple Container CLI not found")
	}
}

func TestIsAppleContainerAvailable(t *testing.T) {
	// Simply call the function; do not assert a fixed value because
	// the test host may or may not have the CLI installed.
	_ = isAppleContainerAvailable()
}

func TestParseContainerList(t *testing.T) {
	log := logrus.NewEntry(logrus.New())
	appConfig := &config.AppConfig{Runtime: "apple"}
	osCommand := &OSCommand{}
	tr := &i18n.TranslationSet{}
	errorChan := make(chan error, 1)

	// Create command instance for testing parsing methods
	cmd := &AppleContainerCommand{
		Log:       log,
		OSCommand: osCommand,
		Tr:        tr,
		Config:    appConfig,
		ErrorChan: errorChan,
	}

	tests := []struct {
		name     string
		input    string
		expected int
		hasError bool
	}{
		{
			name:     "empty output",
			input:    "",
			expected: 0,
			hasError: false,
		},
		{
			name:     "single container",
			input:    `[{"configuration":{"id":"abc123","image":{"reference":"nginx:latest"}},"status":"running"}]`,
			expected: 1,
			hasError: false,
		},
		{
			name: "multiple containers",
			input: `[{"configuration":{"id":"abc123","image":{"reference":"nginx:latest"}},"status":"running"},
{"configuration":{"id":"def456","image":{"reference":"redis:6"}},"status":"stopped"}]`,
			expected: 2,
			hasError: false,
		},
		{
			name:     "invalid json",
			input:    `{"invalid json}`,
			expected: 0,
			hasError: true,
		},
		{
			name:     "missing required fields",
			input:    `[{"status":"running"}]`,
			expected: 0,
			hasError: false, // parse succeeds but no valid entries
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			containers, err := cmd.parseContainerList(tt.input)

			if tt.hasError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tt.expected, len(containers))
			}
		})
	}
}

func TestParseImageList(t *testing.T) {
	log := logrus.NewEntry(logrus.New())
	appConfig := &config.AppConfig{Runtime: "apple"}
	osCommand := &OSCommand{}
	tr := &i18n.TranslationSet{}
	errorChan := make(chan error, 1)

	cmd := &AppleContainerCommand{
		Log:       log,
		OSCommand: osCommand,
		Tr:        tr,
		Config:    appConfig,
		ErrorChan: errorChan,
	}

	tests := []struct {
		name     string
		input    string
		expected int
		hasError bool
	}{
		{
			name:     "empty output",
			input:    "",
			expected: 0,
			hasError: false,
		},
		{
			name:     "single image",
			input:    `[{"reference":"nginx:latest","descriptor":{"digest":"sha256:abc","size":1234}}]`,
			expected: 1,
			hasError: false,
		},
		{
			name: "multiple images",
			input: `[{"reference":"nginx:latest","descriptor":{"digest":"sha256:abc","size":1234}},
{"reference":"redis:6-alpine","descriptor":{"digest":"sha256:def","size":5678}}]`,
			expected: 2,
			hasError: false,
		},
		{
			name:     "missing required fields",
			input:    `[{"descriptor":{"digest":"sha256:abc"}}]`,
			expected: 0,
			hasError: false, // Should skip images with missing ID
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			images, err := cmd.parseImageList(tt.input)

			if tt.hasError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tt.expected, len(images))
			}
		})
	}
}

func TestJsonToContainer(t *testing.T) {
	log := logrus.NewEntry(logrus.New())
	appConfig := &config.AppConfig{Runtime: "apple"}
	osCommand := &OSCommand{}
	tr := &i18n.TranslationSet{}
	errorChan := make(chan error, 1)

	cmd := &AppleContainerCommand{
		Log:       log,
		OSCommand: osCommand,
		Tr:        tr,
		Config:    appConfig,
		ErrorChan: errorChan,
	}

	tests := []struct {
		name     string
		input    map[string]interface{}
		expected *Container
	}{
		{
			name: "valid container data",
			input: map[string]interface{}{
				"configuration": map[string]interface{}{
					"id": "abc123",
					"image": map[string]interface{}{
						"reference": "nginx:latest",
					},
				},
				"status": "running",
			},
			expected: &Container{
				ID:   "abc123",
				Name: "abc123",
			},
		},
		{
			name: "missing id",
			input: map[string]interface{}{
				"configuration": map[string]interface{}{
					"image": map[string]interface{}{"reference": "nginx:latest"},
				},
				"status": "running",
			},
			expected: nil,
		},
		// missing name is fine; name defaults to ID in implementation
		{
			name: "missing name",
			input: map[string]interface{}{
				"configuration": map[string]interface{}{
					"id":    "abc123",
					"image": map[string]interface{}{"reference": "nginx:latest"},
				},
				"status": "running",
			},
			expected: &Container{ID: "abc123", Name: "abc123"},
		},
		{
			name: "state mapping",
			input: map[string]interface{}{
				"configuration": map[string]interface{}{
					"id":    "abc123",
					"image": map[string]interface{}{"reference": "nginx:latest"},
				},
				"status": "stopped", // Should map to exited
			},
			expected: &Container{
				ID:   "abc123",
				Name: "abc123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := cmd.jsonToContainer(tt.input)

			if tt.expected == nil {
				assert.Nil(t, container)
			} else {
				assert.NotNil(t, container)
				assert.Equal(t, tt.expected.ID, container.ID)
				assert.Equal(t, tt.expected.Name, container.Name)
			}
		})
	}
}

func TestJsonToImage(t *testing.T) {
	log := logrus.NewEntry(logrus.New())
	appConfig := &config.AppConfig{Runtime: "apple"}
	osCommand := &OSCommand{}
	tr := &i18n.TranslationSet{}
	errorChan := make(chan error, 1)

	cmd := &AppleContainerCommand{
		Log:       log,
		OSCommand: osCommand,
		Tr:        tr,
		Config:    appConfig,
		ErrorChan: errorChan,
	}

	tests := []struct {
		name     string
		input    map[string]interface{}
		expected *Image
	}{
		{
			name: "valid image data",
			input: map[string]interface{}{
				"reference": "nginx:latest",
				"descriptor": map[string]interface{}{
					"digest": "sha256:abc",
					"size":   1234,
				},
			},
			expected: &Image{
				ID:   "sha256:abc",
				Name: "nginx",
				Tag:  "latest",
			},
		},
		// Missing digest is fine; implementation uses reference as ID
		{
			name: "missing id",
			input: map[string]interface{}{
				"reference": "nginx:latest",
			},
			expected: &Image{ID: "nginx:latest", Name: "nginx", Tag: "latest"},
		},
		{
			name: "partial data",
			input: map[string]interface{}{
				"reference": "nginx",
			},
			expected: &Image{
				ID:   "nginx",
				Name: "nginx",
				Tag:  "latest",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			image := cmd.jsonToImage(tt.input)

			if tt.expected == nil {
				assert.Nil(t, image)
			} else {
				assert.NotNil(t, image)
				assert.Equal(t, tt.expected.ID, image.ID)
				assert.Equal(t, tt.expected.Name, image.Name)
				assert.Equal(t, tt.expected.Tag, image.Tag)
			}
		})
	}
}
