//go:build !remote

package libpod

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/parallel"
	"github.com/containers/podman/v5/pkg/syncmap"
	"github.com/sirupsen/logrus"
)

type containerNode struct {
	lock       sync.Mutex
	id         string
	container  *Container
	dependsOn  []*containerNode
	dependedOn []*containerNode
}

// ContainerGraph is a dependency graph based on a set of containers.
type ContainerGraph struct {
	nodes              map[string]*containerNode
	noDepNodes         []*containerNode
	notDependedOnNodes map[string]*containerNode
}

// DependencyMap returns the dependency graph as map with the key being a
// container and the value being the containers the key depends on.
func (cg *ContainerGraph) DependencyMap() (dependencies map[*Container][]*Container) {
	dependencies = make(map[*Container][]*Container)
	for _, node := range cg.nodes {
		dependsOn := make([]*Container, len(node.dependsOn))
		for i, d := range node.dependsOn {
			dependsOn[i] = d.container
		}
		dependencies[node.container] = dependsOn
	}
	return dependencies
}

// BuildContainerGraph builds a dependency graph based on the container slice.
func BuildContainerGraph(ctrs []*Container) (*ContainerGraph, error) {
	graph := new(ContainerGraph)
	graph.nodes = make(map[string]*containerNode)
	graph.notDependedOnNodes = make(map[string]*containerNode)

	// Start by building all nodes, with no edges
	for _, ctr := range ctrs {
		ctrNode := new(containerNode)
		ctrNode.id = ctr.ID()
		ctrNode.container = ctr

		graph.nodes[ctr.ID()] = ctrNode
		graph.notDependedOnNodes[ctr.ID()] = ctrNode
	}

	// Now add edges based on dependencies
	for _, node := range graph.nodes {
		deps := node.container.Dependencies()
		for _, dep := range deps {
			// Get the dep's node
			depNode, ok := graph.nodes[dep]
			if !ok {
				return nil, fmt.Errorf("container %s depends on container %s not found in input list: %w", node.id, dep, define.ErrNoSuchCtr)
			}

			// Add the dependent node to the node's dependencies
			// And add the node to the dependent node's dependedOn
			node.dependsOn = append(node.dependsOn, depNode)
			depNode.dependedOn = append(depNode.dependedOn, node)

			// The dependency now has something depending on it
			delete(graph.notDependedOnNodes, dep)
		}

		// Maintain a list of nodes with no dependencies
		// (no edges coming from them)
		if len(deps) == 0 {
			graph.noDepNodes = append(graph.noDepNodes, node)
		}
	}

	// Need to do cycle detection
	// We cannot start or stop if there are cyclic dependencies
	cycle, err := detectCycles(graph)
	if err != nil {
		return nil, err
	} else if cycle {
		return nil, fmt.Errorf("cycle found in container dependency graph: %w", define.ErrInternal)
	}

	return graph, nil
}

