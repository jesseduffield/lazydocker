package gui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/swarm"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/gui/presentation"
	"github.com/jesseduffield/lazydocker/pkg/gui/types"
	"github.com/jesseduffield/lazydocker/pkg/tasks"
	"github.com/samber/lo"
)

func (gui *Gui) getSwarmServicesPanel() *panels.SideListPanel[*commands.SwarmService] {
	return newSwarmPanel(gui, swarmPanelOpts[*commands.SwarmService]{
		view:    gui.Views.SwarmServices,
		noItems: gui.Tr.SwarmNoServices,
		getID:   func(s *commands.SwarmService) string { return s.ID },
		cells:   presentation.GetSwarmServiceDisplayStrings,
		less:    func(a, b *commands.SwarmService) bool { return a.Name < b.Name },
		filter:  func(s *commands.SwarmService) bool { return gui.isInSelectedSwarmStack(s.Stack) },
		tabs: []swarmTab[*commands.SwarmService]{
			{key: "logs", title: gui.Tr.LogsTitle, render: func(s *commands.SwarmService) tasks.TaskFunc {
				return gui.renderSwarmLogs(s.ID, false, s.IsTTY())
			}},
			{key: "tasks", title: gui.Tr.SwarmTasksTitle, live: func(s *commands.SwarmService) string {
				return swarmTasksTable(s.Tasks, true)
			}},
			{key: "config", title: gui.Tr.ConfigTitle, live: func(s *commands.SwarmService) string { return swarmYaml(s.Service) }},
		},
	})
}

func (gui *Gui) getSwarmStacksPanel() *panels.SideListPanel[*commands.SwarmStack] {
	return newSwarmPanel(gui, swarmPanelOpts[*commands.SwarmStack]{
		view:    gui.Views.SwarmStacks,
		noItems: gui.Tr.SwarmNoStacks,
		getID:   func(s *commands.SwarmStack) string { return s.Name },
		cells:   presentation.GetSwarmStackDisplayStrings,
		less:    func(a, b *commands.SwarmStack) bool { return a.Name < b.Name },
		onSelect: func(*commands.SwarmStack) error {
			if err := gui.Panels.SwarmServices.RerenderList(); err != nil {
				return err
			}
			return gui.Panels.SwarmTasks.RerenderList()
		},
		tabs: []swarmTab[*commands.SwarmStack]{
			{key: "services", title: gui.Tr.ServicesTitle, live: func(s *commands.SwarmStack) string {
				return swarmTable(s.Services, presentation.GetSwarmServiceDisplayStrings)
			}},
			{key: "tasks", title: gui.Tr.SwarmTasksTitle, live: func(s *commands.SwarmStack) string {
				return swarmTasksTable(lo.FlatMap(s.Services, func(s *commands.SwarmService, _ int) []*commands.SwarmTask { return s.Tasks }), false)
			}},
		},
	})
}

func (gui *Gui) getSwarmTasksPanel() *panels.SideListPanel[*commands.SwarmTask] {
	return newSwarmPanel(gui, swarmPanelOpts[*commands.SwarmTask]{
		view:    gui.Views.SwarmTasks,
		noItems: gui.Tr.SwarmNoTasks,
		getID:   func(t *commands.SwarmTask) string { return t.ID },
		cells:   presentation.GetSwarmTaskDisplayStrings,
		less:    swarmTaskLess,
		filter: func(t *commands.SwarmTask) bool {
			return (gui.State.ShowSwarmTaskHistory || t.IsCurrent()) && gui.isInSelectedSwarmStack(t.Stack)
		},
		tabs: []swarmTab[*commands.SwarmTask]{
			{key: "logs", title: gui.Tr.LogsTitle, render: func(t *commands.SwarmTask) tasks.TaskFunc {
				return gui.renderSwarmLogs(t.ID, true, t.IsTTY())
			}},
			{key: "config", title: gui.Tr.ConfigTitle, live: func(t *commands.SwarmTask) string { return swarmYaml(t.Task) }},
		},
	})
}

