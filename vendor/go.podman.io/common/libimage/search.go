//go:build !remote

package libimage

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libimage/filter"
	registryTransport "go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/pkg/sysregistriesv2"
	"go.podman.io/image/v5/transports/alltransports"
	"go.podman.io/image/v5/types"
	"golang.org/x/sync/semaphore"
)

const (
	searchTruncLength = 44
	searchMaxQueries  = 25
	// Let's follow Firefox by limiting parallel downloads to 6.  We do the
	// same when pulling images in c/image.
	searchMaxParallel = int64(6)
)

// SearchResult is holding image-search related data.
type SearchResult struct {
	// Index is the image index (e.g., "docker.io" or "quay.io")
	Index string
	// Name is the canonical name of the image (e.g., "docker.io/library/alpine").
	Name string
	// Description of the image.
	Description string
	// Stars is the number of stars of the image.
	Stars int
	// Official indicates if it's an official image.
	Official string
	// Automated indicates if the image was created by an automated build.
	Automated string
	// Tag is the image tag
	Tag string
}

// SearchOptions customize searching images.
type SearchOptions struct {
	// Filter allows to filter the results.
	Filter filter.SearchFilter
	// Limit limits the number of queries per index (default: 25). Must be
	// greater than 0 to overwrite the default value.
	Limit int
	// NoTrunc avoids the output to be truncated.
	NoTrunc bool
	// Authfile is the path to the authentication file.
	Authfile string
	// Path to the certificates directory.
	CertDirPath string
	// Username to use when authenticating at a container registry.
	Username string
	// Password to use when authenticating at a container registry.
	Password string
	// Credentials is an alternative way to specify credentials in format
	// "username[:password]".  Cannot be used in combination with
	// Username/Password.
	Credentials string
	// IdentityToken is used to authenticate the user and get
	// an access token for the registry.
	IdentityToken string `json:"identitytoken,omitempty"`
	// InsecureSkipTLSVerify allows to skip TLS verification.
	InsecureSkipTLSVerify types.OptionalBool
	// ListTags returns the search result with available tags
	ListTags bool
	// Registries to search if the specified term does not include a
	// registry.  If set, the unqualified-search registries in
	// containers-registries.conf(5) are ignored.
	Registries []string
}

// Search searches term.  If term includes a registry, only this registry will
// be used for searching.  Otherwise, the unqualified-search registries in
// containers-registries.conf(5) or the ones specified in the options will be
// used.
func (r *Runtime) Search(ctx context.Context, term string, options *SearchOptions) ([]SearchResult, error) {
	if options == nil {
		options = &SearchOptions{}
	}

	var searchRegistries []string

	// Try to extract a registry from the specified search term.  We
	// consider everything before the first slash to be the registry.  Note
	// that we cannot use the reference parser from the containers/image
	// library as the search term may container arbitrary input such as
	// wildcards.  See bugzilla.redhat.com/show_bug.cgi?id=1846629.
	perhapsRegistry, perhapsTerm, ok := strings.Cut(term, "/")
	switch {
	case ok:
		searchRegistries = []string{perhapsRegistry}
		term = perhapsTerm
	case len(options.Registries) > 0:
		searchRegistries = options.Registries
	default:
		regs, err := sysregistriesv2.UnqualifiedSearchRegistries(r.systemContextCopy())
		if err != nil {
			return nil, err
		}
		searchRegistries = regs
	}

	logrus.Debugf("Searching images matching term %s at the following registries %s", term, searchRegistries)

	// searchOutputData is used as a return value for searching in parallel.
	type searchOutputData struct {
		data []SearchResult
		err  error
	}

	sem := semaphore.NewWeighted(searchMaxParallel)
	wg := sync.WaitGroup{}
	wg.Add(len(searchRegistries))
	data := make([]searchOutputData, len(searchRegistries))

	for i := range searchRegistries {
		if err := sem.Acquire(ctx, 1); err != nil {
			return nil, err
		}
		index := i
		go func() {
			defer sem.Release(1)
			defer wg.Done()
			searchOutput, err := r.searchImageInRegistry(ctx, term, searchRegistries[index], options)
			data[index] = searchOutputData{data: searchOutput, err: err}
		}()
	}

	wg.Wait()
	results := []SearchResult{}
	var multiErr error
	for _, d := range data {
		if d.err != nil {
			multiErr = multierror.Append(multiErr, d.err)
			continue
		}
		results = append(results, d.data...)
	}

	// Optimistically assume that one successfully searched registry
	// includes what the user is looking for.
	if len(results) > 0 {
		return results, nil
	}
	return results, multiErr
}

