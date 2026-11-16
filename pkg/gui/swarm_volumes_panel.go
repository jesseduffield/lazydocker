package gui

import (
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/gui/presentation"
)

// swarm volumes are read-only: named volumes live on the nodes running the
// tasks, where the manager can't reach them, and cluster volumes are managed
// by their CSI driver
func (gui *Gui) getSwarmVolumesPanel() *panels.SideListPanel[*commands.SwarmVolume] {
	return newSwarmPanel(gui, swarmPanelOpts[*commands.SwarmVolume]{
		view:    gui.Views.SwarmVolumes,
		noItems: gui.Tr.NoVolumes,
		getID:   func(v *commands.SwarmVolume) string { return v.Name },
		cells:   presentation.GetSwarmVolumeDisplayStrings,
		less:    func(a, b *commands.SwarmVolume) bool { return a.Name < b.Name },
		filter:  func(v *commands.SwarmVolume) bool { return gui.isInSelectedSwarmStack(v.Stack) },
		tabs: []swarmTab[*commands.SwarmVolume]{
			{key: "services", title: gui.Tr.ServicesTitle, live: func(v *commands.SwarmVolume) string { return swarmServicesTable(v.UsedBy) }},
			{key: "config", title: gui.Tr.ConfigTitle, live: func(v *commands.SwarmVolume) string {
				if v.Volume != nil {
					return swarmYaml(v.Volume)
				}
				return swarmYaml(v.Mounts)
			}},
		},
	})
}
