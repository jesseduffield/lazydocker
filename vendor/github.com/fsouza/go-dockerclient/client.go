// Copyright 2013 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package docker provides a client for the Docker remote API.
//
// See https://goo.gl/o2v3rk for more details on the remote API.
package docker

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/docker/docker/pkg/homedir"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/docker/pkg/stdcopy"
)

const (
	userAgent = "go-dockerclient"

	unixProtocol      = "unix"
	namedPipeProtocol = "npipe"
)

var (
	// ErrInvalidEndpoint is returned when the endpoint is not a valid HTTP URL.
	ErrInvalidEndpoint = errors.New("invalid endpoint")

	// ErrConnectionRefused is returned when the client cannot connect to the given endpoint.
	ErrConnectionRefused = errors.New("cannot connect to Docker endpoint")

	// ErrInactivityTimeout is returned when a streamable call has been inactive for some time.
	ErrInactivityTimeout = errors.New("inactivity time exceeded timeout")

	apiVersion112, _ = NewAPIVersion("1.12")
	apiVersion118, _ = NewAPIVersion("1.18")
	apiVersion119, _ = NewAPIVersion("1.19")
	apiVersion121, _ = NewAPIVersion("1.21")
	apiVersion124, _ = NewAPIVersion("1.24")
	apiVersion125, _ = NewAPIVersion("1.25")
	apiVersion135, _ = NewAPIVersion("1.35")
)

// APIVersion is an internal representation of a version of the Remote API.
type APIVersion []int

// NewAPIVersion returns an instance of APIVersion for the given string.
//
// The given string must be in the form <major>.<minor>.<patch>, where <major>,
// <minor> and <patch> are integer numbers.
func NewAPIVersion(input string) (APIVersion, error) {
	if !strings.Contains(input, ".") {
		return nil, fmt.Errorf("unable to parse version %q", input)
	}
	raw := strings.Split(input, "-")
	arr := strings.Split(raw[0], ".")
	ret := make(APIVersion, len(arr))
	var err error
	for i, val := range arr {
		ret[i], err = strconv.Atoi(val)
		if err != nil {
			return nil, fmt.Errorf("unable to parse version %q: %q is not an integer", input, val)
		}
	}
	return ret, nil
}

func (version APIVersion) String() string {
	parts := make([]string, len(version))
	for i, val := range version {
		parts[i] = strconv.Itoa(val)
	}
	return strings.Join(parts, ".")
}

// LessThan is a function for comparing APIVersion structs.
func (version APIVersion) LessThan(other APIVersion) bool {
	return version.compare(other) < 0
}

// LessThanOrEqualTo is a function for comparing APIVersion structs.
func (version APIVersion) LessThanOrEqualTo(other APIVersion) bool {
	return version.compare(other) <= 0
}

// GreaterThan is a function for comparing APIVersion structs.
func (version APIVersion) GreaterThan(other APIVersion) bool {
	return version.compare(other) > 0
}

// GreaterThanOrEqualTo is a function for comparing APIVersion structs.
func (version APIVersion) GreaterThanOrEqualTo(other APIVersion) bool {
	return version.compare(other) >= 0
}

func (version APIVersion) compare(other APIVersion) int {
	for i, v := range version {
		if i <= len(other)-1 {
			otherVersion := other[i]

			if v < otherVersion {
				return -1
			} else if v > otherVersion {
				return 1
			}
		}
	}
	if len(version) > len(other) {
		return 1
	}
	if len(version) < len(other) {
		return -1
	}
	return 0
}

// Client is the basic type of this package. It provides methods for
// interaction with the API.
type Client struct {
	SkipServerVersionCheck bool
	HTTPClient             *http.Client
	TLSConfig              *tls.Config
	Dialer                 Dialer

	endpoint            string
	endpointURL         *url.URL
	eventMonitor        *eventMonitoringState
	requestedAPIVersion APIVersion
	serverAPIVersion    APIVersion
	expectedAPIVersion  APIVersion
}

// Dialer is an interface that allows network connections to be dialed
// (net.Dialer fulfills this interface) and named pipes (a shim using
// winio.DialPipe)
type Dialer interface {
	Dial(network, address string) (net.Conn, error)
}

