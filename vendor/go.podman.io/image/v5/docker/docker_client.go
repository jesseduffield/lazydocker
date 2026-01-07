package docker

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/distribution/registry/api/errcode"
	v2 "github.com/docker/distribution/registry/api/v2"
	"github.com/docker/go-connections/tlsconfig"
	digest "github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/iolimits"
	"go.podman.io/image/v5/internal/multierr"
	"go.podman.io/image/v5/internal/set"
	"go.podman.io/image/v5/internal/useragent"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/pkg/docker/config"
	"go.podman.io/image/v5/pkg/sysregistriesv2"
	"go.podman.io/image/v5/pkg/tlsclientconfig"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/homedir"
)

const (
	dockerHostname   = "docker.io"
	dockerV1Hostname = "index.docker.io"
	dockerRegistry   = "registry-1.docker.io"

	resolvedPingV2URL       = "%s://%s/v2/"
	tagsPath                = "/v2/%s/tags/list"
	manifestPath            = "/v2/%s/manifests/%s"
	blobsPath               = "/v2/%s/blobs/%s"
	blobUploadPath          = "/v2/%s/blobs/uploads/"
	extensionsSignaturePath = "/extensions/v2/%s/signatures/%s"

	minimumTokenLifetimeSeconds = 60

	extensionSignatureSchemaVersion = 2        // extensionSignature.Version
	extensionSignatureTypeAtomic    = "atomic" // extensionSignature.Type

	backoffNumIterations = 5
	backoffInitialDelay  = 2 * time.Second
	backoffMaxDelay      = 60 * time.Second
)

type certPath struct {
	path     string
	absolute bool
}

var (
	homeCertDir     = filepath.FromSlash(".config/containers/certs.d")
	perHostCertDirs = []certPath{
		{path: etcDir + "/containers/certs.d", absolute: true},
		{path: etcDir + "/docker/certs.d", absolute: true},
	}
)

// extensionSignature and extensionSignatureList come from github.com/openshift/origin/pkg/dockerregistry/server/signaturedispatcher.go:
// signature represents a Docker image signature.
type extensionSignature struct {
	Version int    `json:"schemaVersion"` // Version specifies the schema version
	Name    string `json:"name"`          // Name must be in "sha256:<digest>@signatureName" format
	Type    string `json:"type"`          // Type is optional, of not set it will be defaulted to "AtomicImageV1"
	Content []byte `json:"content"`       // Content contains the signature
}

// signatureList represents list of Docker image signatures.
type extensionSignatureList struct {
	Signatures []extensionSignature `json:"signatures"`
}

// bearerToken records a cached token we can use to authenticate.
type bearerToken struct {
	token          string
	expirationTime time.Time
}

// dockerClient is configuration for dealing with a single container registry.
type dockerClient struct {
	// The following members are set by newDockerClient and do not change afterwards.
	sys       *types.SystemContext
	registry  string
	userAgent string

	// tlsClientConfig is setup by newDockerClient and will be used and updated
	// by detectProperties(). Callers can edit tlsClientConfig.InsecureSkipVerify in the meantime.
	tlsClientConfig *tls.Config
	// The following members are not set by newDockerClient and must be set by callers if needed.
	auth                   types.DockerAuthConfig
	registryToken          string
	signatureBase          lookasideStorageBase
	useSigstoreAttachments bool
	scope                  authScope

	// The following members are detected registry properties:
	// They are set after a successful detectProperties(), and never change afterwards.
	client             *http.Client
	scheme             string
	challenges         []challenge
	supportsSignatures bool

	// Private state for setupRequestAuth (key: string, value: bearerToken)
	tokenCache sync.Map
	// Private state for detectProperties:
	detectPropertiesOnce  sync.Once // detectPropertiesOnce is used to execute detectProperties() at most once.
	detectPropertiesError error     // detectPropertiesError caches the initial error.
	// Private state for logResponseWarnings
	reportedWarningsLock sync.Mutex
	reportedWarnings     *set.Set[string]
}

type authScope struct {
	resourceType string
	remoteName   string
	actions      string
}

// sendAuth determines whether we need authentication for v2 or v1 endpoint.
type sendAuth int

const (
	// v2 endpoint with authentication.
	v2Auth sendAuth = iota
	// v1 endpoint with authentication.
	// TODO: Get v1Auth working
	// v1Auth
	// no authentication, works for both v1 and v2.
	noAuth
)

// dockerCertDir returns a path to a directory to be consumed by tlsclientconfig.SetupCertificates() depending on ctx and hostPort.
func dockerCertDir(sys *types.SystemContext, hostPort string) (string, error) {
	if sys != nil && sys.DockerCertPath != "" {
		return sys.DockerCertPath, nil
	}
	if sys != nil && sys.DockerPerHostCertDirPath != "" {
		return filepath.Join(sys.DockerPerHostCertDirPath, hostPort), nil
	}

	var (
		hostCertDir     string
		fullCertDirPath string
	)

	for _, perHostCertDir := range append([]certPath{{path: filepath.Join(homedir.Get(), homeCertDir), absolute: false}}, perHostCertDirs...) {
		if sys != nil && sys.RootForImplicitAbsolutePaths != "" && perHostCertDir.absolute {
			hostCertDir = filepath.Join(sys.RootForImplicitAbsolutePaths, perHostCertDir.path)
		} else {
			hostCertDir = perHostCertDir.path
		}

		fullCertDirPath = filepath.Join(hostCertDir, hostPort)
		err := fileutils.Exists(fullCertDirPath)
		if err == nil {
			break
		}
		if os.IsNotExist(err) {
			continue
		}
		if os.IsPermission(err) {
			logrus.Debugf("error accessing certs directory due to permissions: %v", err)
			continue
		}
		return "", err
	}
	return fullCertDirPath, nil
}

