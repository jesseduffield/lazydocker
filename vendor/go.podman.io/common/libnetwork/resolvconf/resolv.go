package resolvconf

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/fileutils"
)

const (
	localhost         = "127.0.0.1"
	systemdResolvedIP = "127.0.0.53"
)

// Params for the New() function.
type Params struct {
	// Path is the path to new resolv.conf file which should be created.
	Path string
	// Namespaces is the list of container namespaces.
	// This is required to fist check for a resolv.conf under /etc/netns,
	// created by "ip netns". Also used to check if the container has a
	// netns in which case localhost nameserver must be filtered.
	Namespaces []specs.LinuxNamespace
	// IPv6Enabled will filter ipv6 nameservers when not set to true.
	IPv6Enabled bool
	// KeepHostServers can be set when it is required to still keep the
	// original resolv.conf nameservers even when explicit Nameservers
	// are set. In this case they will be appended to the given values.
	KeepHostServers bool
	// KeepHostSearches can be set when it is required to still keep the
	// original resolv.conf search domains even when explicit search domains
	// are set in Searches.
	KeepHostSearches bool
	// KeepHostOptions can be set when it is required to still keep the
	// original resolv.conf options even when explicit options are set in
	// Options.
	KeepHostOptions bool
	// Nameservers is a list of nameservers the container should use,
	// instead of the default ones from the host. Set KeepHostServers
	// in order to also keep the hosts resolv.conf nameservers.
	Nameservers []string
	// Searches is a list of dns search domains the container should use,
	// instead of the default ones from the host. Set KeepHostSearches
	// in order to also keep the hosts resolv.conf search domains.
	Searches []string
	// Options is a list of dns options the container should use,
	// instead of the default ones from the host. Set KeepHostOptions
	// in order to also keep the hosts resolv.conf options.
	Options []string

	// resolvConfPath is the path which should be used as base to get the dns
	// options. This should only be used for testing purposes. For all other
	// callers this defaults to /etc/resolv.conf.
	resolvConfPath string
}

func getDefaultResolvConf(params *Params) ([]byte, bool, error) {
	resolveConf := DefaultResolvConf
	// this is only used by testing
	if params.resolvConfPath != "" {
		resolveConf = params.resolvConfPath
	}
	hostNS := true
	for _, ns := range params.Namespaces {
		if ns.Type == specs.NetworkNamespace {
			hostNS = false
			if ns.Path != "" && !strings.HasPrefix(ns.Path, "/proc/") {
				// check for netns created by "ip netns"
				path := filepath.Join("/etc/netns", filepath.Base(ns.Path), "resolv.conf")
				err := fileutils.Exists(path)
				if err == nil {
					resolveConf = path
				}
			}
			break
		}
	}

	contents, err := os.ReadFile(resolveConf)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	if hostNS {
		return contents, hostNS, nil
	}

	ns := getNameservers(contents)
	// Check for local only resolver, in this case we want to get the real nameservers
	// since localhost is not reachable from the netns.
	if len(ns) == 1 {
		var path string
		switch ns[0] {
		case systemdResolvedIP:
			// used by systemd-resolved
			path = "/run/systemd/resolve/resolv.conf"
		case localhost:
			// used by NetworkManager https://github.com/containers/podman/issues/13599
			path = "/run/NetworkManager/no-stub-resolv.conf"
		}
		if path != "" {
			// read the actual resolv.conf file for
			resolvedContents, err := os.ReadFile(path)
			if err != nil {
				// do not error when the file does not exists, the detection logic is not perfect
				if !errors.Is(err, os.ErrNotExist) {
					return nil, false, fmt.Errorf("local resolver detected, but could not read real resolv.conf at %q: %w", path, err)
				}
			} else {
				logrus.Debugf("found local resolver, using %q to get the nameservers", path)
				contents = resolvedContents
			}
		}
	}

	return contents, hostNS, nil
}

// unsetSearchDomainsIfNeeded removes the search domain when they contain a single dot as element.
func unsetSearchDomainsIfNeeded(searches []string) []string {
	if slices.Contains(searches, ".") {
		return nil
	}
	return searches
}

// New creates a new resolv.conf file with the given params.
func New(params *Params) error {
	// short path, if everything is given there is no need to actually read the hosts /etc/resolv.conf
	if len(params.Nameservers) > 0 && len(params.Options) > 0 && len(params.Searches) > 0 &&
		!params.KeepHostServers && !params.KeepHostOptions && !params.KeepHostSearches {
		return build(params.Path, params.Nameservers, unsetSearchDomainsIfNeeded(params.Searches), params.Options)
	}

	content, hostNS, err := getDefaultResolvConf(params)
	if err != nil {
		return fmt.Errorf("failed to get the default /etc/resolv.conf content: %w", err)
	}

	content = filterResolvDNS(content, params.IPv6Enabled, !hostNS)

	nameservers := params.Nameservers
	if len(nameservers) == 0 || params.KeepHostServers {
		nameservers = append(nameservers, getNameservers(content)...)
	}

	searches := unsetSearchDomainsIfNeeded(params.Searches)
	// if no params.Searches then use host ones
	// otherwise make sure that they were no explicitly unset before adding host ones
	if len(params.Searches) == 0 || (params.KeepHostSearches && len(searches) > 0) {
		searches = append(searches, getSearchDomains(content)...)
	}

	options := params.Options
	if len(options) == 0 || params.KeepHostOptions {
		options = append(options, getOptions(content)...)
	}

	return build(params.Path, nameservers, searches, options)
}

// Add will add the given nameservers to the given resolv.conf file.
// It will add the nameserver in front of the existing ones.
func Add(path string, nameservers []string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	nameservers = append(nameservers, getNameservers(contents)...)
	return build(path, nameservers, getSearchDomains(contents), getOptions(contents))
}

// Remove the given nameserver from the given resolv.conf file.
func Remove(path string, nameservers []string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	oldNameservers := getNameservers(contents)
	newNameserver := make([]string, 0, len(oldNameservers))
	for _, ns := range oldNameservers {
		if !slices.Contains(nameservers, ns) {
			newNameserver = append(newNameserver, ns)
		}
	}

	return build(path, newNameserver, getSearchDomains(contents), getOptions(contents))
}
