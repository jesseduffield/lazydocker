package store

import (
	"os"
	"path/filepath"

	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/ioutils"
	"github.com/pkg/errors"
)

const tlsDir = "tls"

type tlsStore struct {
	root string
}

func (s *tlsStore) contextDir(name string) string {
	return filepath.Join(s.root, string(contextdirOf(name)))
}

func (s *tlsStore) endpointDir(name, endpointName string) string {
	return filepath.Join(s.contextDir(name), endpointName)
}

func (s *tlsStore) createOrUpdate(name, endpointName, filename string, data []byte) error {
	parentOfRoot := filepath.Dir(s.root)
	if err := os.MkdirAll(parentOfRoot, 0o755); err != nil {
		return err
	}
	endpointDir := s.endpointDir(name, endpointName)
	if err := os.MkdirAll(endpointDir, 0o700); err != nil {
		return err
	}
	return ioutils.AtomicWriteFile(filepath.Join(endpointDir, filename), data, 0o600)
}

func (s *tlsStore) getData(name, endpointName, filename string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(s.endpointDir(name, endpointName), filename))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errdefs.NotFound(errors.Errorf("TLS data for %s/%s/%s does not exist", name, endpointName, filename))
		}
		return nil, errors.Wrapf(err, "failed to read TLS data for endpoint %s", endpointName)
	}
	return data, nil
}

// remove deletes all TLS data for the given context.
func (s *tlsStore) remove(name string) error {
	if err := os.RemoveAll(s.contextDir(name)); err != nil {
		return errors.Wrapf(err, "failed to remove TLS data")
	}
	return nil
}

func (s *tlsStore) removeEndpoint(name, endpointName string) error {
	if err := os.RemoveAll(s.endpointDir(name, endpointName)); err != nil {
		return errors.Wrapf(err, "failed to remove TLS data for endpoint %s", endpointName)
	}
	return nil
}

func (s *tlsStore) listContextData(name string) (map[string]EndpointFiles, error) {
	contextDir := s.contextDir(name)
	epFSs, err := os.ReadDir(contextDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]EndpointFiles{}, nil
		}
		return nil, errors.Wrapf(err, "failed to list TLS files for context %s", name)
	}
	r := make(map[string]EndpointFiles)
	for _, epFS := range epFSs {
		if epFS.IsDir() {
			fss, err := os.ReadDir(filepath.Join(contextDir, epFS.Name()))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, errors.Wrapf(err, "failed to list TLS files for endpoint %s", epFS.Name())
			}
			var files EndpointFiles
			for _, fs := range fss {
				if !fs.IsDir() {
					files = append(files, fs.Name())
				}
			}
			r[epFS.Name()] = files
		}
	}
	return r, nil
}

// EndpointFiles is a slice of strings representing file names
type EndpointFiles []string
