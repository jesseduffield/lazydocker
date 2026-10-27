package commands

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/samber/lo"
)

// SwarmService : a swarm service
type SwarmService struct {
	ID      string
	Name    string
	Stack   string
	Service swarm.Service
	Tasks   []*SwarmTask
	Swarm   *SwarmCommand
}

// Replicas returns the running/desired task counts (the REPLICAS column of
// `docker service ls`)
func (s *SwarmService) Replicas() (uint64, uint64) {
	if s.Service.ServiceStatus == nil {
		return 0, 0
	}
	return s.Service.ServiceStatus.RunningTasks, s.Service.ServiceStatus.DesiredTasks
}

// Mode returns the service's mode, e.g. replicated or global
func (s *SwarmService) Mode() string {
	mode := s.Service.Spec.Mode
	switch {
	case mode.Global != nil:
		return "global"
	case mode.ReplicatedJob != nil:
		return "replicated-job"
	case mode.GlobalJob != nil:
		return "global-job"
	default:
		return "replicated"
	}
}

// Image returns the service's image without the digest suffix
func (s *SwarmService) Image() string {
	if s.Service.Spec.TaskTemplate.ContainerSpec == nil {
		return ""
	}
	image, _, _ := strings.Cut(s.Service.Spec.TaskTemplate.ContainerSpec.Image, "@sha256:")
	return image
}

// IsTTY reports whether the service's containers have a TTY, which affects how
// their logs are encoded
func (s *SwarmService) IsTTY() bool {
	return s.Service.Spec.TaskTemplate.ContainerSpec != nil && s.Service.Spec.TaskTemplate.ContainerSpec.TTY
}

// IsUpdating reports whether a rolling update/rollback is in progress
func (s *SwarmService) IsUpdating() bool {
	status := s.Service.UpdateStatus
	return status != nil && (status.State == swarm.UpdateStateUpdating || status.State == swarm.UpdateStateRollbackStarted)
}

// UsesSecret reports whether the service references the given secret
func (s *SwarmService) UsesSecret(secretID string) bool {
	spec := s.Service.Spec.TaskTemplate.ContainerSpec
	return spec != nil && lo.SomeBy(spec.Secrets, func(ref *swarm.SecretReference) bool { return ref.SecretID == secretID })
}

// UsesConfig reports whether the service references the given config
func (s *SwarmService) UsesConfig(configID string) bool {
	spec := s.Service.Spec.TaskTemplate.ContainerSpec
	return spec != nil && lo.SomeBy(spec.Configs, func(ref *swarm.ConfigReference) bool { return ref.ConfigID == configID })
}

// UsesNetwork reports whether the service is attached to the given network
func (s *SwarmService) UsesNetwork(n *SwarmNetwork) bool {
	return lo.SomeBy(s.Service.Spec.TaskTemplate.Networks, n.isTarget)
}

// update fetches the latest version of the service, applies mutate to its spec
// and sends the update
func (s *SwarmService) update(mutate func(spec *swarm.ServiceSpec) error, options swarm.ServiceUpdateOptions) error {
	ctx := context.Background()
	service, _, err := s.Swarm.Client.ServiceInspectWithRaw(ctx, s.ID, swarm.ServiceInspectOptions{})
	if err != nil {
		return err
	}

	spec := service.Spec
	if err := mutate(&spec); err != nil {
		return err
	}

	_, err = s.Swarm.Client.ServiceUpdate(ctx, s.ID, service.Version, spec, options)
	return err
}

// updateReplicas sets the replica count of a replicated service to whatever
// getReplicas returns, given the current count
func (s *SwarmService) updateReplicas(getReplicas func(spec *swarm.ServiceSpec, current uint64) uint64) error {
	return s.update(func(spec *swarm.ServiceSpec) error {
		if spec.Mode.Replicated == nil {
			return fmt.Errorf(s.Swarm.Tr.SwarmCannotScaleNonReplicated, s.Name, s.Mode())
		}
		current := uint64(0)
		if spec.Mode.Replicated.Replicas != nil {
			current = *spec.Mode.Replicated.Replicas
		}
		replicas := getReplicas(spec, current)
		spec.Mode.Replicated.Replicas = &replicas
		if replicas > 0 {
			delete(spec.Labels, PreviousReplicasLabel)
		}
		return nil
	}, swarm.ServiceUpdateOptions{})
}

// Scale sets the number of replicas
func (s *SwarmService) Scale(replicas uint64) error {
	return s.updateReplicas(func(*swarm.ServiceSpec, uint64) uint64 { return replicas })
}

// Stop scales the service to 0, remembering the current replica count in a
// label so that Start can restore it
func (s *SwarmService) Stop() error {
	return s.updateReplicas(func(spec *swarm.ServiceSpec, current uint64) uint64 {
		if current > 0 {
			if spec.Labels == nil {
				spec.Labels = map[string]string{}
			}
			spec.Labels[PreviousReplicasLabel] = strconv.FormatUint(current, 10)
		}
		return 0
	})
}

