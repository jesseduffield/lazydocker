package entities

import (
	"net"

	entitiesTypes "github.com/containers/podman/v5/pkg/domain/entities/types"
	"go.podman.io/image/v5/types"
)

// PlayKubeOptions controls playing kube YAML files.
type PlayKubeOptions struct {
	// Annotations - Annotations to add to Pods
	Annotations map[string]string
	// Authfile - path to an authentication file.
	Authfile string
	// Indicator to build all images with Containerfile or Dockerfile
	Build types.OptionalBool
	// CertDir - to a directory containing TLS certifications and keys.
	CertDir string
	// ContextDir - directory containing image contexts used for Build
	ContextDir string
	// Down indicates whether to bring contents of a yaml file "down"
	// as in stop
	Down bool
	// ExitCodePropagation decides how the main PID of the Kube service
	// should exit depending on the containers' exit codes.
	ExitCodePropagation string
	// Replace indicates whether to delete and recreate a yaml file
	Replace bool
	// Do not create /etc/hostname within the pod's containers,
	// instead use the version from the image
	NoHostname bool
	// Do not create /etc/hosts within the pod's containers,
	// instead use the version from the image
	NoHosts bool
	// Username for authenticating against the registry.
	Username string
	// Password for authenticating against the registry.
	Password string
	// Networks - name of the network to connect to.
	Networks []string
	// Quiet - suppress output when pulling images.
	Quiet bool
	// SignaturePolicy - path to a signature-policy file.
	SignaturePolicy string
	// SkipTLSVerify - skip https and certificate validation when
	// contacting container registries.
	SkipTLSVerify types.OptionalBool
	// SeccompProfileRoot - path to a directory containing seccomp
	// profiles.
	SeccompProfileRoot string
	// StaticIPs - Static IP address used by the pod(s).
	StaticIPs []net.IP
	// StaticMACs - Static MAC address used by the pod(s).
	StaticMACs []net.HardwareAddr
	// ConfigMaps - slice of pathnames to kubernetes configmap YAMLs.
	ConfigMaps []string
	// LogDriver for the container. For example: journald
	LogDriver string
	// LogOptions for the log driver for the container.
	LogOptions []string
	// Start - don't start the pod if false
	Start types.OptionalBool
	// ServiceContainer - creates a service container that is started before and is stopped after all pods.
	ServiceContainer bool
	// UseLongAnnotations - use annotations that were not truncated to the
	// Kubernetes maximum length of 63 characters
	UseLongAnnotations bool
	// Userns - define the user namespace to use.
	Userns string
	// IsRemote - was the request triggered by running podman-remote
	IsRemote bool
	// Force - remove volumes on --down
	Force bool
	// PublishPorts - configure how to expose ports configured inside the K8S YAML file
	PublishPorts []string
	// PublishAllPorts - whether to publish all ports defined in the K8S YAML file
	// (containerPort, hostPort) otherwise only hostPort will be published
	PublishAllPorts bool
	// Wait - indicates whether to return after having created the pods
	Wait bool
	// SystemContext - used when building the image
	SystemContext *types.SystemContext
	// Do not prefix container name with pod name
	NoPodPrefix bool
}

// PlayKubePod represents a single pod and associated containers created by play kube
type PlayKubePod = entitiesTypes.PlayKubePod

// PlayKubeVolume represents a single volume created by play kube.
type PlayKubeVolume entitiesTypes.PlayKubeVolume

// PlayKubeReport contains the results of running play kube.
type PlayKubeReport = entitiesTypes.PlayKubeReport
type KubePlayReport = entitiesTypes.KubePlayReport

// PlayKubeDownOptions are options for tearing down pods
type PlayKubeDownOptions struct {
	// Force - remove volumes if passed
	Force bool
}

// PlayKubeDownReport contains the results of tearing down play kube
type PlayKubeTeardown = entitiesTypes.PlayKubeTeardown

type PlaySecret = entitiesTypes.PlaySecret
