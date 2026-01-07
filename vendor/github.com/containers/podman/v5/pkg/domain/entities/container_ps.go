package entities

import (
	"errors"
	"sort"
	"strings"

	"github.com/containers/podman/v5/pkg/domain/entities/types"
)

// ExternalContainerFilter is a function to determine whether a container list is included
// in command output. Container lists to be outputted are tested using the function.
// A true return will include the container list, a false return will exclude it.
type ExternalContainerFilter func(*ListContainer) bool

// ListContainer describes a container suitable for listing
type ListContainer = types.ListContainer

// ListContainerNamespaces contains the identifiers of the container's Linux namespaces
type ListContainerNamespaces = types.ListContainerNamespaces

type SortListContainers []ListContainer

func (a SortListContainers) Len() int      { return len(a) }
func (a SortListContainers) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

type psSortedCommand struct{ SortListContainers }

func (a psSortedCommand) Less(i, j int) bool {
	return strings.Join(a.SortListContainers[i].Command, " ") < strings.Join(a.SortListContainers[j].Command, " ")
}

type psSortedID struct{ SortListContainers }

func (a psSortedID) Less(i, j int) bool {
	return a.SortListContainers[i].ID < a.SortListContainers[j].ID
}

type psSortedImage struct{ SortListContainers }

func (a psSortedImage) Less(i, j int) bool {
	return a.SortListContainers[i].Image < a.SortListContainers[j].Image
}

type psSortedNames struct{ SortListContainers }

func (a psSortedNames) Less(i, j int) bool {
	return a.SortListContainers[i].Names[0] < a.SortListContainers[j].Names[0]
}

type psSortedPod struct{ SortListContainers }

func (a psSortedPod) Less(i, j int) bool {
	return a.SortListContainers[i].Pod < a.SortListContainers[j].Pod
}

type psSortedRunningFor struct{ SortListContainers }

func (a psSortedRunningFor) Less(i, j int) bool {
	return a.SortListContainers[i].StartedAt < a.SortListContainers[j].StartedAt
}

type psSortedStatus struct{ SortListContainers }

func (a psSortedStatus) Less(i, j int) bool {
	return a.SortListContainers[i].State < a.SortListContainers[j].State
}

type psSortedSize struct{ SortListContainers }

func (a psSortedSize) Less(i, j int) bool {
	if a.SortListContainers[i].Size == nil || a.SortListContainers[j].Size == nil {
		return false
	}
	return a.SortListContainers[i].Size.RootFsSize < a.SortListContainers[j].Size.RootFsSize
}

type PsSortedCreateTime struct{ SortListContainers }

func (a PsSortedCreateTime) Less(i, j int) bool {
	return a.SortListContainers[i].Created.Before(a.SortListContainers[j].Created)
}

func SortPsOutput(sortBy string, psOutput SortListContainers) (SortListContainers, error) {
	switch sortBy {
	case "id":
		sort.Sort(psSortedID{psOutput})
	case "image":
		sort.Sort(psSortedImage{psOutput})
	case "command":
		sort.Sort(psSortedCommand{psOutput})
	case "runningfor":
		sort.Sort(psSortedRunningFor{psOutput})
	case "status":
		sort.Sort(psSortedStatus{psOutput})
	case "size":
		sort.Sort(psSortedSize{psOutput})
	case "names":
		sort.Sort(psSortedNames{psOutput})
	case "created":
		sort.Sort(PsSortedCreateTime{psOutput})
	case "pod":
		sort.Sort(psSortedPod{psOutput})
	default:
		return nil, errors.New("invalid option for --sort, options are: command, created, id, image, names, runningfor, size, or status")
	}
	return psOutput, nil
}