// NewClient returns a Client instance ready for communication with the given
// server endpoint. It will use the latest remote API version available in the
// server.
func NewClient(endpoint string) (*Client, error) {
	client, err := NewVersionedClient(endpoint, "")
	if err != nil {
		return nil, err
	}
	client.SkipServerVersionCheck = true
	return client, nil
}

// NewTLSClient returns a Client instance ready for TLS communications with the givens
// server endpoint, key and certificates . It will use the latest remote API version
// available in the server.
func NewTLSClient(endpoint string, cert, key, ca string) (*Client, error) {
	client, err := NewVersionedTLSClient(endpoint, cert, key, ca, "")
	if err != nil {
		return nil, err
	}
	client.SkipServerVersionCheck = true
	return client, nil
}

// NewTLSClientFromBytes returns a Client instance ready for TLS communications with the givens
// server endpoint, key and certificates (passed inline to the function as opposed to being
// read from a local file). It will use the latest remote API version available in the server.
func NewTLSClientFromBytes(endpoint string, certPEMBlock, keyPEMBlock, caPEMCert []byte) (*Client, error) {
	client, err := NewVersionedTLSClientFromBytes(endpoint, certPEMBlock, keyPEMBlock, caPEMCert, "")
	if err != nil {
		return nil, err
	}
	client.SkipServerVersionCheck = true
	return client, nil
}

// NewVersionedClient returns a Client instance ready for communication with
// the given server endpoint, using a specific remote API version.
func NewVersionedClient(endpoint string, apiVersionString string) (*Client, error) {
	u, err := parseEndpoint(endpoint, false)
	if err != nil {
		return nil, err
	}
	var requestedAPIVersion APIVersion
	if strings.Contains(apiVersionString, ".") {
		requestedAPIVersion, err = NewAPIVersion(apiVersionString)
		if err != nil {
			return nil, err
		}
	}
	c := &Client{
		HTTPClient:          defaultClient(),
		Dialer:              &net.Dialer{},
		endpoint:            endpoint,
		endpointURL:         u,
		eventMonitor:        new(eventMonitoringState),
		requestedAPIVersion: requestedAPIVersion,
	}
	c.initializeNativeClient(defaultTransport)
	return c, nil
}

// WithTransport replaces underlying HTTP client of Docker Client by accepting
// a function that returns pointer to a transport object.
func (c *Client) WithTransport(trFunc func() *http.Transport) {
	c.initializeNativeClient(trFunc)
}

// NewVersionnedTLSClient is like NewVersionedClient, but with ann extra n.
//
// Deprecated: Use NewVersionedTLSClient instead.
func NewVersionnedTLSClient(endpoint string, cert, key, ca, apiVersionString string) (*Client, error) {
	return NewVersionedTLSClient(endpoint, cert, key, ca, apiVersionString)
}

// NewVersionedTLSClient returns a Client instance ready for TLS communications with the givens
// server endpoint, key and certificates, using a specific remote API version.
func NewVersionedTLSClient(endpoint string, cert, key, ca, apiVersionString string) (*Client, error) {
	var certPEMBlock []byte
	var keyPEMBlock []byte
	var caPEMCert []byte
	if _, err := os.Stat(cert); !os.IsNotExist(err) {
		certPEMBlock, err = os.ReadFile(cert)
		if err != nil {
			return nil, err
		}
	}
	if _, err := os.Stat(key); !os.IsNotExist(err) {
		keyPEMBlock, err = os.ReadFile(key)
		if err != nil {
			return nil, err
		}
	}
	if _, err := os.Stat(ca); !os.IsNotExist(err) {
		caPEMCert, err = os.ReadFile(ca)
		if err != nil {
			return nil, err
		}
	}
	return NewVersionedTLSClientFromBytes(endpoint, certPEMBlock, keyPEMBlock, caPEMCert, apiVersionString)
}

// NewClientFromEnv returns a Client instance ready for communication created from
// Docker's default logic for the environment variables DOCKER_HOST, DOCKER_TLS_VERIFY, DOCKER_CERT_PATH,
// and DOCKER_API_VERSION.
//
// See https://github.com/docker/docker/blob/1f963af697e8df3a78217f6fdbf67b8123a7db94/docker/docker.go#L68.
// See https://github.com/docker/compose/blob/81707ef1ad94403789166d2fe042c8a718a4c748/compose/cli/docker_client.py#L7.
// See https://github.com/moby/moby/blob/28d7dba41d0c0d9c7f0dafcc79d3c59f2b3f5dc3/client/options.go#L51
func NewClientFromEnv() (*Client, error) {
	apiVersionString := os.Getenv("DOCKER_API_VERSION")
	client, err := NewVersionedClientFromEnv(apiVersionString)
	if err != nil {
		return nil, err
	}
	client.SkipServerVersionCheck = apiVersionString == ""
	return client, nil
}

