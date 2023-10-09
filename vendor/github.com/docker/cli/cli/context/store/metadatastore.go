package store

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/fvbommel/sortorder"
)

const (
	metadataDir = "meta"
	metaFile    = "meta.json"
)

type metadataStore struct {
	root   string
	config Config
}

func (s *metadataStore) contextDir(id contextdir) string {
	return filepath.Join(s.root, string(id))
}

func (s *metadataStore) createOrUpdate(meta Metadata) error {
	contextDir := s.contextDir(contextdirOf(meta.Name))
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		return err
	}
	bytes, err := json.Marshal(&meta)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filepath.Join(contextDir, metaFile), bytes, 0644)
}

func parseTypedOrMap(payload []byte, getter TypeGetter) (interface{}, error) {
	if len(payload) == 0 || string(payload) == "null" {
		return nil, nil
	}
	if getter == nil {
		var res map[string]interface{}
		if err := json.Unmarshal(payload, &res); err != nil {
			return nil, err
		}
		return res, nil
	}
	typed := getter()
	if err := json.Unmarshal(payload, typed); err != nil {
		return nil, err
	}
	return reflect.ValueOf(typed).Elem().Interface(), nil
}

func (s *metadataStore) get(id contextdir) (Metadata, error) {
	contextDir := s.contextDir(id)
	bytes, err := ioutil.ReadFile(filepath.Join(contextDir, metaFile))
	if err != nil {
		return Metadata{}, convertContextDoesNotExist(err)
	}
	var untyped untypedContextMetadata
	r := Metadata{
		Endpoints: make(map[string]interface{}),
	}
	if err := json.Unmarshal(bytes, &untyped); err != nil {
		return Metadata{}, err
	}
	r.Name = untyped.Name
	if r.Metadata, err = parseTypedOrMap(untyped.Metadata, s.config.contextType); err != nil {
		return Metadata{}, err
	}
	for k, v := range untyped.Endpoints {
		if r.Endpoints[k], err = parseTypedOrMap(v, s.config.endpointTypes[k]); err != nil {
			return Metadata{}, err
		}
	}
	return r, err
}

func (s *metadataStore) remove(id contextdir) error {
	contextDir := s.contextDir(id)
	return os.RemoveAll(contextDir)
}

func (s *metadataStore) list() ([]Metadata, error) {
	ctxDirs, err := listRecursivelyMetadataDirs(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var res []Metadata
	for _, dir := range ctxDirs {
		c, err := s.get(contextdir(dir))
		if err != nil {
			return nil, err
		}
		res = append(res, c)
	}
	sort.Slice(res, func(i, j int) bool {
		return sortorder.NaturalLess(res[i].Name, res[j].Name)
	})
	return res, nil
}

func isContextDir(path string) bool {
	s, err := os.Stat(filepath.Join(path, metaFile))
	if err != nil {
		return false
	}
	return !s.IsDir()
}

func listRecursivelyMetadataDirs(root string) ([]string, error) {
	fis, err := ioutil.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, fi := range fis {
		if fi.IsDir() {
			if isContextDir(filepath.Join(root, fi.Name())) {
				result = append(result, fi.Name())
			}
			subs, err := listRecursivelyMetadataDirs(filepath.Join(root, fi.Name()))
			if err != nil {
				return nil, err
			}
			for _, s := range subs {
				result = append(result, fmt.Sprintf("%s/%s", fi.Name(), s))
			}
		}
	}
	return result, nil
}

func convertContextDoesNotExist(err error) error {
	if os.IsNotExist(err) {
		return &contextDoesNotExistError{}
	}
	return err
}

type untypedContextMetadata struct {
	Metadata  json.RawMessage            `json:"metadata,omitempty"`
	Endpoints map[string]json.RawMessage `json:"endpoints,omitempty"`
	Name      string                     `json:"name,omitempty"`
}
