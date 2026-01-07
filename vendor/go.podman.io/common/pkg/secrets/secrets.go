package secrets

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.podman.io/common/pkg/secrets/define"
	"go.podman.io/common/pkg/secrets/filedriver"
	"go.podman.io/common/pkg/secrets/passdriver"
	"go.podman.io/common/pkg/secrets/shelldriver"
	"go.podman.io/storage/pkg/lockfile"
	"go.podman.io/storage/pkg/stringid"
)

// maxSecretSize is the max size for secret data - 512kB.
const maxSecretSize = 512000

// secretIDLength is the character length of a secret ID - 25.
const secretIDLength = 25

// errInvalidPath indicates that the secrets path is invalid.
var errInvalidPath = errors.New("invalid secrets path")

// ErrNoSuchSecret indicates that the secret does not exist.
var ErrNoSuchSecret = define.ErrNoSuchSecret

// errSecretNameInUse indicates that the secret name is already in use.
var errSecretNameInUse = errors.New("secret name in use")

// errInvalidSecretName indicates that the secret name is invalid.
var errInvalidSecretName = errors.New("invalid secret name")

// errInvalidDriver indicates that the driver type is invalid.
var errInvalidDriver = errors.New("invalid driver")

// errInvalidDriverOpt indicates that a driver option is invalid.
var errInvalidDriverOpt = errors.New("invalid driver option")

// errAmbiguous indicates that a secret is ambiguous.
var errAmbiguous = errors.New("secret is ambiguous")

// errDataSize indicates that the secret data is too large or too small.
var errDataSize = errors.New("secret data must be larger than 0 and less than 512000 bytes")

// errIgnoreIfExistsAndReplace indicates that ignoreIfExists and replace cannot be used together.
var errIgnoreIfExistsAndReplace = errors.New("ignoreIfExists and replace cannot be used together")

// secretsFile is the name of the file that the secrets database will be stored in.
var secretsFile = "secrets.json"

// SecretsManager holds information on handling secrets
//
// revive does not like the name because the package is already called secrets
//
//nolint:revive
type SecretsManager struct {
	// secretsPath is the path to the db file where secrets are stored
	secretsDBPath string
	// lockfile is the locker for the secrets file
	lockfile *lockfile.LockFile
	// db is an in-memory cache of the database of secrets
	db *db
}

// Secret defines a secret.
type Secret struct {
	// Name is the name of the secret
	Name string `json:"name"`
	// ID is the unique secret ID
	ID string `json:"id"`
	// Labels are labels on the secret
	Labels map[string]string `json:"labels,omitempty"`
	// Metadata stores other metadata on the secret
	Metadata map[string]string `json:"metadata,omitempty"`
	// CreatedAt is when the secret was created
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when the secret was updated
	UpdatedAt time.Time `json:"updatedAt"`
	// Driver is the driver used to store secret data
	Driver string `json:"driver"`
	// DriverOptions are extra options used to run this driver
	DriverOptions map[string]string `json:"driverOptions"`
}

// SecretsDriver interfaces with the secrets data store.
// The driver stores the actual bytes of secret data, as opposed to
// the secret metadata.
// Currently only the unencrypted filedriver is implemented.
//
// revive does not like the name because the package is already called secrets
//
//nolint:revive
type SecretsDriver interface {
	// List lists all secret ids in the secrets data store
	List() ([]string, error)
	// Lookup gets the secret's data bytes
	Lookup(id string) ([]byte, error)
	// Store stores the secret's data bytes
	Store(id string, data []byte) error
	// Delete deletes a secret's data from the driver
	Delete(id string) error
}

// StoreOptions are optional metadata fields that can be set when storing a new secret.
type StoreOptions struct {
	// DriverOptions are extra options used to run this driver
	DriverOpts map[string]string
	// Metadata stores extra metadata on the secret
	Metadata map[string]string
	// Labels are labels on the secret
	Labels map[string]string
	// Replace existing secret
	Replace bool
	// Ignore if already exists
	IgnoreIfExists bool
}

// NewManager creates a new secrets manager
// rootPath is the directory where the secrets data file resides.
func NewManager(rootPath string) (*SecretsManager, error) {
	manager := new(SecretsManager)

	if !filepath.IsAbs(rootPath) {
		return nil, fmt.Errorf("path must be absolute: %s: %w", rootPath, errInvalidPath)
	}
	// the lockfile functions require that the rootPath dir is executable
	if err := os.MkdirAll(rootPath, 0o700); err != nil {
		return nil, err
	}

	lock, err := lockfile.GetLockFile(filepath.Join(rootPath, "secrets.lock"))
	if err != nil {
		return nil, err
	}
	manager.lockfile = lock
	manager.secretsDBPath = filepath.Join(rootPath, secretsFile)
	manager.db = new(db)
	manager.db.Secrets = make(map[string]Secret)
	manager.db.NameToID = make(map[string]string)
	manager.db.IDToName = make(map[string]string)
	return manager, nil
}

func (s *SecretsManager) newID() (string, error) {
	for {
		newID := stringid.GenerateNonCryptoID()
		// GenerateNonCryptoID() gives 64 characters, so we truncate to correct length
		newID = newID[0:secretIDLength]
		_, err := s.lookupSecret(newID)
		if err != nil {
			if errors.Is(err, define.ErrNoSuchSecret) {
				return newID, nil
			}
			return "", err
		}
	}
}

