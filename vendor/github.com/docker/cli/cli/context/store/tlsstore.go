package store

import (
	"io/ioutil"
	"os"
	"path/filepath"
)

const tlsDir = "tls"

type tlsStore struct {
	root string
}

func (s *tlsStore) contextDir(id contextdir) string {
	return filepath.Join(s.root, string(id))
}

func (s *tlsStore) endpointDir(contextID contextdir, name string) string {
	return filepath.Join(s.root, string(contextID), name)
}

func (s *tlsStore) filePath(contextID contextdir, endpointName, filename string) string {
	return filepath.Join(s.root, string(contextID), endpointName, filename)
}

func (s *tlsStore) createOrUpdate(contextID contextdir, endpointName, filename string, data []byte) error {
	epdir := s.endpointDir(contextID, endpointName)
	parentOfRoot := filepath.Dir(s.root)
	if err := os.MkdirAll(parentOfRoot, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(epdir, 0700); err != nil {
		return err
	}
	return ioutil.WriteFile(s.filePath(contextID, endpointName, filename), data, 0600)
}

func (s *tlsStore) getData(contextID contextdir, endpointName, filename string) ([]byte, error) {
	data, err := ioutil.ReadFile(s.filePath(contextID, endpointName, filename))
	if err != nil {
		return nil, convertTLSDataDoesNotExist(endpointName, filename, err)
	}
	return data, nil
}

func (s *tlsStore) remove(contextID contextdir, endpointName, filename string) error {
	err := os.Remove(s.filePath(contextID, endpointName, filename))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *tlsStore) removeAllEndpointData(contextID contextdir, endpointName string) error {
	return os.RemoveAll(s.endpointDir(contextID, endpointName))
}

func (s *tlsStore) removeAllContextData(contextID contextdir) error {
	return os.RemoveAll(s.contextDir(contextID))
}

func (s *tlsStore) listContextData(contextID contextdir) (map[string]EndpointFiles, error) {
	epFSs, err := ioutil.ReadDir(s.contextDir(contextID))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]EndpointFiles{}, nil
		}
		return nil, err
	}
	r := make(map[string]EndpointFiles)
	for _, epFS := range epFSs {
		if epFS.IsDir() {
			epDir := s.endpointDir(contextID, epFS.Name())
			fss, err := ioutil.ReadDir(epDir)
			if err != nil {
				return nil, err
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

func convertTLSDataDoesNotExist(endpoint, file string, err error) error {
	if os.IsNotExist(err) {
		return &tlsDataDoesNotExistError{endpoint: endpoint, file: file}
	}
	return err
}
