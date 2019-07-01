package commands

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-errors/errors"

	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/mgutz/str"
	"github.com/sirupsen/logrus"
	gitconfig "github.com/tcnksm/go-gitconfig"
)

// Platform stores the os state
type Platform struct {
	os                   string
	shell                string
	shellArg             string
	escapedQuote         string
	openCommand          string
	openLinkCommand      string
	fallbackEscapedQuote string
}

// OSCommand holds all the os commands
type OSCommand struct {
	Log                *logrus.Entry
	Platform           *Platform
	Config             *config.AppConfig
	command            func(string, ...string) *exec.Cmd
	getGlobalGitConfig func(string) (string, error)
	getenv             func(string) string
}

// NewOSCommand os command runner
func NewOSCommand(log *logrus.Entry, config *config.AppConfig) *OSCommand {
	return &OSCommand{
		Log:                log,
		Platform:           getPlatform(),
		Config:             config,
		command:            exec.Command,
		getGlobalGitConfig: gitconfig.Global,
		getenv:             os.Getenv,
	}
}

// SetCommand sets the command function used by the struct.
// To be used for testing only
func (c *OSCommand) SetCommand(cmd func(string, ...string) *exec.Cmd) {
	c.command = cmd
}

// RunCommandWithOutput wrapper around commands returning their output and error
func (c *OSCommand) RunCommandWithOutput(command string) (string, error) {
	cmd := c.ExecutableFromString(command)
	before := time.Now()
	output, err := sanitisedCommandOutput(cmd.CombinedOutput())
	c.Log.Warn(fmt.Sprintf("'%s': %s", command, time.Since(before)))
	return output, err
}

// RunExecutableWithOutput runs an executable file and returns its output
func (c *OSCommand) RunExecutableWithOutput(cmd *exec.Cmd) (string, error) {
	return sanitisedCommandOutput(cmd.CombinedOutput())
}

// RunExecutable runs an executable file and returns an error if there was one
func (c *OSCommand) RunExecutable(cmd *exec.Cmd) error {
	_, err := c.RunExecutableWithOutput(cmd)
	return err
}

// ExecutableFromString takes a string like `git status` and returns an executable command for it
func (c *OSCommand) ExecutableFromString(commandStr string) *exec.Cmd {
	splitCmd := str.ToArgv(commandStr)
	// c.Log.Info(splitCmd)
	return c.command(splitCmd[0], splitCmd[1:]...)
}

// RunCommand runs a command and just returns the error
func (c *OSCommand) RunCommand(command string) error {
	_, err := c.RunCommandWithOutput(command)
	return err
}

// FileType tells us if the file is a file, directory or other
func (c *OSCommand) FileType(path string) string {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return "other"
	}
	if fileInfo.IsDir() {
		return "directory"
	}
	return "file"
}

// RunDirectCommand wrapper around direct commands
func (c *OSCommand) RunDirectCommand(command string) (string, error) {
	c.Log.WithField("command", command).Info("RunDirectCommand")

	return sanitisedCommandOutput(
		c.command(c.Platform.shell, c.Platform.shellArg, command).
			CombinedOutput(),
	)
}

func sanitisedCommandOutput(output []byte, err error) (string, error) {
	outputString := string(output)
	if err != nil {
		// errors like 'exit status 1' are not very useful so we'll create an error
		// from the combined output
		if outputString == "" {
			return "", WrapError(err)
		}
		return outputString, errors.New(outputString)
	}
	return outputString, nil
}

// OpenFile opens a file with the given
func (c *OSCommand) OpenFile(filename string) error {
	commandTemplate := c.Config.UserConfig.OS.OpenCommand
	templateValues := map[string]string{
		"filename": c.Quote(filename),
	}

	command := utils.ResolvePlaceholderString(commandTemplate, templateValues)
	err := c.RunCommand(command)
	return err
}

// OpenLink opens a file with the given
func (c *OSCommand) OpenLink(link string) error {
	commandTemplate := c.Config.UserConfig.OS.OpenLinkCommand
	templateValues := map[string]string{
		"link": c.Quote(link),
	}

	command := utils.ResolvePlaceholderString(commandTemplate, templateValues)
	err := c.RunCommand(command)
	return err
}

// EditFile opens a file in a subprocess using whatever editor is available,
// falling back to core.editor, VISUAL, EDITOR, then vi
func (c *OSCommand) EditFile(filename string) (*exec.Cmd, error) {
	editor, _ := c.getGlobalGitConfig("core.editor")

	if editor == "" {
		editor = c.getenv("VISUAL")
	}
	if editor == "" {
		editor = c.getenv("EDITOR")
	}
	if editor == "" {
		if err := c.RunCommand("which vi"); err == nil {
			editor = "vi"
		}
	}
	if editor == "" {
		return nil, errors.New("No editor defined in $VISUAL, $EDITOR, or git config")
	}

	return c.PrepareSubProcess(editor, filename), nil
}

// PrepareSubProcess iniPrepareSubProcessrocess then tells the Gui to switch to it
func (c *OSCommand) PrepareSubProcess(cmdName string, commandArgs ...string) *exec.Cmd {
	return c.command(cmdName, commandArgs...)
}

