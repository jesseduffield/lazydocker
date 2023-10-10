package commands

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestOSCommandRunCommandWithOutput is a function.
func TestOSCommandRunCommandWithOutput(t *testing.T) {
	type scenario struct {
		command string
		test    func(string, error)
	}

	scenarios := []scenario{
		{
			"echo -n '123'",
			func(output string, err error) {
				assert.NoError(t, err)
				assert.EqualValues(t, "123", output)
			},
		},
		{
			"rmdir unexisting-folder",
			func(output string, err error) {
				assert.Regexp(t, "rmdir.*unexisting-folder.*", err.Error())
			},
		},
	}

	for _, s := range scenarios {
		s.test(NewDummyOSCommand().RunCommandWithOutput(s.command))
	}
}

// TestOSCommandRunCommand is a function.
func TestOSCommandRunCommand(t *testing.T) {
	type scenario struct {
		command string
		test    func(error)
	}

	scenarios := []scenario{
		{
			"rmdir unexisting-folder",
			func(err error) {
				assert.Regexp(t, "rmdir.*unexisting-folder.*", err.Error())
			},
		},
	}

	for _, s := range scenarios {
		s.test(NewDummyOSCommand().RunCommand(s.command))
	}
}

// TestOSCommandEditFile is a function.
func TestOSCommandEditFile(t *testing.T) {
	type scenario struct {
		filename string
		command  func(string, ...string) *exec.Cmd
		getenv   func(string) string
		test     func(*exec.Cmd, error)
	}

	scenarios := []scenario{
		{
			"test",
			func(name string, arg ...string) *exec.Cmd {
				return exec.Command("exit", "1")
			},
			func(env string) string {
				return ""
			},
			func(cmd *exec.Cmd, err error) {
				assert.EqualError(t, err, "No editor defined in $VISUAL or $EDITOR")
			},
		},
		{
			"test",
			func(name string, arg ...string) *exec.Cmd {
				if name == "which" {
					return exec.Command("exit", "1")
				}

				assert.EqualValues(t, "nano", name)

				return exec.Command("exit", "0")
			},
			func(env string) string {
				if env == "VISUAL" {
					return "nano"
				}

				return ""
			},
			func(cmd *exec.Cmd, err error) {
				assert.NoError(t, err)
			},
		},
		{
			"test",
			func(name string, arg ...string) *exec.Cmd {
				if name == "which" {
					return exec.Command("exit", "1")
				}

				assert.EqualValues(t, "emacs", name)

				return exec.Command("exit", "0")
			},
			func(env string) string {
				if env == "EDITOR" {
					return "emacs"
				}

				return ""
			},
			func(cmd *exec.Cmd, err error) {
				assert.NoError(t, err)
			},
		},
		{
			"test",
			func(name string, arg ...string) *exec.Cmd {
				if name == "which" {
					return exec.Command("echo")
				}

				assert.EqualValues(t, "vi", name)

				return exec.Command("exit", "0")
			},
			func(env string) string {
				return ""
			},
			func(cmd *exec.Cmd, err error) {
				assert.NoError(t, err)
			},
		},
	}

	for _, s := range scenarios {
		OSCmd := NewDummyOSCommand()
		OSCmd.command = s.command
		OSCmd.getenv = s.getenv

		s.test(OSCmd.EditFile(s.filename))
	}
}

func TestOSCommandQuote(t *testing.T) {
	osCommand := NewDummyOSCommand()

	osCommand.Platform.os = "linux"

	actual := osCommand.Quote("hello `test`")

	expected := "\"hello \\`test\\`\""

	assert.EqualValues(t, expected, actual)
}

// TestOSCommandQuoteSingleQuote tests the quote function with ' quotes explicitly for Linux
func TestOSCommandQuoteSingleQuote(t *testing.T) {
	osCommand := NewDummyOSCommand()

	osCommand.Platform.os = "linux"

	actual := osCommand.Quote("hello 'test'")

	expected := `"hello 'test'"`

	assert.EqualValues(t, expected, actual)
}

