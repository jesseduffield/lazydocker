package gui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/fatih/color"
	"github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/jesseduffield/pty"
	"golang.org/x/crypto/ssh/terminal"
	"golang.org/x/xerrors"
)

// list panel functions

func (gui *Gui) getContainerContexts() []string {
	return []string{"logs", "config", "stats"}
}

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
		return gui.handleContainerSelect(gui.g, v)
	}

	gui.State.Panels.Containers.SelectedLine = newSelectedLine

	if prevSelectedLine == newSelectedLine && gui.currentViewName() == v.Name() {
		return nil
	}

	return gui.handleContainerSelect(gui.g, v)
}

func (gui *Gui) handleContainerSelect(g *gocui.Gui, v *gocui.View) error {
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

	// if we are in an attached state, we won't do anything here
	// TODO: generalise
	if strings.HasPrefix(gui.State.Panels.Main.ObjectKey, "attached-") {
		return nil
	}

	key := container.ID + "-" + gui.getContainerContexts()[gui.State.Panels.Containers.ContextIndex]
	if gui.State.Panels.Main.ObjectKey == key {
		return nil
	} else {
		gui.State.Panels.Main.ObjectKey = key
	}

	if err := gui.focusPoint(0, gui.State.Panels.Containers.SelectedLine, len(gui.State.Containers), v); err != nil {
		return err
	}

	mainView := gui.getMainView()

	gui.State.Panels.Main.WriterID++
	writerID := gui.State.Panels.Main.WriterID

	mainView.Clear()
	mainView.SetOrigin(0, 0)
	mainView.SetCursor(0, 0)

	switch gui.getContainerContexts()[gui.State.Panels.Containers.ContextIndex] {
	case "logs":
		if err := gui.renderContainerLogs(mainView, container, writerID); err != nil {
			return err
		}
	case "config":
		if err := gui.renderContainerConfig(mainView, container, writerID); err != nil {
			return err
		}
	case "stats":
		if err := gui.renderContainerStats(mainView, container, writerID); err != nil {
			return err
		}
	default:
		return errors.New("Unknown context for containers panel")
	}

	return nil
}

func (gui *Gui) renderContainerConfig(mainView *gocui.View, container *commands.Container, writerID int) error {
	mainView.Autoscroll = false
	mainView.Title = "Config"

	data, err := json.MarshalIndent(&container.Container, "", "  ")
	if err != nil {
		return err
	}
	gui.renderString(gui.g, "main", string(data))

	return nil
}

func (gui *Gui) renderContainerStats(mainView *gocui.View, container *commands.Container, writerID int) error {
	mainView.Autoscroll = false
	mainView.Title = "Stats"

	stream, err := gui.DockerCommand.Client.ContainerStats(context.Background(), container.ID, true)
	if err != nil {
		return err
	}

	go func() {
		cpuUsageHistory := []float64{}
		memoryUsageHistory := []float64{}
		scanner := bufio.NewScanner(stream.Body)
		for scanner.Scan() {
			data := scanner.Bytes()
			var stats commands.ContainerStats
			json.Unmarshal(data, &stats)

			cpuUsageHistory = append(cpuUsageHistory, stats.CalculateContainerCPUPercentage())
			if len(cpuUsageHistory) >= 20 {
				cpuUsageHistory = cpuUsageHistory[1:]
			}

			memoryUsageHistory = append(memoryUsageHistory, stats.CalculateContainerMemoryUsage())
			if len(memoryUsageHistory) >= 20 {
				memoryUsageHistory = memoryUsageHistory[1:]
			}

			width, _ := mainView.Size()

			contents, err := stats.RenderStats(width, cpuUsageHistory, memoryUsageHistory)
			if err != nil {
				gui.createErrorPanel(gui.g, err.Error())
			}

			if gui.State.Panels.Main.WriterID != writerID {
				stream.Body.Close()
				return
			}

			gui.reRenderString(gui.g, "main", contents)
		}

		stream.Body.Close()
	}()

	return nil
}

func (gui *Gui) renderContainerLogs(mainView *gocui.View, container *commands.Container, writerID int) error {
	mainView.Autoscroll = true
	mainView.Title = "Logs"

	var cmd *exec.Cmd
	cmd = gui.OSCommand.RunCustomCommand("docker logs --since=60m --timestamps --follow " + container.ID)

	cmd.Stdout = mainView
	cmd.Start()

	go func() {
		for {
			time.Sleep(time.Second / 100)
			if gui.State.Panels.Main.WriterID != writerID {
				cmd.Process.Kill()
				return
			}
		}
	}()

	return nil
}

func (gui *Gui) refreshContainers() error {
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
	if len(gui.State.Containers)-1 < gui.State.Panels.Containers.SelectedLine {
		gui.State.Panels.Containers.SelectedLine = len(gui.State.Containers) - 1
	}

	gui.g.Update(func(g *gocui.Gui) error {

		containersView.Clear()
		isFocused := gui.g.CurrentView().Name() == "containers"
		list, err := utils.RenderList(gui.State.Containers, utils.IsFocused(isFocused))
		if err != nil {
			return err
		}
		fmt.Fprint(containersView, list)

		if containersView == g.CurrentView() {
			return gui.handleContainerSelect(g, containersView)
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

	panelState := gui.State.Panels.Containers
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.State.Containers), false)

	return gui.handleContainerSelect(gui.g, v)
}

func (gui *Gui) handleContainersPrevLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	panelState := gui.State.Panels.Containers
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.State.Containers), true)

	return gui.handleContainerSelect(gui.g, v)
}