func (gui *Gui) getSwarmNodesPanel() *panels.SideListPanel[*commands.SwarmNode] {
	return newSwarmPanel(gui, swarmPanelOpts[*commands.SwarmNode]{
		view:    gui.Views.SwarmNodes,
		noItems: gui.Tr.SwarmNoNodes,
		getID:   func(n *commands.SwarmNode) string { return n.ID },
		cells:   presentation.GetSwarmNodeDisplayStrings,
		less:    func(a, b *commands.SwarmNode) bool { return a.Hostname() < b.Hostname() },
		tabs: []swarmTab[*commands.SwarmNode]{
			{key: "tasks", title: gui.Tr.SwarmTasksTitle, live: func(n *commands.SwarmNode) string { return swarmTasksTable(n.Tasks, false) }},
			{key: "config", title: gui.Tr.ConfigTitle, live: func(n *commands.SwarmNode) string { return swarmYaml(n.Node) }},
		},
	})
}

func (gui *Gui) getSwarmVolumesPanel() *panels.SideListPanel[*commands.SwarmVolume] {
	return newSwarmPanel(gui, swarmPanelOpts[*commands.SwarmVolume]{
		view:    gui.Views.SwarmVolumes,
		noItems: gui.Tr.NoVolumes,
		getID:   func(v *commands.SwarmVolume) string { return v.Name },
		cells:   presentation.GetSwarmVolumeDisplayStrings,
		less:    func(a, b *commands.SwarmVolume) bool { return a.Name < b.Name },
		filter:  func(v *commands.SwarmVolume) bool { return gui.isInSelectedSwarmStack(v.Stack) },
		tabs: []swarmTab[*commands.SwarmVolume]{
			{key: "services", title: gui.Tr.ServicesTitle, live: func(v *commands.SwarmVolume) string {
				return swarmTable(v.UsedBy, presentation.GetSwarmServiceDisplayStrings)
			}},
			{key: "config", title: gui.Tr.ConfigTitle, live: func(v *commands.SwarmVolume) string {
				if v.Volume != nil {
					return swarmYaml(v.Volume)
				}
				return swarmYaml(v.Mounts)
			}},
		},
	})
}

func (gui *Gui) getSwarmNetworksPanel() *panels.SideListPanel[*commands.SwarmNetwork] {
	return newSwarmPanel(gui, swarmPanelOpts[*commands.SwarmNetwork]{
		view:    gui.Views.SwarmNetworks,
		noItems: gui.Tr.NoNetworks,
		getID:   func(n *commands.SwarmNetwork) string { return n.ID },
		cells:   presentation.GetSwarmNetworkDisplayStrings,
		less:    func(a, b *commands.SwarmNetwork) bool { return a.Name < b.Name },
		filter:  func(n *commands.SwarmNetwork) bool { return gui.isInSelectedSwarmStack(n.Stack) },
		tabs: []swarmTab[*commands.SwarmNetwork]{
			{key: "services", title: gui.Tr.ServicesTitle, live: func(n *commands.SwarmNetwork) string {
				return swarmTable(n.UsedBy, presentation.GetSwarmServiceDisplayStrings)
			}},
			{key: "config", title: gui.Tr.ConfigTitle, live: func(n *commands.SwarmNetwork) string { return swarmYaml(n.Network) }},
		},
	})
}

func (gui *Gui) getSwarmSecretsPanel() *panels.SideListPanel[*commands.SwarmSecret] {
	return newSwarmPanel(gui, swarmPanelOpts[*commands.SwarmSecret]{
		view:    gui.Views.SwarmSecrets,
		noItems: gui.Tr.SwarmNoSecrets,
		getID:   func(s *commands.SwarmSecret) string { return s.ID },
		cells:   presentation.GetSwarmSecretDisplayStrings,
		less:    func(a, b *commands.SwarmSecret) bool { return a.Name < b.Name },
		tabs: []swarmTab[*commands.SwarmSecret]{
			{key: "config", title: gui.Tr.ConfigTitle, live: func(s *commands.SwarmSecret) string { return swarmYaml(s.Secret) }},
		},
	})
}

