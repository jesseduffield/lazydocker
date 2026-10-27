package commands

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/docker/cli/cli/connhelper"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/samber/lo"
)

const (
	// label set by `docker stack deploy` on every resource belonging to a stack
	StackNamespaceLabel = "com.docker.stack.namespace"

	// label we set on a replicated service when stopping it (scaling to 0), so
	// that we know how many replicas to restore when starting it again
	PreviousReplicasLabel = "lazydocker.swarm.previous-replicas"
)

// SwarmCommand talks to a swarm manager: either the docker daemon the rest of
// lazydocker uses, or a remote one (e.g. ssh://user@manager) from the
// swarm.hosts config option or the --swarm-host flag.
type SwarmCommand struct {
	OSCommand *OSCommand
	Tr        *i18n.TranslationSet
	Config    *config.AppConfig
	Client    *client.Client

	// Name is shown in the swarm's tab
	Name string

	// Host is the docker host of a remote swarm. Empty if we're re-using
	// lazydocker's main docker connection.
	Host string

	isManager atomic.Bool
	lastError atomic.Value // string
}

// SwarmState is a snapshot of all the swarm resources we display
type SwarmState struct {
	Services []*SwarmService
	Tasks    []*SwarmTask
	Nodes    []*SwarmNode
	Volumes  []*SwarmVolume
	Networks []*SwarmNetwork
	Secrets  []*SwarmSecret
	Configs  []*SwarmConfig
	Stacks   []*SwarmStack
}

// NewSwarmCommands returns a SwarmCommand for the default docker connection,
// followed by one per configured swarm (plus one for the --swarm-host flag if
// it isn't a configured swarm), and the swarm to start in (nil for local).
// Connecting happens lazily on the first request, so an unreachable swarm
// host does not block startup.
func NewSwarmCommands(osCommand *OSCommand, tr *i18n.TranslationSet, appConfig *config.AppConfig, defaultClient *client.Client) ([]*SwarmCommand, *SwarmCommand, error) {
	newCommand := func(name string, host string) *SwarmCommand {
		cmd := &SwarmCommand{OSCommand: osCommand, Tr: tr, Config: appConfig, Client: defaultClient, Name: name, Host: host}
		cmd.lastError.Store("")
		return cmd
	}

	swarms := []*SwarmCommand{newCommand(tr.SwarmTitle, "")}
	for _, swarmHost := range appConfig.UserConfig.Swarm.Hosts {
		host := strings.TrimSpace(swarmHost.Host)
		if host == "" {
			return nil, nil, fmt.Errorf("swarm %q in swarm.hosts has no host", swarmHost.Name)
		}
		name := cmp.Or(strings.TrimSpace(swarmHost.Name), host)
		swarms = append(swarms, newCommand(name, host))
	}

	var initial *SwarmCommand
	if flag := strings.TrimSpace(appConfig.SwarmHost); flag != "" {
		var ok bool
		initial, ok = lo.Find(swarms[1:], func(c *SwarmCommand) bool { return c.Name == flag || c.Host == flag })
		if !ok {
			initial = newCommand(flag, flag)
			swarms = append(swarms, initial)
		}
	}

	for _, swarm := range swarms[1:] {
		if err := swarm.connectRemote(); err != nil {
			return nil, nil, err
		}
	}

	return swarms, initial, nil
}

// connectRemote creates the client for a remote swarm
func (c *SwarmCommand) connectRemote() error {
	// for ssh:// hosts this tunnels through `ssh <host> docker system
	// dial-stdio`, like the docker CLI does. BatchMode stops ssh from prompting
	// for a password, which would mess up the TUI.
	helper, err := connhelper.GetConnectionHelperWithSSHOpts(c.Host, []string{"-o", "BatchMode=yes"})
	if err != nil {
		return err
	}
	if helper == nil {
		c.Client, err = newDockerClient(c.Host)
	} else {
		c.Client, err = client.NewClientWithOpts(
			client.WithHTTPClient(&http.Client{Transport: &http.Transport{DialContext: helper.Dialer}}),
			client.WithHost(helper.Host),
			client.WithDialContext(helper.Dialer),
			client.WithAPIVersionNegotiation(),
		)
	}
	if err != nil {
		return fmt.Errorf("could not create docker client for swarm host %s: %w", c.Host, err)
	}
	return nil
}

// IsRemote reports whether swarm management uses its own connection
func (c *SwarmCommand) IsRemote() bool {
	return c.Host != ""
}

// Enabled reports whether the swarm gets a tab: either we're talking to a
// swarm manager, or it's a remote swarm (which gets a tab even if
// unreachable, so the error is visible).
func (c *SwarmCommand) Enabled() bool {
	return c.IsRemote() || c.isManager.Load()
}