// newDockerClientFromRef returns a new dockerClient instance for refHostname (a host a specified in the Docker image reference, not canonicalized to dockerRegistry)
// “write” specifies whether the client will be used for "write" access (in particular passed to lookaside.go:toplevelFromSection)
// signatureBase is always set in the return value
// The caller must call .Close() on the returned client when done.
func newDockerClientFromRef(sys *types.SystemContext, ref dockerReference, registryConfig *registryConfiguration, write bool, actions string) (*dockerClient, error) {
	auth, err := config.GetCredentialsForRef(sys, ref.ref)
	if err != nil {
		return nil, fmt.Errorf("getting username and password: %w", err)
	}

	sigBase, err := registryConfig.lookasideStorageBaseURL(ref, write)
	if err != nil {
		return nil, err
	}

	registry := reference.Domain(ref.ref)
	client, err := newDockerClient(sys, registry, ref.ref.Name())
	if err != nil {
		return nil, err
	}
	client.auth = auth
	if sys != nil {
		client.registryToken = sys.DockerBearerRegistryToken
	}
	client.signatureBase = sigBase
	client.useSigstoreAttachments = registryConfig.useSigstoreAttachments(ref)
	client.scope.resourceType = "repository"
	client.scope.actions = actions
	client.scope.remoteName = reference.Path(ref.ref)
	return client, nil
}

// newDockerClient returns a new dockerClient instance for the given registry
// and reference.  The reference is used to query the registry configuration
// and can either be a registry (e.g, "registry.com[:5000]"), a repository
// (e.g., "registry.com[:5000][/some/namespace]/repo").
// Please note that newDockerClient does not set all members of dockerClient
// (e.g., username and password); those must be set by callers if necessary.
// The caller must call .Close() on the returned client when done.
func newDockerClient(sys *types.SystemContext, registry, reference string) (*dockerClient, error) {
	hostName := registry
	if registry == dockerHostname {
		registry = dockerRegistry
	}
	tlsClientConfig := &tls.Config{
		// As of 2025-08, tlsconfig.ClientDefault() differs from Go 1.23 defaults only in CipherSuites;
		// so, limit us to only using that value. If go-connections/tlsconfig changes its policy, we
		// will want to consider that and make a decision whether to follow suit.
		// There is some chance that eventually the Go default will be to require TLS 1.3, and that point
		// we might want to drop the dependency on go-connections entirely.
		CipherSuites: tlsconfig.ClientDefault().CipherSuites,
	}

	// It is undefined whether the host[:port] string for dockerHostname should be dockerHostname or dockerRegistry,
	// because docker/docker does not read the certs.d subdirectory at all in that case.  We use the user-visible
	// dockerHostname here, because it is more symmetrical to read the configuration in that case as well, and because
	// generally the UI hides the existence of the different dockerRegistry.  But note that this behavior is
	// undocumented and may change if docker/docker changes.
	certDir, err := dockerCertDir(sys, hostName)
	if err != nil {
		return nil, err
	}
	if err := tlsclientconfig.SetupCertificates(certDir, tlsClientConfig); err != nil {
		return nil, err
	}

	// Check if TLS verification shall be skipped (default=false) which can
	// be specified in the sysregistriesv2 configuration.
	skipVerify := false
	reg, err := sysregistriesv2.FindRegistry(sys, reference)
	if err != nil {
		return nil, fmt.Errorf("loading registries: %w", err)
	}
	if reg != nil {
		if reg.Blocked {
			return nil, fmt.Errorf("registry %s is blocked in %s or %s", reg.Prefix, sysregistriesv2.ConfigPath(sys), sysregistriesv2.ConfigDirPath(sys))
		}
		skipVerify = reg.Insecure
	}
	tlsClientConfig.InsecureSkipVerify = skipVerify

	userAgent := useragent.DefaultUserAgent
	if sys != nil && sys.DockerRegistryUserAgent != "" {
		userAgent = sys.DockerRegistryUserAgent
	}

	return &dockerClient{
		sys:              sys,
		registry:         registry,
		userAgent:        userAgent,
		tlsClientConfig:  tlsClientConfig,
		reportedWarnings: set.New[string](),
	}, nil
}

// CheckAuth validates the credentials by attempting to log into the registry
// returns an error if an error occurred while making the http request or the status code received was 401
func CheckAuth(ctx context.Context, sys *types.SystemContext, username, password, registry string) error {
	client, err := newDockerClient(sys, registry, registry)
	if err != nil {
		return fmt.Errorf("creating new docker client: %w", err)
	}
	defer client.Close()
	client.auth = types.DockerAuthConfig{
		Username: username,
		Password: password,
	}

	resp, err := client.makeRequest(ctx, http.MethodGet, "/v2/", nil, nil, v2Auth, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := registryHTTPResponseToError(resp)
		if resp.StatusCode == http.StatusUnauthorized {
			err = ErrUnauthorizedForCredentials{Err: err}
		}
		return err
	}
	return nil
}

// SearchResult holds the information of each matching image
// It matches the output returned by the v1 endpoint
type SearchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// StarCount states the number of stars the image has
	StarCount int  `json:"star_count"`
	IsTrusted bool `json:"is_trusted"`
	// IsAutomated states whether the image is an automated build
	IsAutomated bool `json:"is_automated"`
	// IsOfficial states whether the image is an official build
	IsOfficial bool `json:"is_official"`
}

