package commands

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/distribution/reference"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/sirupsen/logrus"
)

// AppleContainerCommand handles interactions with Apple's container CLI
type AppleContainerCommand struct {
	Log       *logrus.Entry
	OSCommand *OSCommand
	Tr        *i18n.TranslationSet
	Config    *config.AppConfig
	ErrorChan chan error

	features map[Feature]bool
}

// internal logging helpers to tolerate nil Log in tests
func (c *AppleContainerCommand) logInfo(args ...interface{}) {
	if c != nil && c.Log != nil {
		c.Log.Info(args...)
	}
}
func (c *AppleContainerCommand) logInfof(format string, args ...interface{}) {
	if c != nil && c.Log != nil {
		c.Log.Infof(format, args...)
	}
}
func (c *AppleContainerCommand) logError(args ...interface{}) {
	if c != nil && c.Log != nil {
		c.Log.Error(args...)
	}
}
func (c *AppleContainerCommand) logDebugf(format string, args ...interface{}) {
	if c != nil && c.Log != nil {
		c.Log.Debugf(format, args...)
	}
}

// NewAppleContainerCommand creates a new Apple Container command handler
func NewAppleContainerCommand(log *logrus.Entry, osCommand *OSCommand, tr *i18n.TranslationSet, config *config.AppConfig, errorChan chan error) (*AppleContainerCommand, error) {
	// Check if Apple Container CLI is available
	if !isAppleContainerAvailable() {
		return nil, fmt.Errorf("apple container CLI not found; ensure 'container' is on PATH")
	}

	cmd := &AppleContainerCommand{
		Log:       log,
		OSCommand: osCommand,
		Tr:        tr,
		Config:    config,
		ErrorChan: errorChan,
		features:  map[Feature]bool{},
	}

	cmd.detectCapabilities()
	return cmd, nil
}

// isAppleContainerAvailable checks if the Apple Container CLI is available
func isAppleContainerAvailable() bool {
	_, err := exec.LookPath("container")
	return err == nil
}

// Supports reports if a capability is supported by the Apple container CLI
func (c *AppleContainerCommand) Supports(f Feature) bool {
	if c == nil {
		return false
	}
	return c.features[f]
}

// detectCapabilities runs lightweight CLI introspection to populate features.
// It errs on the conservative side (feature=false) on any unexpected error.
func (c *AppleContainerCommand) detectCapabilities() {
	feats := map[Feature]bool{}

	help := func(args ...string) string {
		cmd := c.OSCommand.NewCmd("container", append(args, "--help")...)
		out, err := c.OSCommand.RunExecutableWithOutput(cmd)
		if err != nil {
			return ""
		}
		return out
	}

	// Top‑level commands
	root := help()
	contains := func(haystack, needle string) bool { return strings.Contains(haystack, needle) }

	feats[FeatureContainerExec] = contains(root, " exec ") || contains(root, "\nexec ")
	feats[FeatureContainerAttach] = contains(root, " attach ") || contains(root, "\nattach ")
	feats[FeatureContainerTop] = contains(root, " top ") || contains(root, "\ntop ")
	feats[FeatureEventsStream] = contains(root, " events ") || contains(root, "\nevents ")
	feats[FeatureStats] = contains(root, " stats ") || contains(root, "\nstats ")

	// Images namespace
	imagesHelp := help("images")
	feats[FeatureImageHistory] = contains(imagesHelp, " history ") || contains(imagesHelp, "\nhistory ")
	feats[FeatureImagePrune] = contains(imagesHelp, " prune ") || contains(imagesHelp, "\nprune ")
	feats[FeatureImageRemove] = contains(imagesHelp, " rm ") || contains(imagesHelp, " remove ")

	// Volume/Network prune
	volumeHelp := help("volume")
	feats[FeatureVolumePrune] = contains(volumeHelp, " prune ")
	feats[FeatureVolumeCreate] = contains(volumeHelp, " create ") || contains(volumeHelp, "\ncreate ")

	networkHelp := help("network")
	feats[FeatureNetworkPrune] = contains(networkHelp, " prune ")

	// Containers prune (if any)
	containersHelp := help("container")
	feats[FeatureContainerPrune] = contains(containersHelp, " prune ")

	// Services/compose not supported by Apple CLI
	feats[FeatureServices] = false

	// Build/run platform flags
	buildHelp := help("build")
	feats[FeatureBuildPlatform] = contains(buildHelp, " --platform ") || contains(buildHelp, " --os ") || contains(buildHelp, " --arch ")

	runHelp := help("run")
	feats[FeatureRunPlatform] = contains(runHelp, " --platform ") || contains(runHelp, " --os ") || contains(runHelp, " --arch ")

	// SSH agent forward for exec/run
	execHelp := help("exec")
	feats[FeatureSSHAgentForward] = contains(execHelp, " --ssh ") || contains(runHelp, " --ssh ")

	c.features = feats
}

