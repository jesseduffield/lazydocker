package gui

import (
	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/gui/types"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/samber/lo"
)

func (gui *Gui) createCommandMenu(customCommands []config.CustomCommand, commandObject commands.CommandObject, title string, waitingStatus string) error {
	menuItems := lo.Map(customCommands, func(command config.CustomCommand, _ int) *types.MenuItem {
		resolvedCommand := utils.ApplyTemplate(command.Command, commandObject)

		onPress := func() error {
			if command.InternalFunction != nil {
				return command.InternalFunction()
			}

			if command.Shell {
				resolvedCommand = gui.OSCommand.NewCommandStringWithShell(resolvedCommand)
			}

			// if we have a command for attaching, we attach and return the subprocess error
			if command.Attach {
				return gui.runSubprocess(gui.OSCommand.ExecutableFromString(resolvedCommand))
			}

			return gui.WithWaitingStatus(waitingStatus, func() error {
				if err := gui.OSCommand.RunCommand(resolvedCommand); err != nil {
					return gui.createErrorPanel(err.Error())
				}
				return nil
			})
		}

		return &types.MenuItem{
			LabelColumns: []string{
				command.Name,
				utils.ColoredString(utils.WithShortSha(resolvedCommand), color.FgCyan),
			},
			OnPress: onPress,
		}
	})

	return gui.Menu(CreateMenuOptions{
		Title: title,
		Items: menuItems,
	})
}

func (gui *Gui) createCustomCommandMenu(customCommands []config.CustomCommand, commandObject commands.CommandObject) error {
	return gui.createCommandMenu(customCommands, commandObject, gui.Tr.CustomCommandTitle, gui.Tr.RunningCustomCommandStatus)
}

func (gui *Gui) createBulkCommandMenu(customCommands []config.CustomCommand, commandObject commands.CommandObject) error {
	return gui.createCommandMenu(customCommands, commandObject, gui.Tr.BulkCommandTitle, gui.Tr.RunningBulkCommandStatus)
}