func (gui *Gui) getSwarmConfigsPanel() *panels.SideListPanel[*commands.SwarmConfig] {
	return newSwarmPanel(gui, swarmPanelOpts[*commands.SwarmConfig]{
		view:    gui.Views.SwarmConfigs,
		noItems: gui.Tr.SwarmNoConfigs,
		getID:   func(c *commands.SwarmConfig) string { return c.ID },
		cells:   presentation.GetSwarmConfigDisplayStrings,
		less:    func(a, b *commands.SwarmConfig) bool { return a.Name < b.Name },
		tabs: []swarmTab[*commands.SwarmConfig]{
			{key: "content", title: gui.Tr.SwarmContentTitle, live: func(c *commands.SwarmConfig) string { return string(c.Config.Spec.Data) }},
			{key: "config", title: gui.Tr.ConfigTitle, live: func(c *commands.SwarmConfig) string { return swarmYaml(c.Config) }},
		},
	})
}

// sorted by name, then most recent first
func swarmTaskLess(a, b *commands.SwarmTask) bool {
	if a.ServiceName != b.ServiceName {
		return a.ServiceName < b.ServiceName
	}
	if a.Name() != b.Name() {
		return a.Name() < b.Name()
	}
	return a.Task.Status.Timestamp.After(b.Task.Status.Timestamp)
}

func swarmTasksTable(swarmTasks []*commands.SwarmTask, includeHistory bool) string {
	swarmTasks = lo.Filter(swarmTasks, func(t *commands.SwarmTask, _ int) bool { return includeHistory || t.IsCurrent() })
	sort.SliceStable(swarmTasks, func(i, j int) bool { return swarmTaskLess(swarmTasks[i], swarmTasks[j]) })
	return swarmTable(swarmTasks, presentation.GetSwarmTaskDisplayStrings)
}

// withSelected turns a handler for the selected item into a keybinding handler
func withSelected[T comparable](panel *panels.SideListPanel[T], f func(T) error) func(*gocui.Gui, *gocui.View) error {
	return func(*gocui.Gui, *gocui.View) error {
		item, err := panel.GetSelectedItem()
		if err != nil {
			return nil
		}
		return f(item)
	}
}

// withSelectedStack is withSelected for actions on a stack, which don't apply
// to the entry standing for all services
func withSelectedStack(gui *Gui, f func(*commands.SwarmStack) error) func(*gocui.Gui, *gocui.View) error {
	return withSelected(gui.Panels.SwarmStacks, func(stack *commands.SwarmStack) error {
		if stack.IsAll() {
			return gui.createErrorPanel(gui.Tr.SwarmSelectStack)
		}
		return f(stack)
	})
}