// GetContainers retrieves all containers from Apple Container
func (c *AppleContainerCommand) GetContainers() ([]*Container, error) {
	c.logInfo("Getting containers from Apple Container")

	// Execute: container ls --format json
	cmd := c.OSCommand.NewCmd("container", "ls", "--format", "json")
	output, err := c.OSCommand.RunExecutableWithOutput(cmd)
	if err != nil {
		c.logError("Failed to get containers from Apple Container: ", err)
		return nil, fmt.Errorf("failed to get containers: %w", err)
	}

	c.logDebugf("Raw container ls output: %s", output)

	// Parse the JSON output
	containers, err := c.parseContainerList(output)
	if err != nil {
		c.logError("Failed to parse container list: ", err)
		return nil, fmt.Errorf("failed to parse container list: %w", err)
	}

	c.logInfof("Found %d containers", len(containers))
	return containers, nil
}

// parseContainerList parses the JSON output from Apple Container's ls command
func (c *AppleContainerCommand) parseContainerList(output string) ([]*Container, error) {
	if strings.TrimSpace(output) == "" {
		return []*Container{}, nil
	}

	// Apple Container outputs a JSON array of container objects
	var containerArray []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &containerArray); err != nil {
		c.logError("Failed to parse container JSON array: ", err)
		return nil, fmt.Errorf("failed to parse container JSON: %w", err)
	}

	containers := make([]*Container, 0, len(containerArray))
	for _, containerData := range containerArray {
		container := c.jsonToContainer(containerData)
		if container != nil {
			containers = append(containers, container)
		}
	}

	return containers, nil
}

// jsonToContainer converts JSON data to a Container struct
func (c *AppleContainerCommand) jsonToContainer(data map[string]interface{}) *Container {
	// Extract container configuration
	config, ok := data["configuration"].(map[string]interface{})
	if !ok {
		c.logError("Container missing configuration field")
		return nil
	}

	// Extract basic container information
	id, _ := config["id"].(string)
	if id == "" {
		c.logError("Container missing ID field")
		return nil
	}

	// Extract status
	status, _ := data["status"].(string)

	// Extract address if available (schema-tolerant)
	var addr string
	if s, ok := data["addr"].(string); ok {
		addr = s
	}
	if addr == "" {
		if m, ok := data["network"].(map[string]interface{}); ok {
			if s, ok := m["addr"].(string); ok {
				addr = s
			}
		}
	}
	if addr == "" {
		if s, ok := data["ip"].(string); ok {
			addr = s
		}
	}

	// Extract image information
	var imageName string
	if imageInfo, ok := config["image"].(map[string]interface{}); ok {
		imageName, _ = imageInfo["reference"].(string)
	}

	// Create container with Apple Container specific fields
	container := &Container{
		ID:        id,
		Name:      id, // Apple Container uses ID as the name
		OSCommand: c.OSCommand,
		Log:       c.Log,
		Tr:        c.Tr,
		// Note: Client is nil for Apple containers - we don't use Docker client
		Client: nil,
		// Set a reference to the Apple command for container operations
		DockerCommand: c,
		Addr:          addr,
	}

	// Set up container.Container with basic state information
	container.Container.State = status
	container.Container.Status = status

	// Map Apple Container states to Docker-like states for consistency
	switch status {
	case "stopped":
		container.Container.State = "exited"
	case "running":
		container.Container.State = "running"
	}

	// Set image name
	if imageName != "" {
		container.Container.Image = imageName
	}

	c.logDebugf("Parsed container: ID=%s, Name=%s, Image=%s, Status=%s", id, id, imageName, status)
	return container
}

