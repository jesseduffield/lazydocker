//go:build !remote

package libpod

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/storage"

	// SQLite backend for database/sql
	_ "github.com/mattn/go-sqlite3"
)

const schemaVersion = 1

// SQLiteState is a state implementation backed by a SQLite database
type SQLiteState struct {
	valid   bool
	conn    *sql.DB
	runtime *Runtime
}

const (
	// Deal with timezone automatically.
	sqliteOptionLocation = "_loc=auto"
	// Force an fsync after each transaction (https://www.sqlite.org/pragma.html#pragma_synchronous).
	sqliteOptionSynchronous = "&_sync=FULL"
	// Allow foreign keys (https://www.sqlite.org/pragma.html#pragma_foreign_keys).
	sqliteOptionForeignKeys = "&_foreign_keys=1"
	// Make sure that transactions happen exclusively.
	sqliteOptionTXLock = "&_txlock=exclusive"
	// Enforce case sensitivity for LIKE
	sqliteOptionCaseSensitiveLike = "&_cslike=TRUE"

	// Assembled sqlite options used when opening the database.
	sqliteOptions = "db.sql?" +
		sqliteOptionLocation +
		sqliteOptionSynchronous +
		sqliteOptionForeignKeys +
		sqliteOptionTXLock +
		sqliteOptionCaseSensitiveLike
)

// NewSqliteState creates a new SQLite-backed state database.
func NewSqliteState(runtime *Runtime) (_ State, defErr error) {
	logrus.Info("Using sqlite as database backend")
	state := new(SQLiteState)

	basePath := runtime.storageConfig.GraphRoot
	if runtime.storageConfig.TransientStore {
		basePath = runtime.storageConfig.RunRoot
	} else if !runtime.storageSet.StaticDirSet {
		basePath = runtime.config.Engine.StaticDir
	}

	// c/storage is set up *after* the DB - so even though we use the c/s
	// root (or, for transient, runroot) dir, we need to make the dir
	// ourselves.
	if err := os.MkdirAll(basePath, 0o700); err != nil {
		return nil, fmt.Errorf("creating root directory: %w", err)
	}

	// Make sure busy timeout is set to high value to keep retrying when the db is locked.
	// Timeout is in ms, so set it to 100s to have enough time to retry the operations.
	// Some users might want to experiment with different timeout values (#23236)
	// DO NOT DOCUMENT or recommend PODMAN_SQLITE_BUSY_TIMEOUT outside of testing.
	busyTimeout := "100000"
	if env, ok := os.LookupEnv("PODMAN_SQLITE_BUSY_TIMEOUT"); ok {
		logrus.Debugf("PODMAN_SQLITE_BUSY_TIMEOUT is set to %s", env)
		busyTimeout = env
	}
	sqliteOptionBusyTimeout := "&_busy_timeout=" + busyTimeout

	conn, err := sql.Open("sqlite3", filepath.Join(basePath, sqliteOptions+sqliteOptionBusyTimeout))
	if err != nil {
		return nil, fmt.Errorf("initializing sqlite database: %w", err)
	}
	defer func() {
		if defErr != nil {
			if err := conn.Close(); err != nil {
				logrus.Errorf("Error closing SQLite DB connection: %v", err)
			}
		}
	}()

	if err := initSQLiteDB(conn); err != nil {
		return nil, err
	}

	state.conn = conn
	state.valid = true
	state.runtime = runtime

	return state, nil
}

// Close closes the state and prevents further use
func (s *SQLiteState) Close() error {
	if err := s.conn.Close(); err != nil {
		return err
	}

	s.valid = false
	return nil
}