func (r *Runtime) searchImageInRegistry(ctx context.Context, term, registry string, options *SearchOptions) ([]SearchResult, error) {
	// Max number of queries by default is 25
	limit := searchMaxQueries
	if options.Limit > 0 {
		limit = options.Limit
	}

	sys := r.systemContextCopy()
	if options.InsecureSkipTLSVerify != types.OptionalBoolUndefined {
		sys.DockerInsecureSkipTLSVerify = options.InsecureSkipTLSVerify
	}

	if options.Authfile != "" {
		sys.AuthFilePath = options.Authfile
	}

	if options.CertDirPath != "" {
		sys.DockerCertPath = options.CertDirPath
	}

	dockerAuthConfig, err := getDockerAuthConfig(options.Username, options.Password, options.Credentials, options.IdentityToken)
	if err != nil {
		return nil, err
	}
	if dockerAuthConfig != nil {
		sys.DockerAuthConfig = dockerAuthConfig
	}

	if options.ListTags {
		results, err := searchRepositoryTags(ctx, sys, registry, term, options)
		if err != nil {
			return []SearchResult{}, err
		}
		return results, nil
	}

	results, err := registryTransport.SearchRegistry(ctx, sys, registry, term, limit)
	if err != nil {
		return []SearchResult{}, err
	}
	index := registry
	arr := strings.Split(registry, ".")
	if len(arr) > 2 {
		index = strings.Join(arr[len(arr)-2:], ".")
	}

	// limit is the number of results to output
	// if the total number of results is less than the limit, output all
	// if the limit has been set by the user, output those number of queries
	limit = min(len(results), searchMaxQueries)
	if options.Limit != 0 {
		limit = min(len(results), options.Limit)
	}

	paramsArr := []SearchResult{}
	for i := range limit {
		// Check whether query matches filters
		if !filterMatchesAutomatedFilter(&options.Filter, results[i]) || !filterMatchesOfficialFilter(&options.Filter, results[i]) || !filterMatchesStarFilter(&options.Filter, results[i]) {
			continue
		}
		official := ""
		if results[i].IsOfficial {
			official = "[OK]"
		}
		automated := ""
		if results[i].IsAutomated {
			automated = "[OK]"
		}
		description := strings.ReplaceAll(results[i].Description, "\n", " ")
		if len(description) > 44 && !options.NoTrunc {
			description = description[:searchTruncLength] + "..."
		}
		name := registry + "/" + results[i].Name
		if index == "docker.io" && !strings.Contains(results[i].Name, "/") {
			name = index + "/library/" + results[i].Name
		}
		params := SearchResult{
			Index:       registry,
			Name:        name,
			Description: description,
			Official:    official,
			Automated:   automated,
			Stars:       results[i].StarCount,
		}
		paramsArr = append(paramsArr, params)
	}
	return paramsArr, nil
}

func searchRepositoryTags(ctx context.Context, sys *types.SystemContext, registry, term string, options *SearchOptions) ([]SearchResult, error) {
	dockerPrefix := "docker://"
	imageRef, err := alltransports.ParseImageName(fmt.Sprintf("%s/%s", registry, term))
	if err == nil && imageRef.Transport().Name() != registryTransport.Transport.Name() {
		return nil, fmt.Errorf("reference %q must be a docker reference", term)
	} else if err != nil {
		imageRef, err = alltransports.ParseImageName(fmt.Sprintf("%s%s", dockerPrefix, fmt.Sprintf("%s/%s", registry, term)))
		if err != nil {
			return nil, fmt.Errorf("reference %q must be a docker reference", term)
		}
	}
	tags, err := registryTransport.GetRepositoryTags(ctx, sys, imageRef)
	if err != nil {
		return nil, fmt.Errorf("getting repository tags: %v", err)
	}
	limit := min(len(tags), searchMaxQueries)
	if options.Limit != 0 {
		limit = min(len(tags), options.Limit)
	}
	paramsArr := []SearchResult{}
	for i := range limit {
		params := SearchResult{
			Name:  imageRef.DockerReference().Name(),
			Tag:   tags[i],
			Index: registry,
		}
		paramsArr = append(paramsArr, params)
	}
	return paramsArr, nil
}

func filterMatchesStarFilter(f *filter.SearchFilter, result registryTransport.SearchResult) bool {
	return result.StarCount >= f.Stars
}

func filterMatchesAutomatedFilter(f *filter.SearchFilter, result registryTransport.SearchResult) bool {
	if f.IsAutomated != types.OptionalBoolUndefined {
		return result.IsAutomated == (f.IsAutomated == types.OptionalBoolTrue)
	}
	return true
}

func filterMatchesOfficialFilter(f *filter.SearchFilter, result registryTransport.SearchResult) bool {
	if f.IsOfficial != types.OptionalBoolUndefined {
		return result.IsOfficial == (f.IsOfficial == types.OptionalBoolTrue)
	}
	return true
}
