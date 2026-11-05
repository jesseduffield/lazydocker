package commands

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	cliconfig "github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/connhelper"
	ddocker "github.com/docker/cli/cli/context/docker"
	ctxstore "github.com/docker/cli/cli/context/store"
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
// lazydocker uses, or a remote one from a docker context or the --swarm-host
// flag.
type SwarmCommand struct {
	OSCommand *OSCommand
	Tr        *i18n.TranslationSet
	Config    *config.AppConfig
	Client    *client.Client

	// Name is shown in the swarm's tab
	Name string

	// Context is the docker context of a remote swarm, if it comes from one
	Context string

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

// NewSwarmCommands returns the swarms that can get a tab, and the swarm to
// start in (nil for local):
//   - the swarm of lazydocker's main docker connection (to defaultHost)
//   - one per docker context, except the context of the main connection
//   - one for the --swarm-host flag (an ssh host), unless a docker context has
//     the same host
//
// Connecting happens lazily on the first request, so an unreachable swarm host
// does not block startup.
func NewSwarmCommands(osCommand *OSCommand, tr *i18n.TranslationSet, appConfig *config.AppConfig, defaultClient *client.Client, defaultHost string) ([]*SwarmCommand, *SwarmCommand, error) {
	return newSwarmCommands(osCommand, tr, appConfig, defaultClient, defaultHost, newContextStore(cliconfig.ContextStoreDir()))
}

func newSwarmCommands(osCommand *OSCommand, tr *i18n.TranslationSet, appConfig *config.AppConfig, defaultClient *client.Client, defaultHost string, contextStore *ctxstore.ContextStore) ([]*SwarmCommand, *SwarmCommand, error) {
	newCommand := func(name string, host string) *SwarmCommand {
		return &SwarmCommand{OSCommand: osCommand, Tr: tr, Config: appConfig, Name: name, Host: host}
	}
	swarmHostFlag := strings.TrimSpace(appConfig.SwarmHost)

	local := newCommand(tr.SwarmTitle, "")
	local.Client = defaultClient
	swarms := []*SwarmCommand{local}

	dockerContexts, err := contextStore.List()
	if err != nil {
		return nil, nil, fmt.Errorf("could not list docker contexts: %w", err)
	}
	for _, dockerContext := range dockerContexts {
		endpoint, err := ddocker.EndpointFromContext(dockerContext)
		if err != nil {
			// not a docker engine context
			continue
		}
		// like the docker CLI, an empty host means the default one
		endpoint.Host = cmp.Or(endpoint.Host, defaultDockerHost)

		// the main connection's swarm is the local one
		if endpoint.Host == defaultHost {
			continue
		}

		swarm := newCommand(dockerContext.Name, endpoint.Host)
		swarm.Context = dockerContext.Name
		swarm.Client, err = newContextClient(contextStore, dockerContext.Name, endpoint)
		if err != nil {
			return nil, nil, fmt.Errorf("could not create docker client for docker context %s: %w", dockerContext.Name, err)
		}
		swarms = append(swarms, swarm)
	}

	if swarmHostFlag == "" {
		return swarms, nil, nil
	}
	if !isSSHHost(swarmHostFlag) {
		return nil, nil, fmt.Errorf("--swarm-host %s is not an ssh host, e.g. ssh://user@manager", swarmHostFlag)
	}

	// if a docker context has the same host, we use its swarm rather than
	// adding the same swarm twice
	initial, found := lo.Find(swarms[1:], func(swarm *SwarmCommand) bool {
		return swarm.Host == swarmHostFlag
	})
	if !found {
		initial = newCommand(swarmHostFlag, swarmHostFlag)
		initial.Client, err = newSSHClient(swarmHostFlag)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid --swarm-host %s: %w", swarmHostFlag, err)
		}
		swarms = append(swarms, initial)
	}

	return swarms, initial, nil
}

// newContextClient creates the client for a docker context's endpoint. Unlike
// ssh hosts, tcp hosts use the context's TLS config.
func newContextClient(contextStore *ctxstore.ContextStore, contextName string, endpointMeta ddocker.EndpointMeta) (*client.Client, error) {
	if isSSHHost(endpointMeta.Host) {
		return newSSHClient(endpointMeta.Host)
	}

	endpoint, err := ddocker.WithTLSData(contextStore, contextName, endpointMeta)
	if err != nil {
		return nil, err
	}
	clientOpts, err := endpoint.ClientOpts()
	if err != nil {
		return nil, err
	}
	return client.NewClientWithOpts(clientOpts...)
}

func isSSHHost(host string) bool {
	return strings.HasPrefix(host, "ssh://")
}

// newSSHClient creates the client for an ssh:// docker host. Like the docker
// CLI does, it tunnels through `ssh <host> docker system dial-stdio`. BatchMode
// stops ssh from prompting for a password, which would mess up the TUI.
func newSSHClient(host string) (*client.Client, error) {
	helper, err := connhelper.GetConnectionHelperWithSSHOpts(host, []string{"-o", "BatchMode=yes"})
	if err != nil {
		return nil, err
	}
	return client.NewClientWithOpts(
		client.WithHTTPClient(&http.Client{Transport: &http.Transport{DialContext: helper.Dialer}}),
		client.WithHost(helper.Host),
		client.WithDialContext(helper.Dialer),
		client.WithAPIVersionNegotiation(),
	)
}