func (gui *Gui) swarmKeybindings() []*Binding {
	tr := gui.Tr
	p := gui.Panels
	binding := func(view *gocui.View, key rune, handler func(*gocui.Gui, *gocui.View) error, description string) *Binding {
		return &Binding{ViewName: view.Name(), Key: key, Modifier: gocui.ModNone, Handler: handler, Description: description}
	}

	prevMode := wrappedHandler(func() error { return gui.selectModeTab(gui.modeTabIndex() - 1) })
	nextMode := wrappedHandler(func() error { return gui.selectModeTab(gui.modeTabIndex() + 1) })

	return []*Binding{
		binding(gui.Views.SwarmServices, 'm', withSelected(p.SwarmServices, func(s *commands.SwarmService) error { return gui.swarmLogsCmd(s.Name) }), tr.ViewLogs),
		binding(gui.Views.SwarmServices, 's', withSelected(p.SwarmServices, func(s *commands.SwarmService) error {
			return gui.confirmSwarmAction(fmt.Sprintf(tr.SwarmConfirmStopService, s.Name), tr.StoppingStatus, s.Stop)
		}), tr.SwarmStopService),
		binding(gui.Views.SwarmServices, 'u', withSelected(p.SwarmServices, func(s *commands.SwarmService) error { return gui.runSwarmAction(tr.StartingStatus, s.Start) }), tr.SwarmStartService),
		binding(gui.Views.SwarmServices, 'S', withSelected(p.SwarmServices, gui.handleSwarmServiceScale), tr.SwarmScaleService),
		binding(gui.Views.SwarmServices, 'r', withSelected(p.SwarmServices, func(s *commands.SwarmService) error {
			return gui.confirmSwarmAction(fmt.Sprintf(tr.SwarmConfirmRestartService, s.Name), tr.RestartingStatus, s.ForceUpdate)
		}), tr.SwarmRestartService),
		binding(gui.Views.SwarmServices, 'U', withSelected(p.SwarmServices, gui.handleSwarmServiceUpdateImage), tr.SwarmUpdateImage),
		binding(gui.Views.SwarmServices, 'R', withSelected(p.SwarmServices, func(s *commands.SwarmService) error {
			return gui.confirmSwarmAction(fmt.Sprintf(tr.SwarmConfirmRollbackService, s.Name), tr.SwarmRollingBackStatus, s.Rollback)
		}), tr.SwarmRollbackService),
		binding(gui.Views.SwarmServices, 'd', withSelected(p.SwarmServices, func(s *commands.SwarmService) error {
			return gui.confirmSwarmAction(fmt.Sprintf(tr.SwarmConfirmRemoveService, s.Name), tr.RemovingStatus, s.Remove)
		}), tr.SwarmRemoveService),

		binding(gui.Views.SwarmStacks, 'n', gui.handleSwarmStackDeploy, tr.SwarmDeployStack),
		binding(gui.Views.SwarmStacks, 's', withSelectedStack(gui, func(s *commands.SwarmStack) error {
			return gui.confirmSwarmAction(fmt.Sprintf(tr.SwarmConfirmStopStack, s.Name), tr.StoppingStatus, s.Stop)
		}), tr.SwarmStopStack),
		binding(gui.Views.SwarmStacks, 'u', withSelectedStack(gui, func(s *commands.SwarmStack) error { return gui.runSwarmAction(tr.StartingStatus, s.Start) }), tr.SwarmStartStack),
		binding(gui.Views.SwarmStacks, 'd', withSelectedStack(gui, gui.handleSwarmStackRemove), tr.SwarmRemoveStack),

		binding(gui.Views.SwarmTasks, 'm', withSelected(p.SwarmTasks, func(t *commands.SwarmTask) error { return gui.swarmLogsCmd(t.ID) }), tr.ViewLogs),
		binding(gui.Views.SwarmTasks, 'e', func(*gocui.Gui, *gocui.View) error {
			gui.State.ShowSwarmTaskHistory = !gui.State.ShowSwarmTaskHistory
			return p.SwarmTasks.RerenderList()
		}, tr.SwarmToggleTaskHistory),

		binding(gui.Views.SwarmNodes, 'a', withSelected(p.SwarmNodes, gui.handleSwarmNodeAvailabilityMenu), tr.SwarmSetAvailability),
		binding(gui.Views.SwarmNodes, 'p', withSelected(p.SwarmNodes, gui.handleSwarmNodeRoleMenu), tr.SwarmChangeRole),
		binding(gui.Views.SwarmNodes, 'd', withSelected(p.SwarmNodes, gui.handleSwarmNodeRemoveMenu), tr.SwarmRemoveNode),

		binding(gui.Views.SwarmNetworks, 'n', func(*gocui.Gui, *gocui.View) error {
			return gui.createSwarmPrompt(tr.SwarmNamePrompt, "", func(name string) error {
				return gui.runSwarmAction(tr.SwarmCreatingStatus, func() error { return gui.State.Swarm.CreateNetwork(name) })
			})
		}, tr.SwarmCreateNetwork),
		binding(gui.Views.SwarmNetworks, 'a', withSelected(p.SwarmNetworks, func(n *commands.SwarmNetwork) error {
			return gui.swarmAttachMenu(n.Name, n.UsedBy, func(service *commands.SwarmService) error {
				return gui.runSwarmAction(tr.SwarmUpdatingStatus, func() error { return service.AttachNetwork(n) })
			})
		}), tr.SwarmAttachToService),
		binding(gui.Views.SwarmNetworks, 'D', withSelected(p.SwarmNetworks, func(n *commands.SwarmNetwork) error {
			return gui.swarmDetachMenu(n.Name, n.UsedBy, func(service *commands.SwarmService) error { return service.DetachNetwork(n) })
		}), tr.SwarmDetachFromService),
		binding(gui.Views.SwarmNetworks, 'd', withSelected(p.SwarmNetworks, func(n *commands.SwarmNetwork) error {
			return gui.confirmSwarmAction(fmt.Sprintf(tr.SwarmConfirmRemove, n.Name), tr.RemovingStatus, n.Remove)
		}), tr.Remove),

		binding(gui.Views.SwarmSecrets, 'n', func(*gocui.Gui, *gocui.View) error {
			return gui.swarmCreateMenu(tr.SwarmCreateSecret, true, gui.State.Swarm.CreateSecret)
		}, tr.SwarmCreateSecret),
		binding(gui.Views.SwarmSecrets, 'a', withSelected(p.SwarmSecrets, func(s *commands.SwarmSecret) error {
			return gui.swarmAttachMenu(s.Name, s.UsedBy, func(service *commands.SwarmService) error {
				return gui.createSwarmPrompt(tr.SwarmSecretTargetPrompt, s.Name, func(target string) error {
					return gui.runSwarmAction(tr.SwarmUpdatingStatus, func() error { return service.AttachSecret(s, target) })
				})
			})
		}), tr.SwarmAttachToService),
		binding(gui.Views.SwarmSecrets, 'D', withSelected(p.SwarmSecrets, func(s *commands.SwarmSecret) error {
			return gui.swarmDetachMenu(s.Name, s.UsedBy, func(service *commands.SwarmService) error { return service.DetachSecret(s) })
		}), tr.SwarmDetachFromService),
		binding(gui.Views.SwarmSecrets, 'd', withSelected(p.SwarmSecrets, func(s *commands.SwarmSecret) error {
			return gui.confirmSwarmAction(fmt.Sprintf(tr.SwarmConfirmRemove, s.Name), tr.RemovingStatus, s.Remove)
		}), tr.Remove),

		binding(gui.Views.SwarmConfigs, 'n', func(*gocui.Gui, *gocui.View) error {
			return gui.swarmCreateMenu(tr.SwarmCreateConfig, false, gui.State.Swarm.CreateConfig)
		}, tr.SwarmCreateConfig),
		binding(gui.Views.SwarmConfigs, 'a', withSelected(p.SwarmConfigs, func(c *commands.SwarmConfig) error {
			return gui.swarmAttachMenu(c.Name, c.UsedBy, func(service *commands.SwarmService) error {
				return gui.createSwarmPrompt(tr.SwarmConfigTargetPrompt, "/"+c.Name, func(target string) error {
					return gui.runSwarmAction(tr.SwarmUpdatingStatus, func() error { return service.AttachConfig(c, target) })
				})
			})
		}), tr.SwarmAttachToService),
		binding(gui.Views.SwarmConfigs, 'D', withSelected(p.SwarmConfigs, func(c *commands.SwarmConfig) error {
			return gui.swarmDetachMenu(c.Name, c.UsedBy, func(service *commands.SwarmService) error { return service.DetachConfig(c) })
		}), tr.SwarmDetachFromService),
		binding(gui.Views.SwarmConfigs, 'd', withSelected(p.SwarmConfigs, func(c *commands.SwarmConfig) error {
			return gui.confirmSwarmAction(fmt.Sprintf(tr.SwarmConfirmRemove, c.Name), tr.RemovingStatus, c.Remove)
		}), tr.Remove),

		// the Local/<swarm> tabs of the projects window
		binding(gui.Views.Project, '<', prevMode, tr.SwitchLocalSwarm),
		binding(gui.Views.Project, '>', nextMode, tr.SwitchLocalSwarm),
		binding(gui.Views.SwarmStacks, '<', prevMode, tr.SwitchLocalSwarm),
		binding(gui.Views.SwarmStacks, '>', nextMode, tr.SwitchLocalSwarm),
	}
}