// NewVersionedClientFromEnv returns a Client instance ready for TLS communications created from
// Docker's default logic for the environment variables DOCKER_HOST, DOCKER_TLS_VERIFY, and DOCKER_CERT_PATH,
// and using a specific remote API version.
//
// See https://github.com/docker/docker/blob/1f963af697e8df3a78217f6fdbf67b8123a7db94/docker/docker.go#L68.
// See https://github.com/docker/compose/blob/81707ef1ad94403789166d2fe042c8a718a4c748/compose/cli/docker_client.py#L7.
func NewVersionedClientFromEnv(apiVersionString string) (*Client, error) {
	dockerEnv, err := getDockerEnv()
	if err != nil {
		return nil, err
	}
	dockerHost := dockerEnv.dockerHost
	if dockerEnv.dockerTLSVerify {
		parts := strings.SplitN(dockerEnv.dockerHost, "://", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("could not split %s into two parts by ://", dockerHost)
		}
		cert := filepath.Join(dockerEnv.dockerCertPath, "cert.pem")
		key := filepath.Join(dockerEnv.dockerCertPath, "key.pem")
		ca := filepath.Join(dockerEnv.dockerCertPath, "ca.pem")
		return NewVersionedTLSClient(dockerEnv.dockerHost, cert, key, ca, apiVersionString)
	}
	return NewVersionedClient(dockerEnv.dockerHost, apiVersionString)
}

// NewVersionedTLSClientFromBytes returns a Client instance ready for TLS communications with the givens
// server endpoint, key and certificates (passed inline to the function as opposed to being
// read from a local file), using a specific remote API version.
func NewVersionedTLSClientFromBytes(endpoint string, certPEMBlock, keyPEMBlock, caPEMCert []byte, apiVersionString string) (*Client, error) {
	u, err := parseEndpoint(endpoint, true)
	if err != nil {
		return nil, err
	}
	var requestedAPIVersion APIVersion
	if strings.Contains(apiVersionString, ".") {
		requestedAPIVersion, err = NewAPIVersion(apiVersionString)
		if err != nil {
			return nil, err
		}
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if certPEMBlock != nil && keyPEMBlock != nil {
		tlsCert, err := tls.X509KeyPair(certPEMBlock, keyPEMBlock)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{tlsCert}
	}
	if caPEMCert == nil {
		tlsConfig.InsecureSkipVerify = true
	} else {
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caPEMCert) {
			return nil, errors.New("could not add RootCA pem")
		}
		tlsConfig.RootCAs = caPool
	}
	tr := defaultTransport()
	tr.TLSClientConfig = tlsConfig
	if err != nil {
		return nil, err
	}
	c := &Client{
		HTTPClient:          &http.Client{Transport: tr},
		TLSConfig:           tlsConfig,
		Dialer:              &net.Dialer{},
		endpoint:            endpoint,
		endpointURL:         u,
		eventMonitor:        new(eventMonitoringState),
		requestedAPIVersion: requestedAPIVersion,
	}
	c.initializeNativeClient(defaultTransport)
	return c, nil
}

// SetTimeout takes a timeout and applies it to the HTTPClient. It should not
// be called concurrently with any other Client methods.
func (c *Client) SetTimeout(t time.Duration) {
	if c.HTTPClient != nil {
		c.HTTPClient.Timeout = t
	}
}

func (c *Client) checkAPIVersion() error {
	serverAPIVersionString, err := c.getServerAPIVersionString()
	if err != nil {
		return err
	}
	c.serverAPIVersion, err = NewAPIVersion(serverAPIVersionString)
	if err != nil {
		return err
	}
	if c.requestedAPIVersion == nil {
		c.expectedAPIVersion = c.serverAPIVersion
	} else {
		c.expectedAPIVersion = c.requestedAPIVersion
	}
	return nil
}

// Endpoint returns the current endpoint. It's useful for getting the endpoint
// when using functions that get this data from the environment (like
// NewClientFromEnv.
func (c *Client) Endpoint() string {
	return c.endpoint
}

