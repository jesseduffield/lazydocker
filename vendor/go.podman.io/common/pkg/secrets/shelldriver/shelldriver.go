package shelldriver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"go.podman.io/common/pkg/secrets/define"
)

// errMissingConfig indicates that one or more of the external actions are not configured.
var errMissingConfig = errors.New("missing config value")

type driverConfig struct {
	// DeleteCommand contains a shell command that deletes a secret.
	// The secret id is provided as environment variable SECRET_ID
	DeleteCommand string
	// ListCommand contains a shell command that lists all secrets.
	// The output is expected to be one id per line
	ListCommand string
	// LookupCommand contains a shell command that retrieves a secret.
	// The secret id is provided as environment variable SECRET_ID
	LookupCommand string
	// StoreCommand contains a shell command that stores a secret.
	// The secret id is provided as environment variable SECRET_ID
	// The secret value itself is provided over stdin
	StoreCommand string
}

func (cfg *driverConfig) ParseOpts(opts map[string]string) error {
	for key, value := range opts {
		switch key {
		case "delete":
			cfg.DeleteCommand = value
		case "list":
			cfg.ListCommand = value
		case "lookup":
			cfg.LookupCommand = value
		case "store":
			cfg.StoreCommand = value
		default:
			return fmt.Errorf("invalid shell driver option: %q", key)
		}
	}
	if cfg.DeleteCommand == "" ||
		cfg.ListCommand == "" ||
		cfg.LookupCommand == "" ||
		cfg.StoreCommand == "" {
		return errMissingConfig
	}
	return nil
}

// Driver is the passdriver object.
type Driver struct {
	driverConfig
}

// NewDriver creates a new secret driver.
func NewDriver(opts map[string]string) (*Driver, error) {
	cfg := &driverConfig{}
	if err := cfg.ParseOpts(opts); err != nil {
		return nil, err
	}

	driver := &Driver{
		driverConfig: *cfg,
	}

	return driver, nil
}

// List returns all secret IDs.
func (d *Driver) List() (secrets []string, err error) {
	cmd := exec.CommandContext(context.TODO(), "/bin/sh", "-c", d.ListCommand)
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr

	buf := &bytes.Buffer{}
	cmd.Stdout = buf

	err = cmd.Run()
	if err != nil {
		return nil, err
	}

	for part := range bytes.SplitSeq(buf.Bytes(), []byte("\n")) {
		id := strings.Trim(string(part), " \r\n")
		if len(id) > 0 {
			secrets = append(secrets, id)
		}
	}
	sort.Strings(secrets)

	return secrets, nil
}

// Lookup returns the bytes associated with a secret ID.
func (d *Driver) Lookup(id string) ([]byte, error) {
	if strings.Contains(id, "..") {
		return nil, define.ErrInvalidKey
	}

	cmd := exec.CommandContext(context.TODO(), "/bin/sh", "-c", d.LookupCommand)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "SECRET_ID="+id)
	cmd.Stderr = os.Stderr

	buf := &bytes.Buffer{}
	cmd.Stdout = buf

	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", id, define.ErrNoSuchSecret)
	}
	return buf.Bytes(), nil
}

// Store saves the bytes associated with an ID. An error is returned if the ID already exists.
func (d *Driver) Store(id string, data []byte) error {
	if strings.Contains(id, "..") {
		return define.ErrInvalidKey
	}

	cmd := exec.CommandContext(context.TODO(), "/bin/sh", "-c", d.StoreCommand)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "SECRET_ID="+id)

	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = bytes.NewReader(data)

	return cmd.Run()
}

// Delete removes the secret associated with the specified ID.  An error is returned if no matching secret is found.
func (d *Driver) Delete(id string) error {
	if strings.Contains(id, "..") {
		return define.ErrInvalidKey
	}

	cmd := exec.CommandContext(context.TODO(), "/bin/sh", "-c", d.DeleteCommand)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "SECRET_ID="+id)

	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("%s: %w", id, define.ErrNoSuchSecret)
	}

	return nil
}