func (gui *Gui) handleSwarmServiceScale(service *commands.SwarmService) error {
	_, desired := service.Replicas()
	return gui.createSwarmPrompt(fmt.Sprintf(gui.Tr.SwarmScalePrompt, service.Name), strconv.FormatUint(desired, 10), func(input string) error {
		replicas, err := strconv.ParseUint(input, 10, 64)
		if err != nil {
			return gui.createErrorPanel(err.Error())
		}
		return gui.runSwarmAction(gui.Tr.SwarmUpdatingStatus, func() error { return service.Scale(replicas) })
	})
}

func (gui *Gui) handleSwarmServiceUpdateImage(service *commands.SwarmService) error {
	return gui.createSwarmPrompt(fmt.Sprintf(gui.Tr.SwarmUpdateImagePrompt, service.Name), service.Image(), func(image string) error {
		return gui.runSwarmAction(gui.Tr.SwarmUpdatingStatus, func() error { return service.UpdateImage(image) })
	})
}

// stacks are deployed with the docker CLI, which parses the compose file
func (gui *Gui) handleSwarmStackDeploy(g *gocui.Gui, v *gocui.View) error {
	defaultName := filepath.Base(gui.Config.ProjectDir)
	if stack := gui.selectedSwarmStack(); stack != nil && !stack.IsAll() {
		defaultName = stack.Name
	}

	return gui.createSwarmPrompt(gui.Tr.SwarmDeployFilePrompt, "docker-compose.yml", func(composeFile string) error {
		return gui.createSwarmPrompt(gui.Tr.SwarmDeployNamePrompt, defaultName, func(name string) error {
			return gui.runSubprocess(gui.State.Swarm.NewCmd("stack", "deploy", "--with-registry-auth", "--compose-file", composeFile, name))
		})
	})
}

