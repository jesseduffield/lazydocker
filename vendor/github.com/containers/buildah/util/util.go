package util

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"

	"github.com/containers/buildah/define"
	"github.com/docker/distribution/registry/api/errcode"
	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libimage"
	"go.podman.io/common/pkg/config"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/pkg/shortnames"
	"go.podman.io/image/v5/signature"
	"go.podman.io/image/v5/transports/alltransports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
)

const (
	minimumTruncatedIDLength = 3
	// DefaultTransport is a prefix that we apply to an image name if we
	// can't find one in the local Store, in order to generate a source
	// reference for the image that we can then copy to the local Store.
	DefaultTransport = "docker://"
)

// RegistryDefaultPathPrefix contains a per-registry listing of default prefixes
// to prepend to image names that only contain a single path component.
var RegistryDefaultPathPrefix = map[string]string{
	"index.docker.io": "library",
	"docker.io":       "library",
}

// StringInSlice is deprecated, use slices.Contains
func StringInSlice(s string, slice []string) bool {
	return slices.Contains(slice, s)
}

// resolveName checks if name is a valid image name, and if that name doesn't
// include a domain portion, returns a list of the names which it might
// correspond to in the set of configured registries, and the transport used to
// pull the image.
//
// The returned image names never include a transport: prefix, and if transport != "",
// (transport, image) should be a valid input to alltransports.ParseImageName.
// transport == "" indicates that image that already exists in a local storage,
// and the name is valid for store.Image() / storage.Transport.ParseStoreReference().
//
// NOTE: The "list of search registries is empty" check does not count blocked registries,
// and neither the implied "localhost" nor a possible firstRegistry are counted
func resolveName(name string, sc *types.SystemContext, store storage.Store) ([]string, string, error) {
	if name == "" {
		return nil, "", nil
	}

	// Maybe it's a truncated image ID.  Don't prepend a registry name, then.
	if len(name) >= minimumTruncatedIDLength {
		if img, err := store.Image(name); err == nil && img != nil && strings.HasPrefix(img.ID, name) {
			// It's a truncated version of the ID of an image that's present in local storage;
			// we need only expand the ID.
			return []string{img.ID}, "", nil
		}
	}
	// If we're referring to an image by digest, it *must* be local and we
	// should not have any fall through/back logic.
	if strings.HasPrefix(name, "sha256:") {
		d, err := digest.Parse(name)
		if err != nil {
			return nil, "", err
		}
		img, err := store.Image(d.Encoded())
		if err != nil {
			return nil, "", err
		}
		return []string{img.ID}, "", nil
	}

	// Transports are not supported for local image look ups.
	srcRef, err := alltransports.ParseImageName(name)
	if err == nil {
		return []string{srcRef.StringWithinTransport()}, srcRef.Transport().Name(), nil
	}

	var candidates []string
	// Local short-name resolution.
	namedCandidates, err := shortnames.ResolveLocally(sc, name)
	if err != nil {
		return nil, "", err
	}
	for _, named := range namedCandidates {
		candidates = append(candidates, named.String())
	}

	return candidates, DefaultTransport, nil
}

// ExpandNames takes unqualified names, parses them as image names, and returns
// the fully expanded result, including a tag.  Names which don't include a registry
// name will be marked for the most-preferred registry (i.e., the first one in our
// configuration).
func ExpandNames(names []string, systemContext *types.SystemContext, store storage.Store) ([]string, error) {
	expanded := make([]string, 0, len(names))
	for _, n := range names {
		var name reference.Named
		nameList, _, err := resolveName(n, systemContext, store)
		if err != nil {
			return nil, fmt.Errorf("parsing name %q: %w", n, err)
		}
		if len(nameList) == 0 {
			named, err := reference.ParseNormalizedNamed(n)
			if err != nil {
				return nil, fmt.Errorf("parsing name %q: %w", n, err)
			}
			name = named
		} else {
			named, err := reference.ParseNormalizedNamed(nameList[0])
			if err != nil {
				return nil, fmt.Errorf("parsing name %q: %w", nameList[0], err)
			}
			name = named
		}
		name = reference.TagNameOnly(name)
		expanded = append(expanded, name.String())
	}
	return expanded, nil
}

// FindImage locates the locally-stored image which corresponds to a given
// name.  Please note that the second argument has been deprecated and has no
// effect anymore.
func FindImage(store storage.Store, _ string, systemContext *types.SystemContext, image string) (types.ImageReference, *storage.Image, error) {
	runtime, err := libimage.RuntimeFromStore(store, &libimage.RuntimeOptions{SystemContext: systemContext})
	if err != nil {
		return nil, nil, err
	}

	localImage, _, err := runtime.LookupImage(image, nil)
	if err != nil {
		return nil, nil, err
	}
	ref, err := localImage.StorageReference()
	if err != nil {
		return nil, nil, err
	}

	return ref, localImage.StorageImage(), nil
}