// Refresh clears container and pod states after a reboot
func (s *SQLiteState) Refresh() (defErr error) {
	if !s.valid {
		return define.ErrDBClosed
	}

	// Retrieve all containers, pods, and volumes.
	// Maps are indexed by ID (or volume name) so we know which goes where,
	// and store the marshalled state JSON
	ctrStates := make(map[string]string)
	podStates := make(map[string]string)
	volumeStates := make(map[string]string)

	ctrRows, err := s.conn.Query("SELECT ID, JSON FROM ContainerState;")
	if err != nil {
		return fmt.Errorf("querying for container states: %w", err)
	}
	defer ctrRows.Close()

	for ctrRows.Next() {
		var (
			id, stateJSON string
		)
		if err := ctrRows.Scan(&id, &stateJSON); err != nil {
			return fmt.Errorf("scanning container state row: %w", err)
		}

		ctrState := new(ContainerState)

		if err := json.Unmarshal([]byte(stateJSON), ctrState); err != nil {
			return fmt.Errorf("unmarshalling container state json: %w", err)
		}

		// Refresh the state
		resetContainerState(ctrState)

		newJSON, err := json.Marshal(ctrState)
		if err != nil {
			return fmt.Errorf("marshalling container state json: %w", err)
		}

		ctrStates[id] = string(newJSON)
	}
	if err := ctrRows.Err(); err != nil {
		return err
	}

	podRows, err := s.conn.Query("SELECT ID, JSON FROM PodState;")
	if err != nil {
		return fmt.Errorf("querying for pod states: %w", err)
	}
	defer podRows.Close()

	for podRows.Next() {
		var (
			id, stateJSON string
		)
		if err := podRows.Scan(&id, &stateJSON); err != nil {
			return fmt.Errorf("scanning pod state row: %w", err)
		}

		podState := new(podState)

		if err := json.Unmarshal([]byte(stateJSON), podState); err != nil {
			return fmt.Errorf("unmarshalling pod state json: %w", err)
		}

		// Refresh the state
		resetPodState(podState)

		newJSON, err := json.Marshal(podState)
		if err != nil {
			return fmt.Errorf("marshalling pod state json: %w", err)
		}

		podStates[id] = string(newJSON)
	}
	if err := podRows.Err(); err != nil {
		return err
	}

	volRows, err := s.conn.Query("SELECT Name, JSON FROM VolumeState;")
	if err != nil {
		return fmt.Errorf("querying for volume states: %w", err)
	}
	defer volRows.Close()

	for volRows.Next() {
		var (
			name, stateJSON string
		)

		if err := volRows.Scan(&name, &stateJSON); err != nil {
			return fmt.Errorf("scanning volume state row: %w", err)
		}

		volState := new(VolumeState)

		if err := json.Unmarshal([]byte(stateJSON), volState); err != nil {
			return fmt.Errorf("unmarshalling volume state json: %w", err)
		}

		// Refresh the state
		resetVolumeState(volState)

		newJSON, err := json.Marshal(volState)
		if err != nil {
			return fmt.Errorf("marshalling volume state json: %w", err)
		}

		volumeStates[name] = string(newJSON)
	}
	if err := volRows.Err(); err != nil {
		return err
	}

	// Write updated states back to DB, and perform additional maintenance
	// (Remove exit codes and exec sessions)

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning refresh transaction: %w", err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to refresh database state: %v", err)
			}
		}
	}()

	for id, json := range ctrStates {
		if _, err := tx.Exec("UPDATE ContainerState SET JSON=? WHERE ID=?;", json, id); err != nil {
			return fmt.Errorf("updating container state: %w", err)
		}
	}
	for id, json := range podStates {
		if _, err := tx.Exec("UPDATE PodState SET JSON=? WHERE ID=?;", json, id); err != nil {
			return fmt.Errorf("updating pod state: %w", err)
		}
	}
	for name, json := range volumeStates {
		if _, err := tx.Exec("UPDATE VolumeState SET JSON=? WHERE Name=?;", json, name); err != nil {
			return fmt.Errorf("updating volume state: %w", err)
		}
	}

	if _, err := tx.Exec("DELETE FROM ContainerExitCode;"); err != nil {
		return fmt.Errorf("removing container exit codes: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM ContainerExecSession;"); err != nil {
		return fmt.Errorf("removing container exec sessions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

// GetDBConfig retrieves runtime configuration fields that were created when
// the database was first initialized
func (s *SQLiteState) GetDBConfig() (*DBConfig, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	cfg := new(DBConfig)
	var staticDir, tmpDir, graphRoot, runRoot, graphDriver, volumeDir string

	row := s.conn.QueryRow("SELECT StaticDir, TmpDir, GraphRoot, RunRoot, GraphDriver, VolumeDir FROM DBConfig;")

	if err := row.Scan(&staticDir, &tmpDir, &graphRoot, &runRoot, &graphDriver, &volumeDir); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cfg, nil
		}
		return nil, fmt.Errorf("retrieving DB config: %w", err)
	}

	cfg.LibpodRoot = staticDir
	cfg.LibpodTmp = tmpDir
	cfg.StorageRoot = graphRoot
	cfg.StorageTmp = runRoot
	cfg.GraphDriver = graphDriver
	cfg.VolumePath = volumeDir

	return cfg, nil
}

// ValidateDBConfig validates paths in the given runtime against the database
func (s *SQLiteState) ValidateDBConfig(_ *Runtime) (defErr error) {
	if !s.valid {
		return define.ErrDBClosed
	}

	storeOpts, err := storage.DefaultStoreOptions()
	if err != nil {
		return err
	}

	const createRow = `
        INSERT INTO DBconfig VALUES (
                ?, ?, ?,
                ?, ?, ?,
                ?, ?, ?
        );`

	var (
		dbOS, staticDir, tmpDir, graphRoot, runRoot, graphDriver, volumePath string
		runtimeOS                                                            = goruntime.GOOS
		runtimeStaticDir                                                     = filepath.Clean(s.runtime.config.Engine.StaticDir)
		runtimeTmpDir                                                        = filepath.Clean(s.runtime.config.Engine.TmpDir)
		runtimeGraphRoot                                                     = filepath.Clean(s.runtime.StorageConfig().GraphRoot)
		runtimeRunRoot                                                       = filepath.Clean(s.runtime.StorageConfig().RunRoot)
		runtimeGraphDriver                                                   = s.runtime.StorageConfig().GraphDriverName
		runtimeVolumePath                                                    = filepath.Clean(s.runtime.config.Engine.VolumePath)
	)

	// Some fields may be empty, indicating they are set to the default.
	// If so, grab the default from c/storage for them.
	if runtimeGraphRoot == "" {
		runtimeGraphRoot = storeOpts.GraphRoot
	}
	if runtimeRunRoot == "" {
		runtimeRunRoot = storeOpts.RunRoot
	}
	if runtimeGraphDriver == "" {
		runtimeGraphDriver = storeOpts.GraphDriverName
	}

	// We have to do this in a transaction to ensure mutual exclusion.
	// Otherwise we have a race - multiple processes can be checking the
	// row's existence simultaneously, both try to create it, second one to
	// get the transaction lock gets an error.
	// TODO: The transaction isn't strictly necessary, and there's a (small)
	// chance it's a perf hit. If it is, we can move it entirely within the
	// `errors.Is()` block below, with extra validation to ensure the row
	// still does not exist (and, if it does, to retry this function).
	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning database validation transaction: %w", err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to validate database: %v", err)
			}
		}
	}()

	row := tx.QueryRow("SELECT Os, StaticDir, TmpDir, GraphRoot, RunRoot, GraphDriver, VolumeDir FROM DBConfig;")

	if err := row.Scan(&dbOS, &staticDir, &tmpDir, &graphRoot, &runRoot, &graphDriver, &volumePath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if _, err := tx.Exec(createRow, 1, schemaVersion, runtimeOS,
				runtimeStaticDir, runtimeTmpDir, runtimeGraphRoot,
				runtimeRunRoot, runtimeGraphDriver, runtimeVolumePath); err != nil {
				return fmt.Errorf("adding DB config row: %w", err)
			}

			if err := tx.Commit(); err != nil {
				return fmt.Errorf("committing write of database validation row: %w", err)
			}

			return nil
		}

		return fmt.Errorf("retrieving DB config: %w", err)
	}

	// Sometimes, for as-yet unclear reasons, the database value ends up set
	// to the empty string. If it does, this evaluation is always going to
	// fail, and libpod will be unusable.
	// At this point, the check is effectively meaningless - we don't
	// actually know the settings we should be checking against. The best
	// thing we can do (and what BoltDB did in this case) is to compare
	// against the default, on the assumption that is what was in use.
	// TODO: We can't remove this code without breaking existing SQLite DBs
	// that already have incorrect values in the database, but we should
	// investigate why this is happening and try and prevent the creation of
	// new databases with these garbage checks.
	if graphRoot == "" {
		logrus.Debugf("Database uses empty-string graph root, substituting default %q", storeOpts.GraphRoot)
		graphRoot = storeOpts.GraphRoot
	}
	if runRoot == "" {
		logrus.Debugf("Database uses empty-string run root, substituting default %q", storeOpts.RunRoot)
		runRoot = storeOpts.RunRoot
	}
	if graphDriver == "" {
		logrus.Debugf("Database uses empty-string graph driver, substituting default %q", storeOpts.GraphDriverName)
		graphDriver = storeOpts.GraphDriverName
	}

	checkField := func(fieldName, dbVal, ourVal string, isPath bool) error {
		if isPath {
			// Tolerate symlinks when possible - most relevant for OStree systems
			// and rootless containers, where we want to put containers in /home,
			// which is symlinked to /var/home.
			// Ignore ENOENT as reasonable, as some paths may not exist in early Libpod
			// init.
			if dbVal != "" {
				checkedVal, err := evalSymlinksIfExists(dbVal)
				if err != nil {
					return fmt.Errorf("cannot evaluate symlinks on DB %s path %q: %w", fieldName, dbVal, err)
				}
				dbVal = checkedVal
			}
			if ourVal != "" {
				checkedVal, err := evalSymlinksIfExists(ourVal)
				if err != nil {
					return fmt.Errorf("cannot evaluate symlinks on our %s path %q: %w", fieldName, ourVal, err)
				}
				ourVal = checkedVal
			}
		}

		if dbVal != ourVal {
			return fmt.Errorf("database %s %q does not match our %s %q: %w", fieldName, dbVal, fieldName, ourVal, define.ErrDBBadConfig)
		}

		return nil
	}

	if err := checkField("os", dbOS, runtimeOS, false); err != nil {
		return err
	}
	if err := checkField("static dir", staticDir, runtimeStaticDir, true); err != nil {
		return err
	}
	if err := checkField("tmp dir", tmpDir, runtimeTmpDir, true); err != nil {
		return err
	}
	if err := checkField("graph root", graphRoot, runtimeGraphRoot, true); err != nil {
		return err
	}
	if err := checkField("run root", runRoot, runtimeRunRoot, true); err != nil {
		return err
	}
	if err := checkField("graph driver", graphDriver, runtimeGraphDriver, false); err != nil {
		return err
	}
	if err := checkField("volume path", volumePath, runtimeVolumePath, true); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing database validation row: %w", err)
	}
	// Do not return any error after the commit call because the defer will
	// try to roll back the transaction which results in an logged error.

	return nil
}

// GetContainerName returns the name of the container associated with a given
// ID. Returns ErrNoSuchCtr if the ID does not exist.
func (s *SQLiteState) GetContainerName(id string) (string, error) {
	if id == "" {
		return "", define.ErrEmptyID
	}

	if !s.valid {
		return "", define.ErrDBClosed
	}

	var name string

	row := s.conn.QueryRow("SELECT Name FROM ContainerConfig WHERE ID=?;", id)
	if err := row.Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", define.ErrNoSuchCtr
		}

		return "", fmt.Errorf("looking up container %s name: %w", id, err)
	}

	return name, nil
}

