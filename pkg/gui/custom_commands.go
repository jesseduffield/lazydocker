package gui

import (
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

type customCommandOption struct {
	customCommand config.CustomCommand
	description   string
	command       string
	runCommand    bool
	attach        bool
}

// GetDisplayStrings is a function.
func (r *customCommandOption) GetDisplayStrings(isFocused bool) []string {
	return []string{r.description}
}

func (gui *Gui) createCustomCommandMenu(customCommands []config.CustomCommand, commandObject commands.CommandObject) error {
	options := make([]*customCommandOption, len(customCommands)+1)
	for i, command := range customCommands {
		resolvedCommand := utils.ApplyTemplate(command.Command, commandObject)

		options[i] = &customCommandOption{
			customCommand: command,
			description:   resolvedCommand,
			command:       resolvedCommand,
			runCommand:    true,
			attach:        command.Attach,
		}
	}
	options[len(options)-1] = &customCommandOption{
		description: gui.Tr.Cancel,
		runCommand:  false,
	}

	handleMenuPress := func(index int) error {
		option := options[index]
		if !option.runCommand {
			return nil
		}

		// if we have a command for attaching, we attach and return the subprocess error
		if option.customCommand.Attach {
			cmd := gui.OSCommand.ExecutableFromString(option.command)
			gui.SubProcess = cmd
			return gui.Errors.ErrSubProcess
		}

		return gui.WithWaitingStatus(gui.Tr.RunningCustomCommandStatus, func() error {
			if err := gui.OSCommand.RunCommand(option.command); err != nil {
				return gui.createErrorPanel(gui.g, err.Error())
			}
			return nil
		})
	}

	return gui.createMenu("", options, len(options), handleMenuPress)
}