// Detect cycles in a container graph using Tarjan's strongly connected
// components algorithm
// Return true if a cycle is found, false otherwise
func detectCycles(graph *ContainerGraph) (bool, error) {
	type nodeInfo struct {
		index   int
		lowLink int
		onStack bool
	}

	index := 0

	nodes := make(map[string]*nodeInfo)
	stack := make([]*containerNode, 0, len(graph.nodes))

	var strongConnect func(*containerNode) (bool, error)
	strongConnect = func(node *containerNode) (bool, error) {
		logrus.Debugf("Strongconnecting node %s", node.id)

		info := new(nodeInfo)
		info.index = index
		info.lowLink = index
		index++

		nodes[node.id] = info

		stack = append(stack, node)

		info.onStack = true

		logrus.Debugf("Pushed %s onto stack", node.id)

		// Work through all nodes we point to
		for _, successor := range node.dependsOn {
			if _, ok := nodes[successor.id]; !ok {
				logrus.Debugf("Recursing to successor node %s", successor.id)

				cycle, err := strongConnect(successor)
				if err != nil {
					return false, err
				} else if cycle {
					return true, nil
				}

				successorInfo := nodes[successor.id]
				if successorInfo.lowLink < info.lowLink {
					info.lowLink = successorInfo.lowLink
				}
			} else {
				successorInfo := nodes[successor.id]
				if successorInfo.index < info.lowLink && successorInfo.onStack {
					info.lowLink = successorInfo.index
				}
			}
		}

		if info.lowLink == info.index {
			l := len(stack)
			if l == 0 {
				return false, fmt.Errorf("empty stack in detectCycles: %w", define.ErrInternal)
			}

			// Pop off the stack
			topOfStack := stack[l-1]
			stack = stack[:l-1]

			// Popped item is no longer on the stack, mark as such
			topInfo, ok := nodes[topOfStack.id]
			if !ok {
				return false, fmt.Errorf("finding node info for %s: %w", topOfStack.id, define.ErrInternal)
			}
			topInfo.onStack = false

			logrus.Debugf("Finishing node %s. Popped %s off stack", node.id, topOfStack.id)

			// If the top of the stack is not us, we have found a
			// cycle
			if topOfStack.id != node.id {
				return true, nil
			}
		}

		return false, nil
	}

	for id, node := range graph.nodes {
		if _, ok := nodes[id]; !ok {
			cycle, err := strongConnect(node)
			if err != nil {
				return false, err
			} else if cycle {
				return true, nil
			}
		}
	}

	return false, nil
}

// Visit a node on a container graph and start the container, or set an error if
// a dependency failed to start. if restart is true, startNode will restart the node instead of starting it.
func startNode(ctx context.Context, node *containerNode, setError bool, ctrErrors map[string]error, ctrsVisited map[string]bool, restart bool) {
	// First, check if we have already visited the node
	if ctrsVisited[node.id] {
		return
	}

	// If setError is true, a dependency of us failed
	// Mark us as failed and recurse
	if setError {
		// Mark us as visited, and set an error
		ctrsVisited[node.id] = true
		ctrErrors[node.id] = fmt.Errorf("a dependency of container %s failed to start: %w", node.id, define.ErrCtrStateInvalid)

		// Hit anyone who depends on us, and set errors on them too
		for _, successor := range node.dependedOn {
			startNode(ctx, successor, true, ctrErrors, ctrsVisited, restart)
		}

		return
	}

	// Have all our dependencies started?
	// If not, don't visit the node yet
	depsVisited := true
	for _, dep := range node.dependsOn {
		depsVisited = depsVisited && ctrsVisited[dep.id]
	}
	if !depsVisited {
		// Don't visit us yet, all dependencies are not up
		// We'll hit the dependencies eventually, and when we do it will
		// recurse here
		return
	}

	// Going to try to start the container, mark us as visited
	ctrsVisited[node.id] = true

	ctrErrored := false

	// Check if dependencies are running
	// Graph traversal means we should have started them
	// But they could have died before we got here
	// Does not require that the container be locked, we only need to lock
	// the dependencies
	depsStopped, err := node.container.checkDependenciesRunning()
	if err != nil {
		ctrErrors[node.id] = err
		ctrErrored = true
	} else if len(depsStopped) > 0 {
		// Our dependencies are not running
		depsList := strings.Join(depsStopped, ",")
		ctrErrors[node.id] = fmt.Errorf("the following dependencies of container %s are not running: %s: %w", node.id, depsList, define.ErrCtrStateInvalid)
		ctrErrored = true
	}

	// Lock before we start
	node.container.lock.Lock()

	// Sync the container to pick up current state
	if !ctrErrored {
		if err := node.container.syncContainer(); err != nil {
			ctrErrored = true
			ctrErrors[node.id] = err
		}
	}

	// Start the container (only if it is not running)
	if !ctrErrored && len(node.container.config.InitContainerType) < 1 {
		if !restart && node.container.state.State != define.ContainerStateRunning {
			if err := node.container.initAndStart(ctx); err != nil {
				ctrErrored = true
				ctrErrors[node.id] = err
			}
		}
		if restart && node.container.state.State != define.ContainerStatePaused && node.container.state.State != define.ContainerStateUnknown {
			if err := node.container.restartWithTimeout(ctx, node.container.config.StopTimeout); err != nil {
				ctrErrored = true
				ctrErrors[node.id] = err
			}
		}
	}

	node.container.lock.Unlock()

	// Recurse to anyone who depends on us and start them
	for _, successor := range node.dependedOn {
		startNode(ctx, successor, ctrErrored, ctrErrors, ctrsVisited, restart)
	}
}

