package gui

import (
	"fmt"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/gui/presentation"
)

func (gui *Gui) getSwarmSecretsPanel() *panels.SideListPanel[*commands.SwarmSecret] {
	return newSwarmPanel(gui, swarmPanelOpts[*commands.SwarmSecret]{
		view:    gui.Views.SwarmSecrets,
		noItems: gui.Tr.SwarmNoSecrets,
		getID:   func(s *commands.SwarmSecret) string { return s.ID },
		cells:   presentation.GetSwarmSecretDisplayStrings,
		less:    func(a, b *commands.SwarmSecret) bool { return a.Name < b.Name },
		tabs: []swarmTab[*commands.SwarmSecret]{
			{key: "services", title: gui.Tr.ServicesTitle, live: func(s *commands.SwarmSecret) string { return swarmServicesTable(s.UsedBy) }},
			{key: "config", title: gui.Tr.ConfigTitle, live: func(s *commands.SwarmSecret) string { return swarmYaml(s.Secret) }},
		},
	})
}

func (gui *Gui) handleSwarmSecretCreateMenu(g *gocui.Gui, v *gocui.View) error {
	swarm := gui.State.Swarm
	if swarm == nil {
		return nil
	}

	return gui.swarmCreateMenu(gui.Tr.SwarmCreateSecret, true, swarm.CreateSecret)
}

// the secret is mounted into the service's containers at /run/secrets/<name>,
// with the name being asked for
func (gui *Gui) handleSwarmSecretAttachMenu(g *gocui.Gui, v *gocui.View) error {
	secret, err := gui.Panels.SwarmSecrets.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.swarmAttachMenu(secret.Name, secret.UsedBy, func(service *commands.SwarmService) error {
		return gui.createSwarmPrompt(gui.Tr.SwarmSecretTargetPrompt, secret.Name, func(target string) error {
			return gui.runSwarmAction(gui.Tr.SwarmUpdatingStatus, func() error { return service.AttachSecret(secret, target) })
		})
	})
}

func (gui *Gui) handleSwarmSecretDetachMenu(g *gocui.Gui, v *gocui.View) error {
	secret, err := gui.Panels.SwarmSecrets.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.swarmDetachMenu(secret.Name, secret.UsedBy, func(service *commands.SwarmService) error {
		return service.DetachSecret(secret)
	})
}

func (gui *Gui) handleSwarmSecretRemove(g *gocui.Gui, v *gocui.View) error {
	secret, err := gui.Panels.SwarmSecrets.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.confirmSwarmAction(fmt.Sprintf(gui.Tr.SwarmConfirmRemove, secret.Name), gui.Tr.RemovingStatus, secret.Remove)
}
