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
	if c.IsPod && c.Pod != nil {
		return c.Pod.ID
	}
	if c.Container != nil {
		return c.Container.ID
	}
	return ""
}

// Name returns the display name for the item.
func (c *ContainerListItem) Name() string {
	if c.IsPod && c.Pod != nil {
		return c.Pod.Name
	}
	if c.Container != nil {
		return c.Container.Name
	}
	return ""
}

// State returns the state for the item.
func (c *ContainerListItem) State() string {
	if c.IsPod && c.Pod != nil {
		return c.Pod.State()
	}
	if c.Container != nil {
		return c.Container.Summary.State
	}
	return ""
}

// GetContainers returns the containers if this is a pod, nil otherwise.
func (c *ContainerListItem) GetContainers() []*Container {
	if c.IsPod && c.Pod != nil {
		return c.Pod.Containers
	}
	return nil
}

// IsInPod returns true if this is a container that belongs to a pod.
func (c *ContainerListItem) IsInPod() bool {
	if c.IsPod {
		return false
	}
	if c.Container != nil {
		return c.Container.Summary.Pod != ""
	}
	return false
}

// PodID returns the pod ID if this container is in a pod, empty string otherwise.
func (c *ContainerListItem) PodID() string {
	if c.IsPod && c.Pod != nil {
		return c.Pod.ID
	}
	if c.Container != nil {
		return c.Container.Summary.Pod
	}
	return ""
}

// PodName returns the pod name if this container is in a pod, empty string otherwise.
func (c *ContainerListItem) PodName() string {
	if c.IsPod && c.Pod != nil {
		return c.Pod.Name
	}
	if c.Container != nil {
		return c.Container.Summary.PodName
	}
	return ""
}