// GetPodName returns the name of the pod associated with a given ID.
// Returns ErrNoSuchPod if the ID does not exist.
func (s *SQLiteState) GetPodName(id string) (string, error) {
	if id == "" {
		return "", define.ErrEmptyID
	}

	if !s.valid {
		return "", define.ErrDBClosed
	}

	var name string

	row := s.conn.QueryRow("SELECT Name FROM PodConfig WHERE ID=?;", id)
	if err := row.Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", define.ErrNoSuchPod
		}

		return "", fmt.Errorf("looking up pod %s name: %w", id, err)
	}

	return name, nil
}

// Container retrieves a single container from the state by its full ID
func (s *SQLiteState) Container(id string) (*Container, error) {
	if id == "" {
		return nil, define.ErrEmptyID
	}

	if !s.valid {
		return nil, define.ErrDBClosed
	}

	ctrConfig, err := s.getCtrConfig(id)
	if err != nil {
		return nil, err
	}

	ctr := new(Container)
	ctr.config = ctrConfig
	ctr.state = new(ContainerState)
	ctr.runtime = s.runtime

	if err := finalizeCtrSqlite(ctr); err != nil {
		return nil, err
	}

	return ctr, nil
}

// LookupContainerID retrieves a container ID from the state by full or unique
// partial ID or name
func (s *SQLiteState) LookupContainerID(idOrName string) (string, error) {
	if idOrName == "" {
		return "", define.ErrEmptyID
	}

	if !s.valid {
		return "", define.ErrDBClosed
	}

	rows, err := s.conn.Query("SELECT ID, Name FROM ContainerConfig WHERE ContainerConfig.Name=? OR (ContainerConfig.ID LIKE ?);", idOrName, idOrName+"%")
	if err != nil {
		return "", fmt.Errorf("looking up container %q in database: %w", idOrName, err)
	}
	defer rows.Close()

	var (
		id, name string
		resCount uint
	)
	for rows.Next() {
		if err := rows.Scan(&id, &name); err != nil {
			return "", fmt.Errorf("retrieving container %q ID from database: %w", idOrName, err)
		}
		if name == idOrName {
			return id, nil
		}
		resCount++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if resCount == 0 {
		return "", define.ErrNoSuchCtr
	} else if resCount > 1 {
		return "", fmt.Errorf("more than one result for container %q: %w", idOrName, define.ErrCtrExists)
	}

	return id, nil
}

// LookupContainer retrieves a container from the state by full or unique
// partial ID or name
func (s *SQLiteState) LookupContainer(idOrName string) (*Container, error) {
	if idOrName == "" {
		return nil, define.ErrEmptyID
	}

	if !s.valid {
		return nil, define.ErrDBClosed
	}

	rows, err := s.conn.Query("SELECT JSON, Name FROM ContainerConfig WHERE ContainerConfig.Name=? OR (ContainerConfig.ID LIKE ?);", idOrName, idOrName+"%")
	if err != nil {
		return nil, fmt.Errorf("looking up container %q in database: %w", idOrName, err)
	}
	defer rows.Close()

	var (
		rawJSON, name string
		exactName     bool
		resCount      uint
	)
	for rows.Next() {
		if err := rows.Scan(&rawJSON, &name); err != nil {
			return nil, fmt.Errorf("retrieving container %q ID from database: %w", idOrName, err)
		}
		if name == idOrName {
			exactName = true
			break
		}
		resCount++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !exactName {
		if resCount == 0 {
			return nil, fmt.Errorf("no container with name or ID %q found: %w", idOrName, define.ErrNoSuchCtr)
		} else if resCount > 1 {
			return nil, fmt.Errorf("more than one result for container %q: %w", idOrName, define.ErrCtrExists)
		}
	}

	ctr := new(Container)
	ctr.config = new(ContainerConfig)
	ctr.state = new(ContainerState)
	ctr.runtime = s.runtime

	if err := json.Unmarshal([]byte(rawJSON), ctr.config); err != nil {
		return nil, fmt.Errorf("unmarshalling container config JSON: %w", err)
	}

	if err := finalizeCtrSqlite(ctr); err != nil {
		return nil, err
	}

	return ctr, nil
}

// HasContainer checks if a container is present in the state
func (s *SQLiteState) HasContainer(id string) (bool, error) {
	if id == "" {
		return false, define.ErrEmptyID
	}

	if !s.valid {
		return false, define.ErrDBClosed
	}

	row := s.conn.QueryRow("SELECT 1 FROM ContainerConfig WHERE ID=?;", id)

	var check int
	if err := row.Scan(&check); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("looking up container %s in database: %w", id, err)
	} else if check != 1 {
		return false, fmt.Errorf("check digit for container %s lookup incorrect: %w", id, define.ErrInternal)
	}

	return true, nil
}

// AddContainer adds a container to the state
// The container being added cannot belong to a pod
func (s *SQLiteState) AddContainer(ctr *Container) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	if ctr.config.Pod != "" {
		return fmt.Errorf("cannot add a container that belongs to a pod with AddContainer - use AddContainerToPod: %w", define.ErrInvalidArg)
	}

	return s.addContainer(ctr)
}

// RemoveContainer removes a container from the state
// Only removes containers not in pods - for containers that are a member of a
// pod, use RemoveContainerFromPod
func (s *SQLiteState) RemoveContainer(ctr *Container) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if ctr.config.Pod != "" {
		return fmt.Errorf("container %s is part of a pod, use RemoveContainerFromPod instead: %w", ctr.ID(), define.ErrPodExists)
	}

	return s.removeContainer(ctr)
}

// UpdateContainer updates a container's state from the database
func (s *SQLiteState) UpdateContainer(ctr *Container) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	row := s.conn.QueryRow("SELECT JSON FROM ContainerState WHERE ID=?;", ctr.ID())

	var rawJSON string
	if err := row.Scan(&rawJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Container was removed
			ctr.valid = false
			return fmt.Errorf("no container with ID %s found in database: %w", ctr.ID(), define.ErrNoSuchCtr)
		}
	}

	newState := new(ContainerState)
	if err := json.Unmarshal([]byte(rawJSON), newState); err != nil {
		return fmt.Errorf("unmarshalling container %s state JSON: %w", ctr.ID(), err)
	}

	ctr.state = newState

	return nil
}

// SaveContainer saves a container's current state in the database
func (s *SQLiteState) SaveContainer(ctr *Container) (defErr error) {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	stateJSON, err := json.Marshal(ctr.state)
	if err != nil {
		return fmt.Errorf("marshalling container %s state JSON: %w", ctr.ID(), err)
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning container %s save transaction: %w", ctr.ID(), err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to save container %s state: %v", ctr.ID(), err)
			}
		}
	}()

	result, err := tx.Exec("UPDATE ContainerState SET JSON=? WHERE ID=?;", stateJSON, ctr.ID())
	if err != nil {
		return fmt.Errorf("writing container %s state: %w", ctr.ID(), err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("retrieving container %s save rows affected: %w", ctr.ID(), err)
	}
	if rows == 0 {
		ctr.valid = false
		return define.ErrNoSuchCtr
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing container %s state: %w", ctr.ID(), err)
	}

	return nil
}