// BuildImage builds a container image using Apple Container
func (c *AppleContainerCommand) BuildImage(tag, dockerfile string) error {
	c.logInfof("Building image with tag %s using dockerfile %s", tag, dockerfile)

	args := []string{"build", "--tag", tag, "--file", dockerfile, "."}
	if c.Config != nil && c.Config.UserConfig != nil && c.Config.UserConfig.Apple != nil && c.Supports(FeatureBuildPlatform) {
		if v := c.Config.UserConfig.Apple.BuildPlatform; v != "" {
			args = append([]string{"build", "--tag", tag, "--file", dockerfile}, "--platform", v, ".")
		} else {
			if v := c.Config.UserConfig.Apple.BuildOS; v != "" {
				args = append(args[:len(args)-1], "--os", v, ".")
			}
			if v := c.Config.UserConfig.Apple.BuildArch; v != "" {
				args = append(args[:len(args)-1], "--arch", v, ".")
			}
		}
	}
	execCmd := c.OSCommand.NewCmd("container", args...)
	return c.OSCommand.RunExecutable(execCmd)
}

// RunContainer runs a new container using Apple Container
func (c *AppleContainerCommand) RunContainer(name, image string, detached bool) error {
	c.logInfof("Running container %s from image %s", name, image)

	args := []string{"run", "--name", name}
	if detached {
		args = append(args, "--detach")
	}
	// platform flags
	if c.Config != nil && c.Config.UserConfig != nil && c.Config.UserConfig.Apple != nil && c.Supports(FeatureRunPlatform) {
		if v := c.Config.UserConfig.Apple.RunPlatform; v != "" {
			args = append(args, "--platform", v)
		} else {
			if v := c.Config.UserConfig.Apple.RunOS; v != "" {
				args = append(args, "--os", v)
			}
			if v := c.Config.UserConfig.Apple.RunArch; v != "" {
				args = append(args, "--arch", v)
			}
		}
		// ssh agent forward
		if c.Config.UserConfig.Apple.ForwardSSHAgent && c.Supports(FeatureSSHAgentForward) {
			args = append(args, "--ssh")
		}
	}
	args = append(args, image)
	execCmd := c.OSCommand.NewCmd("container", args...)
	return c.OSCommand.RunExecutable(execCmd)
}

// StopContainer stops a running container
func (c *AppleContainerCommand) StopContainer(nameOrID string) error {
	c.logInfof("Stopping container %s", nameOrID)

	execCmd := c.OSCommand.NewCmd("container", "stop", nameOrID)
	return c.OSCommand.RunExecutable(execCmd)
}

// StartContainer starts a stopped container
func (c *AppleContainerCommand) StartContainer(nameOrID string) error {
	c.logInfof("Starting container %s", nameOrID)

	execCmd := c.OSCommand.NewCmd("container", "start", nameOrID)
	return c.OSCommand.RunExecutable(execCmd)
}

// RemoveContainer removes a container
func (c *AppleContainerCommand) RemoveContainer(nameOrID string, force bool) error {
	c.logInfof("Removing container %s (force: %v)", nameOrID, force)

	args := []string{"rm"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, nameOrID)
	execCmd := c.OSCommand.NewCmd("container", args...)
	return c.OSCommand.RunExecutable(execCmd)
}