// Ping pings the docker server
//
// See https://goo.gl/wYfgY1 for more details.
func (c *Client) Ping() error {
	return c.PingWithContext(context.TODO())
}

// PingWithContext pings the docker server
// The context object can be used to cancel the ping request.
//
// See https://goo.gl/wYfgY1 for more details.
func (c *Client) PingWithContext(ctx context.Context) error {
	path := "/_ping"
	resp, err := c.do(http.MethodGet, path, doOptions{context: ctx})
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return newError(resp)
	}
	resp.Body.Close()
	return nil
}

func (c *Client) getServerAPIVersionString() (version string, err error) {
	resp, err := c.do(http.MethodGet, "/version", doOptions{})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("received unexpected status %d while trying to retrieve the server version", resp.StatusCode)
	}
	var versionResponse map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&versionResponse); err != nil {
		return "", err
	}
	if version, ok := (versionResponse["ApiVersion"]).(string); ok {
		return version, nil
	}
	return "", nil
}

type doOptions struct {
	data      any
	forceJSON bool
	headers   map[string]string
	context   context.Context
}

func (c *Client) do(method, path string, doOptions doOptions) (*http.Response, error) {
	var params io.Reader
	if doOptions.data != nil || doOptions.forceJSON {
		buf, err := json.Marshal(doOptions.data)
		if err != nil {
			return nil, err
		}
		params = bytes.NewBuffer(buf)
	}
	if path != "/version" && !c.SkipServerVersionCheck && c.expectedAPIVersion == nil {
		err := c.checkAPIVersion()
		if err != nil {
			return nil, err
		}
	}
	protocol := c.endpointURL.Scheme
	var u string
	switch protocol {
	case unixProtocol, namedPipeProtocol:
		u = c.getFakeNativeURL(path)
	default:
		u = c.getURL(path)
	}

	req, err := http.NewRequest(method, u, params)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if doOptions.data != nil {
		req.Header.Set("Content-Type", "application/json")
	} else if method == http.MethodPost {
		req.Header.Set("Content-Type", "plain/text")
	}

	for k, v := range doOptions.headers {
		req.Header.Set(k, v)
	}

	ctx := doOptions.context
	if ctx == nil {
		ctx = context.Background()
	}

	resp, err := c.HTTPClient.Do(req.WithContext(ctx))
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") {
			return nil, ErrConnectionRefused
		}

		return nil, chooseError(ctx, err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest {
		return nil, newError(resp)
	}
	return resp, nil
}

type streamOptions struct {
	setRawTerminal bool
	rawJSONStream  bool
	useJSONDecoder bool
	headers        map[string]string
	in             io.Reader
	stdout         io.Writer
	stderr         io.Writer
	reqSent        chan struct{}
	// timeout is the initial connection timeout
	timeout time.Duration
	// Timeout with no data is received, it's reset every time new data
	// arrives
	inactivityTimeout time.Duration
	context           context.Context
}

func chooseError(ctx context.Context, err error) error {
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	default:
		return err
	}
}

func (c *Client) stream(method, path string, streamOptions streamOptions) error {
	if (method == http.MethodPost || method == http.MethodPut) && streamOptions.in == nil {
		streamOptions.in = bytes.NewReader(nil)
	}
	if path != "/version" && !c.SkipServerVersionCheck && c.expectedAPIVersion == nil {
		err := c.checkAPIVersion()
		if err != nil {
			return err
		}
	}
	return c.streamURL(method, c.getURL(path), streamOptions)
}