// like deploying, removing a stack is left to the docker CLI. --detach=false
// waits for the tasks to stop, so that the stack's networks can be removed.
func (gui *Gui) handleSwarmStackRemove(stack *commands.SwarmStack) error {
	return gui.confirmSwarmAction(fmt.Sprintf(gui.Tr.SwarmConfirmRemoveStack, stack.Name), gui.Tr.RemovingStatus, func() error {
		output, err := gui.OSCommand.RunExecutableWithOutput(gui.State.Swarm.NewCmd("stack", "rm", "--detach=false", stack.Name))
		if err != nil {
			return errors.New(output)
		}
		return nil
	})
}

func (gui *Gui) handleSwarmNodeAvailabilityMenu(node *commands.SwarmNode) error {
	item := func(availability swarm.NodeAvailability, label string) *types.MenuItem {
		return swarmMenuItem(label, func() error {
			return gui.runSwarmAction(gui.Tr.SwarmUpdatingStatus, func() error {
				return node.Update(func(spec *swarm.NodeSpec) { spec.Availability = availability })
			})
		})
	}

	return gui.swarmMenu(gui.Tr.SwarmSetAvailability,
		item(swarm.NodeAvailabilityActive, gui.Tr.SwarmAvailabilityActive),
		item(swarm.NodeAvailabilityPause, gui.Tr.SwarmAvailabilityPause),
		item(swarm.NodeAvailabilityDrain, gui.Tr.SwarmAvailabilityDrain),
	)
}

