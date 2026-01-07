package util

import (
	"fmt"
	"slices"
	"strings"

	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/filters"
	"go.podman.io/common/pkg/util"
)

func GenerateNetworkFilters(f map[string][]string) ([]types.FilterFunc, error) {
	filterFuncs := make([]types.FilterFunc, 0, len(f))
	for key, filterValues := range f {
		filterFunc, err := createFilterFuncs(key, filterValues)
		if err != nil {
			return nil, err
		}
		filterFuncs = append(filterFuncs, filterFunc)
	}
	return filterFuncs, nil
}

func createFilterFuncs(key string, filterValues []string) (types.FilterFunc, error) {
	switch strings.ToLower(key) {
	case "name":
		// matches one name, regex allowed
		return func(net types.Network) bool {
			return util.StringMatchRegexSlice(net.Name, filterValues)
		}, nil

	case types.Driver:
		// matches network driver
		return func(net types.Network) bool {
			return slices.Contains(filterValues, net.Driver)
		}, nil

	case "id":
		// matches part of one id
		return func(net types.Network) bool {
			return filters.FilterID(net.ID, filterValues)
		}, nil

		// TODO: add dns enabled, internal filter
	}
	return createPruneFilterFuncs(key, filterValues)
}

func GenerateNetworkPruneFilters(f map[string][]string) ([]types.FilterFunc, error) {
	filterFuncs := make([]types.FilterFunc, 0, len(f))
	for key, filterValues := range f {
		filterFunc, err := createPruneFilterFuncs(key, filterValues)
		if err != nil {
			return nil, err
		}
		filterFuncs = append(filterFuncs, filterFunc)
	}
	return filterFuncs, nil
}

func createPruneFilterFuncs(key string, filterValues []string) (types.FilterFunc, error) {
	switch strings.ToLower(key) {
	case "label":
		// matches all labels
		return func(net types.Network) bool {
			return filters.MatchLabelFilters(filterValues, net.Labels)
		}, nil
	case "label!":
		return func(net types.Network) bool {
			return filters.MatchNegatedLabelFilters(filterValues, net.Labels)
		}, nil
	case "until":
		until, err := filters.ComputeUntilTimestamp(filterValues)
		if err != nil {
			return nil, err
		}
		return func(net types.Network) bool {
			return net.Created.Before(until)
		}, nil
	default:
		return nil, fmt.Errorf("invalid filter %q", key)
	}
}
