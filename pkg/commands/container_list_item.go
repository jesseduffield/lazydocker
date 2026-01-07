package commands

// ContainerListItem represents either a pod or a container in the unified list view.
// This allows the container panel to display both pods and containers.
type ContainerListItem struct {
	IsPod     bool
	Pod       *Pod       // Set if IsPod is true
	Container *Container // Set if IsPod is false
	Indent    int        // 0 for pods/standalone containers, 2 for containers in pods
}

// ID returns the unique ID for the item.
func (c *ContainerListItem) ID() string {
	if c.IsPod {
		return c.Pod.ID
	}
	return c.Container.ID
}

// Name returns the display name for the item.
func (c *ContainerListItem) Name() string {
	if c.IsPod {
		return c.Pod.Name
	}
	return c.Container.Name
}

// State returns the state for the item.
func (c *ContainerListItem) State() string {
	if c.IsPod {
		return c.Pod.State()
	}
	return c.Container.Summary.State
}

// GetContainers returns the containers if this is a pod, nil otherwise.
func (c *ContainerListItem) GetContainers() []*Container {
	if c.IsPod {
		return c.Pod.Containers
	}
	return nil
}

// IsInPod returns true if this is a container that belongs to a pod.
func (c *ContainerListItem) IsInPod() bool {
	if c.IsPod {
		return false
	}
	return c.Container.Summary.Pod != ""
}

// PodID returns the pod ID if this container is in a pod, empty string otherwise.
func (c *ContainerListItem) PodID() string {
	if c.IsPod {
		return c.Pod.ID
	}
	return c.Container.Summary.Pod
}

// PodName returns the pod name if this container is in a pod, empty string otherwise.
func (c *ContainerListItem) PodName() string {
	if c.IsPod {
		return c.Pod.Name
	}
	return c.Container.Summary.PodName
}