// ContainerInUse checks if other containers depend on the given container
// It returns a slice of the IDs of the containers depending on the given
// container. If the slice is empty, no containers depend on the given container
func (s *SQLiteState) ContainerInUse(ctr *Container) ([]string, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	if !ctr.valid {
		return nil, define.ErrCtrRemoved
	}

	rows, err := s.conn.Query("SELECT ID FROM ContainerDependency WHERE DependencyID=?;", ctr.ID())
	if err != nil {
		return nil, fmt.Errorf("retrieving containers that depend on container %s: %w", ctr.ID(), err)
	}
	defer rows.Close()

	deps := []string{}
	for rows.Next() {
		var dep string
		if err := rows.Scan(&dep); err != nil {
			return nil, fmt.Errorf("reading containers that depend on %s: %w", ctr.ID(), err)
		}
		deps = append(deps, dep)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return deps, nil
}

// AllContainers retrieves all the containers in the database
// If `loadState` is set, the containers' state will be loaded as well.
func (s *SQLiteState) AllContainers(loadState bool) ([]*Container, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	ctrs := []*Container{}

	if loadState {
		rows, err := s.conn.Query("SELECT ContainerConfig.JSON, ContainerState.JSON AS StateJSON FROM ContainerConfig INNER JOIN ContainerState ON ContainerConfig.ID = ContainerState.ID;")
		if err != nil {
			return nil, fmt.Errorf("retrieving all containers from database: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var configJSON, stateJSON string
			if err := rows.Scan(&configJSON, &stateJSON); err != nil {
				return nil, fmt.Errorf("scanning container from database: %w", err)
			}

			ctr := new(Container)
			ctr.config = new(ContainerConfig)
			ctr.state = new(ContainerState)
			ctr.runtime = s.runtime

			if err := json.Unmarshal([]byte(configJSON), ctr.config); err != nil {
				return nil, fmt.Errorf("unmarshalling container config: %w", err)
			}
			if err := json.Unmarshal([]byte(stateJSON), ctr.state); err != nil {
				return nil, fmt.Errorf("unmarshalling container %s state: %w", ctr.ID(), err)
			}

			ctrs = append(ctrs, ctr)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	} else {
		rows, err := s.conn.Query("SELECT JSON FROM ContainerConfig;")
		if err != nil {
			return nil, fmt.Errorf("retrieving all containers from database: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var rawJSON string
			if err := rows.Scan(&rawJSON); err != nil {
				return nil, fmt.Errorf("scanning container from database: %w", err)
			}

			ctr := new(Container)
			ctr.config = new(ContainerConfig)
			ctr.state = new(ContainerState)
			ctr.runtime = s.runtime

			if err := json.Unmarshal([]byte(rawJSON), ctr.config); err != nil {
				return nil, fmt.Errorf("unmarshalling container config: %w", err)
			}

			ctrs = append(ctrs, ctr)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	for _, ctr := range ctrs {
		if err := finalizeCtrSqlite(ctr); err != nil {
			return nil, err
		}
	}

	return ctrs, nil
}

// GetNetworks returns the networks this container is a part of.
func (s *SQLiteState) GetNetworks(ctr *Container) (map[string]types.PerNetworkOptions, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	if !ctr.valid {
		return nil, define.ErrCtrRemoved
	}

	// if the network mode is not bridge return no networks
	if !ctr.config.NetMode.IsBridge() {
		return nil, nil
	}

	cfg, err := s.getCtrConfig(ctr.ID())
	if err != nil {
		if errors.Is(err, define.ErrNoSuchCtr) {
			ctr.valid = false
		}
		return nil, err
	}

	return cfg.Networks, nil
}

// NetworkConnect adds the given container to the given network. If aliases are
// specified, those will be added to the given network.
func (s *SQLiteState) NetworkConnect(ctr *Container, network string, opts types.PerNetworkOptions) error {
	return s.networkModify(ctr, network, opts, true, false)
}

// NetworkModify will allow you to set new options on an existing connected network
func (s *SQLiteState) NetworkModify(ctr *Container, network string, opts types.PerNetworkOptions) error {
	return s.networkModify(ctr, network, opts, false, false)
}

// NetworkDisconnect disconnects the container from the given network, also
// removing any aliases in the network.
func (s *SQLiteState) NetworkDisconnect(ctr *Container, network string) error {
	return s.networkModify(ctr, network, types.PerNetworkOptions{}, false, true)
}

// GetContainerConfig returns a container config from the database by full ID
func (s *SQLiteState) GetContainerConfig(id string) (*ContainerConfig, error) {
	if len(id) == 0 {
		return nil, define.ErrEmptyID
	}

	if !s.valid {
		return nil, define.ErrDBClosed
	}

	return s.getCtrConfig(id)
}

// AddContainerExitCode adds the exit code for the specified container to the database.
func (s *SQLiteState) AddContainerExitCode(id string, exitCode int32) (defErr error) {
	if len(id) == 0 {
		return define.ErrEmptyID
	}

	if !s.valid {
		return define.ErrDBClosed
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction to add exit code: %w", err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to add exit code: %v", err)
			}
		}
	}()

	if _, err := tx.Exec("INSERT OR REPLACE INTO ContainerExitCode VALUES (?, ?, ?);", id, time.Now().Unix(), exitCode); err != nil {
		return fmt.Errorf("adding container %s exit code %d: %w", id, exitCode, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction to add exit code: %w", err)
	}

	return nil
}

// GetContainerExitCode returns the exit code for the specified container.
func (s *SQLiteState) GetContainerExitCode(id string) (int32, error) {
	if len(id) == 0 {
		return -1, define.ErrEmptyID
	}

	if !s.valid {
		return -1, define.ErrDBClosed
	}

	row := s.conn.QueryRow("SELECT ExitCode FROM ContainerExitCode WHERE ID=?;", id)
	var exitCode int32 = -1
	if err := row.Scan(&exitCode); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return -1, fmt.Errorf("getting exit code of container %s from DB: %w", id, define.ErrNoSuchExitCode)
		}
		return -1, fmt.Errorf("scanning exit code of container %s: %w", id, err)
	}

	return exitCode, nil
}

// GetContainerExitCodeTimeStamp returns the time stamp when the exit code of
// the specified container was added to the database.
func (s *SQLiteState) GetContainerExitCodeTimeStamp(id string) (*time.Time, error) {
	if len(id) == 0 {
		return nil, define.ErrEmptyID
	}

	if !s.valid {
		return nil, define.ErrDBClosed
	}

	row := s.conn.QueryRow("SELECT Timestamp FROM ContainerExitCode WHERE ID=?;", id)

	var timestamp int64
	if err := row.Scan(&timestamp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("getting timestamp for exit code of container %s from DB: %w", id, define.ErrNoSuchExitCode)
		}
		return nil, fmt.Errorf("scanning exit timestamp of container %s: %w", id, err)
	}

	result := time.Unix(timestamp, 0)

	return &result, nil
}

// PruneContainerExitCodes removes exit codes older than 5 minutes unless the associated
// container still exists.
func (s *SQLiteState) PruneContainerExitCodes() (defErr error) {
	if !s.valid {
		return define.ErrDBClosed
	}

	fiveMinsAgo := time.Now().Add(-5 * time.Minute).Unix()

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction to remove old timestamps: %w", err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to remove old timestamps: %v", err)
			}
		}
	}()

	if _, err := tx.Exec("DELETE FROM ContainerExitCode WHERE (Timestamp <= ?) AND (ID NOT IN (SELECT ID FROM ContainerConfig))", fiveMinsAgo); err != nil {
		return fmt.Errorf("removing exit codes with timestamps older than 5 minutes: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction to remove old timestamps: %w", err)
	}

	return nil
}

// AddExecSession adds an exec session to the state.
func (s *SQLiteState) AddExecSession(ctr *Container, session *ExecSession) (defErr error) {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning container %s exec session %s add transaction: %w", ctr.ID(), session.Id, err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to add container %s exec session %s: %v", ctr.ID(), session.Id, err)
			}
		}
	}()

	if _, err := tx.Exec("INSERT INTO ContainerExecSession VALUES (?, ?);", session.Id, ctr.ID()); err != nil {
		return fmt.Errorf("adding container %s exec session %s to database: %w", ctr.ID(), session.Id, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing container %s exec session %s addition: %w", ctr.ID(), session.Id, err)
	}

	return nil
}

// GetExecSession returns the ID of the container an exec session is associated
// with.
func (s *SQLiteState) GetExecSession(id string) (string, error) {
	if !s.valid {
		return "", define.ErrDBClosed
	}

	if id == "" {
		return "", define.ErrEmptyID
	}

	row := s.conn.QueryRow("SELECT ContainerID FROM ContainerExecSession WHERE ID=?;", id)

	var ctrID string
	if err := row.Scan(&ctrID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("no exec session with ID %s found: %w", id, define.ErrNoSuchExecSession)
		}
		return "", fmt.Errorf("retrieving exec session %s from database: %w", id, err)
	}

	return ctrID, nil
}