func (gui *Gui) handleSwarmNodeRoleMenu(node *commands.SwarmNode) error {
	item := func(role swarm.NodeRole, label string) *types.MenuItem {
		return swarmMenuItem(label, func() error {
			return gui.runSwarmAction(gui.Tr.SwarmUpdatingStatus, func() error {
				return node.Update(func(spec *swarm.NodeSpec) { spec.Role = role })
			})
		})
	}

	return gui.swarmMenu(gui.Tr.SwarmChangeRole,
		item(swarm.NodeRoleManager, gui.Tr.SwarmPromote),
		item(swarm.NodeRoleWorker, gui.Tr.SwarmDemote),
	)
}

func (gui *Gui) handleSwarmNodeRemoveMenu(node *commands.SwarmNode) error {
	item := func(force bool, label string) *types.MenuItem {
		return swarmMenuItem(label, func() error {
			return gui.runSwarmAction(gui.Tr.RemovingStatus, func() error { return node.Remove(force) })
		})
	}

	return gui.swarmMenu("", item(false, gui.Tr.Remove), item(true, gui.Tr.SwarmForceRemove))
}

// swarmCreateMenu creates a secret/config from a typed value or a local file
func (gui *Gui) swarmCreateMenu(title string, isSecret bool, create func(name string, data []byte) error) error {
	createWith := func(name string, data []byte) error {
		return gui.runSwarmAction(gui.Tr.SwarmCreatingStatus, func() error { return create(name, data) })
	}

	fromValue := func() error {
		return gui.createSwarmPrompt(gui.Tr.SwarmNamePrompt, "", func(name string) error {
			err := gui.createSwarmPrompt(gui.Tr.SwarmValuePrompt, "", func(value string) error {
				return createWith(name, []byte(value))
			})
			if isSecret {
				gui.Views.Confirmation.Mask = '*'
			}
			return err
		})
	}

	fromFile := func() error {
		return gui.createSwarmPrompt(gui.Tr.SwarmFilePathPrompt, "", func(path string) error {
			if rest, ok := strings.CutPrefix(path, "~/"); ok {
				home, _ := os.UserHomeDir()
				path = filepath.Join(home, rest)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return gui.createErrorPanel(err.Error())
			}
			return gui.createSwarmPrompt(gui.Tr.SwarmNamePrompt, filepath.Base(path), func(name string) error {
				return createWith(name, data)
			})
		})
	}

	return gui.swarmMenu(title,
		swarmMenuItem(gui.Tr.SwarmCreateFromValue, fromValue),
		swarmMenuItem(gui.Tr.SwarmCreateFromFile, fromFile),
	)
}

// swarmAttachMenu picks a service that doesn't use a network/secret/config yet
func (gui *Gui) swarmAttachMenu(name string, usedBy []*commands.SwarmService, onSelect func(*commands.SwarmService) error) error {
	services := lo.Without(gui.Panels.SwarmServices.List.GetAllItems(), usedBy...)
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	items := lo.Map(services, func(service *commands.SwarmService, _ int) *types.MenuItem {
		return swarmMenuItem(service.Name, func() error { return onSelect(service) })
	})

	return gui.swarmMenu(fmt.Sprintf(gui.Tr.SwarmAttachTo, name), items...)
}

func (gui *Gui) swarmDetachMenu(name string, usedBy []*commands.SwarmService, detach func(*commands.SwarmService) error) error {
	items := lo.Map(usedBy, func(service *commands.SwarmService, _ int) *types.MenuItem {
		return swarmMenuItem(service.Name, func() error {
			return gui.runSwarmAction(gui.Tr.SwarmUpdatingStatus, func() error { return detach(service) })
		})
	})

	return gui.swarmMenu(fmt.Sprintf(gui.Tr.SwarmDetachFrom, name), items...)
}
