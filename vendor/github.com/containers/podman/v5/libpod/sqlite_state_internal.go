//go:build !remote

package libpod

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/types"

	// SQLite backend for database/sql
	_ "github.com/mattn/go-sqlite3"
)

func initSQLiteDB(conn *sql.DB) (defErr error) {
	// Start with a transaction to avoid "database locked" errors.
	// See https://github.com/mattn/go-sqlite3/issues/274#issuecomment-1429054597
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to create tables: %v", err)
			}
		}
	}()

	sameSchema, err := migrateSchemaIfNecessary(tx)
	if err != nil {
		return err
	}
	if !sameSchema {
		if err := createSQLiteTables(tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

func migrateSchemaIfNecessary(tx *sql.Tx) (bool, error) {
	// First, check if the DBConfig table exists
	checkRow := tx.QueryRow("SELECT 1 FROM sqlite_master WHERE type='table' AND name='DBConfig';")
	var check int
	if err := checkRow.Scan(&check); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("checking if DB config table exists: %w", err)
	}
	if check != 1 {
		// Table does not exist, fresh database, no need to migrate.
		return false, nil
	}

	row := tx.QueryRow("SELECT SchemaVersion FROM DBConfig;")
	var schemaVer int
	if err := row.Scan(&schemaVer); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Brand-new, unpopulated DB.
			// Schema was just created, so it has to be the latest.
			return false, nil
		}
		return false, fmt.Errorf("scanning schema version from DB config: %w", err)
	}

	// If the schema version 0 or less, it's invalid
	if schemaVer <= 0 {
		return false, fmt.Errorf("database schema version %d is invalid: %w", schemaVer, define.ErrInternal)
	}

	// Same schema -> nothing do to.
	if schemaVer == schemaVersion {
		return true, nil
	}

	// If the DB is a later schema than we support, we have to error
	if schemaVer > schemaVersion {
		return false, fmt.Errorf("database has schema version %d while this libpod version only supports version %d: %w",
			schemaVer, schemaVersion, define.ErrInternal)
	}

	// Perform schema migration here, one version at a time.

	return false, nil
}

