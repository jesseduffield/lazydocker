package gui

import (
	"fmt"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/gui/presentation"
)

func (gui *Gui) getSwarmConfigsPanel() *panels.SideListPanel[*commands.SwarmConfig] {
	return newSwarmPanel(gui, swarmPanelOpts[*commands.SwarmConfig]{
		view:    gui.Views.SwarmConfigs,
		noItems: gui.Tr.SwarmNoConfigs,
		getID:   func(c *commands.SwarmConfig) string { return c.ID },
		cells:   presentation.GetSwarmConfigDisplayStrings,
		less:    func(a, b *commands.SwarmConfig) bool { return a.Name < b.Name },
		tabs: []swarmTab[*commands.SwarmConfig]{
			{key: "content", title: gui.Tr.SwarmContentTitle, live: func(c *commands.SwarmConfig) string { return string(c.Config.Spec.Data) }},
			{key: "services", title: gui.Tr.ServicesTitle, live: func(c *commands.SwarmConfig) string { return swarmServicesTable(c.UsedBy) }},
			{key: "config", title: gui.Tr.ConfigTitle, live: func(c *commands.SwarmConfig) string { return swarmYaml(c.Config) }},
		},
	})
}

func (gui *Gui) handleSwarmConfigCreateMenu(g *gocui.Gui, v *gocui.View) error {
	swarm := gui.State.Swarm
	if swarm == nil {
		return nil
	}

	return gui.swarmCreateMenu(gui.Tr.SwarmCreateConfig, swarm.CreateConfig)
}

// the config is mounted into the service's containers at the path being
// asked for, like `docker service create --config source=<name>,target=<path>`
func (gui *Gui) handleSwarmConfigAttachMenu(g *gocui.Gui, v *gocui.View) error {
	cfg, err := gui.Panels.SwarmConfigs.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.swarmAttachMenu(cfg.Name, cfg.UsedBy, func(service *commands.SwarmService) error {
		return gui.createSwarmPrompt(gui.Tr.SwarmConfigTargetPrompt, "/"+cfg.Name, func(target string) error {
			return gui.runSwarmAction(gui.Tr.SwarmUpdatingStatus, func() error { return service.AttachConfig(cfg, target) })
		})
	})
}

func (gui *Gui) handleSwarmConfigDetachMenu(g *gocui.Gui, v *gocui.View) error {
	cfg, err := gui.Panels.SwarmConfigs.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.swarmDetachMenu(cfg.Name, cfg.UsedBy, func(service *commands.SwarmService) error {
		return service.DetachConfig(cfg)
	})
}

func (gui *Gui) handleSwarmConfigRemove(g *gocui.Gui, v *gocui.View) error {
	cfg, err := gui.Panels.SwarmConfigs.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.confirmSwarmAction(fmt.Sprintf(gui.Tr.SwarmConfirmRemove, cfg.Name), gui.Tr.RemovingStatus, cfg.Remove)
}
