package daemon

import (
	"net/http"
	"path/filepath"
	"time"

	dockerclient "github.com/docker/docker/client"
	"github.com/docker/go-connections/tlsconfig"
	"go.podman.io/image/v5/types"
)

// NewDockerClient initializes a new API client based on the passed SystemContext.
func newDockerClient(sys *types.SystemContext) (*dockerclient.Client, error) {
	host := dockerclient.DefaultDockerHost
	if sys != nil && sys.DockerDaemonHost != "" {
		host = sys.DockerDaemonHost
	}

	opts := []dockerclient.Opt{
		dockerclient.WithHost(host),
		dockerclient.WithAPIVersionNegotiation(),
	}

	// We conditionalize building the TLS configuration only to TLS sockets:
	//
	// The dockerclient.Client implementation differentiates between
	// - Client.proto, which is ~how the connection is establishe (IP / AF_UNIX/Windows)
	// - Client.scheme, which is what is sent over the connection (HTTP with/without TLS).
	//
	// Only Client.proto is set from the URL in dockerclient.WithHost(),
	// Client.scheme is detected based on a http.Client.TLSClientConfig presence;
	// dockerclient.WithHTTPClient with a client that has TLSClientConfig set
	// will, by default, trigger an attempt to use TLS.
	//
	// So, don’t use WithHTTPClient for unix:// sockets at all.
	//
	// Similarly, if we want to communicate over plain HTTP on a TCP socket (http://),
	// we also should not set TLSClientConfig.  We continue to use WithHTTPClient
	// with our slightly non-default settings to avoid a behavior change on updates of c/image.
	//
	// Alternatively we could use dockerclient.WithScheme to drive the TLS/non-TLS logic
	// explicitly, but we would still want to set WithHTTPClient (differently) for https:// and http:// ;
	// so that would not be any simpler.
	serverURL, err := dockerclient.ParseHostURL(host)
	if err != nil {
		return nil, err
	}
	switch serverURL.Scheme {
	case "unix": // Nothing
	case "npipe": // Nothing
	case "http":
		hc := httpConfig()
		opts = append(opts, dockerclient.WithHTTPClient(hc))
	default:
		hc, err := tlsConfig(sys)
		if err != nil {
			return nil, err
		}
		opts = append(opts, dockerclient.WithHTTPClient(hc))
	}

	return dockerclient.NewClientWithOpts(opts...)
}

func tlsConfig(sys *types.SystemContext) (*http.Client, error) {
	options := tlsconfig.Options{}
	if sys != nil && sys.DockerDaemonInsecureSkipTLSVerify {
		options.InsecureSkipVerify = true
	}

	if sys != nil && sys.DockerDaemonCertPath != "" {
		options.CAFile = filepath.Join(sys.DockerDaemonCertPath, "ca.pem")
		options.CertFile = filepath.Join(sys.DockerDaemonCertPath, "cert.pem")
		options.KeyFile = filepath.Join(sys.DockerDaemonCertPath, "key.pem")
	}

	tlsc, err := tlsconfig.Client(options)
	if err != nil {
		return nil, err
	}

	return &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: tlsc,
			// In general we want to follow docker/daemon/client.defaultHTTPClient , as long as it doesn’t affect compatibility.
			// These idle connection limits really only apply to long-running clients, which is not our case here;
			// we include the same values purely for symmetry.
			MaxIdleConns:    6,
			IdleConnTimeout: 30 * time.Second,
		},
		CheckRedirect: dockerclient.CheckRedirect,
	}, nil
}

func httpConfig() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: nil,
			// In general we want to follow docker/daemon/client.defaultHTTPClient , as long as it doesn’t affect compatibility.
			// These idle connection limits really only apply to long-running clients, which is not our case here;
			// we include the same values purely for symmetry.
			MaxIdleConns:    6,
			IdleConnTimeout: 30 * time.Second,
		},
		CheckRedirect: dockerclient.CheckRedirect,
	}
}
