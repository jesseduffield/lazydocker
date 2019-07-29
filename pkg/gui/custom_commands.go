package gui

import (
	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

type customCommandOption struct {
	customCommand config.CustomCommand
	description   string
	command       string
	name          string
	runCommand    bool
	attach        bool
}

// GetDisplayStrings is a function.
func (r *customCommandOption) GetDisplayStrings(isFocused bool) []string {
	return []string{r.name, utils.ColoredString(r.description, color.FgCyan)}
}

func (gui *Gui) createCommandMenu(customCommands []config.CustomCommand, commandObject commands.CommandObject, title string, waitingStatus string) error {
	options := make([]*customCommandOption, len(customCommands)+1)
	for i, command := range customCommands {
		resolvedCommand := utils.ApplyTemplate(command.Command, commandObject)

		options[i] = &customCommandOption{
			customCommand: command,
			description:   utils.WithShortSha(resolvedCommand),
			command:       resolvedCommand,
			runCommand:    true,
			attach:        command.Attach,
			name:          command.Name,
		}
	}
	options[len(options)-1] = &customCommandOption{
		name:       gui.Tr.Cancel,
		runCommand: false,
	}

	handleMenuPress := func(index int) error {
		option := options[index]
		if !option.runCommand {
			return nil
		}

		if option.customCommand.InternalFunction != nil {
			return option.customCommand.InternalFunction()
		}

		// if we have a command for attaching, we attach and return the subprocess error
		if option.customCommand.Attach {
			cmd := gui.OSCommand.ExecutableFromString(option.command)
			gui.SubProcess = cmd
			return gui.Errors.ErrSubProcess
		}

		return gui.WithWaitingStatus(waitingStatus, func() error {
			if err := gui.OSCommand.RunCommand(option.command); err != nil {
				return gui.createErrorPanel(gui.g, err.Error())
			}
			return nil
		})
	}

	return gui.createMenu(title, options, len(options), handleMenuPress)
}

func (gui *Gui) createCustomCommandMenu(customCommands []config.CustomCommand, commandObject commands.CommandObject) error {
	return gui.createCommandMenu(customCommands, commandObject, gui.Tr.CustomCommandTitle, gui.Tr.RunningCustomCommandStatus)
}

func (gui *Gui) createBulkCommandMenu(customCommands []config.CustomCommand, commandObject commands.CommandObject) error {
	return gui.createCommandMenu(customCommands, commandObject, gui.Tr.BulkCommandTitle, gui.Tr.RunningBulkCommandStatus)
}