func (c *Client) streamURL(method, url string, streamOptions streamOptions) error {
	if (method == http.MethodPost || method == http.MethodPut) && streamOptions.in == nil {
		streamOptions.in = bytes.NewReader(nil)
	}
	if !c.SkipServerVersionCheck && c.expectedAPIVersion == nil {
		err := c.checkAPIVersion()
		if err != nil {
			return err
		}
	}

	// make a sub-context so that our active cancellation does not affect parent
	ctx := streamOptions.context
	if ctx == nil {
		ctx = context.Background()
	}
	subCtx, cancelRequest := context.WithCancel(ctx)
	defer cancelRequest()

	req, err := http.NewRequestWithContext(ctx, method, url, streamOptions.in)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "plain/text")
	}
	for key, val := range streamOptions.headers {
		req.Header.Set(key, val)
	}
	var resp *http.Response
	protocol := c.endpointURL.Scheme
	address := c.endpointURL.Path
	if streamOptions.stdout == nil {
		streamOptions.stdout = io.Discard
	}
	if streamOptions.stderr == nil {
		streamOptions.stderr = io.Discard
	}

	if protocol == unixProtocol || protocol == namedPipeProtocol {
		var dial net.Conn
		dial, err = c.Dialer.Dial(protocol, address)
		if err != nil {
			return err
		}
		go func() {
			<-subCtx.Done()
			dial.Close()
		}()
		breader := bufio.NewReader(dial)
		err = req.Write(dial)
		if err != nil {
			return chooseError(subCtx, err)
		}

		// ReadResponse may hang if server does not replay
		if streamOptions.timeout > 0 {
			dial.SetDeadline(time.Now().Add(streamOptions.timeout))
		}

		if streamOptions.reqSent != nil {
			close(streamOptions.reqSent)
		}
		if resp, err = http.ReadResponse(breader, req); err != nil {
			// Cancel timeout for future I/O operations
			if streamOptions.timeout > 0 {
				dial.SetDeadline(time.Time{})
			}
			if strings.Contains(err.Error(), "connection refused") {
				return ErrConnectionRefused
			}

			return chooseError(subCtx, err)
		}
		defer resp.Body.Close()
	} else {
		if resp, err = c.HTTPClient.Do(req.WithContext(subCtx)); err != nil {
			if strings.Contains(err.Error(), "connection refused") {
				return ErrConnectionRefused
			}
			return chooseError(subCtx, err)
		}
		defer resp.Body.Close()
		if streamOptions.reqSent != nil {
			close(streamOptions.reqSent)
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return newError(resp)
	}
	var canceled uint32
	if streamOptions.inactivityTimeout > 0 {
		var ch chan<- struct{}
		resp.Body, ch = handleInactivityTimeout(resp.Body, streamOptions.inactivityTimeout, cancelRequest, &canceled)
		defer close(ch)
	}
	err = handleStreamResponse(resp, &streamOptions)
	if err != nil {
		if atomic.LoadUint32(&canceled) != 0 {
			return ErrInactivityTimeout
		}
		return chooseError(subCtx, err)
	}
	return nil
}

func handleStreamResponse(resp *http.Response, streamOptions *streamOptions) error {
	var err error
	if !streamOptions.useJSONDecoder && resp.Header.Get("Content-Type") != "application/json" {
		if streamOptions.setRawTerminal {
			_, err = io.Copy(streamOptions.stdout, resp.Body)
		} else {
			_, err = stdcopy.StdCopy(streamOptions.stdout, streamOptions.stderr, resp.Body)
		}
		return err
	}
	// if we want to get raw json stream, just copy it back to output
	// without decoding it
	if streamOptions.rawJSONStream {
		_, err = io.Copy(streamOptions.stdout, resp.Body)
		return err
	}
	if st, ok := streamOptions.stdout.(stream); ok {
		err = jsonmessage.DisplayJSONMessagesToStream(resp.Body, st, nil)
	} else {
		err = jsonmessage.DisplayJSONMessagesStream(resp.Body, streamOptions.stdout, 0, false, nil)
	}
	return err
}

type stream interface {
	io.Writer
	FD() uintptr
	IsTerminal() bool
}

type proxyReader struct {
	io.ReadCloser
	calls uint64
}

func (p *proxyReader) callCount() uint64 {
	return atomic.LoadUint64(&p.calls)
}

func (p *proxyReader) Read(data []byte) (int, error) {
	atomic.AddUint64(&p.calls, 1)
	return p.ReadCloser.Read(data)
}

func handleInactivityTimeout(reader io.ReadCloser, timeout time.Duration, cancelRequest func(), canceled *uint32) (io.ReadCloser, chan<- struct{}) {
	done := make(chan struct{})
	proxyReader := &proxyReader{ReadCloser: reader}
	go func() {
		var lastCallCount uint64
		for {
			select {
			case <-time.After(timeout):
			case <-done:
				return
			}
			curCallCount := proxyReader.callCount()
			if curCallCount == lastCallCount {
				atomic.AddUint32(canceled, 1)
				cancelRequest()
				return
			}
			lastCallCount = curCallCount
		}
	}()
	return proxyReader, done
}