// TestOSCommandQuoteDoubleQuote tests the quote function with " quotes explicitly for Linux
func TestOSCommandQuoteDoubleQuote(t *testing.T) {
	osCommand := NewDummyOSCommand()

	osCommand.Platform.os = "linux"

	actual := osCommand.Quote(`hello "test"`)

	expected := `"hello \"test\""`

	assert.EqualValues(t, expected, actual)
}

// TestOSCommandQuoteWindows tests the quote function for Windows
func TestOSCommandQuoteWindows(t *testing.T) {
	osCommand := NewDummyOSCommand()

	osCommand.Platform.os = "windows"

	actual := osCommand.Quote(`hello "test" 'test2'`)

	expected := `\"hello "'"'"test"'"'" 'test2'\"`

	assert.EqualValues(t, expected, actual)
}

// TestOSCommandUnquote is a function.
func TestOSCommandUnquote(t *testing.T) {
	osCommand := NewDummyOSCommand()

	actual := osCommand.Unquote(`hello "test"`)

	expected := "hello test"

	assert.EqualValues(t, expected, actual)
}

// TestOSCommandFileType is a function.
func TestOSCommandFileType(t *testing.T) {
	type scenario struct {
		path  string
		setup func()
		test  func(string)
	}

	scenarios := []scenario{
		{
			"testFile",
			func() {
				if _, err := os.Create("testFile"); err != nil {
					panic(err)
				}
			},
			func(output string) {
				assert.EqualValues(t, "file", output)
			},
		},
		{
			"file with spaces",
			func() {
				if _, err := os.Create("file with spaces"); err != nil {
					panic(err)
				}
			},
			func(output string) {
				assert.EqualValues(t, "file", output)
			},
		},
		{
			"testDirectory",
			func() {
				if err := os.Mkdir("testDirectory", 0o644); err != nil {
					panic(err)
				}
			},
			func(output string) {
				assert.EqualValues(t, "directory", output)
			},
		},
		{
			"nonExistant",
			func() {},
			func(output string) {
				assert.EqualValues(t, "other", output)
			},
		},
	}

	for _, s := range scenarios {
		s.setup()
		s.test(NewDummyOSCommand().FileType(s.path))
		_ = os.RemoveAll(s.path)
	}
}

func TestOSCommandCreateTempFile(t *testing.T) {
	type scenario struct {
		testName string
		filename string
		content  string
		test     func(string, error)
	}

	scenarios := []scenario{
		{
			"valid case",
			"filename",
			"content",
			func(path string, err error) {
				assert.NoError(t, err)

				content, err := os.ReadFile(path)
				assert.NoError(t, err)

				assert.Equal(t, "content", string(content))
			},
		},
	}

	for _, s := range scenarios {
		t.Run(s.testName, func(t *testing.T) {
			s.test(NewDummyOSCommand().CreateTempFile(s.filename, s.content))
		})
	}
}

func TestOSCommandExecutableFromStringWithShellLinux(t *testing.T) {
	osCommand := NewDummyOSCommand()

	osCommand.Platform.os = "linux"

	tests := []struct {
		name       string
		commandStr string
		want       string
	}{
		{
			"success",
			"pwd",
			fmt.Sprintf("%v %v %v", osCommand.Platform.shell, osCommand.Platform.shellArg, "\"pwd\""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := osCommand.NewCommandStringWithShell(tt.commandStr)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOSCommandNewCommandStringWithShellWindows(t *testing.T) {
	osCommand := NewDummyOSCommand()

	osCommand.Platform.os = "windows"

	tests := []struct {
		name       string
		commandStr string
		want       string
	}{
		{
			"success",
			"pwd",
			fmt.Sprintf("%v %v %v", osCommand.Platform.shell, osCommand.Platform.shellArg, "pwd"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := osCommand.NewCommandStringWithShell(tt.commandStr)
			assert.Equal(t, tt.want, got)
		})
	}
}