// Start scales a stopped service back to the replica count it had before
// being stopped (or 1 if we don't know)
func (s *SwarmService) Start() error {
	return s.updateReplicas(func(spec *swarm.ServiceSpec, current uint64) uint64 {
		if current > 0 {
			return current
		}
		if previous, err := strconv.ParseUint(spec.Labels[PreviousReplicasLabel], 10, 64); err == nil && previous > 0 {
			return previous
		}
		return 1
	})
}

// ForceUpdate restarts all of the service's tasks (rolling, respecting the
// service's update config), like `docker service update --force`
func (s *SwarmService) ForceUpdate() error {
	return s.update(func(spec *swarm.ServiceSpec) error {
		spec.TaskTemplate.ForceUpdate++
		return nil
	}, swarm.ServiceUpdateOptions{})
}

// Rollback reverts the service to its previous spec
func (s *SwarmService) Rollback() error {
	return s.update(func(*swarm.ServiceSpec) error { return nil }, swarm.ServiceUpdateOptions{Rollback: "previous"})
}

// Remove removes the service
func (s *SwarmService) Remove() error {
	return s.Swarm.Client.ServiceRemove(context.Background(), s.ID)
}

// updateContainerSpec is like update, for changes to the container spec
func (s *SwarmService) updateContainerSpec(mutate func(spec *swarm.ContainerSpec)) error {
	return s.update(func(spec *swarm.ServiceSpec) error {
		if spec.TaskTemplate.ContainerSpec == nil {
			return errors.New("service has no container spec")
		}
		mutate(spec.TaskTemplate.ContainerSpec)
		return nil
	}, swarm.ServiceUpdateOptions{})
}

// UpdateImage sets a new image for the service, triggering a rolling update
func (s *SwarmService) UpdateImage(image string) error {
	return s.updateContainerSpec(func(spec *swarm.ContainerSpec) { spec.Image = image })
}

// AttachSecret mounts a secret into the service's containers at
// /run/secrets/<target>
func (s *SwarmService) AttachSecret(secret *SwarmSecret, target string) error {
	return s.updateContainerSpec(func(spec *swarm.ContainerSpec) {
		spec.Secrets = append(spec.Secrets, &swarm.SecretReference{
			SecretID:   secret.ID,
			SecretName: secret.Name,
			File:       &swarm.SecretReferenceFileTarget{Name: target, UID: "0", GID: "0", Mode: 0o444},
		})
	})
}

// DetachSecret removes a secret from the service
func (s *SwarmService) DetachSecret(secret *SwarmSecret) error {
	return s.updateContainerSpec(func(spec *swarm.ContainerSpec) {
		spec.Secrets = lo.Reject(spec.Secrets, func(ref *swarm.SecretReference, _ int) bool { return ref.SecretID == secret.ID })
	})
}

// AttachConfig mounts a config into the service's containers at target
func (s *SwarmService) AttachConfig(cfg *SwarmConfig, target string) error {
	return s.updateContainerSpec(func(spec *swarm.ContainerSpec) {
		spec.Configs = append(spec.Configs, &swarm.ConfigReference{
			ConfigID:   cfg.ID,
			ConfigName: cfg.Name,
			File:       &swarm.ConfigReferenceFileTarget{Name: target, UID: "0", GID: "0", Mode: 0o444},
		})
	})
}

// DetachConfig removes a config from the service
func (s *SwarmService) DetachConfig(cfg *SwarmConfig) error {
	return s.updateContainerSpec(func(spec *swarm.ContainerSpec) {
		spec.Configs = lo.Reject(spec.Configs, func(ref *swarm.ConfigReference, _ int) bool { return ref.ConfigID == cfg.ID })
	})
}

// AttachNetwork connects the service to a network
func (s *SwarmService) AttachNetwork(n *SwarmNetwork) error {
	return s.update(func(spec *swarm.ServiceSpec) error {
		spec.TaskTemplate.Networks = append(spec.TaskTemplate.Networks, swarm.NetworkAttachmentConfig{Target: n.ID})
		return nil
	}, swarm.ServiceUpdateOptions{})
}

// DetachNetwork disconnects the service from a network
func (s *SwarmService) DetachNetwork(n *SwarmNetwork) error {
	return s.update(func(spec *swarm.ServiceSpec) error {
		spec.TaskTemplate.Networks = lo.Reject(spec.TaskTemplate.Networks, func(ref swarm.NetworkAttachmentConfig, _ int) bool { return n.isTarget(ref) })
		return nil
	}, swarm.ServiceUpdateOptions{})
}

// SwarmTask : a swarm task (i.e. one replica of a service)
type SwarmTask struct {
	ID          string
	Task        swarm.Task
	ServiceName string
	Stack       string
	NodeName    string
}