// SearchRegistry queries a registry for images that contain "image" in their name
// The limit is the max number of results desired
// Note: The limit value doesn't work with all registries
// for example registry.access.redhat.com returns all the results without limiting it to the limit value
func SearchRegistry(ctx context.Context, sys *types.SystemContext, registry, image string, limit int) ([]SearchResult, error) {
	type V2Results struct {
		// Repositories holds the results returned by the /v2/_catalog endpoint
		Repositories []string `json:"repositories"`
	}
	type V1Results struct {
		// Results holds the results returned by the /v1/search endpoint
		Results []SearchResult `json:"results"`
	}
	v1Res := &V1Results{}

	// Get credentials from authfile for the underlying hostname
	// We can't use GetCredentialsForRef here because we want to search the whole registry.
	auth, err := config.GetCredentials(sys, registry)
	if err != nil {
		return nil, fmt.Errorf("getting username and password: %w", err)
	}

	// The /v2/_catalog endpoint has been disabled for docker.io therefore
	// the call made to that endpoint will fail.  So using the v1 hostname
	// for docker.io for simplicity of implementation and the fact that it
	// returns search results.
	hostname := registry
	if registry == dockerHostname {
		hostname = dockerV1Hostname
		// A search term of library/foo does not find the library/foo image on the docker.io servers,
		// which is surprising - and that Docker is modifying the search term client-side this same way,
		// and it seems convenient to do the same thing.
		// Read more here: https://github.com/containers/image/pull/2133#issue-1928524334
		image = strings.TrimPrefix(image, "library/")
	}

	client, err := newDockerClient(sys, hostname, registry)
	if err != nil {
		return nil, fmt.Errorf("creating new docker client: %w", err)
	}
	defer client.Close()
	client.auth = auth
	if sys != nil {
		client.registryToken = sys.DockerBearerRegistryToken
	}

	// Only try the v1 search endpoint if the search query is not empty. If it is
	// empty skip to the v2 endpoint.
	if image != "" {
		// set up the query values for the v1 endpoint
		u := url.URL{
			Path: "/v1/search",
		}
		q := u.Query()
		q.Set("q", image)
		q.Set("n", strconv.Itoa(limit))
		u.RawQuery = q.Encode()

		logrus.Debugf("trying to talk to v1 search endpoint")
		resp, err := client.makeRequest(ctx, http.MethodGet, u.String(), nil, nil, noAuth, nil)
		if err != nil {
			logrus.Debugf("error getting search results from v1 endpoint %q: %v", registry, err)
		} else {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				logrus.Debugf("error getting search results from v1 endpoint %q: %v", registry, httpResponseToError(resp, ""))
			} else {
				if err := json.NewDecoder(resp.Body).Decode(v1Res); err != nil {
					return nil, err
				}
				return v1Res.Results, nil
			}
		}
	}

	logrus.Debugf("trying to talk to v2 search endpoint")
	searchRes := []SearchResult{}
	path := "/v2/_catalog"
	for len(searchRes) < limit {
		resp, err := client.makeRequest(ctx, http.MethodGet, path, nil, nil, v2Auth, nil)
		if err != nil {
			logrus.Debugf("error getting search results from v2 endpoint %q: %v", registry, err)
			return nil, fmt.Errorf("couldn't search registry %q: %w", registry, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			err := registryHTTPResponseToError(resp)
			logrus.Errorf("error getting search results from v2 endpoint %q: %v", registry, err)
			return nil, fmt.Errorf("couldn't search registry %q: %w", registry, err)
		}
		v2Res := &V2Results{}
		if err := json.NewDecoder(resp.Body).Decode(v2Res); err != nil {
			return nil, err
		}

		for _, repo := range v2Res.Repositories {
			if len(searchRes) == limit {
				break
			}
			if strings.Contains(repo, image) {
				res := SearchResult{
					Name: repo,
				}
				// bugzilla.redhat.com/show_bug.cgi?id=1976283
				// If we have a full match, make sure it's listed as the first result.
				// (Note there might be a full match we never see if we reach the result limit first.)
				if repo == image {
					searchRes = append([]SearchResult{res}, searchRes...)
				} else {
					searchRes = append(searchRes, res)
				}
			}
		}

		link := resp.Header.Get("Link")
		if link == "" {
			break
		}
		linkURLPart, _, _ := strings.Cut(link, ";")
		linkURL, err := url.Parse(strings.Trim(linkURLPart, "<>"))
		if err != nil {
			return searchRes, err
		}

		// can be relative or absolute, but we only want the path (and I
		// guess we're in trouble if it forwards to a new place...)
		path = linkURL.Path
		if linkURL.RawQuery != "" {
			path += "?"
			path += linkURL.RawQuery
		}
	}
	return searchRes, nil
}

// makeRequest creates and executes a http.Request with the specified parameters, adding authentication and TLS options for the Docker client.
// The host name and schema is taken from the client or autodetected, and the path is relative to it, i.e. the path usually starts with /v2/.
func (c *dockerClient) makeRequest(ctx context.Context, method, path string, headers map[string][]string, stream io.Reader, auth sendAuth, extraScope *authScope) (*http.Response, error) {
	if err := c.detectProperties(ctx); err != nil {
		return nil, err
	}

	requestURL, err := c.resolveRequestURL(path)
	if err != nil {
		return nil, err
	}
	return c.makeRequestToResolvedURL(ctx, method, requestURL, headers, stream, -1, auth, extraScope)
}

