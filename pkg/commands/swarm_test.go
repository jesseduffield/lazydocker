package commands

import (
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
)

func uint64Ptr(n uint64) *uint64 {
	return &n
}

func TestSwarmBuildState(t *testing.T) {
	c := &SwarmCommand{}

	services := []swarm.Service{
		{
			ID:            "web-id",
			ServiceStatus: &swarm.ServiceStatus{RunningTasks: 1, DesiredTasks: 2},
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: "shop_web", Labels: map[string]string{StackNamespaceLabel: "shop"}},
				Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: uint64Ptr(2)}},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{
						Image:   "nginx:alpine@sha256:abc",
						Secrets: []*swarm.SecretReference{{SecretID: "secret-id", SecretName: "db_password"}},
						Configs: []*swarm.ConfigReference{{ConfigID: "config-id", ConfigName: "app_conf"}},
						Mounts: []mount.Mount{
							{Type: mount.TypeVolume, Source: "shop_data", Target: "/data"},
							{Type: mount.TypeVolume, Target: "/anonymous"},
							{Type: mount.TypeBind, Source: "/host", Target: "/host"},
						},
					},
					Networks: []swarm.NetworkAttachmentConfig{{Target: "network-id"}},
				},
			},
		},
		{
			ID: "agent-id",
			Spec: swarm.ServiceSpec{
				Annotations:  swarm.Annotations{Name: "agent"},
				Mode:         swarm.ServiceMode{Global: &swarm.GlobalService{}},
				TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "agent"}},
			},
		},
	}
	tasks := []swarm.Task{
		{ID: "t1", ServiceID: "web-id", NodeID: "node-id", Slot: 1, DesiredState: swarm.TaskStateRunning, Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
		{ID: "t2", ServiceID: "web-id", NodeID: "node-id", Slot: 2, DesiredState: swarm.TaskStateRunning, Status: swarm.TaskStatus{State: swarm.TaskStatePreparing}},
		{ID: "t3", ServiceID: "web-id", NodeID: "node-id", Slot: 2, DesiredState: swarm.TaskStateShutdown, Status: swarm.TaskStatus{State: swarm.TaskStateFailed}},
		{ID: "t4", ServiceID: "agent-id", NodeID: "node-id", DesiredState: swarm.TaskStateRunning, Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
	}
	nodes := []swarm.Node{{ID: "node-id", Description: swarm.NodeDescription{Hostname: "manager-1"}}}
	secrets := []swarm.Secret{
		{ID: "secret-id", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "db_password"}}},
		{ID: "unused-secret-id", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "unused"}}},
	}
	configs := []swarm.Config{{ID: "config-id", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "app_conf"}}}}
	volumes := []*volume.Volume{
		{Name: "csi-vol", Driver: "csi-driver", ClusterVolume: &volume.ClusterVolume{ID: "cluster-id"}},
		{Name: "manager-local", Driver: "local"},
	}
	networks := []network.Summary{
		{ID: "network-id", Name: "shop_default", Labels: map[string]string{StackNamespaceLabel: "shop"}},
		{ID: "ingress-id", Name: "ingress"},
	}

	state := c.buildState(services, tasks, nodes, volumes, networks, secrets, configs)

	web := state.Services[0]
	assert.Equal(t, "shop", web.Stack)
	assert.Equal(t, "nginx:alpine", web.Image())
	assert.Equal(t, "replicated", web.Mode())
	assert.Len(t, web.Tasks, 3)

	agent := state.Services[1]
	assert.Equal(t, "global", agent.Mode())

	assert.Equal(t, "shop_web.1", state.Tasks[0].Name())
	assert.Equal(t, "manager-1", state.Tasks[0].NodeName)
	assert.False(t, state.Tasks[2].IsCurrent())
	assert.Equal(t, "agent.node-id", state.Tasks[3].Name())
	assert.Len(t, state.Nodes[0].Tasks, 4)

	assert.Len(t, state.Stacks, 2)
	assert.True(t, state.Stacks[0].IsAll())
	assert.Equal(t, []*SwarmService{web, agent}, state.Stacks[0].Services)
	assert.Equal(t, "shop", state.Stacks[1].Name)
	assert.Equal(t, []*SwarmService{web}, state.Stacks[1].Services)
	assert.Equal(t, "shop", state.Tasks[0].Stack)
	running, desired := state.Stacks[1].Replicas()
	assert.Equal(t, uint64(1), running)
	assert.Equal(t, uint64(2), desired)

	assert.Equal(t, []*SwarmService{web}, state.Secrets[0].UsedBy)
	assert.Empty(t, state.Secrets[1].UsedBy)
	assert.Equal(t, []*SwarmService{web}, state.Configs[0].UsedBy)

	// cluster volumes and named volumes mounted by services, but not the
	// manager's local volumes, anonymous volumes or bind mounts
	assert.Len(t, state.Volumes, 2)
	assert.Equal(t, "csi-vol", state.Volumes[0].Name)
	assert.NotNil(t, state.Volumes[0].Volume)
	assert.Empty(t, state.Volumes[0].UsedBy)
	assert.Equal(t, "shop_data", state.Volumes[1].Name)
	assert.Equal(t, "shop", state.Volumes[1].Stack)
	assert.Nil(t, state.Volumes[1].Volume)
	assert.Equal(t, []*SwarmService{web}, state.Volumes[1].UsedBy)

	assert.Equal(t, "shop", state.Networks[0].Stack)
	assert.Equal(t, []*SwarmService{web}, state.Networks[0].UsedBy)
	assert.Empty(t, state.Networks[1].UsedBy)
}