// resolveNameToReferences tries to create a list of possible references
// (including their transports) from the provided image name.
func ResolveNameToReferences(
	store storage.Store,
	systemContext *types.SystemContext,
	image string,
) (refs []types.ImageReference, err error) {
	names, transport, err := resolveName(image, systemContext, store)
	if err != nil {
		return nil, fmt.Errorf("parsing name %q: %w", image, err)
	}

	if transport != DefaultTransport {
		transport += ":"
	}

	for _, name := range names {
		ref, err := alltransports.ParseImageName(transport + name)
		if err != nil {
			logrus.Debugf("error parsing reference to image %q: %v", name, err)
			continue
		}
		refs = append(refs, ref)
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("locating images with names %v", names)
	}
	return refs, nil
}

// AddImageNames adds the specified names to the specified image.  Please note
// that the second argument has been deprecated and has no effect anymore.
func AddImageNames(store storage.Store, _ string, systemContext *types.SystemContext, image *storage.Image, addNames []string) error {
	runtime, err := libimage.RuntimeFromStore(store, &libimage.RuntimeOptions{SystemContext: systemContext})
	if err != nil {
		return err
	}

	localImage, _, err := runtime.LookupImage(image.ID, nil)
	if err != nil {
		return err
	}

	for _, tag := range addNames {
		if err := localImage.Tag(tag); err != nil {
			return fmt.Errorf("tagging image %s: %w", image.ID, err)
		}
	}

	return nil
}

// GetFailureCause checks the type of the error "err" and returns a new
// error message that reflects the reason of the failure.
// In case err type is not a familiar one the error "defaultError" is returned.
func GetFailureCause(err, defaultError error) error {
	switch nErr := err.(type) {
	case errcode.Errors:
		return err
	case errcode.Error, *url.Error:
		return nErr
	default:
		return defaultError
	}
}

// WriteError writes `lastError` into `w` if not nil and return the next error `err`
func WriteError(w io.Writer, err error, lastError error) error {
	if lastError != nil {
		fmt.Fprintln(w, lastError)
	}
	return err
}

// Runtime is the default command to use to run the container.
func Runtime() string {
	runtime := os.Getenv("BUILDAH_RUNTIME")
	if runtime != "" {
		return runtime
	}

	conf, err := config.Default()
	if err != nil {
		logrus.Warnf("Error loading default container config when searching for local runtime: %v", err)
		return define.DefaultRuntime
	}
	return conf.Engine.OCIRuntime
}

// GetContainerIDs uses ID mappings to compute the container-level IDs that will
// correspond to a UID/GID pair on the host.
func GetContainerIDs(uidmap, gidmap []specs.LinuxIDMapping, uid, gid uint32) (uint32, uint32, error) {
	uidMapped := true
	for _, m := range uidmap {
		uidMapped = false
		if uid >= m.HostID && uid < m.HostID+m.Size {
			uid = (uid - m.HostID) + m.ContainerID
			uidMapped = true
			break
		}
	}
	if !uidMapped {
		return 0, 0, fmt.Errorf("container uses ID mappings (%#v), but doesn't map UID %d", uidmap, uid)
	}
	gidMapped := true
	for _, m := range gidmap {
		gidMapped = false
		if gid >= m.HostID && gid < m.HostID+m.Size {
			gid = (gid - m.HostID) + m.ContainerID
			gidMapped = true
			break
		}
	}
	if !gidMapped {
		return 0, 0, fmt.Errorf("container uses ID mappings (%#v), but doesn't map GID %d", gidmap, gid)
	}
	return uid, gid, nil
}

// GetHostIDs uses ID mappings to compute the host-level IDs that will
// correspond to a UID/GID pair in the container.
func GetHostIDs(uidmap, gidmap []specs.LinuxIDMapping, uid, gid uint32) (uint32, uint32, error) {
	uidMapped := true
	for _, m := range uidmap {
		uidMapped = false
		if uid >= m.ContainerID && uid < m.ContainerID+m.Size {
			uid = (uid - m.ContainerID) + m.HostID
			uidMapped = true
			break
		}
	}
	if !uidMapped {
		return 0, 0, fmt.Errorf("container uses ID mappings (%#v), but doesn't map UID %d", uidmap, uid)
	}
	gidMapped := true
	for _, m := range gidmap {
		gidMapped = false
		if gid >= m.ContainerID && gid < m.ContainerID+m.Size {
			gid = (gid - m.ContainerID) + m.HostID
			gidMapped = true
			break
		}
	}
	if !gidMapped {
		return 0, 0, fmt.Errorf("container uses ID mappings (%#v), but doesn't map GID %d", gidmap, gid)
	}
	return uid, gid, nil
}

