package etchosts

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"go.podman.io/common/pkg/config"
)

const (
	HostContainersInternal = "host.containers.internal"
	HostGateway            = "host-gateway"
	localhost              = "localhost"
	hostDockerInternal     = "host.docker.internal"
)

type HostEntries []HostEntry

type HostEntry struct {
	IP    string
	Names []string
}

// Params for the New() function call.
type Params struct {
	// BaseFile is the file where we read entries from and add entries to
	// the target hosts file. If the name is empty it will not read any entries.
	BaseFile string
	// ExtraHosts is a slice of entries in the "hostname:ip" format.
	// Optional.
	ExtraHosts []string
	// ContainerIPs should contain the main container ipv4 and ipv6 if available
	// with the container name and host name as names set.
	// Optional.
	ContainerIPs HostEntries
	// HostContainersInternalIP is the IP for the host.containers.internal entry.
	// Optional.
	HostContainersInternalIP string
	// TargetFile where the hosts are written to.
	TargetFile string
}

// New will create a new hosts file and write this to the target file.
// This function does not prevent any kind of concurrency problems, it is
// the callers responsibility to avoid concurrent writes to this file.
// The extraHosts are written first, then the hosts from the file baseFile and the
// containerIps. The container ip entry is only added when the name was not already
// added before.
func New(params *Params) error {
	if err := newHost(params); err != nil {
		return fmt.Errorf("failed to create new hosts file: %w", err)
	}
	return nil
}

// Add adds the given entries to the hosts file, entries are only added if
// they are not already present.
// Add is not atomic because it will keep the current file inode. This is
// required to keep bind mounts for containers working.
func Add(file string, entries HostEntries) error {
	if err := add(file, entries); err != nil {
		return fmt.Errorf("failed to add entries to hosts file: %w", err)
	}
	return nil
}

// AddIfExists will add the given entries only if one of the existsEntries
// is in the hosts file. This API is required for podman network connect.
// Since we want to add the same host name for each network ip we want to
// add duplicates and the normal Add() call prevents us from doing so.
// However since we also do not want to overwrite potential entries that
// were added by users manually we first have to check if there are the
// current expected entries in the file. Note that this will only check
// for one match not all. It will also only check that the ip and one of
// the hostnames match like Remove().
func AddIfExists(file string, existsEntries, newEntries HostEntries) error {
	if err := addIfExists(file, existsEntries, newEntries); err != nil {
		return fmt.Errorf("failed to add entries to hosts file: %w", err)
	}
	return nil
}

// Remove will remove the given entries from the file. An entry will be
// removed when the ip and at least one name matches. Not all names have
// to match. If the given entries are not present in the file no error is
// returned.
// Remove is not atomic because it will keep the current file inode. This is
// required to keep bind mounts for containers working.
func Remove(file string, entries HostEntries) error {
	if err := remove(file, entries); err != nil {
		return fmt.Errorf("failed to remove entries from hosts file: %w", err)
	}
	return nil
}

// new see comment on New().
func newHost(params *Params) error {
	entries, err := parseExtraHosts(params.ExtraHosts, params.HostContainersInternalIP)
	if err != nil {
		return err
	}
	entries2, err := parseHostsFile(params.BaseFile)
	if err != nil {
		return err
	}
	entries = append(entries, entries2...)

	// preallocate the slice with enough space for the 3 special entries below
	containerIPs := make(HostEntries, 0, len(params.ContainerIPs)+3)

	// if localhost was not added we add it
	// https://github.com/containers/podman/issues/11411
	lh := []string{localhost}
	l1 := HostEntry{IP: "127.0.0.1", Names: lh}
	l2 := HostEntry{IP: "::1", Names: lh}
	containerIPs = append(containerIPs, l1, l2)
	if params.HostContainersInternalIP != "" {
		e := HostEntry{IP: params.HostContainersInternalIP, Names: []string{HostContainersInternal, hostDockerInternal}}
		containerIPs = append(containerIPs, e)
	}
	containerIPs = append(containerIPs, params.ContainerIPs...)

	return writeHostFile(params.TargetFile, entries, containerIPs)
}

// add see comment on Add().
func add(file string, entries HostEntries) error {
	currentEntries, err := parseHostsFile(file)
	if err != nil {
		return err
	}

	names := make(map[string]struct{})
	for _, entry := range currentEntries {
		for _, name := range entry.Names {
			names[name] = struct{}{}
		}
	}

	// open file in append mode since we only add, we do not have to write existing entries again
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	return addEntriesIfNotExists(f, entries, names)
}