// resolveRequestURL turns a path for c.makeRequest into a full URL.
// Most users should call makeRequest directly, this exists basically to make the URL available for debug logs.
func (c *dockerClient) resolveRequestURL(path string) (*url.URL, error) {
	urlString := fmt.Sprintf("%s://%s%s", c.scheme, c.registry, path)
	res, err := url.Parse(urlString)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Checks if the auth headers in the response contain an indication of a failed
// authorization because of an "insufficient_scope" error. If that's the case,
// returns the required scope to be used for fetching a new token.
func needsRetryWithUpdatedScope(res *http.Response) (bool, *authScope) {
	if res.StatusCode == http.StatusUnauthorized {
		for challenge := range iterateAuthHeader(res.Header) {
			if challenge.Scheme == "bearer" {
				if errmsg, ok := challenge.Parameters["error"]; ok && errmsg == "insufficient_scope" {
					if scope, ok := challenge.Parameters["scope"]; ok && scope != "" {
						if newScope, err := parseAuthScope(scope); err == nil {
							return true, newScope
						} else {
							logrus.WithFields(logrus.Fields{
								"error":     err,
								"scope":     scope,
								"challenge": challenge,
							}).Error("Failed to parse the authentication scope from the given challenge")
						}
					}
				}
			}
		}
	}
	return false, nil
}

// parseRetryAfter determines the delay required by the "Retry-After" header in res and returns it,
// silently falling back to fallbackDelay if the header is missing or invalid.
func parseRetryAfter(res *http.Response, fallbackDelay time.Duration) time.Duration {
	after := res.Header.Get("Retry-After")
	if after == "" {
		return fallbackDelay
	}
	logrus.Debugf("Detected 'Retry-After' header %q", after)
	// First, check if we have a numerical value.
	if num, err := strconv.ParseInt(after, 10, 64); err == nil {
		return time.Duration(num) * time.Second
	}
	// Second, check if we have an HTTP date.
	if t, err := http.ParseTime(after); err == nil {
		// If the delta between the date and now is positive, use it.
		delta := time.Until(t)
		if delta > 0 {
			return delta
		}
		logrus.Debugf("Retry-After date in the past, ignoring it")
		return fallbackDelay
	}
	logrus.Debugf("Invalid Retry-After format, ignoring it")
	return fallbackDelay
}

// makeRequestToResolvedURL creates and executes a http.Request with the specified parameters, adding authentication and TLS options for the Docker client.
// streamLen, if not -1, specifies the length of the data expected on stream.
// makeRequest should generally be preferred.
// In case of an HTTP 429 status code in the response, it may automatically retry a few times.
// TODO(runcom): too many arguments here, use a struct
func (c *dockerClient) makeRequestToResolvedURL(ctx context.Context, method string, requestURL *url.URL, headers map[string][]string, stream io.Reader, streamLen int64, auth sendAuth, extraScope *authScope) (*http.Response, error) {
	delay := backoffInitialDelay
	attempts := 0
	for {
		res, err := c.makeRequestToResolvedURLOnce(ctx, method, requestURL, headers, stream, streamLen, auth, extraScope)
		if err != nil {
			return nil, err
		}
		attempts++

		// By default we use pre-defined scopes per operation. In
		// certain cases, this can fail when our authentication is
		// insufficient, then we might be getting an error back with a
		// Www-Authenticate Header indicating an insufficient scope.
		//
		// Check for that and update the client challenges to retry after
		// requesting a new token
		//
		// We only try this on the first attempt, to not overload an
		// already struggling server.
		// We also cannot retry with a body (stream != nil) as stream
		// was already read
		if attempts == 1 && stream == nil && auth != noAuth {
			if retry, newScope := needsRetryWithUpdatedScope(res); retry {
				logrus.Debug("Detected insufficient_scope error, will retry request with updated scope")
				res.Body.Close()
				// Note: This retry ignores extraScope. That’s, strictly speaking, incorrect, but we don’t currently
				// expect the insufficient_scope errors to happen for those callers. If that changes, we can add support
				// for more than one extra scope.
				res, err = c.makeRequestToResolvedURLOnce(ctx, method, requestURL, headers, stream, streamLen, auth, newScope)
				if err != nil {
					return nil, err
				}
				extraScope = newScope
			}
		}

		if res.StatusCode != http.StatusTooManyRequests || // Only retry on StatusTooManyRequests, success or other failure is returned to caller immediately
			stream != nil || // We can't retry with a body (which is not restartable in the general case)
			attempts == backoffNumIterations {
			return res, nil
		}
		// close response body before retry or context done
		res.Body.Close()

		delay = min(parseRetryAfter(res, delay), backoffMaxDelay)
		logrus.Debugf("Too many requests to %s: sleeping for %f seconds before next attempt", requestURL.Redacted(), delay.Seconds())
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
			// Nothing
		}
		delay *= 2 // If the registry does not specify a delay, back off exponentially.
	}
}

// makeRequestToResolvedURLOnce creates and executes a http.Request with the specified parameters, adding authentication and TLS options for the Docker client.
// streamLen, if not -1, specifies the length of the data expected on stream.
// makeRequest should generally be preferred.
// Note that no exponential back off is performed when receiving an http 429 status code.
func (c *dockerClient) makeRequestToResolvedURLOnce(ctx context.Context, method string, resolvedURL *url.URL, headers map[string][]string, stream io.Reader, streamLen int64, auth sendAuth, extraScope *authScope) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, resolvedURL.String(), stream)
	if err != nil {
		return nil, err
	}
	if streamLen != -1 { // Do not blindly overwrite if streamLen == -1, http.NewRequestWithContext above can figure out the length of bytes.Reader and similar objects without us having to compute it.
		req.ContentLength = streamLen
	}
	req.Header.Set("Docker-Distribution-API-Version", "registry/2.0")
	for n, h := range headers {
		for _, hh := range h {
			req.Header.Add(n, hh)
		}
	}
	req.Header.Add("User-Agent", c.userAgent)
	if auth == v2Auth {
		if err := c.setupRequestAuth(req, extraScope); err != nil {
			return nil, err
		}
	}
	logrus.Debugf("%s %s", method, resolvedURL.Redacted())
	res, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if warnings := res.Header.Values("Warning"); len(warnings) != 0 {
		c.logResponseWarnings(res, warnings)
	}
	return res, nil
}