// RemoveExecSession removes references to the given exec session in the
// database.
func (s *SQLiteState) RemoveExecSession(session *ExecSession) (defErr error) {
	if !s.valid {
		return define.ErrDBClosed
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning container %s exec session %s remove transaction: %w", session.ContainerId, session.Id, err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to remove container %s exec session %s: %v", session.ContainerId, session.Id, err)
			}
		}
	}()

	result, err := tx.Exec("DELETE FROM ContainerExecSession WHERE ID=?;", session.Id)
	if err != nil {
		return fmt.Errorf("removing container %s exec session %s from database: %w", session.ContainerId, session.Id, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("retrieving container %s exec session %s removal rows modified: %w", session.ContainerId, session.Id, err)
	}
	if rows == 0 {
		return define.ErrNoSuchExecSession
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing container %s exec session %s removal: %w", session.ContainerId, session.Id, err)
	}

	return nil
}

// GetContainerExecSessions retrieves the IDs of all exec sessions running in a
// container that the database is aware of (IE, were added via AddExecSession).
func (s *SQLiteState) GetContainerExecSessions(ctr *Container) ([]string, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	if !ctr.valid {
		return nil, define.ErrCtrRemoved
	}

	rows, err := s.conn.Query("SELECT ID FROM ContainerExecSession WHERE ContainerID=?;", ctr.ID())
	if err != nil {
		return nil, fmt.Errorf("querying container %s exec sessions: %w", ctr.ID(), err)
	}
	defer rows.Close()

	var sessions []string
	for rows.Next() {
		var session string
		if err := rows.Scan(&session); err != nil {
			return nil, fmt.Errorf("scanning container %s exec sessions row: %w", ctr.ID(), err)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return sessions, nil
}

// RemoveContainerExecSessions removes all exec sessions attached to a given
// container.
func (s *SQLiteState) RemoveContainerExecSessions(ctr *Container) (defErr error) {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning container %s exec session removal transaction: %w", ctr.ID(), err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to remove container %s exec sessions: %v", ctr.ID(), err)
			}
		}
	}()

	if _, err := tx.Exec("DELETE FROM ContainerExecSession WHERE ContainerID=?;", ctr.ID()); err != nil {
		return fmt.Errorf("removing container %s exec sessions from database: %w", ctr.ID(), err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing container %s exec session removal: %w", ctr.ID(), err)
	}

	return nil
}

// RewriteContainerConfig rewrites a container's configuration.
// DO NOT USE TO: Change container dependencies, change pod membership, change
// container ID.
// WARNING: This function is DANGEROUS. Do not use without reading the full
// comment on this function in state.go.
// TODO: Once BoltDB is removed, this can be combined with SafeRewriteContainerConfig.
func (s *SQLiteState) RewriteContainerConfig(ctr *Container, newCfg *ContainerConfig) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	return s.rewriteContainerConfig(ctr, newCfg)
}

// SafeRewriteContainerConfig rewrites a container's configuration in a more
// limited fashion than RewriteContainerConfig. It is marked as safe to use
// under most circumstances, unlike RewriteContainerConfig.
// DO NOT USE TO: Change container dependencies, change pod membership, change
// locks, change container ID.
// TODO: Once BoltDB is removed, this can be combined with RewriteContainerConfig.
func (s *SQLiteState) SafeRewriteContainerConfig(ctr *Container, oldName, newName string, newCfg *ContainerConfig) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	if newName != "" && newCfg.Name != newName {
		return fmt.Errorf("new name %s for container %s must match name in given container config: %w", newName, ctr.ID(), define.ErrInvalidArg)
	}
	if newName != "" && oldName == "" {
		return fmt.Errorf("must provide old name for container if a new name is given: %w", define.ErrInvalidArg)
	}

	return s.rewriteContainerConfig(ctr, newCfg)
}

// RewritePodConfig rewrites a pod's configuration.
// WARNING: This function is DANGEROUS. Do not use without reading the full
// comment on this function in state.go.
func (s *SQLiteState) RewritePodConfig(pod *Pod, newCfg *PodConfig) (defErr error) {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !pod.valid {
		return define.ErrPodRemoved
	}

	json, err := json.Marshal(newCfg)
	if err != nil {
		return fmt.Errorf("error marshalling pod %s config JSON: %w", pod.ID(), err)
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction to rewrite pod %s config: %w", pod.ID(), err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to rewrite pod %s config: %v", pod.ID(), err)
			}
		}
	}()

	results, err := tx.Exec("UPDATE PodConfig SET Name=?, JSON=? WHERE ID=?;", newCfg.Name, json, pod.ID())
	if err != nil {
		return fmt.Errorf("updating pod config table with new configuration for pod %s: %w", pod.ID(), err)
	}
	rows, err := results.RowsAffected()
	if err != nil {
		return fmt.Errorf("retrieving pod %s config rewrite rows affected: %w", pod.ID(), err)
	}
	if rows == 0 {
		pod.valid = false
		return fmt.Errorf("no pod with ID %s found in DB: %w", pod.ID(), define.ErrNoSuchPod)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction to rewrite pod %s config: %w", pod.ID(), err)
	}

	return nil
}

// RewriteVolumeConfig rewrites a volume's configuration.
// WARNING: This function is DANGEROUS. Do not use without reading the full
// comment on this function in state.go.
func (s *SQLiteState) RewriteVolumeConfig(volume *Volume, newCfg *VolumeConfig) (defErr error) {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !volume.valid {
		return define.ErrVolumeRemoved
	}

	json, err := json.Marshal(newCfg)
	if err != nil {
		return fmt.Errorf("error marshalling volume %s new config JSON: %w", volume.Name(), err)
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction to rewrite volume %s config: %w", volume.Name(), err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to rewrite volume %s config: %v", volume.Name(), err)
			}
		}
	}()

	results, err := tx.Exec("UPDATE VolumeConfig SET Name=?, JSON=? WHERE Name=?;", newCfg.Name, json, volume.Name())
	if err != nil {
		return fmt.Errorf("updating volume config table with new configuration for volume %s: %w", volume.Name(), err)
	}
	rows, err := results.RowsAffected()
	if err != nil {
		return fmt.Errorf("retrieving volume %s config rewrite rows affected: %w", volume.Name(), err)
	}
	if rows == 0 {
		volume.valid = false
		return fmt.Errorf("no volume with name %q found in DB: %w", volume.Name(), define.ErrNoSuchVolume)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction to rewrite volume %s config: %w", volume.Name(), err)
	}

	return nil
}

// Pod retrieves a pod given its full ID
func (s *SQLiteState) Pod(id string) (*Pod, error) {
	if id == "" {
		return nil, define.ErrEmptyID
	}

	if !s.valid {
		return nil, define.ErrDBClosed
	}

	row := s.conn.QueryRow("SELECT JSON FROM PodConfig WHERE ID=?;", id)
	var rawJSON string
	if err := row.Scan(&rawJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, define.ErrNoSuchPod
		}
		return nil, fmt.Errorf("retrieving pod %s config from DB: %w", id, err)
	}

	ctrCfg := new(ContainerConfig)
	if err := json.Unmarshal([]byte(rawJSON), ctrCfg); err != nil {
		return nil, fmt.Errorf("unmarshalling container %s config: %w", id, err)
	}

	return s.createPod(rawJSON)
}

