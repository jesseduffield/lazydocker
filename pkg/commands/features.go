package commands

// Feature represents a runtime capability that may or may not be supported
// by a given container runtime implementation.
type Feature int

const (
	// Image features
	FeatureImageHistory Feature = iota
	FeatureImageRemove
	FeatureImagePrune

	// Container features
	FeatureContainerAttach
	FeatureContainerExec
	FeatureContainerTop
	FeatureContainerPrune

	// Volume/Network
	FeatureVolumePrune
	FeatureNetworkPrune

	// Services / compose
	FeatureServices

	// Telemetry/streaming
	FeatureEventsStream
	FeatureStats

	// Newer Apple CLI capabilities
	FeatureVolumeCreate
	FeatureBuildPlatform
	FeatureRunPlatform
	FeatureSSHAgentForward
)