// ExecCommand executes a command in a running container
func (c *AppleContainerCommand) ExecCommand(nameOrID, command string) error {
	c.logInfof("Executing command in container %s: %s", nameOrID, command)

	// Keep string form to preserve exact command; quoting handled by ExecutableFromString
	cmdStr := fmt.Sprintf("container exec %s %s", nameOrID, command)
	execCmd := c.OSCommand.ExecutableFromString(cmdStr)
	return c.OSCommand.RunExecutable(execCmd)
}

// GetImages retrieves all images from Apple Container
func (c *AppleContainerCommand) GetImages() ([]*Image, error) {
	c.logInfo("Getting images from Apple Container")

	// Execute: container images list --format json
	cmd := c.OSCommand.NewCmd("container", "images", "list", "--format", "json")
	output, err := c.OSCommand.RunExecutableWithOutput(cmd)
	if err != nil {
		c.logError("Failed to get images from Apple Container: ", err)
		return nil, fmt.Errorf("failed to get images: %w", err)
	}

	// Parse the JSON output (implementation similar to containers)
	images, err := c.parseImageList(output)
	if err != nil {
		c.logError("Failed to parse image list: ", err)
		return nil, fmt.Errorf("failed to parse image list: %w", err)
	}

	c.logInfof("Found %d images", len(images))
	return images, nil
}

// parseImageList parses the JSON output from Apple Container's images list command
func (c *AppleContainerCommand) parseImageList(output string) ([]*Image, error) {
	if strings.TrimSpace(output) == "" {
		return []*Image{}, nil
	}

	// Apple Container outputs a JSON array of image objects
	var imageArray []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &imageArray); err != nil {
		c.logError("Failed to parse image JSON array: ", err)
		return nil, fmt.Errorf("failed to parse image JSON: %w", err)
	}

	images := make([]*Image, 0, len(imageArray))
	for _, imageData := range imageArray {
		image := c.jsonToImage(imageData)
		if image != nil {
			images = append(images, image)
		}
	}

	return images, nil
}

// jsonToImage converts JSON data to an Image struct
func (c *AppleContainerCommand) jsonToImage(data map[string]interface{}) *Image {
	// Extract reference (image name)
	refStr, _ := data["reference"].(string)
	if refStr == "" {
		c.logError("Image missing reference field")
		return nil
	}

	// Extract descriptor information
	var digest string
	var size float64
	if descriptor, ok := data["descriptor"].(map[string]interface{}); ok {
		digest, _ = descriptor["digest"].(string)
		size, _ = descriptor["size"].(float64)
	}

	// Parse reference robustly (handles registries with ports and digests)
	var repository, tag string
	if named, err := reference.ParseNormalizedNamed(refStr); err == nil {
		repository = reference.FamiliarName(named)
		if t, ok := named.(reference.Tagged); ok {
			tag = t.Tag()
		} else {
			tag = "latest"
		}
		if digest == "" {
			if d, ok := named.(reference.Digested); ok {
				digest = d.Digest().String()
			}
		}
	} else {
		// Fallback
		parts := strings.Split(refStr, ":")
		repository = parts[0]
		tag = "latest"
		if len(parts) > 1 {
			tag = parts[1]
		}
	}

	// Use digest as ID if available, otherwise use reference
	id := digest
	if id == "" {
		id = refStr
	}

	image := &Image{
		ID:        id,
		Name:      repository,
		Tag:       tag,
		OSCommand: c.OSCommand,
		Log:       c.Log,
	}

	// Store additional info in the Image.Image field
	image.Image.RepoTags = []string{refStr}
	if size > 0 {
		image.Image.Size = int64(size)
	}

	c.logDebugf("Parsed image: ID=%s, Name=%s, Tag=%s, Reference=%s", id, repository, tag, refStr)
	return image
}