// Initialize all required tables for the SQLite state
func createSQLiteTables(tx *sql.Tx) error {
	// Technically we could split the "CREATE TABLE IF NOT EXISTS" and ");"
	// bits off each command and add them in the for loop where we actually
	// run the SQL, but that seems unnecessary.
	const dbConfig = `
        CREATE TABLE IF NOT EXISTS DBConfig(
                ID            INTEGER PRIMARY KEY NOT NULL,
                SchemaVersion INTEGER NOT NULL,
                OS            TEXT    NOT NULL,
                StaticDir     TEXT    NOT NULL,
                TmpDir        TEXT    NOT NULL,
                GraphRoot     TEXT    NOT NULL,
                RunRoot       TEXT    NOT NULL,
                GraphDriver   TEXT    NOT NULL,
                VolumeDir     TEXT    NOT NULL,
                CHECK (ID IN (1))
        );`

	const idNamespace = `
        CREATE TABLE IF NOT EXISTS IDNamespace(
                ID TEXT PRIMARY KEY NOT NULL
        );`

	const containerConfig = `
        CREATE TABLE IF NOT EXISTS ContainerConfig(
                ID              TEXT    PRIMARY KEY NOT NULL,
                Name            TEXT    UNIQUE NOT NULL,
                PodID           TEXT,
                JSON            TEXT    NOT NULL,
                FOREIGN KEY (ID)    REFERENCES IDNamespace(ID)    DEFERRABLE INITIALLY DEFERRED,
                FOREIGN KEY (ID)    REFERENCES ContainerState(ID) DEFERRABLE INITIALLY DEFERRED,
                FOREIGN KEY (PodID) REFERENCES PodConfig(ID)
        );`

	const containerState = `
        CREATE TABLE IF NOT EXISTS ContainerState(
                ID       TEXT    PRIMARY KEY NOT NULL,
                State    INTEGER NOT NULL,
                ExitCode INTEGER,
                JSON     TEXT    NOT NULL,
                FOREIGN KEY (ID) REFERENCES ContainerConfig(ID) DEFERRABLE INITIALLY DEFERRED,
                CHECK (ExitCode BETWEEN -1 AND 255)
        );`

	const containerExecSession = `
        CREATE TABLE IF NOT EXISTS ContainerExecSession(
                ID          TEXT PRIMARY KEY NOT NULL,
                ContainerID TEXT NOT NULL,
                FOREIGN KEY (ContainerID) REFERENCES ContainerConfig(ID)
        );`

	const containerDependency = `
        CREATE TABLE IF NOT EXISTS ContainerDependency(
                ID           TEXT NOT NULL,
                DependencyID TEXT NOT NULL,
                PRIMARY KEY (ID, DependencyID),
                FOREIGN KEY (ID)           REFERENCES ContainerConfig(ID) DEFERRABLE INITIALLY DEFERRED,
                FOREIGN KEY (DependencyID) REFERENCES ContainerConfig(ID),
                CHECK (ID <> DependencyID)
        );`

	const containerVolume = `
        CREATE TABLE IF NOT EXISTS ContainerVolume(
                ContainerID TEXT NOT NULL,
                VolumeName  TEXT NOT NULL,
                PRIMARY KEY (ContainerID, VolumeName),
                FOREIGN KEY (ContainerID) REFERENCES ContainerConfig(ID) DEFERRABLE INITIALLY DEFERRED,
                FOREIGN KEY (VolumeName)  REFERENCES VolumeConfig(Name)
        );`

	const containerExitCode = `
        CREATE TABLE IF NOT EXISTS ContainerExitCode(
                ID        TEXT    PRIMARY KEY NOT NULL,
                Timestamp INTEGER NOT NULL,
                ExitCode  INTEGER NOT NULL,
                CHECK (ExitCode BETWEEN -1 AND 255)
        );`

	const podConfig = `
        CREATE TABLE IF NOT EXISTS PodConfig(
                ID              TEXT    PRIMARY KEY NOT NULL,
                Name            TEXT    UNIQUE NOT NULL,
                JSON            TEXT    NOT NULL,
                FOREIGN KEY (ID) REFERENCES IDNamespace(ID) DEFERRABLE INITIALLY DEFERRED,
                FOREIGN KEY (ID) REFERENCES PodState(ID)    DEFERRABLE INITIALLY DEFERRED
        );`

	const podState = `
        CREATE TABLE IF NOT EXISTS PodState(
                ID               TEXT PRIMARY KEY NOT NULL,
                InfraContainerID TEXT,
                JSON             TEXT NOT NULL,
                FOREIGN KEY (ID)               REFERENCES PodConfig(ID)       DEFERRABLE INITIALLY DEFERRED,
                FOREIGN KEY (InfraContainerID) REFERENCES ContainerConfig(ID) DEFERRABLE INITIALLY DEFERRED
        );`

	const volumeConfig = `
        CREATE TABLE IF NOT EXISTS VolumeConfig(
                Name            TEXT    PRIMARY KEY NOT NULL,
                StorageID       TEXT,
                JSON            TEXT    NOT NULL,
                FOREIGN KEY (Name) REFERENCES VolumeState(Name) DEFERRABLE INITIALLY DEFERRED
        );`

	const volumeState = `
        CREATE TABLE IF NOT EXISTS VolumeState(
                Name TEXT PRIMARY KEY NOT NULL,
                JSON TEXT NOT NULL,
                FOREIGN KEY (Name) REFERENCES VolumeConfig(Name) DEFERRABLE INITIALLY DEFERRED
        );`

	tables := map[string]string{
		"DBConfig":             dbConfig,
		"IDNamespace":          idNamespace,
		"ContainerConfig":      containerConfig,
		"ContainerState":       containerState,
		"ContainerExecSession": containerExecSession,
		"ContainerDependency":  containerDependency,
		"ContainerVolume":      containerVolume,
		"ContainerExitCode":    containerExitCode,
		"PodConfig":            podConfig,
		"PodState":             podState,
		"VolumeConfig":         volumeConfig,
		"VolumeState":          volumeState,
	}

	for tblName, cmd := range tables {
		if _, err := tx.Exec(cmd); err != nil {
			return fmt.Errorf("creating table %s: %w", tblName, err)
		}
	}
	return nil
}