// Quote wraps a message in platform-specific quotation marks
func (c *OSCommand) Quote(message string) string {
	message = strings.Replace(message, "`", "\\`", -1)
	escapedQuote := c.Platform.escapedQuote
	if strings.Contains(message, c.Platform.escapedQuote) {
		escapedQuote = c.Platform.fallbackEscapedQuote
	}
	return escapedQuote + message + escapedQuote
}

// Unquote removes wrapping quotations marks if they are present
// this is needed for removing quotes from staged filenames with spaces
func (c *OSCommand) Unquote(message string) string {
	return strings.Replace(message, `"`, "", -1)
}

// AppendLineToFile adds a new line in file
func (c *OSCommand) AppendLineToFile(filename, line string) error {
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return WrapError(err)
	}
	defer f.Close()

	_, err = f.WriteString("\n" + line)
	if err != nil {
		return WrapError(err)
	}
	return nil
}

// CreateTempFile writes a string to a new temp file and returns the file's name
func (c *OSCommand) CreateTempFile(filename, content string) (string, error) {
	tmpfile, err := ioutil.TempFile("", filename)
	if err != nil {
		c.Log.Error(err)
		return "", WrapError(err)
	}

	if _, err := tmpfile.WriteString(content); err != nil {
		c.Log.Error(err)
		return "", WrapError(err)
	}
	if err := tmpfile.Close(); err != nil {
		c.Log.Error(err)
		return "", WrapError(err)
	}

	return tmpfile.Name(), nil
}

// Remove removes a file or directory at the specified path
func (c *OSCommand) Remove(filename string) error {
	err := os.RemoveAll(filename)
	return WrapError(err)
}

// FileExists checks whether a file exists at the specified path
func (c *OSCommand) FileExists(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// RunPreparedCommand takes a pointer to an exec.Cmd and runs it
// this is useful if you need to give your command some environment variables
// before running it
func (c *OSCommand) RunPreparedCommand(cmd *exec.Cmd) error {
	out, err := cmd.CombinedOutput()
	outString := string(out)
	c.Log.Info(outString)
	if err != nil {
		if len(outString) == 0 {
			return err
		}
		return errors.New(outString)
	}
	return nil
}

// GetLazydockerPath returns the path of the currently executed file
func (c *OSCommand) GetLazydockerPath() string {
	ex, err := os.Executable() // get the executable path for git to use
	if err != nil {
		ex = os.Args[0] // fallback to the first call argument if needed
	}
	return filepath.ToSlash(ex)
}

// RunCustomCommand returns the pointer to a custom command
func (c *OSCommand) RunCustomCommand(command string) *exec.Cmd {
	return c.PrepareSubProcess(c.Platform.shell, c.Platform.shellArg, command)
}

// PipeCommands runs a heap of commands and pipes their inputs/outputs together like A | B | C
func (c *OSCommand) PipeCommands(commandStrings ...string) error {

	cmds := make([]*exec.Cmd, len(commandStrings))

	for i, str := range commandStrings {
		cmds[i] = c.ExecutableFromString(str)
	}

	for i := 0; i < len(cmds)-1; i++ {
		stdout, err := cmds[i].StdoutPipe()
		if err != nil {
			return err
		}

		cmds[i+1].Stdin = stdout
	}

	// keeping this here in case I adapt this code for some other purpose in the future
	// cmds[len(cmds)-1].Stdout = os.Stdout

	finalErrors := []string{}

	wg := sync.WaitGroup{}
	wg.Add(len(cmds))

	for _, cmd := range cmds {
		currentCmd := cmd
		go func() {
			stderr, err := currentCmd.StderrPipe()
			if err != nil {
				c.Log.Error(err)
			}

			if err := currentCmd.Start(); err != nil {
				c.Log.Error(err)
			}

			if b, err := ioutil.ReadAll(stderr); err == nil {
				if len(b) > 0 {
					finalErrors = append(finalErrors, string(b))
				}
			}

			if err := currentCmd.Wait(); err != nil {
				c.Log.Error(err)
			}

			wg.Done()
		}()
	}

	wg.Wait()

	if len(finalErrors) > 0 {
		return errors.New(strings.Join(finalErrors, "\n"))
	}
	return nil
}

// Kill kills a process. If the process has Setpgid == true, then we have anticipated that it might spawn its own child processes, so we've given it a process group ID (PGID) equal to its process id (PID) and given its child processes will inherit the PGID, we can kill that group, rather than killing the process itself.
func (c *OSCommand) Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		// somebody got to it before we were able to, poor bastard
		return nil
	}
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid == true {
		// minus sign means we're talking about a PGID as opposed to a PID
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	return cmd.Process.Kill()
}

// PrepareForChildren sets Setpgid to true on the cmd, so that when we run it as a sideproject, we can kill its group rather than the process itself. This is because some commands, like `docker-compose logs` spawn multiple children processes, and killing the parent process isn't sufficient for killing those child processes. We set the group id here, and then in subprocess.go we check if the group id is set and if so, we kill the whole group rather than just the one process.
func (c *OSCommand) PrepareForChildren(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}
