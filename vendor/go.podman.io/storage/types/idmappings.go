package types

import (
	"fmt"
	"os"

	"go.podman.io/storage/pkg/idtools"
)

// AutoUserNsOptions defines how to automatically create a user namespace.
type AutoUserNsOptions struct {
	// Size defines the size for the user namespace.  If it is set to a
	// value bigger than 0, the user namespace will have exactly this size.
	// If it is not set, some heuristics will be used to find its size.
	Size uint32
	// InitialSize defines the minimum size for the user namespace.
	// The created user namespace will have at least this size.
	InitialSize uint32
	// PasswdFile to use if the container uses a volume.
	PasswdFile string
	// GroupFile to use if the container uses a volume.
	GroupFile string
	// AdditionalUIDMappings specified additional UID mappings to include in
	// the generated user namespace.
	AdditionalUIDMappings []idtools.IDMap
	// AdditionalGIDMappings specified additional GID mappings to include in
	// the generated user namespace.
	AdditionalGIDMappings []idtools.IDMap
}

// IDMappingOptions are used for specifying how ID mapping should be set up for
// a layer or container.
type IDMappingOptions struct {
	// UIDMap and GIDMap are used for setting up a layer's root filesystem
	// for use inside of a user namespace where ID mapping is being used.
	// If HostUIDMapping/HostGIDMapping is true, no mapping of the
	// respective type will be used.  Otherwise, if UIDMap and/or GIDMap
	// contain at least one mapping, one or both will be used.  By default,
	// if neither of those conditions apply, if the layer has a parent
	// layer, the parent layer's mapping will be used, and if it does not
	// have a parent layer, the mapping which was passed to the Store
	// object when it was initialized will be used.
	HostUIDMapping bool
	HostGIDMapping bool
	UIDMap         []idtools.IDMap
	GIDMap         []idtools.IDMap
	AutoUserNs     bool
	AutoUserNsOpts AutoUserNsOptions
}

// ParseIDMapping takes idmappings and subuid and subgid maps and returns a storage mapping
func ParseIDMapping(UIDMapSlice, GIDMapSlice []string, subUIDMap, subGIDMap string) (*IDMappingOptions, error) {
	options := IDMappingOptions{
		HostUIDMapping: true,
		HostGIDMapping: true,
	}
	if subGIDMap == "" && subUIDMap != "" {
		subGIDMap = subUIDMap
	}
	if subUIDMap == "" && subGIDMap != "" {
		subUIDMap = subGIDMap
	}
	if len(GIDMapSlice) == 0 && len(UIDMapSlice) != 0 {
		GIDMapSlice = UIDMapSlice
	}
	if len(UIDMapSlice) == 0 && len(GIDMapSlice) != 0 {
		UIDMapSlice = GIDMapSlice
	}
	if len(UIDMapSlice) == 0 && subUIDMap == "" && os.Getuid() != 0 {
		UIDMapSlice = []string{fmt.Sprintf("0:%d:1", os.Getuid())}
	}
	if len(GIDMapSlice) == 0 && subGIDMap == "" && os.Getuid() != 0 {
		GIDMapSlice = []string{fmt.Sprintf("0:%d:1", os.Getgid())}
	}

	if subUIDMap != "" && subGIDMap != "" {
		mappings, err := idtools.NewIDMappings(subUIDMap, subGIDMap)
		if err != nil {
			return nil, fmt.Errorf("failed to create NewIDMappings for uidmap=%s gidmap=%s: %w", subUIDMap, subGIDMap, err)
		}
		options.UIDMap = mappings.UIDs()
		options.GIDMap = mappings.GIDs()
	}
	parsedUIDMap, err := idtools.ParseIDMap(UIDMapSlice, "UID")
	if err != nil {
		return nil, fmt.Errorf("failed to create ParseUIDMap UID=%s: %w", UIDMapSlice, err)
	}
	parsedGIDMap, err := idtools.ParseIDMap(GIDMapSlice, "GID")
	if err != nil {
		return nil, fmt.Errorf("failed to create ParseGIDMap GID=%s: %w", UIDMapSlice, err)
	}
	options.UIDMap = append(options.UIDMap, parsedUIDMap...)
	options.GIDMap = append(options.GIDMap, parsedGIDMap...)
	if len(options.UIDMap) > 0 {
		options.HostUIDMapping = false
	}
	if len(options.GIDMap) > 0 {
		options.HostGIDMapping = false
	}
	return &options, nil
}