func TestNewSwarmCommands(t *testing.T) {
	hosts := []config.SwarmHost{
		{Name: "prod", Host: "tcp://prod:2376"},
		{Host: "tcp://staging:2376"},
	}

	scenarios := []struct {
		testName        string
		swarmHost       string
		expectedNames   []string
		expectedInitial string
	}{
		{testName: "no flag starts in local mode", expectedNames: []string{"Swarm", "prod", "tcp://staging:2376"}},
		{testName: "flag names a configured swarm", swarmHost: "prod", expectedNames: []string{"Swarm", "prod", "tcp://staging:2376"}, expectedInitial: "prod"},
		{testName: "flag is a configured host", swarmHost: "tcp://prod:2376", expectedNames: []string{"Swarm", "prod", "tcp://staging:2376"}, expectedInitial: "prod"},
		{testName: "flag is another host", swarmHost: "tcp://other:2376", expectedNames: []string{"Swarm", "prod", "tcp://staging:2376", "tcp://other:2376"}, expectedInitial: "tcp://other:2376"},
	}

	for _, s := range scenarios {
		t.Run(s.testName, func(t *testing.T) {
			appConfig := &config.AppConfig{UserConfig: &config.UserConfig{Swarm: config.SwarmConfig{Hosts: hosts}}, SwarmHost: s.swarmHost}
			swarms, initial, err := NewSwarmCommands(nil, &i18n.TranslationSet{SwarmTitle: "Swarm"}, appConfig, nil)
			assert.NoError(t, err)

			assert.Equal(t, s.expectedNames, lo.Map(swarms, func(c *SwarmCommand, _ int) string { return c.Name }))
			assert.False(t, swarms[0].IsRemote())
			if s.expectedInitial == "" {
				assert.Nil(t, initial)
			} else {
				assert.Equal(t, s.expectedInitial, initial.Name)
				assert.True(t, initial.Enabled())
			}
		})
	}

	_, _, err := NewSwarmCommands(nil, &i18n.TranslationSet{}, &config.AppConfig{UserConfig: &config.UserConfig{Swarm: config.SwarmConfig{Hosts: []config.SwarmHost{{Name: "broken"}}}}}, nil)
	assert.Error(t, err)
}