type hijackOptions struct {
	success        chan struct{}
	setRawTerminal bool
	in             io.Reader
	stdout         io.Writer
	stderr         io.Writer
	data           any
}

// CloseWaiter is an interface with methods for closing the underlying resource
// and then waiting for it to finish processing.
type CloseWaiter interface {
	io.Closer
	Wait() error
}

type waiterFunc func() error

func (w waiterFunc) Wait() error { return w() }

type closerFunc func() error

func (c closerFunc) Close() error { return c() }

func (c *Client) hijack(method, path string, hijackOptions hijackOptions) (CloseWaiter, error) {
	if path != "/version" && !c.SkipServerVersionCheck && c.expectedAPIVersion == nil {
		err := c.checkAPIVersion()
		if err != nil {
			return nil, err
		}
	}
	var params io.Reader
	if hijackOptions.data != nil {
		buf, err := json.Marshal(hijackOptions.data)
		if err != nil {
			return nil, err
		}
		params = bytes.NewBuffer(buf)
	}
	req, err := http.NewRequest(method, c.getURL(path), params)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "tcp")
	protocol := c.endpointURL.Scheme
	address := c.endpointURL.Path
	if protocol != unixProtocol && protocol != namedPipeProtocol {
		protocol = "tcp"
		address = c.endpointURL.Host
	}
	var dial net.Conn
	if c.TLSConfig != nil && protocol != unixProtocol && protocol != namedPipeProtocol {
		netDialer, ok := c.Dialer.(*net.Dialer)
		if !ok {
			return nil, ErrTLSNotSupported
		}
		dial, err = tlsDialWithDialer(netDialer, protocol, address, c.TLSConfig)
		if err != nil {
			return nil, err
		}
	} else {
		dial, err = c.Dialer.Dial(protocol, address)
		if err != nil {
			return nil, err
		}
	}

	errs := make(chan error, 1)
	quit := make(chan struct{})
	go func() {
		//lint:ignore SA1019 the alternative doesn't quite work, so keep using the deprecated thing.
		clientconn := httputil.NewClientConn(dial, nil)
		defer clientconn.Close()
		clientconn.Do(req)
		if hijackOptions.success != nil {
			hijackOptions.success <- struct{}{}
			<-hijackOptions.success
		}
		rwc, br := clientconn.Hijack()
		defer rwc.Close()

		errChanOut := make(chan error, 1)
		errChanIn := make(chan error, 2)
		if hijackOptions.stdout == nil && hijackOptions.stderr == nil {
			close(errChanOut)
		} else {
			// Only copy if hijackOptions.stdout and/or hijackOptions.stderr is actually set.
			// Otherwise, if the only stream you care about is stdin, your attach session
			// will "hang" until the container terminates, even though you're not reading
			// stdout/stderr
			if hijackOptions.stdout == nil {
				hijackOptions.stdout = io.Discard
			}
			if hijackOptions.stderr == nil {
				hijackOptions.stderr = io.Discard
			}

			go func() {
				defer func() {
					if hijackOptions.in != nil {
						if closer, ok := hijackOptions.in.(io.Closer); ok {
							closer.Close()
						}
						errChanIn <- nil
					}
				}()

				var err error
				if hijackOptions.setRawTerminal {
					_, err = io.Copy(hijackOptions.stdout, br)
				} else {
					_, err = stdcopy.StdCopy(hijackOptions.stdout, hijackOptions.stderr, br)
				}
				errChanOut <- err
			}()
		}

		go func() {
			var err error
			if hijackOptions.in != nil {
				_, err = io.Copy(rwc, hijackOptions.in)
			}
			errChanIn <- err
			rwc.(interface {
				CloseWrite() error
			}).CloseWrite()
		}()

		var errIn error
		select {
		case errIn = <-errChanIn:
		case <-quit:
		}

		var errOut error
		select {
		case errOut = <-errChanOut:
		case <-quit:
		}

		if errIn != nil {
			errs <- errIn
		} else {
			errs <- errOut
		}
	}()

	return struct {
		closerFunc
		waiterFunc
	}{
		closerFunc(func() error { close(quit); return nil }),
		waiterFunc(func() error { return <-errs }),
	}, nil
}

