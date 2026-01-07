package passdriver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"go.podman.io/common/pkg/secrets/define"
	"go.podman.io/storage/pkg/fileutils"
)

type driverConfig struct {
	// Root contains the root directory where the secrets are stored
	Root string
	// KeyID contains the key id that will be used for encryption (i.e. user@domain.tld)
	KeyID string
	// GPGHomedir is the homedir where the GPG keys are stored
	GPGHomedir string
}

func (cfg *driverConfig) ParseOpts(opts map[string]string) {
	if val, ok := opts["root"]; ok {
		cfg.Root = val
		cfg.findGpgID() // try to find a .gpg-id in the parent directories of Root
	}
	if val, ok := opts["key"]; ok {
		cfg.KeyID = val
	}
	if val, ok := opts["gpghomedir"]; ok {
		cfg.GPGHomedir = val
	}
}

func defaultDriverConfig() *driverConfig {
	cfg := &driverConfig{}

	if home, err := os.UserHomeDir(); err == nil {
		defaultLocations := []string{
			filepath.Join(home, ".password-store"),
			filepath.Join(home, ".local/share/gopass/stores/root"),
		}
		for _, path := range defaultLocations {
			if stat, err := os.Stat(path); err != nil || !stat.IsDir() {
				continue
			}
			cfg.Root = path
			bs, err := os.ReadFile(filepath.Join(path, ".gpg-id"))
			if err != nil {
				continue
			}
			cfg.KeyID = string(bytes.Trim(bs, "\r\n"))
			break
		}
	}

	return cfg
}

func (cfg *driverConfig) findGpgID() {
	path := cfg.Root
	for len(path) > 1 {
		if err := fileutils.Exists(filepath.Join(path, ".gpg-id")); err == nil {
			bs, err := os.ReadFile(filepath.Join(path, ".gpg-id"))
			if err != nil {
				continue
			}
			cfg.KeyID = string(bytes.Trim(bs, "\r\n"))
			break
		}
		path = filepath.Dir(path)
	}
}

// Driver is the passdriver object.
type Driver struct {
	driverConfig
}

// NewDriver creates a new secret driver.
func NewDriver(opts map[string]string) (*Driver, error) {
	cfg := defaultDriverConfig()
	cfg.ParseOpts(opts)

	driver := &Driver{
		driverConfig: *cfg,
	}

	return driver, nil
}

// List returns all secret IDs.
func (d *Driver) List() (secrets []string, err error) {
	files, err := os.ReadDir(d.Root)
	if err != nil {
		return nil, fmt.Errorf("failed to read secret directory: %w", err)
	}
	for _, f := range files {
		fileName := f.Name()
		withoutSuffix := fileName[:len(fileName)-len(".gpg")]
		secrets = append(secrets, withoutSuffix)
	}
	sort.Strings(secrets)
	return secrets, nil
}

// Lookup returns the bytes associated with a secret ID.
func (d *Driver) Lookup(id string) ([]byte, error) {
	out := &bytes.Buffer{}
	key, err := d.getPath(id)
	if err != nil {
		return nil, err
	}
	if err := d.gpg(context.TODO(), nil, out, "--decrypt", key); err != nil {
		return nil, fmt.Errorf("%s: %w", id, define.ErrNoSuchSecret)
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("%s: %w", id, define.ErrNoSuchSecret)
	}
	return out.Bytes(), nil
}

// Store saves the bytes associated with an ID. An error is returned if the ID already exists.
func (d *Driver) Store(id string, data []byte) error {
	if _, err := d.Lookup(id); err == nil {
		return fmt.Errorf("%s: %w", id, define.ErrSecretIDExists)
	}
	in := bytes.NewReader(data)
	key, err := d.getPath(id)
	if err != nil {
		return err
	}
	return d.gpg(context.TODO(), in, nil, "--encrypt", "-r", d.KeyID, "-o", key)
}

// Delete removes the secret associated with the specified ID.  An error is returned if no matching secret is found.
func (d *Driver) Delete(id string) error {
	key, err := d.getPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(key); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s: %w", id, define.ErrNoSuchSecret)
		}
		return fmt.Errorf("%s: %w", id, err)
	}
	return nil
}

func (d *Driver) gpg(ctx context.Context, in io.Reader, out io.Writer, args ...string) error {
	if d.GPGHomedir != "" {
		args = append([]string{"--homedir", d.GPGHomedir}, args...)
	}
	cmd := exec.CommandContext(ctx, "gpg", args...)
	cmd.Env = os.Environ()
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func (d *Driver) getPath(id string) (string, error) {
	path, err := filepath.Abs(filepath.Join(d.Root, id))
	if err != nil {
		return "", define.ErrInvalidKey
	}
	if !strings.HasPrefix(path, d.Root) {
		return "", define.ErrInvalidKey
	}
	return path + ".gpg", nil
}
