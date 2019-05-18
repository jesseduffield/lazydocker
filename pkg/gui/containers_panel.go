package gui

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

// list panel functions

func (gui *Gui) getSelectedContainer(g *gocui.Gui) (*commands.Container, error) {
	selectedLine := gui.State.Panels.Containers.SelectedLine
	if selectedLine == -1 {
		return &commands.Container{}, gui.Errors.ErrNoContainers
	}

	return gui.State.Containers[selectedLine], nil
}

func (gui *Gui) handleContainersFocus(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	cx, cy := v.Cursor()
	_, oy := v.Origin()

	prevSelectedLine := gui.State.Panels.Containers.SelectedLine
	newSelectedLine := cy - oy

	if newSelectedLine > len(gui.State.Containers)-1 || len(utils.Decolorise(gui.State.Containers[newSelectedLine].Name)) < cx {
		return gui.handleContainerSelect(gui.g, v, false)
	}

	gui.State.Panels.Containers.SelectedLine = newSelectedLine

	if prevSelectedLine == newSelectedLine && gui.currentViewName() == v.Name() {
		return gui.handleContainerPress(gui.g, v)
	} else {
		return gui.handleContainerSelect(gui.g, v, true)
	}
}

func (gui *Gui) handleContainerSelect(g *gocui.Gui, v *gocui.View, alreadySelected bool) error {
	if _, err := gui.g.SetCurrentView(v.Name()); err != nil {
		return err
	}

	container, err := gui.getSelectedContainer(g)
	if err != nil {
		if err != gui.Errors.ErrNoContainers {
			return err
		}
		return gui.renderString(g, "main", gui.Tr.SLocalize("NoChangedContainers"))
	}

	if err := gui.focusPoint(0, gui.State.Panels.Containers.SelectedLine, len(gui.State.Containers), v); err != nil {
		return err
	}

	// data, err := json.MarshalIndent(&container.Container, "", "  ")
	// if err != nil {
	// 	return err
	// }

	mainView := gui.getMainView()

	gui.State.MainWriterID++
	writerID := gui.State.MainWriterID
	mainView.Clear()
	mainView.SetOrigin(0, 0)
	mainView.SetCursor(0, 0)
	mainView.Autoscroll = true

	var cmd *exec.Cmd
	cmd = gui.OSCommand.RunCustomCommand("docker logs --since=60m --timestamps " + container.ID)

	cmd.Stdout = mainView
	cmd.Start()

	go func() {
		for {
			time.Sleep(time.Second / 100)
			if gui.State.MainWriterID != writerID {
				cmd.Process.Kill()
				return
			}
		}
	}()

	return nil
}

func (gui *Gui) refreshContainers() error {
	selectedContainer, _ := gui.getSelectedContainer(gui.g)

	containersView := gui.getContainersView()
	if containersView == nil {
		// if the containersView hasn't been instantiated yet we just return
		return nil
	}
	if err := gui.refreshStateContainers(); err != nil {
		return err
	}

	if len(gui.State.Containers) > 0 && gui.State.Panels.Containers.SelectedLine == -1 {
		gui.State.Panels.Containers.SelectedLine = 0
	}

	gui.g.Update(func(g *gocui.Gui) error {

		containersView.Clear()
		isFocused := gui.g.CurrentView().Name() == "containers"
		list, err := utils.RenderList(gui.State.Containers, isFocused)
		if err != nil {
			return err
		}
		fmt.Fprint(containersView, list)

		if containersView == g.CurrentView() {
			newSelectedContainer, _ := gui.getSelectedContainer(gui.g)
			alreadySelected := newSelectedContainer.Name == selectedContainer.Name
			return gui.handleContainerSelect(g, containersView, alreadySelected)
		}
		return nil
	})

	return nil
}

func (gui *Gui) refreshStateContainers() error {
	containers, err := gui.DockerCommand.GetContainers()
	if err != nil {
		return err
	}

	gui.State.Containers = containers

	return nil
}

func (gui *Gui) handleContainersNextLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	gui.Log.Warn("nexting")
	gui.Log.Warn(spew.Sdump(gui.State.Panels.Containers))
	gui.Log.Warn(len(gui.State.Containers))

	panelState := gui.State.Panels.Containers
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.State.Containers), false)

	gui.Log.Warn("nexting2")
	gui.Log.Warn(spew.Sdump(gui.State.Panels.Containers))
	gui.Log.Warn(len(gui.State.Containers))

	return gui.handleContainerSelect(gui.g, v, false)
}

func (gui *Gui) handleContainersPrevLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	panelState := gui.State.Panels.Containers
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.State.Containers), true)

	return gui.handleContainerSelect(gui.g, v, false)
}

func (gui *Gui) handleContainerPress(g *gocui.Gui, v *gocui.View) error {
	return nil
}