// SystemStart starts the Apple Container system services
func (c *AppleContainerCommand) SystemStart() error {
	c.logInfo("Starting Apple Container system services")
	return c.OSCommand.RunExecutable(c.OSCommand.NewCmd("container", "system", "start"))
}

// SystemStop stops the Apple Container system services
func (c *AppleContainerCommand) SystemStop() error {
	c.logInfo("Stopping Apple Container system services")
	return c.OSCommand.RunExecutable(c.OSCommand.NewCmd("container", "system", "stop"))
}

// SystemStatus gets the status of Apple Container system services
func (c *AppleContainerCommand) SystemStatus() (map[string]interface{}, error) {
	c.logInfo("Getting Apple Container system status")

	cmd := c.OSCommand.NewCmd("container", "system", "status", "--format", "json")
	output, err := c.OSCommand.RunExecutableWithOutput(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to get system status: %w", err)
	}

	var status map[string]interface{}
	if err := json.Unmarshal([]byte(output), &status); err != nil {
		return nil, fmt.Errorf("failed to parse system status: %w", err)
	}

	return status, nil
}

// NewCommandObject creates a new command object for Apple Container (implements LimitedDockerCommand)
func (c *AppleContainerCommand) NewCommandObject(obj CommandObject) CommandObject {
	// For Apple Container, we don't need to modify the command object
	// as we don't use docker-compose
	return obj
}

// GetContainerLogs returns a command to get logs for a container
func (c *AppleContainerCommand) GetContainerLogs(containerID string, follow bool, tail string) *exec.Cmd {
	args := []string{"logs"}
	if follow {
		args = append(args, "--follow")
	}
	if tail != "" && tail != "all" {
		args = append(args, "-n", tail)
	}
	args = append(args, containerID)

	return c.OSCommand.NewCmd("container", args...)
}

// GetNetworks retrieves all networks from Apple Container
func (c *AppleContainerCommand) GetNetworks() ([]*Network, error) {
	c.logInfo("Getting networks from Apple Container")

	// Execute: container network list --format json
	cmd := c.OSCommand.NewCmd("container", "network", "list", "--format", "json")
	output, err := c.OSCommand.RunExecutableWithOutput(cmd)
	if err != nil {
		c.logError("Failed to get networks from Apple Container: ", err)
		return nil, fmt.Errorf("failed to get networks: %w", err)
	}

	// Parse the JSON output
	networks, err := c.parseNetworkList(output)
	if err != nil {
		c.logError("Failed to parse network list: ", err)
		return nil, fmt.Errorf("failed to parse network list: %w", err)
	}

	c.logInfof("Found %d networks", len(networks))
	return networks, nil
}

// parseNetworkList parses the JSON output from Apple Container's network list command
func (c *AppleContainerCommand) parseNetworkList(output string) ([]*Network, error) {
	if strings.TrimSpace(output) == "" {
		return []*Network{}, nil
	}

	// Apple Container outputs a JSON array of network objects
	var networkArray []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &networkArray); err != nil {
		c.logError("Failed to parse network JSON array: ", err)
		return nil, fmt.Errorf("failed to parse network JSON: %w", err)
	}

	networks := make([]*Network, 0, len(networkArray))
	for _, networkData := range networkArray {
		network := c.jsonToNetwork(networkData)
		if network != nil {
			networks = append(networks, network)
		}
	}

	return networks, nil
}

// jsonToNetwork converts JSON data to a Network struct
func (c *AppleContainerCommand) jsonToNetwork(data map[string]interface{}) *Network {
	// Extract network ID
	id, _ := data["id"].(string)
	if id == "" {
		c.logError("Network missing ID field")
		return nil
	}

	// Extract state
	state, _ := data["state"].(string)

	// Extract network config
	var driver string
	if config, ok := data["config"].(map[string]interface{}); ok {
		mode, _ := config["mode"].(string)
		driver = mode // Use mode as driver for display
	}

	// Create network struct
	network := &Network{
		Name:      id,
		OSCommand: c.OSCommand,
		Log:       c.Log,
	}

	// Set network details
	network.Network.ID = id
	network.Network.Name = id
	network.Network.Driver = driver

	c.logDebugf("Parsed network: ID=%s, Name=%s, Driver=%s, State=%s", id, id, driver, state)
	return network
}