// GetHostRootIDs uses ID mappings in spec to compute the host-level IDs that will
// correspond to UID/GID 0/0 in the container.
func GetHostRootIDs(spec *specs.Spec) (uint32, uint32, error) {
	if spec == nil || spec.Linux == nil {
		return 0, 0, nil
	}
	return GetHostIDs(spec.Linux.UIDMappings, spec.Linux.GIDMappings, 0, 0)
}

// GetPolicyContext sets up, initializes and returns a new context for the specified policy
func GetPolicyContext(ctx *types.SystemContext) (*signature.PolicyContext, error) {
	policy, err := signature.DefaultPolicy(ctx)
	if err != nil {
		return nil, err
	}

	policyContext, err := signature.NewPolicyContext(policy)
	if err != nil {
		return nil, err
	}
	return policyContext, nil
}

// logIfNotErrno logs the error message unless err is either nil or one of the
// listed syscall.Errno values.  It returns true if it logged an error.
func logIfNotErrno(err error, what string, ignores ...syscall.Errno) (logged bool) {
	if err == nil {
		return false
	}
	if errno, ok := err.(syscall.Errno); ok && slices.Contains(ignores, errno) {
		return false
	}
	logrus.Error(what)
	return true
}

// LogIfNotRetryable logs "what" if err is set and is not an EINTR or EAGAIN
// syscall.Errno.  Returns "true" if we can continue.
func LogIfNotRetryable(err error, what string) (retry bool) {
	return !logIfNotErrno(err, what, syscall.EINTR, syscall.EAGAIN)
}

// LogIfUnexpectedWhileDraining logs "what" if err is set and is not an EINTR
// or EAGAIN or EIO syscall.Errno.
func LogIfUnexpectedWhileDraining(err error, what string) {
	logIfNotErrno(err, what, syscall.EINTR, syscall.EAGAIN, syscall.EIO)
}

// TruncateString trims the given string to the provided maximum amount of
// characters and shortens it with `...`.
func TruncateString(str string, to int) string {
	newStr := str
	if len(str) > to {
		const tr = "..."
		if to > len(tr) {
			to -= len(tr)
		}
		newStr = str[0:to] + tr
	}
	return newStr
}

// fileExistsAndNotADir - Check to see if a file exists
// and that it is not a directory.
func fileExistsAndNotADir(path string) (bool, error) {
	file, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return !file.IsDir(), nil
}

// FindLocalRuntime find the local runtime of the
// system searching through the config file for
// possible locations.
func FindLocalRuntime(runtime string) string {
	var localRuntime string
	conf, err := config.Default()
	if err != nil {
		logrus.Debugf("Error loading container config when searching for local runtime.")
		return localRuntime
	}
	for _, val := range conf.Engine.OCIRuntimes[runtime] {
		exists, err := fileExistsAndNotADir(val)
		if err != nil {
			logrus.Errorf("Failed to determine if file exists and is not a directory: %v", err)
		}
		if exists {
			localRuntime = val
			break
		}
	}
	return localRuntime
}

// MergeEnv merges two lists of environment variables, avoiding duplicates.
func MergeEnv(defaults, overrides []string) []string {
	s := make([]string, 0, len(defaults)+len(overrides))
	index := make(map[string]int)
	for _, envSpec := range append(defaults, overrides...) {
		envVar := strings.SplitN(envSpec, "=", 2)
		if i, ok := index[envVar[0]]; ok {
			s[i] = envSpec
			continue
		}
		s = append(s, envSpec)
		index[envVar[0]] = len(s) - 1
	}
	return s
}

type byDestination []specs.Mount

func (m byDestination) Len() int {
	return len(m)
}

func (m byDestination) Less(i, j int) bool {
	iparts, jparts := m.parts(i), m.parts(j)
	switch {
	case iparts < jparts:
		return true
	case iparts > jparts:
		return false
	}
	return filepath.Clean(m[i].Destination) < filepath.Clean(m[j].Destination)
}

func (m byDestination) Swap(i, j int) {
	m[i], m[j] = m[j], m[i]
}

func (m byDestination) parts(i int) int {
	return strings.Count(filepath.Clean(m[i].Destination), string(os.PathSeparator))
}

func SortMounts(m []specs.Mount) []specs.Mount {
	sort.Stable(byDestination(m))
	return m
}

func VerifyTagName(imageSpec string) (types.ImageReference, error) {
	ref, err := alltransports.ParseImageName(imageSpec)
	if err != nil {
		if ref, err = alltransports.ParseImageName(DefaultTransport + imageSpec); err != nil {
			return nil, err
		}
	}
	return ref, nil
}