func (gui *Gui) handleContainersPrevContext(g *gocui.Gui, v *gocui.View) error {
	contexts := gui.getContainerContexts()
	if gui.State.Panels.Containers.ContextIndex >= len(contexts)-1 {
		gui.State.Panels.Containers.ContextIndex = 0
	} else {
		gui.State.Panels.Containers.ContextIndex++
	}

	gui.handleContainerSelect(gui.g, v)

	return nil
}

func (gui *Gui) handleContainersNextContext(g *gocui.Gui, v *gocui.View) error {
	contexts := gui.getContainerContexts()
	if gui.State.Panels.Containers.ContextIndex <= 0 {
		gui.State.Panels.Containers.ContextIndex = len(contexts) - 1
	} else {
		gui.State.Panels.Containers.ContextIndex--
	}

	gui.handleContainerSelect(gui.g, v)

	return nil
}

type removeContainerOption struct {
	description   string
	command       string
	configOptions types.ContainerRemoveOptions
	runCommand    bool
}

// GetDisplayStrings is a function.
func (r *removeContainerOption) GetDisplayStrings(isFocused bool) []string {
	return []string{r.description, color.New(color.FgRed).Sprint(r.command)}
}

func (gui *Gui) handleContainersRemoveMenu(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.getSelectedContainer(g)
	if err != nil {
		return nil
	}

	options := []*removeContainerOption{
		{
			description:   gui.Tr.SLocalize("remove"),
			command:       "docker rm " + container.ID[1:10],
			configOptions: types.ContainerRemoveOptions{},
			runCommand:    true,
		},
		{
			description:   gui.Tr.SLocalize("removeWithVolumes"),
			command:       "docker rm --volumes " + container.ID[1:10],
			configOptions: types.ContainerRemoveOptions{RemoveVolumes: true},
			runCommand:    true,
		},
		{
			description: gui.Tr.SLocalize("cancel"),
			runCommand:  false,
		},
	}

	handleMenuPress := func(index int) error {
		if !options[index].runCommand {
			return nil
		}
		configOptions := options[index].configOptions
		if cerr := container.Remove(configOptions); cerr != nil {
			var originalErr commands.ComplexError
			if xerrors.As(cerr, &originalErr) {
				if originalErr.Code == commands.MustStopContainer {
					return gui.createConfirmationPanel(gui.g, v, gui.Tr.SLocalize("Confirm"), gui.Tr.SLocalize("mustForceToRemoveContainer"), func(g *gocui.Gui, v *gocui.View) error {
						configOptions.Force = true
						if err := container.Remove(configOptions); err != nil {
							return err
						}
						return gui.refreshContainers()
					}, nil)
				}
			} else {
				return gui.createErrorPanel(gui.g, cerr.Error())
			}
		}

		return gui.refreshContainers()
	}

	return gui.createMenu("", options, len(options), handleMenuPress)
}

func (gui *Gui) handleContainerStop(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.getSelectedContainer(g)
	if err != nil {
		return nil
	}

	return gui.createConfirmationPanel(gui.g, v, gui.Tr.SLocalize("Confirm"), gui.Tr.SLocalize("StopContainer"), func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.SLocalize("StoppingStatus"), func() error {
			if err := container.Stop(); err != nil {
				return gui.createErrorPanel(gui.g, err.Error())
			}

			return gui.refreshContainers()
		})

	}, nil)
}

func (gui *Gui) handleContainerRestart(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.getSelectedContainer(g)
	if err != nil {
		return nil
	}

	return gui.WithWaitingStatus(gui.Tr.SLocalize("RestartingStatus"), func() error {
		if err := container.Restart(); err != nil {
			return gui.createErrorPanel(gui.g, err.Error())
		}

		return gui.refreshContainers()
	})
}

func (gui *Gui) handleContainerAttach(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.getSelectedContainer(g)
	if err != nil {
		return nil
	}

	gui.State.Panels.Main.WriterID++

	// c := container.Attach()

	// // Create arbitrary command.
	c := exec.Command("bash")

	// Start the command with a pty.
	ptmx, err := pty.Start(c)
	if err != nil {
		return err
	}

	go func() {
		// Set stdin in raw mode.
		oldState, err := terminal.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			panic(err)
		}
		defer func() { _ = terminal.Restore(int(os.Stdin.Fd()), oldState) }() // Best effort.
		defer func() { _ = ptmx.Close() }()                                   // Best effort.

		go func() { _, _ = io.Copy(ptmx, os.Stdin) }()

		mainView := gui.getMainView()
		mainView.Clear()
		mainView.Autoscroll = true
		gui.State.Panels.Main.ObjectKey = "attached-" + container.ID

		scanner := bufio.NewScanner(ptmx)
		scanner.Split(bufio.ScanBytes)

		content := ""
		for scanner.Scan() {
			content += scanner.Text()
			gui.Log.Warn("content")
			gui.Log.Warn(content)

			gui.renderString(gui.g, "main", content)
		}

		// reset object key
		gui.State.Panels.Main.ObjectKey = ""
		gui.State.Panels.Main.WriterID++

		gui.handleContainerSelect(gui.g, v)
	}()

	return nil
}

func (gui *Gui) handleServiceRestart(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.getSelectedContainer(g)
	if err != nil {
		return nil
	}

	return gui.WithWaitingStatus(gui.Tr.SLocalize("RestartingStatus"), func() error {
		if err := container.RestartService(); err != nil {
			return gui.createErrorPanel(gui.g, err.Error())
		}

		return gui.refreshContainers()
	})
}