// Contains all details required for traversing the container graph.
type nodeTraversal struct {
	// Optional. but *MUST* be locked.
	// Should NOT be changed once a traversal is started.
	pod *Pod
	// Function to execute on the individual container being acted on.
	// Should NOT be changed once a traversal is started.
	actionFunc func(ctr *Container, pod *Pod) error
	// Shared set of errors for all containers currently acted on.
	ctrErrors *syncmap.Map[string, error]
	// Shared set of what containers have been visited.
	ctrsVisited *syncmap.Map[string, bool]
}

// Perform a traversal of the graph in an inwards direction - meaning from nodes
// with no dependencies, recursing inwards to the nodes they depend on.
// Safe to run in parallel on multiple nodes.
func traverseNodeInwards(node *containerNode, nodeDetails *nodeTraversal, setError bool) {
	node.lock.Lock()

	// If we already visited this node, we're done.
	visited := nodeDetails.ctrsVisited.Exists(node.id)
	if visited {
		node.lock.Unlock()
		return
	}

	// Someone who depends on us failed.
	// Mark us as failed and recurse.
	if setError {
		nodeDetails.ctrsVisited.Put(node.id, true)
		nodeDetails.ctrErrors.Put(node.id, fmt.Errorf("a container that depends on container %s could not be stopped: %w", node.id, define.ErrCtrStateInvalid))

		node.lock.Unlock()

		// Hit anyone who depends on us, set errors there as well.
		for _, successor := range node.dependsOn {
			traverseNodeInwards(successor, nodeDetails, true)
		}

		return
	}

	// Does anyone still depend on us?
	// Cannot stop if true. Once all our dependencies have been stopped,
	// we will be stopped.
	for _, dep := range node.dependedOn {
		// The container that depends on us hasn't been removed yet.
		// OK to continue on
		ok := nodeDetails.ctrsVisited.Exists(dep.id)
		if !ok {
			node.lock.Unlock()
			return
		}
	}

	ctrErrored := false
	if err := nodeDetails.actionFunc(node.container, nodeDetails.pod); err != nil {
		ctrErrored = true
		nodeDetails.ctrErrors.Put(node.id, err)
	}

	// Mark as visited *only after* finished with operation.
	// This ensures that the operation has completed, one way or the other.
	// If an error was set, only do this after the viral ctrErrored
	// propagates in traverseNodeInwards below.
	// Same with the node lock - we don't want to release it until we are
	// marked as visited.
	if !ctrErrored {
		nodeDetails.ctrsVisited.Put(node.id, true)

		node.lock.Unlock()
	}

	// Recurse to anyone who we depend on and work on them
	for _, successor := range node.dependsOn {
		traverseNodeInwards(successor, nodeDetails, ctrErrored)
	}

	// If we propagated an error, finally mark us as visited here, after
	// all nodes we traverse to have already been marked failed.
	// If we don't do this, there is a race condition where a node could try
	// and perform its operation before it was marked failed by the
	// traverseNodeInwards triggered by this process.
	if ctrErrored {
		nodeDetails.ctrsVisited.Put(node.id, true)

		node.lock.Unlock()
	}
}