// logResponseWarnings logs warningHeaders from res, if any.
func (c *dockerClient) logResponseWarnings(res *http.Response, warningHeaders []string) {
	c.reportedWarningsLock.Lock()
	defer c.reportedWarningsLock.Unlock()

	for _, header := range warningHeaders {
		warningString := parseRegistryWarningHeader(header)
		if warningString == "" {
			logrus.Debugf("Ignored Warning: header from registry: %q", header)
		} else {
			if !c.reportedWarnings.Contains(warningString) {
				c.reportedWarnings.Add(warningString)
				// Note that reportedWarnings is based only on warningString, so that we don’t
				// repeat the same warning for every request - but the warning includes the URL;
				// so it may not be specific to that URL.
				logrus.Warnf("Warning from registry (first encountered at %q): %q", res.Request.URL.Redacted(), warningString)
			} else {
				logrus.Debugf("Repeated warning from registry at %q: %q", res.Request.URL.Redacted(), warningString)
			}
		}
	}
}

// parseRegistryWarningHeader parses a Warning: header per RFC 7234, limited to the warning
// values allowed by opencontainers/distribution-spec.
// It returns the warning string if the header has the expected format, or "" otherwise.
func parseRegistryWarningHeader(header string) string {
	const expectedPrefix = `299 - "`
	const expectedSuffix = `"`

	// warning-value = warn-code SP warn-agent SP warn-text	[ SP warn-date ]
	// distribution-spec requires warn-code=299, warn-agent="-", warn-date missing
	header, ok := strings.CutPrefix(header, expectedPrefix)
	if !ok {
		return ""
	}
	header, ok = strings.CutSuffix(header, expectedSuffix)
	if !ok {
		return ""
	}

	// ”Recipients that process the value of a quoted-string MUST handle a quoted-pair
	// as if it were replaced by the octet following the backslash.”, so let’s do that…
	res := strings.Builder{}
	afterBackslash := false
	for _, c := range []byte(header) { // []byte because escaping is defined in terms of bytes, not Unicode code points
		switch {
		case c == 0x7F || (c < ' ' && c != '\t'):
			return "" // Control characters are forbidden
		case afterBackslash:
			res.WriteByte(c)
			afterBackslash = false
		case c == '"':
			// This terminates the warn-text and warn-date, forbidden by distribution-spec, follows,
			// or completely invalid input.
			return ""
		case c == '\\':
			afterBackslash = true
		default:
			res.WriteByte(c)
		}
	}
	if afterBackslash {
		return ""
	}
	return res.String()
}

// we're using the challenges from the /v2/ ping response and not the one from the destination
// URL in this request because:
//
// 1) docker does that as well
// 2) gcr.io is sending 401 without a WWW-Authenticate header in the real request
//
// debugging: https://github.com/containers/image/pull/211#issuecomment-273426236 and follows up
func (c *dockerClient) setupRequestAuth(req *http.Request, extraScope *authScope) error {
	if len(c.challenges) == 0 {
		return nil
	}
	schemeNames := make([]string, 0, len(c.challenges))
	for _, challenge := range c.challenges {
		schemeNames = append(schemeNames, challenge.Scheme)
		switch challenge.Scheme {
		case "basic":
			req.SetBasicAuth(c.auth.Username, c.auth.Password)
			return nil
		case "bearer":
			registryToken := c.registryToken
			if registryToken == "" {
				cacheKey := ""
				scopes := []authScope{c.scope}
				if extraScope != nil {
					// Using ':' as a separator here is unambiguous because getBearerToken below
					// uses the same separator when formatting a remote request (and because
					// repository names that we create can't contain colons, and extraScope values
					// coming from a server come from `parseAuthScope`, which also splits on colons).
					cacheKey = fmt.Sprintf("%s:%s:%s", extraScope.resourceType, extraScope.remoteName, extraScope.actions)
					if colonCount := strings.Count(cacheKey, ":"); colonCount != 2 {
						return fmt.Errorf(
							"Internal error: there must be exactly 2 colons in the cacheKey ('%s') but got %d",
							cacheKey,
							colonCount,
						)
					}
					scopes = append(scopes, *extraScope)
				}
				var token bearerToken
				t, inCache := c.tokenCache.Load(cacheKey)
				if inCache {
					token = t.(bearerToken)
				}
				if !inCache || time.Now().After(token.expirationTime) {
					var (
						t   *bearerToken
						err error
					)
					if c.auth.IdentityToken != "" {
						t, err = c.getBearerTokenOAuth2(req.Context(), challenge, scopes)
					} else {
						t, err = c.getBearerToken(req.Context(), challenge, scopes)
					}
					if err != nil {
						return err
					}

					token = *t
					c.tokenCache.Store(cacheKey, token)
				}
				registryToken = token.token
			}
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", registryToken))
			return nil
		default:
			logrus.Debugf("no handler for %s authentication", challenge.Scheme)
		}
	}
	logrus.Infof("None of the challenges sent by server (%s) are supported, trying an unauthenticated request anyway", strings.Join(schemeNames, ", "))
	return nil
}