func (c *Client) getURL(path string) string {
	urlStr := strings.TrimRight(c.endpointURL.String(), "/")
	if c.endpointURL.Scheme == unixProtocol || c.endpointURL.Scheme == namedPipeProtocol {
		urlStr = ""
	}
	if c.requestedAPIVersion != nil {
		return fmt.Sprintf("%s/v%s%s", urlStr, c.requestedAPIVersion, path)
	}
	return fmt.Sprintf("%s%s", urlStr, path)
}

func (c *Client) getPath(basepath string, opts any) (string, error) {
	queryStr, requiredAPIVersion := queryStringVersion(opts)
	return c.pathVersionCheck(basepath, queryStr, requiredAPIVersion)
}

func (c *Client) pathVersionCheck(basepath, queryStr string, requiredAPIVersion APIVersion) (string, error) {
	urlStr := strings.TrimRight(c.endpointURL.String(), "/")
	if c.endpointURL.Scheme == unixProtocol || c.endpointURL.Scheme == namedPipeProtocol {
		urlStr = ""
	}
	if c.requestedAPIVersion != nil {
		if c.requestedAPIVersion.GreaterThanOrEqualTo(requiredAPIVersion) {
			return fmt.Sprintf("%s/v%s%s?%s", urlStr, c.requestedAPIVersion, basepath, queryStr), nil
		}
		return "", fmt.Errorf("API %s requires version %s, requested version %s is insufficient",
			basepath, requiredAPIVersion, c.requestedAPIVersion)
	}
	if requiredAPIVersion != nil {
		return fmt.Sprintf("%s/v%s%s?%s", urlStr, requiredAPIVersion, basepath, queryStr), nil
	}
	return fmt.Sprintf("%s%s?%s", urlStr, basepath, queryStr), nil
}

// getFakeNativeURL returns the URL needed to make an HTTP request over a UNIX
// domain socket to the given path.
func (c *Client) getFakeNativeURL(path string) string {
	u := *c.endpointURL // Copy.

	// Override URL so that net/http will not complain.
	u.Scheme = "http"
	u.Host = "unix.sock" // Doesn't matter what this is - it's not used.
	u.Path = ""
	urlStr := strings.TrimRight(u.String(), "/")
	if c.requestedAPIVersion != nil {
		return fmt.Sprintf("%s/v%s%s", urlStr, c.requestedAPIVersion, path)
	}
	return fmt.Sprintf("%s%s", urlStr, path)
}

func queryStringVersion(opts any) (string, APIVersion) {
	if opts == nil {
		return "", nil
	}
	value := reflect.ValueOf(opts)
	if value.Kind() == reflect.Ptr {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return "", nil
	}
	var apiVersion APIVersion
	items := url.Values(map[string][]string{})
	for i := 0; i < value.NumField(); i++ {
		field := value.Type().Field(i)
		if field.PkgPath != "" {
			continue
		}
		key := field.Tag.Get("qs")
		if key == "" {
			key = strings.ToLower(field.Name)
		} else if key == "-" {
			continue
		}
		if addQueryStringValue(items, key, value.Field(i)) {
			verstr := field.Tag.Get("ver")
			if verstr != "" {
				ver, _ := NewAPIVersion(verstr)
				if apiVersion == nil {
					apiVersion = ver
				} else if ver.GreaterThan(apiVersion) {
					apiVersion = ver
				}
			}
		}
	}
	return items.Encode(), apiVersion
}

func queryString(opts any) string {
	s, _ := queryStringVersion(opts)
	return s
}

func addQueryStringValue(items url.Values, key string, v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			items.Add(key, "1")
			return true
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Int() > 0 {
			items.Add(key, strconv.FormatInt(v.Int(), 10))
			return true
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if v.Uint() > 0 {
			items.Add(key, strconv.FormatUint(v.Uint(), 10))
			return true
		}
	case reflect.Float32, reflect.Float64:
		if v.Float() > 0 {
			items.Add(key, strconv.FormatFloat(v.Float(), 'f', -1, 64))
			return true
		}
	case reflect.String:
		if v.String() != "" {
			items.Add(key, v.String())
			return true
		}
	case reflect.Ptr:
		if !v.IsNil() {
			if b, err := json.Marshal(v.Interface()); err == nil {
				items.Add(key, string(b))
				return true
			}
		}
	case reflect.Map:
		if len(v.MapKeys()) > 0 {
			if b, err := json.Marshal(v.Interface()); err == nil {
				items.Add(key, string(b))
				return true
			}
		}
	case reflect.Array, reflect.Slice:
		vLen := v.Len()
		var valuesAdded int
		if vLen > 0 {
			for i := 0; i < vLen; i++ {
				if addQueryStringValue(items, key, v.Index(i)) {
					valuesAdded++
				}
			}
		}
		return valuesAdded > 0
	}
	return false
}

