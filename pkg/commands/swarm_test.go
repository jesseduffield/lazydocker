package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
)

func TestSwarmBuildState(t *testing.T) {
	c := &SwarmCommand{}

	services := []swarm.Service{
		{
			ID:            "web-id",
			ServiceStatus: &swarm.ServiceStatus{RunningTasks: 1, DesiredTasks: 2},
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: "shop_web", Labels: map[string]string{StackNamespaceLabel: "shop"}},
				Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: lo.ToPtr(uint64(2))}},
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

	// sorted by name
	agent, web := state.Services[0], state.Services[1]
	assert.Equal(t, "shop", web.Stack)
	assert.Equal(t, "nginx:alpine", web.Image())
	assert.Equal(t, "replicated", web.Mode())
	assert.Len(t, web.Tasks, 3)

	assert.Equal(t, "global", agent.Mode())

	assert.Equal(t, "shop_web.1", state.Tasks[0].Name())
	assert.Equal(t, "manager-1", state.Tasks[0].NodeName)
	assert.Same(t, c, state.Tasks[0].Swarm)
	assert.False(t, state.Tasks[2].IsCurrent())
	assert.Equal(t, "agent.node-id", state.Tasks[3].Name())
	assert.Len(t, state.Nodes[0].Tasks, 4)

	assert.Len(t, state.Stacks, 2)
	assert.True(t, state.Stacks[0].IsAll())
	assert.Equal(t, []*SwarmService{agent, web}, state.Stacks[0].Services)
	assert.Equal(t, "shop", state.Stacks[1].Name)
	assert.Same(t, c, state.Stacks[1].Swarm)
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

	// says which host is invalid
	_, _, err = NewSwarmCommands(nil, &i18n.TranslationSet{}, &config.AppConfig{UserConfig: &config.UserConfig{Swarm: config.SwarmConfig{Hosts: []config.SwarmHost{{Host: "ssh://manager?x=1"}}}}}, nil)
	assert.ErrorContains(t, err, "ssh://manager?x=1")
}

// the digest our fake docker API resolves every image to
const fakeImageDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// newFakeSwarmService returns a service with the given spec, served by a fake
// docker API, along with the spec that updating the service sends (zero if
// it isn't updated)
func newFakeSwarmService(t *testing.T, spec swarm.ServiceSpec) (*SwarmService, *swarm.ServiceSpec) {
	t.Helper()

	sent := &swarm.ServiceSpec{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/services/id"):
			_ = json.NewEncoder(w).Encode(swarm.Service{ID: "id", Spec: spec})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/services/id/update"):
			_ = json.NewDecoder(r.Body).Decode(sent)
			_ = json.NewEncoder(w).Encode(swarm.ServiceUpdateResponse{})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/distribution/"):
			fmt.Fprintf(w, `{"Descriptor": {"digest": %q}}`, fakeImageDigest)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	dockerClient, err := client.NewClientWithOpts(client.WithHost("tcp://"+server.Listener.Addr().String()), client.WithVersion("1.45"))
	assert.NoError(t, err)
	return &SwarmService{ID: "id", Name: "web", Service: swarm.Service{ID: "id", Spec: spec}, Swarm: &SwarmCommand{Client: dockerClient}}, sent
}

func TestSwarmServiceReplicas(t *testing.T) {
	scale := func(replicas uint64) func(*SwarmService) error {
		return func(s *SwarmService) error { return s.Scale(replicas) }
	}

	scenarios := []struct {
		testName         string
		replicas         uint64
		previous         string
		action           func(*SwarmService) error
		expectedReplicas uint64
		expectedPrevious string
	}{
		{testName: "stopping remembers the replica count", replicas: 3, action: (*SwarmService).Stop, expectedPrevious: "3"},
		{testName: "starting restores it", previous: "3", action: (*SwarmService).Start, expectedReplicas: 3},
		{testName: "starting without it starts one replica", action: (*SwarmService).Start, expectedReplicas: 1},
		{testName: "scaling to 0 remembers the replica count", replicas: 3, action: scale(0), expectedPrevious: "3"},
		{testName: "scaling up forgets it", previous: "3", action: scale(4), expectedReplicas: 4},
	}

	for _, s := range scenarios {
		t.Run(s.testName, func(t *testing.T) {
			spec := swarm.ServiceSpec{Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: lo.ToPtr(s.replicas)}}}
			if s.previous != "" {
				spec.Labels = map[string]string{PreviousReplicasLabel: s.previous}
			}
			service, sent := newFakeSwarmService(t, spec)

			assert.NoError(t, s.action(service))
			assert.Equal(t, s.expectedReplicas, *sent.Mode.Replicated.Replicas)
			assert.Equal(t, s.expectedPrevious, sent.Labels[PreviousReplicasLabel])
		})
	}
}

func TestSwarmStackStart(t *testing.T) {
	replicated := func(replicas uint64) swarm.ServiceSpec {
		return swarm.ServiceSpec{Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: lo.ToPtr(replicas)}}}
	}
	running, runningSent := newFakeSwarmService(t, replicated(2))
	stopped, stoppedSent := newFakeSwarmService(t, replicated(0))

	assert.NoError(t, (&SwarmStack{Name: "shop", Services: []*SwarmService{running, stopped}}).Start())
	assert.Equal(t, uint64(1), *stoppedSent.Mode.Replicated.Replicas)
	// an unchanged spec would replace the running service's previous spec,
	// the one Rollback restores
	assert.Equal(t, swarm.ServiceSpec{}, *runningSent)
}

func TestSwarmServiceUpdateImage(t *testing.T) {
	service, sent := newFakeSwarmService(t, swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.26"}},
	})

	assert.NoError(t, service.UpdateImage("nginx:1.27"))
	// pinned to the digest the registry has for the tag
	assert.Equal(t, "nginx:1.27@"+fakeImageDigest, sent.TaskTemplate.ContainerSpec.Image)
}

func TestSwarmCommandUpdateIsManager(t *testing.T) {
	scenarios := []struct {
		testName    string
		swarmHeader string
		expected    bool
	}{
		{testName: "manager", swarmHeader: "active/manager", expected: true},
		{testName: "worker", swarmHeader: "active/worker", expected: false},
		// engines before 23.0 don't send the header, so we ask /info
		{testName: "no Swarm header", expected: true},
	}

	for _, s := range scenarios {
		t.Run(s.testName, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/_ping"):
					if s.swarmHeader != "" {
						w.Header().Set("Swarm", s.swarmHeader)
					}
				case strings.HasSuffix(r.URL.Path, "/info"):
					_ = json.NewEncoder(w).Encode(system.Info{Swarm: swarm.Info{ControlAvailable: true}})
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)

			dockerClient, err := client.NewClientWithOpts(client.WithHost("tcp://"+server.Listener.Addr().String()), client.WithVersion("1.41"))
			assert.NoError(t, err)
			local := &SwarmCommand{Client: dockerClient}
			local.UpdateIsManager()
			assert.Equal(t, s.expected, local.Enabled())
		})
	}
}
