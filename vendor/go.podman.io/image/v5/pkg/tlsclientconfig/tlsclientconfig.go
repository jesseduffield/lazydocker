package tlsclientconfig

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// SetupCertificates opens all .crt, .cert, and .key files in dir and appends / loads certs and key pairs as appropriate to tlsc
func SetupCertificates(dir string, tlsc *tls.Config) error {
	logrus.Debugf("Looking for TLS certificates and private keys in %s", dir)
	fs, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		if os.IsPermission(err) {
			logrus.Debugf("Skipping scan of %s due to permission error: %v", dir, err)
			return nil
		}
		return err
	}

	for _, f := range fs {
		fullPath := filepath.Join(dir, f.Name())
		if strings.HasSuffix(f.Name(), ".crt") {
			logrus.Debugf(" crt: %s", fullPath)
			data, err := os.ReadFile(fullPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					// file must have been removed between the directory listing
					// and the open call, ignore that as it is a expected race
					continue
				}
				return err
			}
			if tlsc.RootCAs == nil {
				systemPool, err := x509.SystemCertPool()
				if err != nil {
					return fmt.Errorf("unable to get system cert pool: %w", err)
				}
				tlsc.RootCAs = systemPool
			}
			tlsc.RootCAs.AppendCertsFromPEM(data)
		}
		if base, ok := strings.CutSuffix(f.Name(), ".cert"); ok {
			certName := f.Name()
			keyName := base + ".key"
			logrus.Debugf(" cert: %s", fullPath)
			if !hasFile(fs, keyName) {
				return fmt.Errorf("missing key %s for client certificate %s. Note that CA certificates should use the extension .crt", keyName, certName)
			}
			cert, err := tls.LoadX509KeyPair(filepath.Join(dir, certName), filepath.Join(dir, keyName))
			if err != nil {
				return err
			}
			tlsc.Certificates = append(slices.Clone(tlsc.Certificates), cert)
		}
		if base, ok := strings.CutSuffix(f.Name(), ".key"); ok {
			keyName := f.Name()
			certName := base + ".cert"
			logrus.Debugf(" key: %s", fullPath)
			if !hasFile(fs, certName) {
				return fmt.Errorf("missing client certificate %s for key %s", certName, keyName)
			}
		}
	}
	return nil
}

func hasFile(files []os.DirEntry, name string) bool {
	return slices.ContainsFunc(files, func(f os.DirEntry) bool {
		return f.Name() == name
	})
}

// NewTransport Creates a default transport
func NewTransport() *http.Transport {
	direct := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         direct.DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConns:        100,
	}
	return tr
}
