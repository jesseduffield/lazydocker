package docker

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"os"
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

	// Deprecated: Use of encrypted TLS private keys has been deprecated, and
	// will be removed in a future release. Golang has deprecated support for
	// legacy PEM encryption (as specified in RFC 1423), as it is insecure by
	// design (see https://go-review.googlesource.com/c/go/+/264159).
	TLSPassword string
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
func (c *Endpoint) tlsConfig() (*tls.Config, error) {
	if c.TLSData == nil && !c.SkipTLSVerify {
		// there is no specific tls config
		return nil, nil
	}
	var tlsOpts []func(*tls.Config)
	if c.TLSData != nil && c.TLSData.CA != nil {
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(c.TLSData.CA) {
			return nil, errors.New("failed to retrieve context tls info: ca.pem seems invalid")
		}
		tlsOpts = append(tlsOpts, func(cfg *tls.Config) {
			cfg.RootCAs = certPool
		})
	}
	if c.TLSData != nil && c.TLSData.Key != nil && c.TLSData.Cert != nil {
		keyBytes := c.TLSData.Key
		pemBlock, _ := pem.Decode(keyBytes)
		if pemBlock == nil {
			return nil, fmt.Errorf("no valid private key found")
		}

		var err error
		// TODO should we follow Golang, and deprecate RFC 1423 encryption, and produce a warning (or just error)? see https://github.com/docker/cli/issues/3212
		if x509.IsEncryptedPEMBlock(pemBlock) { //nolint: staticcheck // SA1019: x509.IsEncryptedPEMBlock is deprecated, and insecure by design
			keyBytes, err = x509.DecryptPEMBlock(pemBlock, []byte(c.TLSPassword)) //nolint: staticcheck // SA1019: x509.IsEncryptedPEMBlock is deprecated, and insecure by design
			if err != nil {
				return nil, errors.Wrap(err, "private key is encrypted, but could not decrypt it")
			}
			keyBytes = pem.EncodeToMemory(&pem.Block{Type: pemBlock.Type, Bytes: keyBytes})
		}

		x509cert, err := tls.X509KeyPair(c.TLSData.Cert, keyBytes)
		if err != nil {
			return nil, errors.Wrap(err, "failed to retrieve context tls info")
		}
		tlsOpts = append(tlsOpts, func(cfg *tls.Config) {
			cfg.Certificates = []tls.Certificate{x509cert}
		})
	}
	if c.SkipTLSVerify {
		tlsOpts = append(tlsOpts, func(cfg *tls.Config) {
			cfg.InsecureSkipVerify = true
		})
	}
	return tlsconfig.ClientDefault(tlsOpts...), nil
}

// ClientOpts returns a slice of Client options to configure an API client with this endpoint
func (c *Endpoint) ClientOpts() ([]client.Opt, error) {
	var result []client.Opt
	if c.Host != "" {
		helper, err := connhelper.GetConnectionHelper(c.Host)
		if err != nil {
			return nil, err
		}
		if helper == nil {
			tlsConfig, err := c.tlsConfig()
			if err != nil {
				return nil, err
			}
			result = append(result,
				withHTTPClient(tlsConfig),
				client.WithHost(c.Host),
			)

		} else {
			httpClient := &http.Client{
				// No tls
				// No proxy
				Transport: &http.Transport{
					DialContext: helper.Dialer,
				},
			}
			result = append(result,
				client.WithHTTPClient(httpClient),
				client.WithHost(helper.Host),
				client.WithDialContext(helper.Dialer),
			)
		}
	}

	version := os.Getenv("DOCKER_API_VERSION")
	if version != "" {
		result = append(result, client.WithVersion(version))
	} else {
		result = append(result, client.WithAPIVersionNegotiation())
	}
	return result, nil
}

func withHTTPClient(tlsConfig *tls.Config) func(*client.Client) error {
	return func(c *client.Client) error {
		if tlsConfig == nil {
			// Use the default HTTPClient
			return nil
		}

		httpClient := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
				DialContext: (&net.Dialer{
					KeepAlive: 30 * time.Second,
					Timeout:   30 * time.Second,
				}).DialContext,
			},
			CheckRedirect: client.CheckRedirect,
		}
		return client.WithHTTPClient(httpClient)(c)
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