// Stop all containers in the given graph, assumed to be a graph of pod.
// Pod is mandatory and should be locked.
func stopContainerGraph(ctx context.Context, graph *ContainerGraph, pod *Pod, timeout *uint, cleanup bool) (map[string]error, error) {
	// Are there actually any containers in the graph?
	// If not, return immediately.
	if len(graph.nodes) == 0 {
		return map[string]error{}, nil
	}

	nodeDetails := new(nodeTraversal)
	nodeDetails.pod = pod
	nodeDetails.ctrErrors = syncmap.New[string, error]()
	nodeDetails.ctrsVisited = syncmap.New[string, bool]()

	traversalFunc := func(ctr *Container, _ *Pod) error {
		ctr.lock.Lock()
		defer ctr.lock.Unlock()

		if err := ctr.syncContainer(); err != nil {
			return err
		}

		realTimeout := ctr.config.StopTimeout
		if timeout != nil {
			realTimeout = *timeout
		}

		if err := ctr.stop(realTimeout); err != nil && !errors.Is(err, define.ErrCtrStateInvalid) && !errors.Is(err, define.ErrCtrStopped) {
			return err
		}

		if cleanup {
			return ctr.fullCleanup(ctx, false)
		}

		return nil
	}
	nodeDetails.actionFunc = traversalFunc

	doneChans := make([]<-chan error, 0, len(graph.notDependedOnNodes))

	// Parallel enqueue jobs for all our starting nodes.
	if len(graph.notDependedOnNodes) == 0 {
		return nil, fmt.Errorf("no containers in pod %s are not dependencies of other containers, unable to stop", pod.ID())
	}
	for _, node := range graph.notDependedOnNodes {
		doneChan := parallel.Enqueue(ctx, func() error {
			traverseNodeInwards(node, nodeDetails, false)
			return nil
		})
		doneChans = append(doneChans, doneChan)
	}

	// We don't care about the returns values, these functions always return nil
	// But we do need all of the parallel jobs to terminate.
	for _, doneChan := range doneChans {
		<-doneChan
	}

	return nodeDetails.ctrErrors.Underlying(), nil
}

// Remove all containers in the given graph
// Pod is optional, and must be locked if given.
func removeContainerGraph(ctx context.Context, graph *ContainerGraph, pod *Pod, timeout *uint, force bool) (map[string]*ContainerNamedVolume, map[string]bool, map[string]error, error) {
	// Are there actually any containers in the graph?
	// If not, return immediately.
	if len(graph.nodes) == 0 {
		return nil, nil, nil, nil
	}

	nodeDetails := new(nodeTraversal)
	nodeDetails.pod = pod
	nodeDetails.ctrErrors = syncmap.New[string, error]()
	nodeDetails.ctrsVisited = syncmap.New[string, bool]()

	ctrNamedVolumes := syncmap.New[string, *ContainerNamedVolume]()

	traversalFunc := func(ctr *Container, pod *Pod) error {
		ctr.lock.Lock()
		defer ctr.lock.Unlock()

		if err := ctr.syncContainer(); err != nil {
			return err
		}

		for _, vol := range ctr.config.NamedVolumes {
			ctrNamedVolumes.Put(vol.Name, vol)
		}

		if pod != nil && pod.state.InfraContainerID == ctr.ID() {
			pod.state.InfraContainerID = ""
			if err := pod.save(); err != nil {
				return fmt.Errorf("error removing infra container %s from pod %s: %w", ctr.ID(), pod.ID(), err)
			}
		}

		opts := ctrRmOpts{
			Force:     force,
			RemovePod: true,
			Timeout:   timeout,
		}

		if _, _, err := ctr.runtime.removeContainer(ctx, ctr, opts); err != nil {
			return err
		}

		return nil
	}
	nodeDetails.actionFunc = traversalFunc

	doneChans := make([]<-chan error, 0, len(graph.notDependedOnNodes))

	// Parallel enqueue jobs for all our starting nodes.
	if len(graph.notDependedOnNodes) == 0 {
		return nil, nil, nil, fmt.Errorf("no containers in graph are not dependencies of other containers, unable to stop")
	}
	for _, node := range graph.notDependedOnNodes {
		doneChan := parallel.Enqueue(ctx, func() error {
			traverseNodeInwards(node, nodeDetails, false)
			return nil
		})
		doneChans = append(doneChans, doneChan)
	}

	// We don't care about the returns values, these functions always return nil
	// But we do need all of the parallel jobs to terminate.
	for _, doneChan := range doneChans {
		<-doneChan
	}

	// Safe to use Underlying as the SyncMap passes out of scope as we return
	return ctrNamedVolumes.Underlying(), nodeDetails.ctrsVisited.Underlying(), nodeDetails.ctrErrors.Underlying(), nil
}