func (c *dockerClient) getBearerTokenOAuth2(ctx context.Context, challenge challenge,
	scopes []authScope) (*bearerToken, error) {
	realm, ok := challenge.Parameters["realm"]
	if !ok {
		return nil, errors.New("missing realm in bearer auth challenge")
	}

	authReq, err := http.NewRequestWithContext(ctx, http.MethodPost, realm, nil)
	if err != nil {
		return nil, err
	}

	// Make the form data required against the oauth2 authentication
	// More details here: https://docs.docker.com/registry/spec/auth/oauth/
	params := authReq.URL.Query()
	if service, ok := challenge.Parameters["service"]; ok && service != "" {
		params.Add("service", service)
	}

	for _, scope := range scopes {
		if scope.resourceType != "" && scope.remoteName != "" && scope.actions != "" {
			params.Add("scope", fmt.Sprintf("%s:%s:%s", scope.resourceType, scope.remoteName, scope.actions))
		}
	}
	params.Add("grant_type", "refresh_token")
	params.Add("refresh_token", c.auth.IdentityToken)
	params.Add("client_id", "containers/image")

	authReq.Body = io.NopCloser(strings.NewReader(params.Encode()))
	authReq.Header.Add("User-Agent", c.userAgent)
	authReq.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	logrus.Debugf("%s %s", authReq.Method, authReq.URL.Redacted())
	res, err := c.client.Do(authReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if err := httpResponseToError(res, "Trying to obtain access token"); err != nil {
		return nil, err
	}

	return newBearerTokenFromHTTPResponseBody(res)
}

func (c *dockerClient) getBearerToken(ctx context.Context, challenge challenge,
	scopes []authScope) (*bearerToken, error) {
	realm, ok := challenge.Parameters["realm"]
	if !ok {
		return nil, errors.New("missing realm in bearer auth challenge")
	}

	authReq, err := http.NewRequestWithContext(ctx, http.MethodGet, realm, nil)
	if err != nil {
		return nil, err
	}

	params := authReq.URL.Query()
	if c.auth.Username != "" {
		params.Add("account", c.auth.Username)
	}

	if service, ok := challenge.Parameters["service"]; ok && service != "" {
		params.Add("service", service)
	}

	for _, scope := range scopes {
		if scope.resourceType != "" && scope.remoteName != "" && scope.actions != "" {
			params.Add("scope", fmt.Sprintf("%s:%s:%s", scope.resourceType, scope.remoteName, scope.actions))
		}
	}

	authReq.URL.RawQuery = params.Encode()

	if c.auth.Username != "" && c.auth.Password != "" {
		authReq.SetBasicAuth(c.auth.Username, c.auth.Password)
	}
	authReq.Header.Add("User-Agent", c.userAgent)

	logrus.Debugf("%s %s", authReq.Method, authReq.URL.Redacted())
	res, err := c.client.Do(authReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if err := httpResponseToError(res, "Requesting bearer token"); err != nil {
		return nil, err
	}

	return newBearerTokenFromHTTPResponseBody(res)
}

// newBearerTokenFromHTTPResponseBody parses a http.Response to obtain a bearerToken.
// The caller is still responsible for ensuring res.Body is closed.
func newBearerTokenFromHTTPResponseBody(res *http.Response) (*bearerToken, error) {
	blob, err := iolimits.ReadAtMost(res.Body, iolimits.MaxAuthTokenBodySize)
	if err != nil {
		return nil, err
	}

	var token struct {
		Token          string    `json:"token"`
		AccessToken    string    `json:"access_token"`
		ExpiresIn      int       `json:"expires_in"`
		IssuedAt       time.Time `json:"issued_at"`
		expirationTime time.Time
	}
	if err := json.Unmarshal(blob, &token); err != nil {
		const bodySampleLength = 50
		bodySample := blob
		if len(bodySample) > bodySampleLength {
			bodySample = bodySample[:bodySampleLength]
		}
		return nil, fmt.Errorf("decoding bearer token (last URL %q, body start %q): %w", res.Request.URL.Redacted(), string(bodySample), err)
	}

	bt := &bearerToken{
		token: token.Token,
	}
	if bt.token == "" {
		bt.token = token.AccessToken
	}

	if token.ExpiresIn < minimumTokenLifetimeSeconds {
		token.ExpiresIn = minimumTokenLifetimeSeconds
		logrus.Debugf("Increasing token expiration to: %d seconds", token.ExpiresIn)
	}
	if token.IssuedAt.IsZero() {
		token.IssuedAt = time.Now().UTC()
	}
	bt.expirationTime = token.IssuedAt.Add(time.Duration(token.ExpiresIn) * time.Second)
	return bt, nil
}

// detectPropertiesHelper performs the work of detectProperties which executes
// it at most once.
func (c *dockerClient) detectPropertiesHelper(ctx context.Context) error {
	// We overwrite the TLS clients `InsecureSkipVerify` only if explicitly
	// specified by the system context
	if c.sys != nil && c.sys.DockerInsecureSkipTLSVerify != types.OptionalBoolUndefined {
		c.tlsClientConfig.InsecureSkipVerify = c.sys.DockerInsecureSkipTLSVerify == types.OptionalBoolTrue
	}
	tr := tlsclientconfig.NewTransport()
	tr.TLSClientConfig = c.tlsClientConfig
	// if set DockerProxyURL explicitly, use the DockerProxyURL instead of system proxy
	if c.sys != nil && c.sys.DockerProxyURL != nil {
		tr.Proxy = http.ProxyURL(c.sys.DockerProxyURL)
	}
	c.client = &http.Client{Transport: tr}

	ping := func(scheme string) error {
		pingURL, err := url.Parse(fmt.Sprintf(resolvedPingV2URL, scheme, c.registry))
		if err != nil {
			return err
		}
		resp, err := c.makeRequestToResolvedURL(ctx, http.MethodGet, pingURL, nil, nil, -1, noAuth, nil)
		if err != nil {
			logrus.Debugf("Ping %s err %s (%#v)", pingURL.Redacted(), err.Error(), err)
			return err
		}
		defer resp.Body.Close()
		logrus.Debugf("Ping %s status %d", pingURL.Redacted(), resp.StatusCode)
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
			return registryHTTPResponseToError(resp)
		}
		c.challenges = slices.Collect(iterateAuthHeader(resp.Header))
		c.scheme = scheme
		c.supportsSignatures = resp.Header.Get("X-Registry-Supports-Signatures") == "1"
		return nil
	}
	err := ping("https")
	if err != nil && c.tlsClientConfig.InsecureSkipVerify {
		err = ping("http")
	}
	if err != nil {
		err = fmt.Errorf("pinging container registry %s: %w", c.registry, err)
	}
	return err
}

// detectProperties detects various properties of the registry.
// See the dockerClient documentation for members which are affected by this.
func (c *dockerClient) detectProperties(ctx context.Context) error {
	c.detectPropertiesOnce.Do(func() { c.detectPropertiesError = c.detectPropertiesHelper(ctx) })
	return c.detectPropertiesError
}

// fetchManifest fetches a manifest for (the repo of ref) + tagOrDigest.
// The caller is responsible for ensuring tagOrDigest uses the expected format.
func (c *dockerClient) fetchManifest(ctx context.Context, ref dockerReference, tagOrDigest string) ([]byte, string, error) {
	path := fmt.Sprintf(manifestPath, reference.Path(ref.ref), tagOrDigest)
	headers := map[string][]string{
		"Accept": manifest.DefaultRequestedManifestMIMETypes,
	}
	res, err := c.makeRequest(ctx, http.MethodGet, path, headers, nil, v2Auth, nil)
	if err != nil {
		return nil, "", err
	}
	logrus.Debugf("Content-Type from manifest GET is %q", res.Header.Get("Content-Type"))
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("reading manifest %s in %s: %w", tagOrDigest, ref.ref.Name(), registryHTTPResponseToError(res))
	}

	manblob, err := iolimits.ReadAtMost(res.Body, iolimits.MaxManifestBodySize)
	if err != nil {
		return nil, "", err
	}
	return manblob, simplifyContentType(res.Header.Get("Content-Type")), nil
}