// LastError returns the error from the most recent refresh, if any
func (c *SwarmCommand) LastError() string {
	return c.lastError.Load().(string)
}

// Refresh fetches all swarm resources. If the daemon is not a swarm manager it
// returns an empty state.
func (c *SwarmCommand) Refresh() (*SwarmState, error) {
	state, err := c.refresh()
	if err != nil {
		c.lastError.Store(err.Error())
		return &SwarmState{}, err
	}
	c.lastError.Store("")
	return state, nil
}

// UpdateIsManager checks whether the docker host is a swarm manager, without
// fetching any swarm resources
func (c *SwarmCommand) UpdateIsManager() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = c.updateIsManager(ctx)
}

func (c *SwarmCommand) updateIsManager(ctx context.Context) error {
	info, err := c.Client.Info(ctx)
	if err != nil {
		return err
	}
	c.isManager.Store(info.Swarm.ControlAvailable)
	return nil
}

func (c *SwarmCommand) refresh() (*SwarmState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.updateIsManager(ctx); err != nil {
		return nil, err
	}
	if !c.isManager.Load() {
		if c.IsRemote() {
			return nil, fmt.Errorf(c.Tr.SwarmNotAManager, c.Host)
		}
		return &SwarmState{}, nil
	}

	services, err := c.Client.ServiceList(ctx, swarm.ServiceListOptions{Status: true})
	if err != nil {
		return nil, err
	}
	tasks, err := c.Client.TaskList(ctx, swarm.TaskListOptions{})
	if err != nil {
		return nil, err
	}
	nodes, err := c.Client.NodeList(ctx, swarm.NodeListOptions{})
	if err != nil {
		return nil, err
	}
	volumes, err := c.Client.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, err
	}
	networks, err := c.Client.NetworkList(ctx, network.ListOptions{Filters: filters.NewArgs(filters.Arg("scope", "swarm"))})
	if err != nil {
		return nil, err
	}
	secrets, err := c.Client.SecretList(ctx, swarm.SecretListOptions{})
	if err != nil {
		return nil, err
	}
	configs, err := c.Client.ConfigList(ctx, swarm.ConfigListOptions{})
	if err != nil {
		return nil, err
	}

	return c.buildState(services, tasks, nodes, volumes.Volumes, networks, secrets, configs), nil
}

