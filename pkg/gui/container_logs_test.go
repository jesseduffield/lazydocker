package gui

import (
	"bytes"
	"context"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/stretchr/testify/assert"
)

func TestPipeContainerLogs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pipeThrough commands in this test need a unix shell")
	}

	type scenario struct {
		testName    string
		pipeThrough string
		tty         bool
		logs        io.Reader
		test        func(string, error)
	}

	scenarios := []scenario{
		{
			testName:    "pipes tty logs through the command",
			pipeThrough: "tr a-z A-Z",
			tty:         true,
			logs:        strings.NewReader("hello\nworld\n"),
			test: func(output string, err error) {
				assert.NoError(t, err)
				assert.EqualValues(t, "HELLO\nWORLD\n", output)
			},
		},
		{
			testName:    "pipes both stdout and stderr of non-tty logs through the command",
			pipeThrough: "tr a-z A-Z",
			tty:         false,
			logs:        multiplexedLogs("from stdout\n", "from stderr\n"),
			test: func(output string, err error) {
				assert.NoError(t, err)
				assert.EqualValues(t, "FROM STDOUT\nFROM STDERR\n", output)
			},
		},
		{
			testName:    "returns an error and shows stderr when the command fails",
			pipeThrough: "not-a-real-command-for-lazydocker",
			tty:         true,
			logs:        strings.NewReader("hello\n"),
			test: func(output string, err error) {
				assert.Error(t, err)
				assert.Contains(t, output, "not found")
			},
		},
	}

	for _, s := range scenarios {
		t.Run(s.testName, func(t *testing.T) {
			ctr := &commands.Container{
				Details: container.InspectResponse{Config: &container.Config{Tty: s.tty}},
			}

			var output bytes.Buffer
			err := newPipeThroughGui(s.pipeThrough).pipeContainerLogs(ctr, context.Background(), &output, s.logs)
			s.test(output.String(), err)
		})
	}
}

func newPipeThroughGui(pipeThrough string) *Gui {
	userConfig := config.GetDefaultConfig()
	userConfig.Logs.PipeThrough = pipeThrough

	return &Gui{
		OSCommand: commands.NewDummyOSCommand(),
		Config:    &config.AppConfig{UserConfig: &userConfig},
	}
}

// multiplexedLogs builds a log stream in the format docker uses for containers without a tty, where stdout and
// stderr are interleaved in one stream
func multiplexedLogs(stdout string, stderr string) io.Reader {
	var buf bytes.Buffer
	_, _ = stdcopy.NewStdWriter(&buf, stdcopy.Stdout).Write([]byte(stdout))
	_, _ = stdcopy.NewStdWriter(&buf, stdcopy.Stderr).Write([]byte(stderr))
	return &buf
}