// getExternalBlob returns the reader of the first available blob URL from urls, which must not be empty.
// This function can return nil reader when no url is supported by this function. In this case, the caller
// should fallback to fetch the non-external blob (i.e. pull from the registry).
func (c *dockerClient) getExternalBlob(ctx context.Context, urls []string) (io.ReadCloser, int64, error) {
	if len(urls) == 0 {
		return nil, 0, errors.New("internal error: getExternalBlob called with no URLs")
	}
	var remoteErrors []error
	for _, u := range urls {
		blobURL, err := url.Parse(u)
		if err != nil || (blobURL.Scheme != "http" && blobURL.Scheme != "https") {
			continue // unsupported url. skip this url.
		}
		// NOTE: we must not authenticate on additional URLs as those
		//       can be abused to leak credentials or tokens.  Please
		//       refer to CVE-2020-15157 for more information.
		resp, err := c.makeRequestToResolvedURL(ctx, http.MethodGet, blobURL, nil, nil, -1, noAuth, nil)
		if err != nil {
			remoteErrors = append(remoteErrors, err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			err := fmt.Errorf("error fetching external blob from %q: %w", u, newUnexpectedHTTPStatusError(resp))
			remoteErrors = append(remoteErrors, err)
			logrus.Debug(err)
			resp.Body.Close()
			continue
		}

		size, err := getBlobSize(resp)
		if err != nil {
			size = -1
		}
		return resp.Body, size, nil
	}
	if remoteErrors == nil {
		return nil, 0, nil // fallback to non-external blob
	}
	return nil, 0, fmt.Errorf("failed fetching external blob from all urls: %w", multierr.Format("", ", ", "", remoteErrors))
}

func getBlobSize(resp *http.Response) (int64, error) {
	hdrs := resp.Header.Values("Content-Length")
	if len(hdrs) == 0 {
		return -1, errors.New(`Missing "Content-Length" header in response`)
	}
	hdr := hdrs[0] // Equivalent to resp.Header.Get(…)
	size, err := strconv.ParseInt(hdr, 10, 64)
	if err != nil { // Go’s response reader should already reject such values.
		return -1, err
	}
	if size < 0 { // '-' is not a valid character in Content-Length, so negative values are invalid. Go’s response reader should already reject such values.
		return -1, fmt.Errorf(`Invalid negative "Content-Length" %q`, hdr)
	}
	return size, nil
}

// getBlob returns a stream for the specified blob in ref, and the blob’s size (or -1 if unknown).
// The Digest field in BlobInfo is guaranteed to be provided, Size may be -1 and MediaType may be optionally provided.
// May update BlobInfoCache, preferably after it knows for certain that a blob truly exists at a specific location.
func (c *dockerClient) getBlob(ctx context.Context, ref dockerReference, info types.BlobInfo, cache types.BlobInfoCache) (io.ReadCloser, int64, error) {
	if len(info.URLs) != 0 {
		r, s, err := c.getExternalBlob(ctx, info.URLs)
		if err != nil {
			return nil, 0, err
		} else if r != nil {
			return r, s, nil
		}
	}

	if err := info.Digest.Validate(); err != nil { // Make sure info.Digest.String() does not contain any unexpected characters
		return nil, 0, err
	}
	path := fmt.Sprintf(blobsPath, reference.Path(ref.ref), info.Digest.String())
	logrus.Debugf("Downloading %s", path)
	res, err := c.makeRequest(ctx, http.MethodGet, path, nil, nil, v2Auth, nil)
	if err != nil {
		return nil, 0, err
	}
	if res.StatusCode != http.StatusOK {
		err := registryHTTPResponseToError(res)
		res.Body.Close()
		return nil, 0, fmt.Errorf("fetching blob: %w", err)
	}
	cache.RecordKnownLocation(ref.Transport(), bicTransportScope(ref), info.Digest, newBICLocationReference(ref))
	blobSize, err := getBlobSize(res)
	if err != nil {
		// See above, we don't guarantee returning a size
		logrus.Debugf("failed to get blob size: %v", err)
		blobSize = -1
	}

	reconnectingReader, err := newBodyReader(ctx, c, path, res.Body)
	if err != nil {
		res.Body.Close()
		return nil, 0, err
	}
	return reconnectingReader, blobSize, nil
}

// getOCIDescriptorContents returns the contents a blob specified by descriptor in ref, which must fit within limit.
func (c *dockerClient) getOCIDescriptorContents(ctx context.Context, ref dockerReference, desc imgspecv1.Descriptor, maxSize int, cache types.BlobInfoCache) ([]byte, error) {
	// Note that this copies all kinds of attachments: attestations, and whatever else is there,
	// not just signatures. We leave the signature consumers to decide based on the MIME type.

	if err := desc.Digest.Validate(); err != nil { // .Algorithm() might panic without this check
		return nil, fmt.Errorf("invalid digest %q: %w", desc.Digest.String(), err)
	}
	digestAlgorithm := desc.Digest.Algorithm()
	if !digestAlgorithm.Available() {
		return nil, fmt.Errorf("invalid digest %q: unsupported digest algorithm %q", desc.Digest.String(), digestAlgorithm.String())
	}

	reader, _, err := c.getBlob(ctx, ref, manifest.BlobInfoFromOCI1Descriptor(desc), cache)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	payload, err := iolimits.ReadAtMost(reader, maxSize)
	if err != nil {
		return nil, fmt.Errorf("reading blob %s in %s: %w", desc.Digest.String(), ref.ref.Name(), err)
	}
	actualDigest := digestAlgorithm.FromBytes(payload)
	if actualDigest != desc.Digest {
		return nil, fmt.Errorf("digest mismatch, expected %q, got %q", desc.Digest.String(), actualDigest.String())
	}
	return payload, nil
}

// isManifestUnknownError returns true iff err from fetchManifest is a “manifest unknown” error.
func isManifestUnknownError(err error) bool {
	// docker/distribution, and as defined in the spec
	var ec errcode.ErrorCoder
	if errors.As(err, &ec) && ec.ErrorCode() == v2.ErrorCodeManifestUnknown {
		return true
	}
	// registry.redhat.io as of October 2022
	var e errcode.Error
	if errors.As(err, &e) && e.ErrorCode() == errcode.ErrorCodeUnknown && e.Message == "Not Found" {
		return true
	}
	// Harbor v2.10.2
	if errors.As(err, &e) && e.ErrorCode() == errcode.ErrorCodeUnknown && strings.Contains(strings.ToLower(e.Message), "not found") {
		return true
	}

	// opencontainers/distribution-spec does not require the errcode.Error payloads to be used,
	// but specifies that the HTTP status must be 404.
	var unexpected *unexpectedHTTPResponseError
	if errors.As(err, &unexpected) && unexpected.StatusCode == http.StatusNotFound {
		return true
	}
	return false
}

// getSigstoreAttachmentManifest loads and parses the manifest for sigstore attachments for
// digest in ref.
// It returns (nil, nil) if the manifest does not exist.
func (c *dockerClient) getSigstoreAttachmentManifest(ctx context.Context, ref dockerReference, digest digest.Digest) (*manifest.OCI1, error) {
	tag, err := sigstoreAttachmentTag(digest)
	if err != nil {
		return nil, err
	}
	sigstoreRef, err := reference.WithTag(reference.TrimNamed(ref.ref), tag)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("Looking for sigstore attachments in %s", sigstoreRef.String())
	manifestBlob, mimeType, err := c.fetchManifest(ctx, ref, tag)
	if err != nil {
		// FIXME: Are we going to need better heuristics??
		// This alone is probably a good enough reason for sigstore to be opt-in only,
		// otherwise we would just break ordinary copies.
		if isManifestUnknownError(err) {
			logrus.Debugf("Fetching sigstore attachment manifest failed, assuming it does not exist: %v", err)
			return nil, nil
		}
		logrus.Debugf("Fetching sigstore attachment manifest failed: %v", err)
		return nil, err
	}
	if mimeType != imgspecv1.MediaTypeImageManifest {
		// FIXME: Try anyway??
		return nil, fmt.Errorf("unexpected MIME type for sigstore attachment manifest %s: %q",
			sigstoreRef.String(), mimeType)
	}
	res, err := manifest.OCI1FromManifest(manifestBlob)
	if err != nil {
		return nil, fmt.Errorf("parsing manifest %s: %w", sigstoreRef.String(), err)
	}
	return res, nil
}

// getExtensionsSignatures returns signatures from the X-Registry-Supports-Signatures API extension,
// using the original data structures.
func (c *dockerClient) getExtensionsSignatures(ctx context.Context, ref dockerReference, manifestDigest digest.Digest) (*extensionSignatureList, error) {
	if err := manifestDigest.Validate(); err != nil { // Make sure manifestDigest.String() does not contain any unexpected characters
		return nil, err
	}
	path := fmt.Sprintf(extensionsSignaturePath, reference.Path(ref.ref), manifestDigest)
	res, err := c.makeRequest(ctx, http.MethodGet, path, nil, nil, v2Auth, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading signatures for %s in %s: %w", manifestDigest, ref.ref.Name(), registryHTTPResponseToError(res))
	}

	body, err := iolimits.ReadAtMost(res.Body, iolimits.MaxSignatureListBodySize)
	if err != nil {
		return nil, err
	}

	var parsedBody extensionSignatureList
	if err := json.Unmarshal(body, &parsedBody); err != nil {
		return nil, fmt.Errorf("decoding signature list: %w", err)
	}
	return &parsedBody, nil
}

// sigstoreAttachmentTag returns a sigstore attachment tag for the specified digest.
func sigstoreAttachmentTag(d digest.Digest) (string, error) {
	if err := d.Validate(); err != nil { // Make sure d.String() doesn’t contain any unexpected characters
		return "", err
	}
	return strings.Replace(d.String(), ":", "-", 1) + ".sig", nil
}

// Close removes resources associated with an initialized dockerClient, if any.
func (c *dockerClient) Close() error {
	if c.client != nil {
		c.client.CloseIdleConnections()
	}
	return nil
}