func (c *SwarmCommand) buildState(services []swarm.Service, tasks []swarm.Task, nodes []swarm.Node, volumes []*volume.Volume, networks []network.Summary, secrets []swarm.Secret, configs []swarm.Config) *SwarmState {
	state := &SwarmState{}

	nodesByID := map[string]*SwarmNode{}
	for _, n := range nodes {
		node := &SwarmNode{ID: n.ID, Node: n, Swarm: c}
		nodesByID[n.ID] = node
		state.Nodes = append(state.Nodes, node)
	}

	servicesByID := map[string]*SwarmService{}
	stacksByName := map[string]*SwarmStack{}
	for _, s := range services {
		service := &SwarmService{ID: s.ID, Name: s.Spec.Name, Stack: s.Spec.Labels[StackNamespaceLabel], Service: s, Swarm: c}
		servicesByID[s.ID] = service
		state.Services = append(state.Services, service)

		if service.Stack != "" {
			stack, ok := stacksByName[service.Stack]
			if !ok {
				stack = &SwarmStack{Name: service.Stack}
				stacksByName[service.Stack] = stack
				state.Stacks = append(state.Stacks, stack)
			}
			stack.Services = append(stack.Services, service)
		}
	}
	for _, stack := range state.Stacks {
		sort.Slice(stack.Services, func(i, j int) bool { return stack.Services[i].Name < stack.Services[j].Name })
	}
	// like a compose project, the selected stack filters the services and
	// tasks panels; this entry shows all of them, including those not
	// deployed as part of a stack
	state.Stacks = append([]*SwarmStack{{Services: state.Services}}, state.Stacks...)

	for _, t := range tasks {
		task := &SwarmTask{ID: t.ID, Task: t}
		if service, ok := servicesByID[t.ServiceID]; ok {
			task.ServiceName = service.Name
			task.Stack = service.Stack
			service.Tasks = append(service.Tasks, task)
		}
		if node, ok := nodesByID[t.NodeID]; ok {
			task.NodeName = node.Hostname()
			node.Tasks = append(node.Tasks, task)
		}
		state.Tasks = append(state.Tasks, task)
	}

	// besides cluster (CSI) volumes, which are managed by the swarm, we show the
	// named volumes the services mount. Those live on the nodes running the
	// tasks, so the manager can't list or remove them.
	volumesByName := map[string]*SwarmVolume{}
	for _, v := range volumes {
		if v.ClusterVolume != nil {
			vol := &SwarmVolume{Name: v.Name, Driver: v.Driver, Stack: v.Labels[StackNamespaceLabel], Volume: v}
			volumesByName[v.Name] = vol
			state.Volumes = append(state.Volumes, vol)
		}
	}
	for _, service := range state.Services {
		spec := service.Service.Spec.TaskTemplate.ContainerSpec
		if spec == nil {
			continue
		}
		for _, m := range spec.Mounts {
			if (m.Type != mount.TypeVolume && m.Type != mount.TypeCluster) || m.Source == "" {
				continue
			}
			vol, ok := volumesByName[m.Source]
			if !ok {
				vol = &SwarmVolume{Name: m.Source, Driver: "local", Stack: service.Stack}
				if m.VolumeOptions != nil && m.VolumeOptions.DriverConfig != nil && m.VolumeOptions.DriverConfig.Name != "" {
					vol.Driver = m.VolumeOptions.DriverConfig.Name
				}
				volumesByName[m.Source] = vol
				state.Volumes = append(state.Volumes, vol)
			}
			vol.Mounts = append(vol.Mounts, m)
			if !lo.Contains(vol.UsedBy, service) {
				vol.UsedBy = append(vol.UsedBy, service)
			}
		}
	}

	for _, n := range networks {
		swarmNetwork := &SwarmNetwork{ID: n.ID, Name: n.Name, Stack: n.Labels[StackNamespaceLabel], Network: n, Swarm: c}
		for _, service := range state.Services {
			if service.UsesNetwork(swarmNetwork) {
				swarmNetwork.UsedBy = append(swarmNetwork.UsedBy, service)
			}
		}
		state.Networks = append(state.Networks, swarmNetwork)
	}

	for _, s := range secrets {
		secret := &SwarmSecret{ID: s.ID, Name: s.Spec.Name, Secret: s, Swarm: c}
		for _, service := range state.Services {
			if service.UsesSecret(s.ID) {
				secret.UsedBy = append(secret.UsedBy, service)
			}
		}
		state.Secrets = append(state.Secrets, secret)
	}

	for _, cf := range configs {
		cfg := &SwarmConfig{ID: cf.ID, Name: cf.Spec.Name, Config: cf, Swarm: c}
		for _, service := range state.Services {
			if service.UsesConfig(cf.ID) {
				cfg.UsedBy = append(cfg.UsedBy, service)
			}
		}
		state.Configs = append(state.Configs, cfg)
	}

	return state
}

// CreateSecret creates a swarm secret. Never include data in errors.
func (c *SwarmCommand) CreateSecret(name string, data []byte) error {
	_, err := c.Client.SecretCreate(context.Background(), swarm.SecretSpec{Annotations: swarm.Annotations{Name: name}, Data: data})
	return err
}

// CreateNetwork creates an overlay network that services (and, being
// attachable, standalone containers) can use
func (c *SwarmCommand) CreateNetwork(name string) error {
	_, err := c.Client.NetworkCreate(context.Background(), name, network.CreateOptions{Driver: "overlay", Scope: "swarm", Attachable: true})
	return err
}

// CreateConfig creates a swarm config
func (c *SwarmCommand) CreateConfig(name string, data []byte) error {
	_, err := c.Client.ConfigCreate(context.Background(), swarm.ConfigSpec{Annotations: swarm.Annotations{Name: name}, Data: data})
	return err
}

// NewCmd builds a docker CLI command targeting the swarm we're managing. We
// use the CLI only where the API isn't enough: stack deploy/rm (compose file
// parsing) and following logs full-screen.
func (c *SwarmCommand) NewCmd(args ...string) *exec.Cmd {
	cmd := c.OSCommand.NewCmd("docker", args...)
	if c.IsRemote() {
		cmd.Env = append(cmd.Env, "DOCKER_HOST="+c.Host)
	}
	return cmd
}

// Logs streams the logs of a service or, if task is true, of a single
// task. The caller must close the reader.
func (c *SwarmCommand) Logs(ctx context.Context, id string, task bool) (io.ReadCloser, error) {
	logsConfig := c.Config.UserConfig.Logs
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: logsConfig.Timestamps,
		Since:      logsConfig.Since,
		Tail:       logsConfig.Tail,
		// tells us which task/node each line came from, so we can prefix it
		Details: true,
	}
	if task {
		return c.Client.TaskLogs(ctx, id, options)
	}
	return c.Client.ServiceLogs(ctx, id, options)
}
