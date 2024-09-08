package context

import (
	"os"

	"github.com/docker/cli/cli/context/store"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	caKey   = "ca.pem"
	certKey = "cert.pem"
	keyKey  = "key.pem"
)

// TLSData holds ca/cert/key raw data
type TLSData struct {
	CA   []byte
	Key  []byte
	Cert []byte
}

// ToStoreTLSData converts TLSData to the store representation
func (data *TLSData) ToStoreTLSData() *store.EndpointTLSData {
	if data == nil {
		return nil
	}
	result := store.EndpointTLSData{
		Files: make(map[string][]byte),
	}
	if data.CA != nil {
		result.Files[caKey] = data.CA
	}
	if data.Cert != nil {
		result.Files[certKey] = data.Cert
	}
	if data.Key != nil {
		result.Files[keyKey] = data.Key
	}
	return &result
}

// LoadTLSData loads TLS data from the store
func LoadTLSData(s store.Reader, contextName, endpointName string) (*TLSData, error) {
	tlsFiles, err := s.ListTLSFiles(contextName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to retrieve TLS files for context %q", contextName)
	}
	if epTLSFiles, ok := tlsFiles[endpointName]; ok {
		var tlsData TLSData
		for _, f := range epTLSFiles {
			data, err := s.GetTLSData(contextName, endpointName, f)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to retrieve TLS data (%s) for context %q", f, contextName)
			}
			switch f {
			case caKey:
				tlsData.CA = data
			case certKey:
				tlsData.Cert = data
			case keyKey:
				tlsData.Key = data
			default:
				logrus.Warnf("unknown file in context %s TLS bundle: %s", contextName, f)
			}
		}
		return &tlsData, nil
	}
	return nil, nil
}

// TLSDataFromFiles reads files into a TLSData struct (or returns nil if all paths are empty)
func TLSDataFromFiles(caPath, certPath, keyPath string) (*TLSData, error) {
	var (
		ca, cert, key []byte
		err           error
	)
	if caPath != "" {
		if ca, err = os.ReadFile(caPath); err != nil {
			return nil, err
		}
	}
	if certPath != "" {
		if cert, err = os.ReadFile(certPath); err != nil {
			return nil, err
		}
	}
	if keyPath != "" {
		if key, err = os.ReadFile(keyPath); err != nil {
			return nil, err
		}
	}
	if ca == nil && cert == nil && key == nil {
		return nil, nil
	}
	return &TLSData{CA: ca, Cert: cert, Key: key}, nil
}