// LookupPod retrieves a pod from a full or unique partial ID, or a name.
func (s *SQLiteState) LookupPod(idOrName string) (*Pod, error) {
	if idOrName == "" {
		return nil, define.ErrEmptyID
	}

	if !s.valid {
		return nil, define.ErrDBClosed
	}

	rows, err := s.conn.Query("SELECT JSON, Name FROM PodConfig WHERE PodConfig.Name=? OR (PodConfig.ID LIKE ?);", idOrName, idOrName+"%")
	if err != nil {
		return nil, fmt.Errorf("looking up pod %q in database: %w", idOrName, err)
	}
	defer rows.Close()

	var (
		rawJSON, name string
		exactName     bool
		resCount      uint
	)
	for rows.Next() {
		if err := rows.Scan(&rawJSON, &name); err != nil {
			return nil, fmt.Errorf("error retrieving pod %q ID from database: %w", idOrName, err)
		}
		if name == idOrName {
			exactName = true
			break
		}
		resCount++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !exactName {
		if resCount == 0 {
			return nil, fmt.Errorf("no pod with name or ID %s found: %w", idOrName, define.ErrNoSuchPod)
		} else if resCount > 1 {
			return nil, fmt.Errorf("more than one result for pod %q: %w", idOrName, define.ErrCtrExists)
		}
	}

	return s.createPod(rawJSON)
}

// HasPod checks if a pod with the given ID exists in the state
func (s *SQLiteState) HasPod(id string) (bool, error) {
	if id == "" {
		return false, define.ErrEmptyID
	}

	if !s.valid {
		return false, define.ErrDBClosed
	}

	row := s.conn.QueryRow("SELECT 1 FROM PodConfig WHERE ID=?;", id)

	var check int
	if err := row.Scan(&check); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("looking up pod %s in database: %w", id, err)
	} else if check != 1 {
		return false, fmt.Errorf("check digit for pod %s lookup incorrect: %w", id, define.ErrInternal)
	}

	return true, nil
}

// PodHasContainer checks if the given pod has a container with the given ID
func (s *SQLiteState) PodHasContainer(pod *Pod, id string) (bool, error) {
	if id == "" {
		return false, define.ErrEmptyID
	}

	if !s.valid {
		return false, define.ErrDBClosed
	}

	if !pod.valid {
		return false, define.ErrPodRemoved
	}

	var check int
	row := s.conn.QueryRow("SELECT 1 FROM ContainerConfig WHERE ID=? AND PodID=?;", id, pod.ID())
	if err := row.Scan(&check); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("checking if pod %s has container %s in database: %w", pod.ID(), id, err)
	} else if check != 1 {
		return false, fmt.Errorf("check digit for pod %s lookup incorrect: %w", id, define.ErrInternal)
	}

	return true, nil
}

// PodContainersByID returns the IDs of all containers present in the given pod
func (s *SQLiteState) PodContainersByID(pod *Pod) ([]string, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	if !pod.valid {
		return nil, define.ErrPodRemoved
	}

	rows, err := s.conn.Query("SELECT ID FROM ContainerConfig WHERE PodID=?;", pod.ID())
	if err != nil {
		return nil, fmt.Errorf("retrieving container IDs of pod %s from database: %w", pod.ID(), err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning container from database: %w", err)
		}

		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ids, nil
}

// PodContainers returns all the containers present in the given pod
func (s *SQLiteState) PodContainers(pod *Pod) ([]*Container, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	if !pod.valid {
		return nil, define.ErrPodRemoved
	}

	rows, err := s.conn.Query("SELECT JSON FROM ContainerConfig WHERE PodID=?;", pod.ID())
	if err != nil {
		return nil, fmt.Errorf("retrieving containers of pod %s from database: %w", pod.ID(), err)
	}
	defer rows.Close()

	var ctrs []*Container
	for rows.Next() {
		var rawJSON string
		if err := rows.Scan(&rawJSON); err != nil {
			return nil, fmt.Errorf("scanning container from database: %w", err)
		}

		ctr := new(Container)
		ctr.config = new(ContainerConfig)
		ctr.state = new(ContainerState)
		ctr.runtime = s.runtime

		if err := json.Unmarshal([]byte(rawJSON), ctr.config); err != nil {
			return nil, fmt.Errorf("unmarshalling container config: %w", err)
		}

		ctrs = append(ctrs, ctr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, ctr := range ctrs {
		if err := finalizeCtrSqlite(ctr); err != nil {
			return nil, err
		}
	}

	return ctrs, nil
}

// AddPod adds the given pod to the state.
func (s *SQLiteState) AddPod(pod *Pod) (defErr error) {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !pod.valid {
		return define.ErrPodRemoved
	}

	infraID := sql.NullString{}
	if pod.state.InfraContainerID != "" {
		if err := infraID.Scan(pod.state.InfraContainerID); err != nil {
			return fmt.Errorf("scanning infra container ID %q: %w", pod.state.InfraContainerID, err)
		}
	}

	configJSON, err := json.Marshal(pod.config)
	if err != nil {
		return fmt.Errorf("marshalling pod config json: %w", err)
	}

	stateJSON, err := json.Marshal(pod.state)
	if err != nil {
		return fmt.Errorf("marshalling pod state json: %w", err)
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning pod create transaction: %w", err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to create pod: %v", err)
			}
		}
	}()

	// TODO: explore whether there's a more idiomatic way to do error checks for the name.
	// There is a sqlite3.ErrConstraintUnique error but I (vrothberg) couldn't find a way
	// to work with the returned errors yet.
	var check int
	row := tx.QueryRow("SELECT 1 FROM PodConfig WHERE Name=?;", pod.Name())
	if err := row.Scan(&check); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("checking if pod name %s exists in database: %w", pod.Name(), err)
		}
	} else if check != 0 {
		return fmt.Errorf("name %q is in use: %w", pod.Name(), define.ErrPodExists)
	}

	if _, err := tx.Exec("INSERT INTO IDNamespace VALUES (?);", pod.ID()); err != nil {
		return fmt.Errorf("adding pod id to database: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO PodConfig VALUES (?, ?, ?);", pod.ID(), pod.Name(), configJSON); err != nil {
		return fmt.Errorf("adding pod config to database: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO PodState VALUES (?, ?, ?);", pod.ID(), infraID, stateJSON); err != nil {
		return fmt.Errorf("adding pod state to database: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

// RemovePod removes the given pod from the state.
// Only empty pods can be removed.
func (s *SQLiteState) RemovePod(pod *Pod) (defErr error) {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !pod.valid {
		return define.ErrPodRemoved
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning pod %s removal transaction: %w", pod.ID(), err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to remove pod %s: %v", pod.ID(), err)
			}
		}
	}()

	var check int
	row := tx.QueryRow("SELECT 1 FROM ContainerConfig WHERE PodID=? AND ID!=?;", pod.ID(), pod.state.InfraContainerID)
	if err := row.Scan(&check); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("checking if pod %s has containers in database: %w", pod.ID(), err)
		}
	} else if check != 0 {
		return fmt.Errorf("pod %s is not empty: %w", pod.ID(), define.ErrCtrExists)
	}

	checkResult := func(result sql.Result) error {
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("retrieving pod %s delete rows affected: %w", pod.ID(), err)
		}
		if rows == 0 {
			pod.valid = false
			return define.ErrNoSuchPod
		}
		return nil
	}

	result, err := tx.Exec("DELETE FROM IDNamespace WHERE ID=?;", pod.ID())
	if err != nil {
		return fmt.Errorf("removing pod %s id from database: %w", pod.ID(), err)
	}
	if err := checkResult(result); err != nil {
		return err
	}

	result, err = tx.Exec("DELETE FROM PodConfig WHERE ID=?;", pod.ID())
	if err != nil {
		return fmt.Errorf("removing pod %s config from database: %w", pod.ID(), err)
	}
	if err := checkResult(result); err != nil {
		return err
	}

	result, err = tx.Exec("DELETE FROM PodState WHERE ID=?;", pod.ID())
	if err != nil {
		return fmt.Errorf("removing pod %s state from database: %w", pod.ID(), err)
	}
	if err := checkResult(result); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing pod %s removal transaction: %w", pod.ID(), err)
	}

	return nil
}

