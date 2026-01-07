package entities

import (
	"github.com/containers/podman/v5/pkg/domain/entities/types"
)

// GenerateSystemdOptions control the generation of systemd unit files.
type GenerateSystemdOptions struct {
	Name                   bool
	New                    bool
	RestartPolicy          *string
	RestartSec             *uint
	StartTimeout           *uint
	StopTimeout            *uint
	ContainerPrefix        string
	PodPrefix              string
	Separator              string
	NoHeader               bool
	TemplateUnitFile       bool
	Wants                  []string
	After                  []string
	Requires               []string
	AdditionalEnvVariables []string
}

// GenerateSystemdReport
type GenerateSystemdReport = types.GenerateSystemdReport

// GenerateKubeOptions control the generation of Kubernetes YAML files.
type GenerateKubeOptions struct {
	// PodmanOnly - add podman-only reserved annotations in the generated YAML file (Cannot be used by Kubernetes)
	PodmanOnly bool
	// Service - generate YAML for a Kubernetes _service_ object.
	Service bool
	// Type - the k8s kind to be generated i.e Pod or Deployment
	Type string
	// Replicas - the value to set in the replicas field for a Deployment
	Replicas int32
	// UseLongAnnotations - don't truncate annotations to the Kubernetes maximum length of 63 characters
	UseLongAnnotations bool
}

type KubeGenerateOptions = GenerateKubeOptions

// GenerateKubeReport
type GenerateKubeReport = types.GenerateKubeReport

type GenerateSpecReport = types.GenerateSpecReport

type GenerateSpecOptions struct {
	ID       string
	FileName string
	Compact  bool
	Name     bool
}