// Get the config of a container with the given ID from the database
func (s *SQLiteState) getCtrConfig(id string) (*ContainerConfig, error) {
	row := s.conn.QueryRow("SELECT JSON FROM ContainerConfig WHERE ID=?;", id)

	var rawJSON string
	if err := row.Scan(&rawJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, define.ErrNoSuchCtr
		}
		return nil, fmt.Errorf("retrieving container %s config from DB: %w", id, err)
	}

	ctrCfg := new(ContainerConfig)

	if err := json.Unmarshal([]byte(rawJSON), ctrCfg); err != nil {
		return nil, fmt.Errorf("unmarshalling container %s config: %w", id, err)
	}

	return ctrCfg, nil
}

// Finalize a container that was pulled out of the database.
func finalizeCtrSqlite(ctr *Container) error {
	// Get the lock
	lock, err := ctr.runtime.lockManager.RetrieveLock(ctr.config.LockID)
	if err != nil {
		return fmt.Errorf("retrieving lock for container %s: %w", ctr.ID(), err)
	}
	ctr.lock = lock

	// Get the OCI runtime
	if ctr.config.OCIRuntime == "" {
		ctr.ociRuntime = ctr.runtime.defaultOCIRuntime
	} else {
		// Handle legacy containers which might use a literal path for
		// their OCI runtime name.
		runtimeName := ctr.config.OCIRuntime
		ociRuntime, ok := ctr.runtime.ociRuntimes[runtimeName]
		if !ok {
			runtimeSet := false

			// If the path starts with a / and exists, make a new
			// OCI runtime for it using the full path.
			if strings.HasPrefix(runtimeName, "/") {
				if stat, err := os.Stat(runtimeName); err == nil && !stat.IsDir() {
					newOCIRuntime, err := newConmonOCIRuntime(runtimeName, []string{runtimeName}, ctr.runtime.conmonPath, ctr.runtime.runtimeFlags, ctr.runtime.config)
					if err == nil {
						// TODO: There is a potential risk of concurrent map modification here.
						// This is an unlikely case, though.
						ociRuntime = newOCIRuntime
						ctr.runtime.ociRuntimes[runtimeName] = ociRuntime
						runtimeSet = true
					}
				}
			}

			if !runtimeSet {
				// Use a MissingRuntime implementation
				ociRuntime = getMissingRuntime(runtimeName, ctr.runtime)
			}
		}
		ctr.ociRuntime = ociRuntime
	}

	ctr.valid = true

	return nil
}

// Finalize a pod that was pulled out of the database.
func (s *SQLiteState) createPod(rawJSON string) (*Pod, error) {
	config := new(PodConfig)
	if err := json.Unmarshal([]byte(rawJSON), config); err != nil {
		return nil, fmt.Errorf("unmarshalling pod config: %w", err)
	}
	lock, err := s.runtime.lockManager.RetrieveLock(config.LockID)
	if err != nil {
		return nil, fmt.Errorf("retrieving lock for pod %s: %w", config.ID, err)
	}

	pod := new(Pod)
	pod.config = config
	pod.state = new(podState)
	pod.lock = lock
	pod.runtime = s.runtime
	pod.valid = true

	return pod, nil
}

// Finalize a volume that was pulled out of the database
func finalizeVolumeSqlite(vol *Volume) error {
	// Get the lock
	lock, err := vol.runtime.lockManager.RetrieveLock(vol.config.LockID)
	if err != nil {
		return fmt.Errorf("retrieving lock for volume %s: %w", vol.Name(), err)
	}
	vol.lock = lock

	// Retrieve volume driver
	if vol.UsesVolumeDriver() {
		plugin, err := vol.runtime.getVolumePlugin(vol.config)
		if err != nil {
			// We want to fail gracefully here, to ensure that we
			// can still remove volumes even if their plugin is
			// missing. Otherwise, we end up with volumes that
			// cannot even be retrieved from the database and will
			// cause things like `volume ls` to fail.
			logrus.Errorf("Volume %s uses volume plugin %s, but it cannot be accessed - some functionality may not be available: %v", vol.Name(), vol.config.Driver, err)
		} else {
			vol.plugin = plugin
		}
	}

	vol.valid = true

	return nil
}

