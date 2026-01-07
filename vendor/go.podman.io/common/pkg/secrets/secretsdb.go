package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"go.podman.io/common/pkg/secrets/define"
)

type db struct {
	// Secrets maps a secret id to secret metadata
	Secrets map[string]Secret `json:"secrets"`
	// NameToID maps a secret name to a secret id
	NameToID map[string]string `json:"nameToID"`
	// IDToName maps a secret id to a secret name
	IDToName map[string]string `json:"idToName"`
	// lastModified is the time when the database was last modified on the file system
	lastModified time.Time
}

// loadDB loads database data into the in-memory cache if it has been modified.
func (s *SecretsManager) loadDB() error {
	// check if the db file exists
	fileInfo, err := os.Stat(s.secretsDBPath)
	if err != nil {
		if !os.IsExist(err) {
			// If the file doesn't exist, then there's no reason to update the db cache,
			// the db cache will show no entries anyway.
			// The file will be created later on a store()
			return nil
		}
		return err
	}

	// We check if the file has been modified after the last time it was loaded into the cache.
	// If the file has been modified, then we know that our cache is not up-to-date, so we load
	// the db into the cache.
	if s.db.lastModified.Equal(fileInfo.ModTime()) {
		return nil
	}

	file, err := os.Open(s.secretsDBPath)
	if err != nil {
		return err
	}
	defer file.Close()
	if err != nil {
		return err
	}

	byteValue, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	unmarshalled := new(db)
	if err := json.Unmarshal(byteValue, unmarshalled); err != nil {
		return err
	}
	s.db = unmarshalled
	s.db.lastModified = fileInfo.ModTime()

	return nil
}

// getNameAndID takes a secret's name, ID, or partial ID, and returns both its name and full ID.
func (s *SecretsManager) getNameAndID(nameOrID string) (name, id string, err error) {
	name, id, err = s.getExactNameAndID(nameOrID)
	if err == nil {
		return name, id, nil
	} else if !errors.Is(err, define.ErrNoSuchSecret) {
		return "", "", err
	}

	// ID prefix may have been given, iterate through all IDs.
	// ID and partial ID has a max length of 25, so we return if its greater than that.
	if len(nameOrID) > secretIDLength {
		return "", "", fmt.Errorf("no secret with name or id %q: %w", nameOrID, define.ErrNoSuchSecret)
	}
	exists := false
	var foundID, foundName string
	for id, name := range s.db.IDToName {
		if strings.HasPrefix(id, nameOrID) {
			if exists {
				return "", "", fmt.Errorf("more than one result secret with prefix %s: %w", nameOrID, errAmbiguous)
			}
			exists = true
			foundID = id
			foundName = name
		}
	}

	if exists {
		return foundName, foundID, nil
	}
	return "", "", fmt.Errorf("no secret with name or id %q: %w", nameOrID, define.ErrNoSuchSecret)
}

// getExactNameAndID takes a secret's name or ID and returns both its name and full ID.
func (s *SecretsManager) getExactNameAndID(nameOrID string) (name, id string, err error) {
	err = s.loadDB()
	if err != nil {
		return "", "", err
	}
	if name, ok := s.db.IDToName[nameOrID]; ok {
		id := nameOrID
		return name, id, nil
	}

	if id, ok := s.db.NameToID[nameOrID]; ok {
		name := nameOrID
		return name, id, nil
	}

	return "", "", fmt.Errorf("no secret with name or id %q: %w", nameOrID, define.ErrNoSuchSecret)
}

// exactSecretExists checks if the secret exists, given a name or ID
// Does not match partial name or IDs.
func (s *SecretsManager) exactSecretExists(nameOrID string) (bool, error) {
	_, _, err := s.getExactNameAndID(nameOrID)
	if err != nil {
		if errors.Is(err, define.ErrNoSuchSecret) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// lookupAll gets all secrets stored.
func (s *SecretsManager) lookupAll() (map[string]Secret, error) {
	err := s.loadDB()
	if err != nil {
		return nil, err
	}
	return s.db.Secrets, nil
}

// lookupSecret returns a secret with the given name, ID, or partial ID.
func (s *SecretsManager) lookupSecret(nameOrID string) (*Secret, error) {
	err := s.loadDB()
	if err != nil {
		return nil, err
	}
	_, id, err := s.getNameAndID(nameOrID)
	if err != nil {
		return nil, err
	}
	allSecrets, err := s.lookupAll()
	if err != nil {
		return nil, err
	}
	if secret, ok := allSecrets[id]; ok {
		return &secret, nil
	}

	return nil, fmt.Errorf("no secret with name or id %q: %w", nameOrID, define.ErrNoSuchSecret)
}

// Store creates a new secret in the secrets database.
// It deals with only storing metadata, not data payload.
func (s *SecretsManager) store(entry *Secret) error {
	err := s.loadDB()
	if err != nil {
		return err
	}

	s.db.Secrets[entry.ID] = *entry
	s.db.NameToID[entry.Name] = entry.ID
	s.db.IDToName[entry.ID] = entry.Name

	marshalled, err := json.MarshalIndent(s.db, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(s.secretsDBPath, marshalled, 0o600)
	if err != nil {
		return err
	}

	return nil
}

// delete deletes a secret from the secrets database, given a name, ID, or partial ID.
// It deals with only deleting metadata, not data payload.
func (s *SecretsManager) delete(nameOrID string) error {
	name, id, err := s.getNameAndID(nameOrID)
	if err != nil {
		return err
	}
	err = s.loadDB()
	if err != nil {
		return err
	}
	delete(s.db.Secrets, id)
	delete(s.db.NameToID, name)
	delete(s.db.IDToName, id)
	marshalled, err := json.MarshalIndent(s.db, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(s.secretsDBPath, marshalled, 0o600)
	if err != nil {
		return err
	}
	return nil
}