// Close closes the connection to a remote swarm. The local swarm shares
// lazydocker's main docker client, which isn't ours to close.
func (c *SwarmCommand) Close() error {
	if !c.IsRemote() {
		return nil
	}
	return c.Client.Close()
}

// IsRemote reports whether swarm management uses its own connection
func (c *SwarmCommand) IsRemote() bool {
	return c.Host != ""
}

// Enabled reports whether the swarm gets a tab: either it's a remote swarm, or
// we're talking to a swarm manager. Remote swarms get a tab right away, before
// we have connected: if they turn out to be unreachable or not a swarm
// manager, their panels show the error.
func (c *SwarmCommand) Enabled() bool {
	return c.IsRemote() || c.isManager.Load()
}

// LastError returns the error from the most recent refresh, if any
func (c *SwarmCommand) LastError() string {
	msg, _ := c.lastError.Load().(string)
	return msg
}

// Refresh fetches all swarm resources, and remembers whether that failed for
// LastError. The local docker host not being a swarm manager isn't a failure:
// the state is just empty then.
func (c *SwarmCommand) Refresh() (*SwarmState, error) {
	state, err := c.refresh()
	if err != nil {
		c.lastError.Store(err.Error())
		return nil, err
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
	// the Swarm header of /_ping spares us the much slower /info, but engines
	// before 23.0 (API 1.42) don't send it
	ping, err := c.Client.Ping(ctx)
	if err != nil {
		return err
	}
	if ping.SwarmStatus != nil {
		c.isManager.Store(ping.SwarmStatus.ControlAvailable)
		return nil
	}
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

func (c *SwarmCommand) buildState(
	services []swarm.Service,
	tasks []swarm.Task,
	nodes []swarm.Node,
	volumes []*volume.Volume,
	networks []network.Summary,
	secrets []swarm.Secret,
	configs []swarm.Config,
) *SwarmState {
	state := &SwarmState{}

	nodesByID := map[string]*SwarmNode{}
	for _, n := range nodes {
		node := &SwarmNode{ID: n.ID, Node: n, Swarm: c}
		nodesByID[n.ID] = node
		state.Nodes = append(state.Nodes, node)
	}

	// sorted by name, and so is every list of services built from them (a
	// stack's, or those using a volume, network, secret or config)
	sort.Slice(services, func(i, j int) bool { return services[i].Spec.Name < services[j].Spec.Name })
	servicesByID := map[string]*SwarmService{}
	stacksByName := map[string]*SwarmStack{}
	for _, s := range services {
		service := &SwarmService{ID: s.ID, Name: s.Spec.Name, Stack: s.Spec.Labels[StackNamespaceLabel], Service: s, Swarm: c}
		servicesByID[s.ID] = service
		state.Services = append(state.Services, service)

		if service.Stack != "" {
			stack, ok := stacksByName[service.Stack]
			if !ok {
				stack = &SwarmStack{Name: service.Stack, Swarm: c}
				stacksByName[service.Stack] = stack
				state.Stacks = append(state.Stacks, stack)
			}
			stack.Services = append(stack.Services, service)
		}
	}
	// like a compose project, the selected stack filters the services, tasks,
	// volumes and networks panels; this entry shows all of them, including
	// those not deployed as part of a stack
	state.Stacks = append([]*SwarmStack{{Services: state.Services, Swarm: c}}, state.Stacks...)

	for _, t := range tasks {
		task := &SwarmTask{ID: t.ID, Task: t, Swarm: c}
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
		swarmNetwork.UsedBy = lo.Filter(state.Services, func(service *SwarmService, _ int) bool { return service.UsesNetwork(swarmNetwork) })
		state.Networks = append(state.Networks, swarmNetwork)
	}

	for _, s := range secrets {
		secret := &SwarmSecret{ID: s.ID, Name: s.Spec.Name, Secret: s, Swarm: c}
		secret.UsedBy = lo.Filter(state.Services, func(service *SwarmService, _ int) bool { return service.UsesSecret(s.ID) })
		state.Secrets = append(state.Secrets, secret)
	}

	for _, cf := range configs {
		cfg := &SwarmConfig{ID: cf.ID, Name: cf.Spec.Name, Config: cf, Swarm: c}
		cfg.UsedBy = lo.Filter(state.Services, func(service *SwarmService, _ int) bool { return service.UsesConfig(cf.ID) })
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
// use the CLI only where the API isn't enough: stack deploy (compose file
// parsing) and rm.
func (c *SwarmCommand) NewCmd(args ...string) *exec.Cmd {
	// both flags take precedence over DOCKER_HOST, which lazydocker may have
	// set for its own connection
	if c.Context != "" {
		// the context also brings its TLS config
		args = append([]string{"--context", c.Context}, args...)
	} else if c.IsRemote() {
		args = append([]string{"--host", c.Host}, args...)
	}
	return c.OSCommand.NewCmd("docker", args...)
}

// logsOptions returns the options for following a service's or task's logs
func (c *SwarmCommand) logsOptions() container.LogsOptions {
	logsConfig := c.Config.UserConfig.Logs
	return container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: logsConfig.Timestamps,
		Since:      logsConfig.Since,
		Tail:       logsConfig.Tail,
		// tells us which task/node each line came from, so we can prefix it
		Details: true,
	}
}