func (s *SQLiteState) rewriteContainerConfig(ctr *Container, newCfg *ContainerConfig) (defErr error) {
	json, err := json.Marshal(newCfg)
	if err != nil {
		return fmt.Errorf("error marshalling container %s new config JSON: %w", ctr.ID(), err)
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction to rewrite container %s config: %w", ctr.ID(), err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to rewrite container %s config: %v", ctr.ID(), err)
			}
		}
	}()

	results, err := tx.Exec("UPDATE ContainerConfig SET Name=?, JSON=? WHERE ID=?;", newCfg.Name, json, ctr.ID())
	if err != nil {
		return fmt.Errorf("updating container config table with new configuration for container %s: %w", ctr.ID(), err)
	}
	rows, err := results.RowsAffected()
	if err != nil {
		return fmt.Errorf("retrieving container %s config rewrite rows affected: %w", ctr.ID(), err)
	}
	if rows == 0 {
		ctr.valid = false
		return define.ErrNoSuchCtr
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction to rewrite container %s config: %w", ctr.ID(), err)
	}

	return nil
}

func (s *SQLiteState) addContainer(ctr *Container) (defErr error) {
	configJSON, err := json.Marshal(ctr.config)
	if err != nil {
		return fmt.Errorf("marshalling container config json: %w", err)
	}

	stateJSON, err := json.Marshal(ctr.state)
	if err != nil {
		return fmt.Errorf("marshalling container state json: %w", err)
	}
	deps := ctr.Dependencies()

	podID := sql.NullString{}
	if ctr.config.Pod != "" {
		podID.Valid = true
		podID.String = ctr.config.Pod
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning container create transaction: %w", err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to create container: %v", err)
			}
		}
	}()

	// TODO: There has to be a better way of doing this
	var check int
	row := tx.QueryRow("SELECT 1 FROM ContainerConfig WHERE Name=?;", ctr.Name())
	if err := row.Scan(&check); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("checking if container name %s exists in database: %w", ctr.Name(), err)
		}
	} else if check != 0 {
		return fmt.Errorf("name %q is in use: %w", ctr.Name(), define.ErrCtrExists)
	}

	if _, err := tx.Exec("INSERT INTO IDNamespace VALUES (?);", ctr.ID()); err != nil {
		return fmt.Errorf("adding container id to database: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO ContainerConfig VALUES (?, ?, ?, ?);", ctr.ID(), ctr.Name(), podID, configJSON); err != nil {
		return fmt.Errorf("adding container config to database: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO ContainerState VALUES (?, ?, ?, ?);", ctr.ID(), int(ctr.state.State), ctr.state.ExitCode, stateJSON); err != nil {
		return fmt.Errorf("adding container state to database: %w", err)
	}
	for _, dep := range deps {
		// Check if the dependency is in the same pod
		var depPod sql.NullString
		row := tx.QueryRow("SELECT PodID FROM ContainerConfig WHERE ID=?;", dep)
		if err := row.Scan(&depPod); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("container dependency %s does not exist in database: %w", dep, define.ErrNoSuchCtr)
			}
		}
		switch {
		case ctr.config.Pod == "" && depPod.Valid:
			return fmt.Errorf("container dependency %s is part of a pod, but container is not: %w", dep, define.ErrInvalidArg)
		case ctr.config.Pod != "" && !depPod.Valid:
			return fmt.Errorf("container dependency %s is not part of pod, but this container belongs to pod %s: %w", dep, ctr.config.Pod, define.ErrInvalidArg)
		case ctr.config.Pod != "" && depPod.String != ctr.config.Pod:
			return fmt.Errorf("container dependency %s is part of pod %s but container is part of pod %s, pods must match: %w", dep, depPod.String, ctr.config.Pod, define.ErrInvalidArg)
		}

		if _, err := tx.Exec("INSERT INTO ContainerDependency VALUES (?, ?);", ctr.ID(), dep); err != nil {
			return fmt.Errorf("adding container dependency %s to database: %w", dep, err)
		}
	}
	volMap := make(map[string]bool)
	for _, vol := range ctr.config.NamedVolumes {
		if _, ok := volMap[vol.Name]; !ok {
			if _, err := tx.Exec("INSERT INTO ContainerVolume VALUES (?, ?);", ctr.ID(), vol.Name); err != nil {
				return fmt.Errorf("adding container volume %s to database: %w", vol.Name, err)
			}
			volMap[vol.Name] = true
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

// removeContainer remove the specified container from the database.
func (s *SQLiteState) removeContainer(ctr *Container) (defErr error) {
	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning container %s removal transaction: %w", ctr.ID(), err)
	}

	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to remove container %s: %v", ctr.ID(), err)
			}
		}
	}()

	if err := s.removeContainerWithTx(ctr.ID(), tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing container %s removal transaction: %w", ctr.ID(), err)
	}

	return nil
}