// RemovePodContainers removes all containers in a pod.
func (s *SQLiteState) RemovePodContainers(pod *Pod) (defErr error) {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !pod.valid {
		return define.ErrPodRemoved
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning removal transaction for containers of pod %s: %w", pod.ID(), err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to remove containers of pod %s: %v", pod.ID(), err)
			}
		}
	}()

	rows, err := tx.Query("SELECT ID FROM ContainerConfig WHERE PodID=?;", pod.ID())
	if err != nil {
		return fmt.Errorf("retrieving container IDs of pod %s from database: %w", pod.ID(), err)
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scanning container from database: %w", err)
		}

		if err := s.removeContainerWithTx(id, tx); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing pod containers %s removal transaction: %w", pod.ID(), err)
	}

	return nil
}

// AddContainerToPod adds the given container to an existing pod
// The container will be added to the state and the pod
func (s *SQLiteState) AddContainerToPod(pod *Pod, ctr *Container) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !pod.valid {
		return define.ErrPodRemoved
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	if ctr.config.Pod != pod.ID() {
		return fmt.Errorf("container %s is not part of pod %s: %w", ctr.ID(), pod.ID(), define.ErrNoSuchCtr)
	}

	return s.addContainer(ctr)
}

// RemoveContainerFromPod removes a container from an existing pod
// The container will also be removed from the state
func (s *SQLiteState) RemoveContainerFromPod(pod *Pod, ctr *Container) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !pod.valid {
		return define.ErrPodRemoved
	}

	if ctr.config.Pod == "" {
		return fmt.Errorf("container %s is not part of a pod, use RemoveContainer instead: %w", ctr.ID(), define.ErrNoSuchPod)
	}

	if ctr.config.Pod != pod.ID() {
		return fmt.Errorf("container %s is not part of pod %s: %w", ctr.ID(), pod.ID(), define.ErrInvalidArg)
	}

	return s.removeContainer(ctr)
}

// UpdatePod updates a pod's state from the database.
func (s *SQLiteState) UpdatePod(pod *Pod) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !pod.valid {
		return define.ErrPodRemoved
	}

	row := s.conn.QueryRow("SELECT JSON FROM PodState WHERE ID=?;", pod.ID())

	var rawJSON string
	if err := row.Scan(&rawJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Pod was removed
			pod.valid = false
			return fmt.Errorf("no pod with ID %s found in database: %w", pod.ID(), define.ErrNoSuchPod)
		}
	}

	newState := new(podState)
	if err := json.Unmarshal([]byte(rawJSON), newState); err != nil {
		return fmt.Errorf("unmarshalling pod %s state JSON: %w", pod.ID(), err)
	}

	pod.state = newState

	return nil
}

// SavePod saves a pod's state to the database.
func (s *SQLiteState) SavePod(pod *Pod) (defErr error) {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !pod.valid {
		return define.ErrPodRemoved
	}

	stateJSON, err := json.Marshal(pod.state)
	if err != nil {
		return fmt.Errorf("marshalling pod %s state JSON: %w", pod.ID(), err)
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning pod %s save transaction: %w", pod.ID(), err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to save pod %s state: %v", pod.ID(), err)
			}
		}
	}()

	result, err := tx.Exec("UPDATE PodState SET JSON=? WHERE ID=?;", stateJSON, pod.ID())
	if err != nil {
		return fmt.Errorf("writing pod %s state: %w", pod.ID(), err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("retrieving pod %s save rows affected: %w", pod.ID(), err)
	}
	if rows == 0 {
		pod.valid = false
		return define.ErrNoSuchPod
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing pod %s state: %w", pod.ID(), err)
	}

	return nil
}

// AllPods returns all pods present in the state.
func (s *SQLiteState) AllPods() ([]*Pod, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	pods := []*Pod{}
	rows, err := s.conn.Query("SELECT JSON FROM PodConfig;")
	if err != nil {
		return nil, fmt.Errorf("retrieving all pods from database: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var rawJSON string
		if err := rows.Scan(&rawJSON); err != nil {
			return nil, fmt.Errorf("scanning pod from database: %w", err)
		}

		pod, err := s.createPod(rawJSON)
		if err != nil {
			return nil, err
		}

		pods = append(pods, pod)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return pods, nil
}

// AddVolume adds the given volume to the state. It also adds ctrDepID to
// the sub bucket holding the container dependencies that this volume has
func (s *SQLiteState) AddVolume(volume *Volume) (defErr error) {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !volume.valid {
		return define.ErrVolumeRemoved
	}

	cfgJSON, err := json.Marshal(volume.config)
	if err != nil {
		return fmt.Errorf("marshalling volume %s configuration json: %w", volume.Name(), err)
	}

	volState := volume.state
	if volState == nil {
		volState = new(VolumeState)
	}

	stateJSON, err := json.Marshal(volState)
	if err != nil {
		return fmt.Errorf("marshalling volume %s state json: %w", volume.Name(), err)
	}

	storageID := sql.NullString{}
	if volume.config.StorageID != "" {
		storageID.Valid = true
		storageID.String = volume.config.StorageID
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning volume create transaction: %w", err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to create volume: %v", err)
			}
		}
	}()

	// TODO: There has to be a better way of doing this
	var check int
	row := tx.QueryRow("SELECT 1 FROM VolumeConfig WHERE Name=?;", volume.Name())
	if err := row.Scan(&check); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("checking if volume name %s exists in database: %w", volume.Name(), err)
		}
	} else if check != 0 {
		return fmt.Errorf("name %q is in use: %w", volume.Name(), define.ErrVolumeExists)
	}

	if _, err := tx.Exec("INSERT INTO VolumeConfig VALUES (?, ?, ?);", volume.Name(), storageID, cfgJSON); err != nil {
		return fmt.Errorf("adding volume %s config to database: %w", volume.Name(), err)
	}

	if _, err := tx.Exec("INSERT INTO VolumeState VALUES (?, ?);", volume.Name(), stateJSON); err != nil {
		return fmt.Errorf("adding volume %s state to database: %w", volume.Name(), err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

// RemoveVolume removes the given volume from the state
func (s *SQLiteState) RemoveVolume(volume *Volume) (defErr error) {
	if !s.valid {
		return define.ErrDBClosed
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning volume %s removal transaction: %w", volume.Name(), err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to remove volume %s: %v", volume.Name(), err)
			}
		}
	}()

	rows, err := tx.Query("SELECT ContainerID FROM ContainerVolume WHERE VolumeName=?;", volume.Name())
	if err != nil {
		return fmt.Errorf("querying for containers using volume %s: %w", volume.Name(), err)
	}
	defer rows.Close()

	var ctrs []string
	for rows.Next() {
		var ctr string
		if err := rows.Scan(&ctr); err != nil {
			return fmt.Errorf("error scanning row for containers using volume %s: %w", volume.Name(), err)
		}
		ctrs = append(ctrs, ctr)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ctrs) > 0 {
		return fmt.Errorf("volume %s is in use by containers %s: %w", volume.Name(), strings.Join(ctrs, ","), define.ErrVolumeBeingUsed)
	}

	// TODO TODO TODO:
	// Need to verify that at least 1 row was deleted from VolumeConfig.
	// Otherwise return ErrNoSuchVolume

	if _, err := tx.Exec("DELETE FROM VolumeConfig WHERE Name=?;", volume.Name()); err != nil {
		return fmt.Errorf("removing volume %s config from DB: %w", volume.Name(), err)
	}

	if _, err := tx.Exec("DELETE FROM VolumeState WHERE Name=?;", volume.Name()); err != nil {
		return fmt.Errorf("removing volume %s state from DB: %w", volume.Name(), err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction to remove volume %s: %w", volume.Name(), err)
	}

	return nil
}