// addIfExists see comment on AddIfExists().
func addIfExists(file string, existsEntries, newEntries HostEntries) error {
	// special case when there are no existing entries do a normal add
	// this can happen when we connect a network which was not connected
	// to any other networks before
	if len(existsEntries) == 0 {
		return add(file, newEntries)
	}

	currentEntries, err := parseHostsFile(file)
	if err != nil {
		return err
	}

	for _, entry := range currentEntries {
		if !checkIfEntryExists(entry, existsEntries) {
			// keep looking for existing entries
			continue
		}
		// if we have a matching existing entry add the new entries
		// open file in append mode since we only add, we do not have to write existing entries again
		f, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()

		for _, e := range newEntries {
			if _, err = f.WriteString(formatLine(e.IP, e.Names)); err != nil {
				return err
			}
		}
		return nil
	}
	// no match found is no error
	return nil
}

// remove see comment on Remove().
func remove(file string, entries HostEntries) error {
	currentEntries, err := parseHostsFile(file)
	if err != nil {
		return err
	}

	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, entry := range currentEntries {
		if checkIfEntryExists(entry, entries) {
			continue
		}
		if _, err = f.WriteString(formatLine(entry.IP, entry.Names)); err != nil {
			return err
		}
	}
	return nil
}

func checkIfEntryExists(current HostEntry, entries HostEntries) bool {
	//  check if the current entry equals one of the given entries
	for _, rm := range entries {
		if current.IP == rm.IP {
			// it is enough if one of the names match, in this case we remove the full entry
			for _, name := range current.Names {
				if slices.Contains(rm.Names, name) {
					return true
				}
			}
		}
	}
	return false
}

// parseExtraHosts converts a slice of "name1;name2;name3:ip" string to entries.
// Each entry can contain one or more hostnames separated by semicolons and an IP address separated by a colon.
// Because podman and buildah both store the extra hosts in this format,
// we convert it here instead of having to do this on the caller side.
func parseExtraHosts(extraHosts []string, hostContainersInternalIP string) (HostEntries, error) {
	entries := make(HostEntries, 0, len(extraHosts))
	for _, entry := range extraHosts {
		namesString, ip, ok := strings.Cut(entry, ":")
		if !ok {
			return nil, fmt.Errorf("unable to parse host entry %q: incorrect format", entry)
		}
		if namesString == "" {
			return nil, fmt.Errorf("hostname in host entry %q is empty", entry)
		}
		if ip == "" {
			return nil, fmt.Errorf("IP address in host entry %q is empty", entry)
		}
		if ip == HostGateway {
			if hostContainersInternalIP == "" {
				return nil, fmt.Errorf("unable to replace %q of host entry %q: host containers internal IP address is empty", HostGateway, entry)
			}
			ip = hostContainersInternalIP
		}
		names := strings.Split(namesString, ";")
		e := HostEntry{IP: ip, Names: names}
		entries = append(entries, e)
	}
	return entries, nil
}

// parseHostsFile parses a given host file and returns all entries in it.
// Note that this will remove all comments and spaces.
func parseHostsFile(file string) (HostEntries, error) {
	// empty file is valid, in this case we skip adding entries from the file
	if file == "" {
		return nil, nil
	}

	f, err := os.Open(file)
	if err != nil {
		// do not error when the default hosts file does not exists
		// https://github.com/containers/podman/issues/12667
		if errors.Is(err, os.ErrNotExist) && file == config.DefaultHostsFile {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	entries := HostEntries{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// split of the comments
		line := scanner.Text()
		if c := strings.IndexByte(line, '#'); c != -1 {
			line = line[:c]
		}
		fields := strings.Fields(line)
		// if we only have a ip without names we skip it
		if len(fields) < 2 {
			continue
		}

		e := HostEntry{IP: fields[0], Names: fields[1:]}
		entries = append(entries, e)
	}

	return entries, scanner.Err()
}

// writeHostFile write the entries to the given file.
func writeHostFile(file string, userEntries, containerIPs HostEntries) error {
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()

	names := make(map[string]struct{})
	for _, entry := range userEntries {
		for _, name := range entry.Names {
			names[name] = struct{}{}
		}
		if _, err = f.WriteString(formatLine(entry.IP, entry.Names)); err != nil {
			return err
		}
	}

	return addEntriesIfNotExists(f, containerIPs, names)
}

// addEntriesIfNotExists only adds the entries for names that are not already
// in the hosts file, otherwise we start overwriting user entries.
func addEntriesIfNotExists(f io.StringWriter, containerIPs HostEntries, names map[string]struct{}) error {
	for _, entry := range containerIPs {
		freeNames := make([]string, 0, len(entry.Names))
		for _, name := range entry.Names {
			if _, ok := names[name]; !ok {
				freeNames = append(freeNames, name)
			}
		}
		if len(freeNames) > 0 {
			if _, err := f.WriteString(formatLine(entry.IP, freeNames)); err != nil {
				return err
			}
		}
	}
	return nil
}

// formatLine converts the given ip and names to a valid hosts line.
// The returned string includes the newline.
func formatLine(ip string, names []string) string {
	return ip + "\t" + strings.Join(names, " ") + "\n"
}