// removeContainerWithTx removes the container with the specified transaction.
// Callers are responsible for committing.
func (s *SQLiteState) removeContainerWithTx(id string, tx *sql.Tx) error {
	// TODO TODO TODO:
	// Need to verify that at least 1 row was deleted from ContainerConfig.
	// Otherwise return ErrNoSuchCtr.
	if _, err := tx.Exec("DELETE FROM IDNamespace WHERE ID=?;", id); err != nil {
		return fmt.Errorf("removing container %s id from database: %w", id, err)
	}
	if _, err := tx.Exec("DELETE FROM ContainerConfig WHERE ID=?;", id); err != nil {
		return fmt.Errorf("removing container %s config from database: %w", id, err)
	}
	if _, err := tx.Exec("DELETE FROM ContainerState WHERE ID=?;", id); err != nil {
		return fmt.Errorf("removing container %s state from database: %w", id, err)
	}
	if _, err := tx.Exec("DELETE FROM ContainerDependency WHERE ID=?;", id); err != nil {
		return fmt.Errorf("removing container %s dependencies from database: %w", id, err)
	}
	if _, err := tx.Exec("DELETE FROM ContainerVolume WHERE ContainerID=?;", id); err != nil {
		return fmt.Errorf("removing container %s volumes from database: %w", id, err)
	}
	if _, err := tx.Exec("DELETE FROM ContainerExecSession WHERE ContainerID=?;", id); err != nil {
		return fmt.Errorf("removing container %s exec sessions from database: %w", id, err)
	}
	return nil
}

// networkModify allows you to modify or add a new network, to add a new network use the new bool
func (s *SQLiteState) networkModify(ctr *Container, network string, opts types.PerNetworkOptions, new, disconnect bool) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	if network == "" {
		return fmt.Errorf("network names must not be empty: %w", define.ErrInvalidArg)
	}

	if new && disconnect {
		return fmt.Errorf("new and disconnect are mutually exclusive: %w", define.ErrInvalidArg)
	}

	// Grab a fresh copy of the config, in case anything changed
	newCfg, err := s.getCtrConfig(ctr.ID())
	if err != nil && errors.Is(err, define.ErrNoSuchCtr) {
		ctr.valid = false
		return define.ErrNoSuchCtr
	}

	_, ok := newCfg.Networks[network]
	if new && ok {
		return fmt.Errorf("container %s is already connected to network %s: %w", ctr.ID(), network, define.ErrNetworkConnected)
	}
	if !ok && (!new || disconnect) {
		return fmt.Errorf("container %s is not connected to network %s: %w", ctr.ID(), network, define.ErrNoSuchNetwork)
	}

	if !disconnect {
		if newCfg.Networks == nil {
			newCfg.Networks = make(map[string]types.PerNetworkOptions)
		}
		newCfg.Networks[network] = opts
	} else {
		delete(newCfg.Networks, network)
	}

	if err := s.rewriteContainerConfig(ctr, newCfg); err != nil {
		return err
	}

	ctr.config = newCfg

	return nil
}