// GetContainerMounts retrieves mount information for a container
func (c *AppleContainerCommand) GetContainerMounts(containerID string) (string, error) {
	// Execute container inspect to get full details
	cmd := c.OSCommand.NewCmd("container", "inspect", containerID)
	output, err := c.OSCommand.RunExecutableWithOutput(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}

	// Parse the JSON output
	var containers []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &containers); err != nil {
		return "", fmt.Errorf("failed to parse container inspect JSON: %w", err)
	}

	if len(containers) == 0 {
		return "No container found", nil
	}

	containerData := containers[0]
	config, ok := containerData["configuration"].(map[string]interface{})
	if !ok {
		return "No configuration found", nil
	}

	mounts, ok := config["mounts"].([]interface{})
	if !ok || len(mounts) == 0 {
		return "No mounts configured for this container", nil
	}

	// Format mount information
	var result strings.Builder
	result.WriteString("Volume Mounts:\n")
	result.WriteString("==============\n\n")

	for i, mount := range mounts {
		mountMap, ok := mount.(map[string]interface{})
		if !ok {
			continue
		}

		source, _ := mountMap["source"].(string)
		destination, _ := mountMap["destination"].(string)
		options, _ := mountMap["options"].([]interface{})

		// Get mount type
		mountType := "unknown"
		if typeInfo, ok := mountMap["type"].(map[string]interface{}); ok {
			if _, ok := typeInfo["virtiofs"]; ok {
				mountType = "virtiofs"
			} else if _, ok := typeInfo["tmpfs"]; ok {
				mountType = "tmpfs"
			}
		}

		result.WriteString(fmt.Sprintf("Mount %d:\n", i+1))
		result.WriteString(fmt.Sprintf("  Type:        %s\n", mountType))
		result.WriteString(fmt.Sprintf("  Source:      %s\n", source))
		result.WriteString(fmt.Sprintf("  Destination: %s\n", destination))

		if len(options) > 0 {
			optStrings := make([]string, len(options))
			for j, opt := range options {
				optStrings[j] = fmt.Sprintf("%v", opt)
			}
			result.WriteString(fmt.Sprintf("  Options:     %s\n", strings.Join(optStrings, ", ")))
		}

		result.WriteString("\n")
	}

	return result.String(), nil
}

// RefreshVolumes gets the volumes and stores them for Apple Container
func (c *AppleContainerCommand) RefreshVolumes() ([]*Volume, error) {
	c.logInfo("Getting volumes from Apple Container")

	// Execute: container volume list --format json
	cmd := c.OSCommand.NewCmd("container", "volume", "list", "--format", "json")
	output, err := c.OSCommand.RunExecutableWithOutput(cmd)
	if err != nil {
		c.logError("Failed to get volumes from Apple Container: ", err)
		return nil, fmt.Errorf("failed to get volumes: %w", err)
	}

	c.logDebugf("Raw volume ls output: %s", output)

	// Parse the JSON output
	volumes, err := c.parseVolumeList(output)
	if err != nil {
		c.logError("Failed to parse volume list: ", err)
		return nil, fmt.Errorf("failed to parse volume list: %w", err)
	}

	c.logInfof("Found %d volumes", len(volumes))
	return volumes, nil
}

// parseVolumeList parses the JSON output from Apple Container's volume list command
func (c *AppleContainerCommand) parseVolumeList(output string) ([]*Volume, error) {
	if strings.TrimSpace(output) == "" {
		return []*Volume{}, nil
	}

	// Apple Container outputs a JSON array of volume objects
	var volumeArray []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &volumeArray); err != nil {
		c.logError("Failed to parse volume JSON array: ", err)
		return nil, fmt.Errorf("failed to parse volume JSON: %w", err)
	}

	volumes := make([]*Volume, 0, len(volumeArray))
	for _, volumeData := range volumeArray {
		volume := c.jsonToVolume(volumeData)
		if volume != nil {
			volumes = append(volumes, volume)
		}
	}

	return volumes, nil
}