// UpdateVolume updates the volume's state from the database.
func (s *SQLiteState) UpdateVolume(volume *Volume) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !volume.valid {
		return define.ErrVolumeRemoved
	}

	row := s.conn.QueryRow("SELECT JSON FROM VolumeState WHERE Name=?;", volume.Name())

	var stateJSON string
	if err := row.Scan(&stateJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			volume.valid = false
			return define.ErrNoSuchVolume
		}
		return fmt.Errorf("scanning volume %s state JSON: %w", volume.Name(), err)
	}

	newState := new(VolumeState)
	if err := json.Unmarshal([]byte(stateJSON), newState); err != nil {
		return fmt.Errorf("unmarshalling volume %s state: %w", volume.Name(), err)
	}

	volume.state = newState

	return nil
}

// SaveVolume saves the volume's state to the database.
func (s *SQLiteState) SaveVolume(volume *Volume) (defErr error) {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !volume.valid {
		return define.ErrVolumeRemoved
	}

	stateJSON, err := json.Marshal(volume.state)
	if err != nil {
		return fmt.Errorf("marshalling volume %s state JSON: %w", volume.Name(), err)
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction to rewrite volume %s state: %w", volume.Name(), err)
	}
	defer func() {
		if defErr != nil {
			if err := tx.Rollback(); err != nil {
				logrus.Errorf("Rolling back transaction to rewrite volume %s state: %v", volume.Name(), err)
			}
		}
	}()

	results, err := tx.Exec("UPDATE VolumeState SET JSON=? WHERE Name=?;", stateJSON, volume.Name())
	if err != nil {
		return fmt.Errorf("updating volume %s state in DB: %w", volume.Name(), err)
	}
	rows, err := results.RowsAffected()
	if err != nil {
		return fmt.Errorf("retrieving volume %s state rewrite rows affected: %w", volume.Name(), err)
	}
	if rows == 0 {
		volume.valid = false
		return define.ErrNoSuchVolume
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction to rewrite volume %s state: %w", volume.Name(), err)
	}

	return nil
}

// AllVolumes returns all volumes present in the state.
func (s *SQLiteState) AllVolumes() ([]*Volume, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	rows, err := s.conn.Query("SELECT JSON FROM VolumeConfig;")
	if err != nil {
		return nil, fmt.Errorf("querying database for all volumes: %w", err)
	}
	defer rows.Close()

	var volumes []*Volume

	for rows.Next() {
		var configJSON string
		if err := rows.Scan(&configJSON); err != nil {
			return nil, fmt.Errorf("scanning volume config from database: %w", err)
		}
		vol := new(Volume)
		vol.config = new(VolumeConfig)
		vol.state = new(VolumeState)
		vol.runtime = s.runtime

		if err := json.Unmarshal([]byte(configJSON), vol.config); err != nil {
			return nil, fmt.Errorf("unmarshalling volume config: %w", err)
		}

		if err := finalizeVolumeSqlite(vol); err != nil {
			return nil, err
		}

		volumes = append(volumes, vol)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return volumes, nil
}

// Volume retrieves a volume from full name.
func (s *SQLiteState) Volume(name string) (*Volume, error) {
	if name == "" {
		return nil, define.ErrEmptyID
	}

	if !s.valid {
		return nil, define.ErrDBClosed
	}

	row := s.conn.QueryRow("SELECT JSON FROM VolumeConfig WHERE Name=?;", name)

	var configJSON string

	if err := row.Scan(&configJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, define.ErrNoSuchVolume
		}
		return nil, fmt.Errorf("querying volume %s: %w", name, err)
	}

	vol := new(Volume)
	vol.config = new(VolumeConfig)
	vol.state = new(VolumeState)
	vol.runtime = s.runtime

	if err := json.Unmarshal([]byte(configJSON), vol.config); err != nil {
		return nil, fmt.Errorf("unmarshalling volume %s config JSON: %w", name, err)
	}

	if err := finalizeVolumeSqlite(vol); err != nil {
		return nil, err
	}

	return vol, nil
}

// LookupVolume locates a volume from a unique partial name.
func (s *SQLiteState) LookupVolume(name string) (*Volume, error) {
	if name == "" {
		return nil, define.ErrEmptyID
	}

	if !s.valid {
		return nil, define.ErrDBClosed
	}

	escaper := strings.NewReplacer("\\", "\\\\", "_", "\\_", "%", "\\%")
	queryString := escaper.Replace(name) + "%"
	rows, err := s.conn.Query("SELECT Name, JSON FROM VolumeConfig WHERE Name LIKE ? ESCAPE '\\' ORDER BY LENGTH(Name) ASC;", queryString)
	if err != nil {
		return nil, fmt.Errorf("querying database for volume %s: %w", name, err)
	}
	defer rows.Close()

	var foundName, configJSON string
	for rows.Next() {
		if foundName != "" {
			return nil, fmt.Errorf("more than one result for volume name %s: %w", name, define.ErrVolumeExists)
		}
		if err := rows.Scan(&foundName, &configJSON); err != nil {
			return nil, fmt.Errorf("retrieving volume %s config from database: %w", name, err)
		}
		if foundName == name {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if foundName == "" {
		return nil, fmt.Errorf("no volume with name %q found: %w", name, define.ErrNoSuchVolume)
	}

	vol := new(Volume)
	vol.config = new(VolumeConfig)
	vol.state = new(VolumeState)
	vol.runtime = s.runtime

	if err := json.Unmarshal([]byte(configJSON), vol.config); err != nil {
		return nil, fmt.Errorf("unmarshalling volume %s config JSON: %w", name, err)
	}

	if err := finalizeVolumeSqlite(vol); err != nil {
		return nil, err
	}

	return vol, nil
}

// HasVolume returns true if the given volume exists in the state.
// Otherwise it returns false.
func (s *SQLiteState) HasVolume(name string) (bool, error) {
	if name == "" {
		return false, define.ErrEmptyID
	}

	if !s.valid {
		return false, define.ErrDBClosed
	}

	row := s.conn.QueryRow("SELECT 1 FROM VolumeConfig WHERE Name=?;", name)

	var check int
	if err := row.Scan(&check); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("looking up volume %s in database: %w", name, err)
	}
	if check != 1 {
		return false, fmt.Errorf("check digit for volume %s lookup incorrect: %w", name, define.ErrInternal)
	}

	return true, nil
}

// VolumeInUse checks if any container is using the volume.
// It returns a slice of the IDs of the containers using the given
// volume. If the slice is empty, no containers use the given volume.
func (s *SQLiteState) VolumeInUse(volume *Volume) ([]string, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	if !volume.valid {
		return nil, define.ErrVolumeRemoved
	}

	rows, err := s.conn.Query("SELECT ContainerID FROM ContainerVolume WHERE VolumeName=?;", volume.Name())
	if err != nil {
		return nil, fmt.Errorf("querying database for containers using volume %s: %w", volume.Name(), err)
	}
	defer rows.Close()

	var ctrs []string
	for rows.Next() {
		var ctr string
		if err := rows.Scan(&ctr); err != nil {
			return nil, fmt.Errorf("scanning container ID for container using volume %s: %w", volume.Name(), err)
		}
		ctrs = append(ctrs, ctr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ctrs, nil
}

// ContainerIDIsVolume checks if the given c/storage container ID is used as
// backing storage for a volume.
func (s *SQLiteState) ContainerIDIsVolume(id string) (bool, error) {
	if !s.valid {
		return false, define.ErrDBClosed
	}

	row := s.conn.QueryRow("SELECT 1 FROM VolumeConfig WHERE StorageID=?;", id)
	var checkDigit int
	if err := row.Scan(&checkDigit); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("error retrieving volumes using storage ID %s: %w", id, err)
	}
	if checkDigit != 1 {
		return false, fmt.Errorf("check digit for volumes using storage ID %s was incorrect: %w", id, define.ErrInternal)
	}

	return true, nil
}
