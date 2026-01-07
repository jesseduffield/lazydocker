package config

import "fmt"

// DBBackend determines which supported database backend Podman should use.
type DBBackend int

const (
	// Unsupported database backend.  Used as a sane base value for the type.
	DBBackendUnsupported DBBackend = iota
	// BoltDB backend.
	DBBackendBoltDB
	// SQLite backend.
	DBBackendSQLite

	// DBBackendDefault describes that no explicit backend has been set.
	// It should default to sqlite unless there is already an existing boltdb,
	// this allows for backwards compatibility on upgrades. The actual detection
	// logic must live in podman as we only know there were to look for the file.
	DBBackendDefault

	stringBoltDB = "boltdb"
	stringSQLite = "sqlite"
)

// String returns the DBBackend's string representation.
func (d DBBackend) String() string {
	switch d {
	case DBBackendBoltDB:
		return stringBoltDB
	case DBBackendSQLite:
		return stringSQLite
	case DBBackendDefault:
		return ""
	default:
		return fmt.Sprintf("unsupported database backend: %d", d)
	}
}

// Validate returns whether the DBBackend is supported.
func (d DBBackend) Validate() error {
	switch d {
	case DBBackendBoltDB, DBBackendSQLite, DBBackendDefault:
		return nil
	default:
		return fmt.Errorf("unsupported database backend: %d", d)
	}
}

// ParseDBBackend parses the specified string into a DBBackend.
// An error is return for unsupported backends.
func ParseDBBackend(raw string) (DBBackend, error) {
	// NOTE: this function should be used for parsing the user-specified
	// values on Podman's CLI.
	switch raw {
	case stringBoltDB:
		return DBBackendBoltDB, nil
	case stringSQLite:
		return DBBackendSQLite, nil
	case "":
		return DBBackendDefault, nil
	default:
		return DBBackendUnsupported, fmt.Errorf("unsupported database backend: %q", raw)
	}
}
