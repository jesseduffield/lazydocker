package docker

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/http"
	"time"

	"github.com/docker/cli/cli/connhelper"
	"github.com/docker/cli/cli/context"
	"github.com/docker/cli/cli/context/store"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/tlsconfig"
	"github.com/pkg/errors"
)

// EndpointMeta is a typed wrapper around a context-store generic endpoint describing
// a Docker Engine endpoint, without its tls config
type EndpointMeta = context.EndpointMetaBase

// Endpoint is a typed wrapper around a context-store generic endpoint describing
// a Docker Engine endpoint, with its tls data
type Endpoint struct {
	EndpointMeta
	TLSData *context.TLSData
}

// WithTLSData loads TLS materials for the endpoint
func WithTLSData(s store.Reader, contextName string, m EndpointMeta) (Endpoint, error) {
	tlsData, err := context.LoadTLSData(s, contextName, DockerEndpoint)
	if err != nil {
		return Endpoint{}, err
	}
	return Endpoint{
		EndpointMeta: m,
		TLSData:      tlsData,
	}, nil
}

// tlsConfig extracts a context docker endpoint TLS config
func (ep *Endpoint) tlsConfig() (*tls.Config, error) {
	if ep.TLSData == nil && !ep.SkipTLSVerify {
		// there is no specific tls config
		return nil, nil
	}
	var tlsOpts []func(*tls.Config)
	if ep.TLSData != nil && ep.TLSData.CA != nil {
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(ep.TLSData.CA) {
			return nil, errors.New("failed to retrieve context tls info: ca.pem seems invalid")
		}
		tlsOpts = append(tlsOpts, func(cfg *tls.Config) {
			cfg.RootCAs = certPool
		})
	}
	if ep.TLSData != nil && ep.TLSData.Key != nil && ep.TLSData.Cert != nil {
		keyBytes := ep.TLSData.Key
		pemBlock, _ := pem.Decode(keyBytes)
		if pemBlock == nil {
			return nil, errors.New("no valid private key found")
		}
		if x509.IsEncryptedPEMBlock(pemBlock) { //nolint:staticcheck // SA1019: x509.IsEncryptedPEMBlock is deprecated, and insecure by design
			return nil, errors.New("private key is encrypted - support for encrypted private keys has been removed, see https://docs.docker.com/go/deprecated/")
		}

		x509cert, err := tls.X509KeyPair(ep.TLSData.Cert, keyBytes)
		if err != nil {
			return nil, errors.Wrap(err, "failed to retrieve context tls info")
		}
		tlsOpts = append(tlsOpts, func(cfg *tls.Config) {
			cfg.Certificates = []tls.Certificate{x509cert}
		})
	}
	if ep.SkipTLSVerify {
		tlsOpts = append(tlsOpts, func(cfg *tls.Config) {
			cfg.InsecureSkipVerify = true
		})
	}
	return tlsconfig.ClientDefault(tlsOpts...), nil
}

// ClientOpts returns a slice of Client options to configure an API client with this endpoint
func (ep *Endpoint) ClientOpts() ([]client.Opt, error) {
	var result []client.Opt
	if ep.Host != "" {
		helper, err := connhelper.GetConnectionHelper(ep.Host)
		if err != nil {
			return nil, err
		}
		if helper == nil {
			tlsConfig, err := ep.tlsConfig()
			if err != nil {
				return nil, err
			}
			result = append(result,
				withHTTPClient(tlsConfig),
				client.WithHost(ep.Host),
			)
		} else {
			result = append(result,
				client.WithHTTPClient(&http.Client{
					// No TLS, and no proxy.
					Transport: &http.Transport{
						DialContext: helper.Dialer,
					},
				}),
				client.WithHost(helper.Host),
				client.WithDialContext(helper.Dialer),
			)
		}
	}

	result = append(result, client.WithVersionFromEnv(), client.WithAPIVersionNegotiation())
	return result, nil
}

func withHTTPClient(tlsConfig *tls.Config) func(*client.Client) error {
	return func(c *client.Client) error {
		if tlsConfig == nil {
			// Use the default HTTPClient
			return nil
		}
		return client.WithHTTPClient(&http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
				DialContext: (&net.Dialer{
					KeepAlive: 30 * time.Second,
					Timeout:   30 * time.Second,
				}).DialContext,
			},
			CheckRedirect: client.CheckRedirect,
		})(c)
	}
}

// EndpointFromContext parses a context docker endpoint metadata into a typed EndpointMeta structure
func EndpointFromContext(metadata store.Metadata) (EndpointMeta, error) {
	ep, ok := metadata.Endpoints[DockerEndpoint]
	if !ok {
		return EndpointMeta{}, errors.New("cannot find docker endpoint in context")
	}
	typed, ok := ep.(EndpointMeta)
	if !ok {
		return EndpointMeta{}, errors.Errorf("endpoint %q is not of type EndpointMeta", DockerEndpoint)
	}
	return typed, nil
}