// Store takes a name, creates a secret and stores the secret metadata and the secret payload.
// It returns a generated ID that is associated with the secret.
// The max size for secret data is 512kB.
func (s *SecretsManager) Store(name string, data []byte, driverType string, options StoreOptions) (string, error) {
	err := validateSecretName(name)
	if err != nil {
		return "", err
	}

	if len(data) == 0 || len(data) >= maxSecretSize {
		return "", errDataSize
	}

	if options.IgnoreIfExists && options.Replace {
		return "", errIgnoreIfExistsAndReplace
	}

	var secr *Secret
	s.lockfile.Lock()
	defer s.lockfile.Unlock()

	exist, err := s.exactSecretExists(name)
	if err != nil {
		return "", err
	}

	if exist {
		if !options.Replace && !options.IgnoreIfExists {
			return "", fmt.Errorf("%s: %w", name, errSecretNameInUse)
		}
		secr, err = s.lookupSecret(name)
		if err != nil {
			return "", err
		}
		if options.IgnoreIfExists {
			return secr.ID, nil
		}
		secr.UpdatedAt = time.Now()
	} else {
		secr = new(Secret)
		secr.Name = name
		secr.CreatedAt = time.Now()
		secr.UpdatedAt = secr.CreatedAt
	}

	if options.Metadata == nil {
		options.Metadata = make(map[string]string)
	}
	if options.Labels == nil {
		options.Labels = make(map[string]string)
	}
	if options.DriverOpts == nil {
		options.DriverOpts = make(map[string]string)
	}

	secr.Driver = driverType
	secr.Metadata = options.Metadata
	secr.DriverOptions = options.DriverOpts
	secr.Labels = options.Labels

	driver, err := getDriver(driverType, options.DriverOpts)
	if err != nil {
		return "", err
	}

	if options.Replace {
		err := driver.Delete(secr.ID)
		if err != nil {
			if !errors.Is(err, define.ErrNoSuchSecret) {
				return "", fmt.Errorf("deleting driver secret %s: %w", secr.ID, err)
			}
		} else {
			if err := s.delete(secr.ID); err != nil && !errors.Is(err, define.ErrNoSuchSecret) {
				return "", fmt.Errorf("deleting secret %s: %w", secr.ID, err)
			}
		}
	}

	secr.ID, err = s.newID()
	if err != nil {
		return "", err
	}

	err = driver.Store(secr.ID, data)
	if err != nil {
		return "", fmt.Errorf("creating secret %s: %w", name, err)
	}

	err = s.store(secr)
	if err != nil {
		return "", fmt.Errorf("creating secret %s: %w", name, err)
	}

	return secr.ID, nil
}

// Delete removes all secret metadata and secret data associated with the specified secret.
// Delete takes a name, ID, or partial ID.
func (s *SecretsManager) Delete(nameOrID string) (string, error) {
	s.lockfile.Lock()
	defer s.lockfile.Unlock()

	secret, err := s.lookupSecret(nameOrID)
	if err != nil {
		return "", err
	}
	secretID := secret.ID

	driver, err := getDriver(secret.Driver, secret.DriverOptions)
	if err != nil {
		return "", err
	}

	err = driver.Delete(secretID)
	if err != nil {
		return "", fmt.Errorf("deleting secret %s: %w", nameOrID, err)
	}

	err = s.delete(secretID)
	if err != nil {
		return "", fmt.Errorf("deleting secret %s: %w", nameOrID, err)
	}
	return secretID, nil
}

// Lookup gives a secret's metadata given its name, ID, or partial ID.
func (s *SecretsManager) Lookup(nameOrID string) (*Secret, error) {
	s.lockfile.Lock()
	defer s.lockfile.Unlock()

	return s.lookupSecret(nameOrID)
}

// List lists all secrets.
func (s *SecretsManager) List() ([]Secret, error) {
	s.lockfile.Lock()
	defer s.lockfile.Unlock()

	secrets, err := s.lookupAll()
	if err != nil {
		return nil, err
	}
	return slices.Collect(maps.Values(secrets)), nil
}

// LookupSecretData returns secret metadata as well as secret data in bytes.
// The secret data can be looked up using its name, ID, or partial ID.
func (s *SecretsManager) LookupSecretData(nameOrID string) (*Secret, []byte, error) {
	s.lockfile.Lock()
	defer s.lockfile.Unlock()

	secret, err := s.lookupSecret(nameOrID)
	if err != nil {
		return nil, nil, err
	}
	driver, err := getDriver(secret.Driver, secret.DriverOptions)
	if err != nil {
		return nil, nil, err
	}
	data, err := driver.Lookup(secret.ID)
	if err != nil {
		return nil, nil, err
	}
	return secret, data, nil
}

// validateSecretName checks if the secret name is valid.
func validateSecretName(name string) error {
	if len(name) == 0 || len(name) > 253 || strings.ContainsAny(name, ",/=\000") {
		return fmt.Errorf("secret name %q can not include '=', '/', ',', or the '\\0' (NULL) and be between 1 and 253 characters: %w", name, errInvalidSecretName)
	}
	return nil
}

// getDriver creates a new driver.
func getDriver(name string, opts map[string]string) (SecretsDriver, error) {
	switch name {
	case "file":
		if path, ok := opts["path"]; ok {
			return filedriver.NewDriver(path)
		}
		return nil, fmt.Errorf("need path for filedriver: %w", errInvalidDriverOpt)
	case "pass":
		return passdriver.NewDriver(opts)
	case "shell":
		return shelldriver.NewDriver(opts)
	}
	return nil, errInvalidDriver
}