// Name returns the task's name the way docker displays it: <service>.<slot>
// for replicated services, <service>.<node id> for global ones
func (t *SwarmTask) Name() string {
	if t.Task.Slot != 0 {
		return fmt.Sprintf("%s.%d", t.ServiceName, t.Task.Slot)
	}
	return t.ServiceName + "." + t.Task.NodeID
}

// IsCurrent reports whether the task is part of the service's desired state,
// as opposed to history (shut down / failed tasks)
func (t *SwarmTask) IsCurrent() bool {
	return t.Task.DesiredState != swarm.TaskStateShutdown && t.Task.DesiredState != swarm.TaskStateRemove
}

// IsTTY reports whether the task's container has a TTY
func (t *SwarmTask) IsTTY() bool {
	return t.Task.Spec.ContainerSpec != nil && t.Task.Spec.ContainerSpec.TTY
}

// SwarmNode : a swarm node
type SwarmNode struct {
	ID    string
	Node  swarm.Node
	Tasks []*SwarmTask
	Swarm *SwarmCommand
}

// Hostname returns the node's hostname, falling back to its ID
func (n *SwarmNode) Hostname() string {
	if n.Node.Description.Hostname != "" {
		return n.Node.Description.Hostname
	}
	return n.ID
}

// Update fetches the latest version of the node, applies mutate to its spec
// (e.g. to change its availability or role) and sends the update
func (n *SwarmNode) Update(mutate func(spec *swarm.NodeSpec)) error {
	ctx := context.Background()
	node, _, err := n.Swarm.Client.NodeInspectWithRaw(ctx, n.ID)
	if err != nil {
		return err
	}
	spec := node.Spec
	mutate(&spec)
	return n.Swarm.Client.NodeUpdate(ctx, n.ID, node.Version, spec)
}

// Remove removes the node from the swarm
func (n *SwarmNode) Remove(force bool) error {
	return n.Swarm.Client.NodeRemove(context.Background(), n.ID, swarm.NodeRemoveOptions{Force: force})
}

// SwarmVolume : a cluster (CSI) volume, or a named volume mounted by services.
// The latter exists separately on each node running one of their tasks.
type SwarmVolume struct {
	Name   string
	Driver string
	Stack  string
	// nil unless it's a cluster volume
	Volume *volume.Volume
	// how the services mount it
	Mounts []mount.Mount
	UsedBy []*SwarmService
}

// SwarmNetwork : a swarm scoped network, e.g. an overlay network
type SwarmNetwork struct {
	ID      string
	Name    string
	Stack   string
	Network network.Summary
	UsedBy  []*SwarmService
	Swarm   *SwarmCommand
}

// services reference networks by ID, but a name works too
func (n *SwarmNetwork) isTarget(ref swarm.NetworkAttachmentConfig) bool {
	return ref.Target == n.ID || ref.Target == n.Name
}

// Remove removes the network
func (n *SwarmNetwork) Remove() error {
	return n.Swarm.Client.NetworkRemove(context.Background(), n.ID)
}

// SwarmSecret : a swarm secret. The docker API never returns a secret's data.
type SwarmSecret struct {
	ID     string
	Name   string
	Secret swarm.Secret
	UsedBy []*SwarmService
	Swarm  *SwarmCommand
}

// Remove removes the secret
func (s *SwarmSecret) Remove() error {
	return s.Swarm.Client.SecretRemove(context.Background(), s.ID)
}

// SwarmConfig : a swarm config
type SwarmConfig struct {
	ID     string
	Name   string
	Config swarm.Config
	UsedBy []*SwarmService
	Swarm  *SwarmCommand
}

// Remove removes the config
func (c *SwarmConfig) Remove() error {
	return c.Swarm.Client.ConfigRemove(context.Background(), c.ID)
}

// SwarmStack : the services deployed with `docker stack deploy` under one name
type SwarmStack struct {
	// empty for the entry that stands for all services
	Name     string
	Services []*SwarmService
}

// IsAll reports whether this is the entry standing for all services, rather
// than an actual stack
func (s *SwarmStack) IsAll() bool {
	return s.Name == ""
}

// Replicas returns the total running/desired replicas across the stack
func (s *SwarmStack) Replicas() (uint64, uint64) {
	var running, desired uint64
	for _, service := range s.Services {
		r, d := service.Replicas()
		running += r
		desired += d
	}
	return running, desired
}

// Stop scales every replicated service of the stack to 0
func (s *SwarmStack) Stop() error {
	return s.forEachReplicatedService((*SwarmService).Stop)
}

// Start restores every stopped replicated service of the stack
func (s *SwarmStack) Start() error {
	return s.forEachReplicatedService((*SwarmService).Start)
}

func (s *SwarmStack) forEachReplicatedService(f func(*SwarmService) error) error {
	errs := []error{}
	for _, service := range s.Services {
		if service.Service.Spec.Mode.Replicated != nil {
			errs = append(errs, f(service))
		}
	}
	return errors.Join(errs...)
}