// jsonToVolume converts JSON data to a Volume struct
func (c *AppleContainerCommand) jsonToVolume(data map[string]interface{}) *Volume {
	// Extract volume name/ID
	name, _ := data["name"].(string)
	if name == "" {
		// Try alternative field names
		name, _ = data["id"].(string)
		if name == "" {
			c.logError("Volume missing name/id field")
			return nil
		}
	}

	// Extract driver (if available)
	driver, _ := data["driver"].(string)
	if driver == "" {
		driver = "local" // Default driver
	}

	// Extract mountpoint (if available)
	mountpoint, _ := data["mountpoint"].(string)

	// Create volume with Apple Container specific fields
	volume := &Volume{
		Name:      name,
		OSCommand: c.OSCommand,
		Log:       c.Log,
		// Note: Client is nil for Apple containers - we don't use Docker client
		Client: nil,
		// Set a reference to the Apple command for volume operations
		DockerCommand: c,
	}

	// Set up volume.Volume with basic information
	volume.Volume.Driver = driver
	volume.Volume.Mountpoint = mountpoint
	volume.Volume.Name = name
	volume.Volume.Scope = "local" // Apple Container volumes are typically local

	// Extract labels if available
	if labelsData, ok := data["labels"].(map[string]interface{}); ok {
		labels := make(map[string]string)
		for k, v := range labelsData {
			if vStr, ok := v.(string); ok {
				labels[k] = vStr
			}
		}
		volume.Volume.Labels = labels
	}

	// Extract options if available
	if optionsData, ok := data["options"].(map[string]interface{}); ok {
		options := make(map[string]string)
		for k, v := range optionsData {
			if vStr, ok := v.(string); ok {
				options[k] = vStr
			}
		}
		volume.Volume.Options = options
	}

	c.logDebugf("Parsed volume: Name=%s, Driver=%s, Mountpoint=%s", name, driver, mountpoint)
	return volume
}

// PruneVolumes prunes volumes for Apple Container
func (c *AppleContainerCommand) PruneVolumes() error {
	c.logInfo("Pruning volumes from Apple Container")

	// Execute: container volume prune --force
	err := c.OSCommand.RunExecutable(c.OSCommand.NewCmd("container", "volume", "prune", "--force"))
	if err != nil {
		c.logError("Failed to prune volumes from Apple Container: ", err)
		return fmt.Errorf("failed to prune volumes: %w", err)
	}

	c.logInfo("Successfully pruned volumes")
	return nil
}

// RemoveVolume removes a volume for Apple Container
func (c *AppleContainerCommand) RemoveVolume(name string, force bool) error {
	c.logInfof("Removing volume %s (force: %v)", name, force)

	args := []string{"volume", "rm"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, name)
	return c.OSCommand.RunExecutable(c.OSCommand.NewCmd("container", args...))
}

// CreateVolume creates a named volume with optional key=value options
func (c *AppleContainerCommand) CreateVolume(name string, opts map[string]string) error {
	if name == "" {
		return fmt.Errorf("volume name required")
	}
	if !c.Supports(FeatureVolumeCreate) {
		return fmt.Errorf("volume create not supported by this runtime")
	}
	args := []string{"volume", "create", "--name", name}
	for k, v := range opts {
		if k == "" {
			continue
		}
		if v != "" {
			args = append(args, "--opt", fmt.Sprintf("%s=%s", k, v))
		} else {
			args = append(args, "--opt", k)
		}
	}
	return c.OSCommand.RunExecutable(c.OSCommand.NewCmd("container", args...))
}