// Error represents failures in the API. It represents a failure from the API.
type Error struct {
	Status  int
	Message string
}

func newError(resp *http.Response) *Error {
	type ErrMsg struct {
		Message string `json:"message"`
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return &Error{Status: resp.StatusCode, Message: fmt.Sprintf("cannot read body, err: %v", err)}
	}
	var emsg ErrMsg
	err = json.Unmarshal(data, &emsg)
	if err != nil {
		return &Error{Status: resp.StatusCode, Message: string(data)}
	}
	return &Error{Status: resp.StatusCode, Message: emsg.Message}
}

func (e *Error) Error() string {
	return fmt.Sprintf("API error (%d): %s", e.Status, e.Message)
}

func parseEndpoint(endpoint string, tls bool) (*url.URL, error) {
	if endpoint != "" && !strings.Contains(endpoint, "://") {
		endpoint = "tcp://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, ErrInvalidEndpoint
	}
	if tls && u.Scheme != "unix" {
		u.Scheme = "https"
	}
	switch u.Scheme {
	case unixProtocol, namedPipeProtocol:
		return u, nil
	case "http", "https", "tcp":
		_, port, err := net.SplitHostPort(u.Host)
		if err != nil {
			var e *net.AddrError
			if errors.As(err, &e) {
				if e.Err == "missing port in address" {
					return u, nil
				}
			}
			return nil, ErrInvalidEndpoint
		}
		number, err := strconv.ParseInt(port, 10, 64)
		if err == nil && number > 0 && number < 65536 {
			if u.Scheme == "tcp" {
				if tls {
					u.Scheme = "https"
				} else {
					u.Scheme = "http"
				}
			}
			return u, nil
		}
		return nil, ErrInvalidEndpoint
	default:
		return nil, ErrInvalidEndpoint
	}
}

type dockerEnv struct {
	dockerHost      string
	dockerTLSVerify bool
	dockerCertPath  string
}

func getDockerEnv() (*dockerEnv, error) {
	dockerHost := os.Getenv("DOCKER_HOST")
	var err error
	if dockerHost == "" {
		dockerHost = defaultHost
	}
	dockerTLSVerify := os.Getenv("DOCKER_TLS_VERIFY") != ""
	var dockerCertPath string
	if dockerTLSVerify {
		dockerCertPath = os.Getenv("DOCKER_CERT_PATH")
		if dockerCertPath == "" {
			home := homedir.Get()
			if home == "" {
				return nil, errors.New("environment variable HOME must be set if DOCKER_CERT_PATH is not set")
			}
			dockerCertPath = filepath.Join(home, ".docker")
			dockerCertPath, err = filepath.Abs(dockerCertPath)
			if err != nil {
				return nil, err
			}
		}
	}
	return &dockerEnv{
		dockerHost:      dockerHost,
		dockerTLSVerify: dockerTLSVerify,
		dockerCertPath:  dockerCertPath,
	}, nil
}

// defaultTransport returns a new http.Transport with similar default values to
// http.DefaultTransport, but with idle connections and keepalives disabled.
func defaultTransport() *http.Transport {
	transport := defaultPooledTransport()
	transport.DisableKeepAlives = true
	transport.MaxIdleConnsPerHost = -1
	return transport
}

// defaultPooledTransport returns a new http.Transport with similar default
// values to http.DefaultTransport. Do not use this for transient transports as
// it can leak file descriptors over time. Only use this for transports that
// will be re-used for the same host(s).
func defaultPooledTransport() *http.Transport {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConnsPerHost:   runtime.GOMAXPROCS(0) + 1,
	}
	return transport
}

// defaultClient returns a new http.Client with similar default values to
// http.Client, but with a non-shared Transport, idle connections disabled, and
// keepalives disabled.
func defaultClient() *http.Client {
	return &http.Client{
		Transport: defaultTransport(),
	}
}
