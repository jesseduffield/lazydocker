// FIXME(thaJeztah): remove once we are a module; the go:build directive prevents go from downgrading language version to go1.16:
//go:build go1.21

package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/ioutils"
	"github.com/fvbommel/sortorder"
	"github.com/pkg/errors"
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
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		return err
	}
	bytes, err := json.Marshal(&meta)
	if err != nil {
		return err
	}
	return ioutils.AtomicWriteFile(filepath.Join(contextDir, metaFile), bytes, 0o644)
}

func parseTypedOrMap(payload []byte, getter TypeGetter) (any, error) {
	if len(payload) == 0 || string(payload) == "null" {
		return nil, nil
	}
	if getter == nil {
		var res map[string]any
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

func (s *metadataStore) get(name string) (Metadata, error) {
	m, err := s.getByID(contextdirOf(name))
	if err != nil {
		return m, errors.Wrapf(err, "context %q", name)
	}
	return m, nil
}

func (s *metadataStore) getByID(id contextdir) (Metadata, error) {
	fileName := filepath.Join(s.contextDir(id), metaFile)
	bytes, err := os.ReadFile(fileName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Metadata{}, errdefs.NotFound(errors.Wrap(err, "context not found"))
		}
		return Metadata{}, err
	}
	var untyped untypedContextMetadata
	r := Metadata{
		Endpoints: make(map[string]any),
	}
	if err := json.Unmarshal(bytes, &untyped); err != nil {
		return Metadata{}, fmt.Errorf("parsing %s: %v", fileName, err)
	}
	r.Name = untyped.Name
	if r.Metadata, err = parseTypedOrMap(untyped.Metadata, s.config.contextType); err != nil {
		return Metadata{}, fmt.Errorf("parsing %s: %v", fileName, err)
	}
	for k, v := range untyped.Endpoints {
		if r.Endpoints[k], err = parseTypedOrMap(v, s.config.endpointTypes[k]); err != nil {
			return Metadata{}, fmt.Errorf("parsing %s: %v", fileName, err)
		}
	}
	return r, err
}

func (s *metadataStore) remove(name string) error {
	if err := os.RemoveAll(s.contextDir(contextdirOf(name))); err != nil {
		return errors.Wrapf(err, "failed to remove metadata")
	}
	return nil
}

func (s *metadataStore) list() ([]Metadata, error) {
	ctxDirs, err := listRecursivelyMetadataDirs(s.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	res := make([]Metadata, 0, len(ctxDirs))
	for _, dir := range ctxDirs {
		c, err := s.getByID(contextdir(dir))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, errors.Wrap(err, "failed to read metadata")
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
	fis, err := os.ReadDir(root)
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
				result = append(result, filepath.Join(fi.Name(), s))
			}
		}
	}
	return result, nil
}

type untypedContextMetadata struct {
	Metadata  json.RawMessage            `json:"metadata,omitempty"`
	Endpoints map[string]json.RawMessage `json:"endpoints,omitempty"`
	Name      string                     `json:"name,omitempty"`
}
