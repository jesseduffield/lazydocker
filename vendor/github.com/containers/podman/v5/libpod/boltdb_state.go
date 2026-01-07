//go:build !remote

package libpod

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/sirupsen/logrus"
	bolt "go.etcd.io/bbolt"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/storage/pkg/fileutils"
)

// BoltState is a state implementation backed by a Bolt DB
type BoltState struct {
	valid   bool
	dbPath  string
	dbLock  sync.Mutex
	runtime *Runtime
}

// A brief description of the format of the BoltDB state:
// At the top level, the following buckets are created:
// - idRegistryBkt: Maps ID to Name for containers and pods.
//   Used to ensure container and pod IDs are globally unique.
// - nameRegistryBkt: Maps Name to ID for containers and pods.
//   Used to ensure container and pod names are globally unique.
// - ctrBkt: Contains a sub-bucket for each container in the state.
//   Each sub-bucket has config and state keys holding the container's JSON
//   encoded configuration and state (respectively), an optional netNS key
//   containing the path to the container's network namespace, a dependencies
//   bucket containing the container's dependencies, and an optional pod key
//   containing the ID of the pod the container is joined to.
//   After updates to include exec sessions, may also include an exec bucket
//   with the IDs of exec sessions currently in use by the container.
// - allCtrsBkt: Map of ID to name containing only containers. Used for
//   container lookup operations.
// - podBkt: Contains a sub-bucket for each pod in the state.
//   Each sub-bucket has config and state keys holding the pod's JSON encoded
//   configuration and state, plus a containers sub bucket holding the IDs of
//   containers in the pod.
// - allPodsBkt: Map of ID to name containing only pods. Used for pod lookup
//   operations.
// - execBkt: Map of exec session ID to container ID - used for resolving
//   exec session IDs to the containers that hold the exec session.
// - networksBkt: Contains all network names as key with their options json
//   encoded as value.
// - aliasesBkt - Deprecated, use the networksBkt. Used to contain a bucket
//   for each CNI network which contain a map of network alias (an extra name
//   for containers in DNS) to the ID of the container holding the alias.
//   Aliases must be unique per-network, and cannot conflict with names
//   registered in nameRegistryBkt.
// - runtimeConfigBkt: Contains configuration of the libpod instance that
//   initially created the database. This must match for any further instances
//   that access the database, to ensure that state mismatches with
//   containers/storage do not occur.
// - exitCodeBucket/exitCodeTimeStampBucket: (#14559) exit codes must be part
//   of the database to resolve a previous race condition when one process waits
//   for the exit file to be written and another process removes it along with
//   the container during auto-removal.  The same race would happen trying to
//   read the exit code from the containers bucket.  Hence, exit codes go into
//   their own bucket.  To avoid the rather expensive JSON (un)marshalling, we
//   have two buckets: one for the exit codes, the other for the timestamps.

// NewBoltState creates a new bolt-backed state database
func NewBoltState(path string, runtime *Runtime) (State, error) {
	logrus.Info("Using boltdb as database backend")
	state := new(BoltState)
	state.dbPath = path
	state.runtime = runtime

	logrus.Debugf("Initializing boltdb state at %s", path)

	ciDesiredDB := os.Getenv("CI_DESIRED_DATABASE")

	// BoltDB is deprecated and, as of Podman 5.0, we no longer allow the
	// creation of new Bolt states.
	// If the DB does not already exist, error out.
	// To continue testing in CI, allow creation iff an undocumented env
	// var is set.
	if ciDesiredDB != "boltdb" {
		if err := fileutils.Exists(path); err != nil && errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("the BoltDB backend has been deprecated, no new BoltDB databases can be created: %w", define.ErrInvalidArg)
		}
	} else {
		logrus.Debugf("Allowing deprecated database backend due to CI_DESIRED_DATABASE.")
	}

	// TODO: Up this to ERROR level in 5.8
	if os.Getenv("SUPPRESS_BOLTDB_WARNING") == "" && ciDesiredDB != "boltdb" {
		logrus.Warnf("The deprecated BoltDB database driver is in use. This driver will be removed in the upcoming Podman 6.0 release in mid 2026. It is advised that you migrate to SQLite to avoid issues when this occurs. Set SUPPRESS_BOLTDB_WARNING environment variable to remove this message.")
	}

	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", path, err)
	}
	// Everywhere else, we use s.deferredCloseDBCon(db) to ensure the state's DB
	// mutex is also unlocked.
	// However, here, the mutex has not been locked, since we just created
	// the DB connection, and it hasn't left this function yet - no risk of
	// concurrent access.
	// As such, just a db.Close() is fine here.
	defer db.Close()

	createBuckets := [][]byte{
		idRegistryBkt,
		nameRegistryBkt,
		ctrBkt,
		allCtrsBkt,
		podBkt,
		allPodsBkt,
		volBkt,
		allVolsBkt,
		execBkt,
		runtimeConfigBkt,
		exitCodeBkt,
		exitCodeTimeStampBkt,
		volCtrsBkt,
	}

	// Does the DB need an update?
	needsUpdate := false
	err = db.View(func(tx *bolt.Tx) error {
		for _, bkt := range createBuckets {
			if test := tx.Bucket(bkt); test == nil {
				needsUpdate = true
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("checking DB schema: %w", err)
	}

	if !needsUpdate {
		state.valid = true
		return state, nil
	}

	// Ensure schema is properly created in DB
	err = db.Update(func(tx *bolt.Tx) error {
		for _, bkt := range createBuckets {
			if _, err := tx.CreateBucketIfNotExists(bkt); err != nil {
				return fmt.Errorf("creating bucket %s: %w", string(bkt), err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("creating buckets for DB: %w", err)
	}

	state.valid = true

	return state, nil
}

// Close closes the state and prevents further use
func (s *BoltState) Close() error {
	s.valid = false
	return nil
}

// Refresh clears container and pod states after a reboot
func (s *BoltState) Refresh() error {
	if !s.valid {
		return define.ErrDBClosed
	}

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	err = db.Update(func(tx *bolt.Tx) error {
		idBucket, err := getIDBucket(tx)
		if err != nil {
			return err
		}

		namesBucket, err := getNamesBucket(tx)
		if err != nil {
			return err
		}

		ctrsBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		podsBucket, err := getPodBucket(tx)
		if err != nil {
			return err
		}

		allVolsBucket, err := getAllVolsBucket(tx)
		if err != nil {
			return err
		}

		volBucket, err := getVolBucket(tx)
		if err != nil {
			return err
		}

		execBucket, err := getExecBucket(tx)
		if err != nil {
			return err
		}

		exitCodeBucket, err := getExitCodeBucket(tx)
		if err != nil {
			return err
		}

		timeStampBucket, err := getExitCodeTimeStampBucket(tx)
		if err != nil {
			return err
		}

		// Clear all exec exit codes
		toRemoveExitCodes := []string{}
		err = exitCodeBucket.ForEach(func(id, _ []byte) error {
			toRemoveExitCodes = append(toRemoveExitCodes, string(id))
			return nil
		})
		if err != nil {
			return fmt.Errorf("reading exit codes bucket: %w", err)
		}
		for _, id := range toRemoveExitCodes {
			if err := exitCodeBucket.Delete([]byte(id)); err != nil {
				return fmt.Errorf("removing exit code for ID %s: %w", id, err)
			}
		}

		toRemoveTimeStamps := []string{}
		err = timeStampBucket.ForEach(func(id, _ []byte) error {
			toRemoveTimeStamps = append(toRemoveTimeStamps, string(id))
			return nil
		})
		if err != nil {
			return fmt.Errorf("reading timestamps bucket: %w", err)
		}
		for _, id := range toRemoveTimeStamps {
			if err := timeStampBucket.Delete([]byte(id)); err != nil {
				return fmt.Errorf("removing timestamp for ID %s: %w", id, err)
			}
		}

		// Iterate through all IDs. Check if they are containers.
		// If they are, unmarshal their state, and then clear
		// PID, mountpoint, and state for all of them
		// Then save the modified state
		// Also clear all network namespaces
		toRemoveIDs := []string{}
		err = idBucket.ForEach(func(id, _ []byte) error {
			ctrBkt := ctrsBucket.Bucket(id)
			if ctrBkt == nil {
				// It's a pod
				podBkt := podsBucket.Bucket(id)
				if podBkt == nil {
					// This is neither a pod nor a container
					// Something is seriously wrong, but
					// continue on and try to clean up the
					// state and become consistent.
					// Just note what needs to be removed
					// for now - ForEach says you shouldn't
					// remove things from the table during
					// it.
					logrus.Errorf("Database issue: dangling ID %s found (not a pod or container) - removing", string(id))
					toRemoveIDs = append(toRemoveIDs, string(id))
					return nil
				}

				// Get the state
				stateBytes := podBkt.Get(stateKey)
				if stateBytes == nil {
					return fmt.Errorf("pod %s missing state key: %w", string(id), define.ErrInternal)
				}

				state := new(podState)

				if err := json.Unmarshal(stateBytes, state); err != nil {
					return fmt.Errorf("unmarshalling state for pod %s: %w", string(id), err)
				}

				// Refresh the state
				resetPodState(state)

				newStateBytes, err := json.Marshal(state)
				if err != nil {
					return fmt.Errorf("marshalling modified state for pod %s: %w", string(id), err)
				}

				if err := podBkt.Put(stateKey, newStateBytes); err != nil {
					return fmt.Errorf("updating state for pod %s in DB: %w", string(id), err)
				}

				// It's not a container, nothing to do
				return nil
			}

			// First, delete the network namespace
			if err := ctrBkt.Delete(netNSKey); err != nil {
				return fmt.Errorf("removing network namespace for container %s: %w", string(id), err)
			}

			stateBytes := ctrBkt.Get(stateKey)
			if stateBytes == nil {
				// Badly formatted container bucket
				return fmt.Errorf("container %s missing state in DB: %w", string(id), define.ErrInternal)
			}

			state := new(ContainerState)

			if err := json.Unmarshal(stateBytes, state); err != nil {
				return fmt.Errorf("unmarshalling state for container %s: %w", string(id), err)
			}

			resetContainerState(state)

			newStateBytes, err := json.Marshal(state)
			if err != nil {
				return fmt.Errorf("marshalling modified state for container %s: %w", string(id), err)
			}

			if err := ctrBkt.Put(stateKey, newStateBytes); err != nil {
				return fmt.Errorf("updating state for container %s in DB: %w", string(id), err)
			}

			// Delete all exec sessions, if there are any
			ctrExecBkt := ctrBkt.Bucket(execBkt)
			if ctrExecBkt != nil {
				// Can't delete in a ForEach, so build a list of
				// what to remove then remove.
				toRemove := []string{}
				err = ctrExecBkt.ForEach(func(id, _ []byte) error {
					toRemove = append(toRemove, string(id))
					return nil
				})
				if err != nil {
					return err
				}
				for _, execID := range toRemove {
					if err := ctrExecBkt.Delete([]byte(execID)); err != nil {
						return fmt.Errorf("removing exec session %s from container %s: %w", execID, string(id), err)
					}
				}
			}

			return nil
		})
		if err != nil {
			return err
		}

		// Remove dangling IDs.
		for _, id := range toRemoveIDs {
			// Look up the ID to see if we also have a dangling name
			// in the DB.
			name := idBucket.Get([]byte(id))
			if name != nil {
				if testID := namesBucket.Get(name); testID != nil {
					logrus.Infof("Found dangling name %s (ID %s) in database", string(name), id)
					if err := namesBucket.Delete(name); err != nil {
						return fmt.Errorf("removing dangling name %s (ID %s) from database: %w", string(name), id, err)
					}
				}
			}
			if err := idBucket.Delete([]byte(id)); err != nil {
				return fmt.Errorf("removing dangling ID %s from database: %w", id, err)
			}
		}

		// Now refresh volumes
		err = allVolsBucket.ForEach(func(id, _ []byte) error {
			dbVol := volBucket.Bucket(id)
			if dbVol == nil {
				return fmt.Errorf("inconsistency in state - volume %s is in all volumes bucket but volume not found: %w", string(id), define.ErrInternal)
			}

			// Get the state
			volStateBytes := dbVol.Get(stateKey)
			if volStateBytes == nil {
				// If the volume doesn't have a state, nothing to do
				return nil
			}

			oldState := new(VolumeState)

			if err := json.Unmarshal(volStateBytes, oldState); err != nil {
				return fmt.Errorf("unmarshalling state for volume %s: %w", string(id), err)
			}

			resetVolumeState(oldState)

			newState, err := json.Marshal(oldState)
			if err != nil {
				return fmt.Errorf("marshalling state for volume %s: %w", string(id), err)
			}

			if err := dbVol.Put(stateKey, newState); err != nil {
				return fmt.Errorf("storing new state for volume %s: %w", string(id), err)
			}

			return nil
		})
		if err != nil {
			return err
		}

		// Now refresh exec sessions
		// We want to remove them all, but for-each can't modify buckets
		// So we have to make a list of what to operate on, then do the
		// work.
		toRemoveExec := []string{}
		err = execBucket.ForEach(func(id, _ []byte) error {
			toRemoveExec = append(toRemoveExec, string(id))
			return nil
		})
		if err != nil {
			return err
		}

		for _, execSession := range toRemoveExec {
			if err := execBucket.Delete([]byte(execSession)); err != nil {
				return fmt.Errorf("deleting exec session %s registry from database: %w", execSession, err)
			}
		}

		return nil
	})
	return err
}

// GetDBConfig retrieves runtime configuration fields that were created when
// the database was first initialized
func (s *BoltState) GetDBConfig() (*DBConfig, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	cfg := new(DBConfig)

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		configBucket, err := getRuntimeConfigBucket(tx)
		if err != nil {
			return err
		}

		// Some of these may be nil
		// When we convert to string, Go will coerce them to ""
		// That's probably fine - we could raise an error if the key is
		// missing, but just not including it is also OK.
		libpodRoot := configBucket.Get(staticDirKey)
		libpodTmp := configBucket.Get(tmpDirKey)
		storageRoot := configBucket.Get(graphRootKey)
		storageTmp := configBucket.Get(runRootKey)
		graphDriver := configBucket.Get(graphDriverKey)
		volumePath := configBucket.Get(volPathKey)

		cfg.LibpodRoot = string(libpodRoot)
		cfg.LibpodTmp = string(libpodTmp)
		cfg.StorageRoot = string(storageRoot)
		cfg.StorageTmp = string(storageTmp)
		cfg.GraphDriver = string(graphDriver)
		cfg.VolumePath = string(volumePath)

		return nil
	})
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// ValidateDBConfig validates paths in the given runtime against the database
func (s *BoltState) ValidateDBConfig(runtime *Runtime) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	// Check runtime configuration
	if err := checkRuntimeConfig(db, runtime); err != nil {
		return err
	}

	return nil
}

// GetContainerName returns the name associated with a given ID.
// Returns ErrNoSuchCtr if the ID does not exist.
func (s *BoltState) GetContainerName(id string) (string, error) {
	if id == "" {
		return "", define.ErrEmptyID
	}

	if !s.valid {
		return "", define.ErrDBClosed
	}

	idBytes := []byte(id)

	db, err := s.getDBCon()
	if err != nil {
		return "", err
	}
	defer s.deferredCloseDBCon(db)

	name := ""

	err = db.View(func(tx *bolt.Tx) error {
		idBkt, err := getIDBucket(tx)
		if err != nil {
			return err
		}

		ctrsBkt, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		nameBytes := idBkt.Get(idBytes)
		if nameBytes == nil {
			return define.ErrNoSuchCtr
		}

		ctrExists := ctrsBkt.Bucket(idBytes)
		if ctrExists == nil {
			return define.ErrNoSuchCtr
		}

		name = string(nameBytes)
		return nil
	})
	if err != nil {
		return "", err
	}

	return name, nil
}

// GetPodName returns the name associated with a given ID.
// Returns ErrNoSuchPod if the ID does not exist.
func (s *BoltState) GetPodName(id string) (string, error) {
	if id == "" {
		return "", define.ErrEmptyID
	}

	if !s.valid {
		return "", define.ErrDBClosed
	}

	idBytes := []byte(id)

	db, err := s.getDBCon()
	if err != nil {
		return "", err
	}
	defer s.deferredCloseDBCon(db)

	name := ""

	err = db.View(func(tx *bolt.Tx) error {
		idBkt, err := getIDBucket(tx)
		if err != nil {
			return err
		}

		podBkt, err := getPodBucket(tx)
		if err != nil {
			return err
		}

		nameBytes := idBkt.Get(idBytes)
		if nameBytes == nil {
			return define.ErrNoSuchPod
		}

		podExists := podBkt.Bucket(idBytes)
		if podExists == nil {
			return define.ErrNoSuchPod
		}

		name = string(nameBytes)
		return nil
	})
	if err != nil {
		return "", err
	}

	return name, nil
}

// Container retrieves a single container from the state by its full ID
func (s *BoltState) Container(id string) (*Container, error) {
	if id == "" {
		return nil, define.ErrEmptyID
	}

	if !s.valid {
		return nil, define.ErrDBClosed
	}

	ctrID := []byte(id)

	ctr := new(Container)
	ctr.config = new(ContainerConfig)
	ctr.state = new(ContainerState)

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		return s.getContainerFromDB(ctrID, ctr, ctrBucket, false)
	})
	if err != nil {
		return nil, err
	}

	return ctr, nil
}

// LookupContainerID retrieves a container ID from the state by full or unique
// partial ID or name
func (s *BoltState) LookupContainerID(idOrName string) (string, error) {
	if idOrName == "" {
		return "", define.ErrEmptyID
	}

	if !s.valid {
		return "", define.ErrDBClosed
	}

	db, err := s.getDBCon()
	if err != nil {
		return "", err
	}
	defer s.deferredCloseDBCon(db)

	var id []byte
	err = db.View(func(tx *bolt.Tx) error {
		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		namesBucket, err := getNamesBucket(tx)
		if err != nil {
			return err
		}

		fullID, err := s.lookupContainerID(idOrName, ctrBucket, namesBucket)
		id = fullID
		return err
	})

	if err != nil {
		return "", err
	}

	retID := string(id)
	return retID, nil
}

// LookupContainer retrieves a container from the state by full or unique
// partial ID or name
func (s *BoltState) LookupContainer(idOrName string) (*Container, error) {
	if idOrName == "" {
		return nil, define.ErrEmptyID
	}

	if !s.valid {
		return nil, define.ErrDBClosed
	}

	ctr := new(Container)
	ctr.config = new(ContainerConfig)
	ctr.state = new(ContainerState)

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		namesBucket, err := getNamesBucket(tx)
		if err != nil {
			return err
		}

		id, err := s.lookupContainerID(idOrName, ctrBucket, namesBucket)
		if err != nil {
			return err
		}

		return s.getContainerFromDB(id, ctr, ctrBucket, false)
	})
	if err != nil {
		return nil, err
	}

	return ctr, nil
}

// HasContainer checks if a container is present in the state
func (s *BoltState) HasContainer(id string) (bool, error) {
	if id == "" {
		return false, define.ErrEmptyID
	}

	if !s.valid {
		return false, define.ErrDBClosed
	}

	ctrID := []byte(id)

	db, err := s.getDBCon()
	if err != nil {
		return false, err
	}
	defer s.deferredCloseDBCon(db)

	exists := false

	err = db.View(func(tx *bolt.Tx) error {
		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		ctrDB := ctrBucket.Bucket(ctrID)
		if ctrDB != nil {
			exists = true
		}

		return nil
	})
	if err != nil {
		return false, err
	}

	return exists, nil
}

// AddContainer adds a container to the state
// The container being added cannot belong to a pod
func (s *BoltState) AddContainer(ctr *Container) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	if ctr.config.Pod != "" {
		return fmt.Errorf("cannot add a container that belongs to a pod with AddContainer - use AddContainerToPod: %w", define.ErrInvalidArg)
	}

	return s.addContainer(ctr, nil)
}

// RemoveContainer removes a container from the state
// Only removes containers not in pods - for containers that are a member of a
// pod, use RemoveContainerFromPod
func (s *BoltState) RemoveContainer(ctr *Container) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if ctr.config.Pod != "" {
		return fmt.Errorf("container %s is part of a pod, use RemoveContainerFromPod instead: %w", ctr.ID(), define.ErrPodExists)
	}

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	err = db.Update(func(tx *bolt.Tx) error {
		return s.removeContainer(ctr, nil, tx)
	})
	return err
}

// UpdateContainer updates a container's state from the database
func (s *BoltState) UpdateContainer(ctr *Container) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	ctrID := []byte(ctr.ID())

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	return db.View(func(tx *bolt.Tx) error {
		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}
		return s.getContainerStateDB(ctrID, ctr, ctrBucket)
	})
}

// SaveContainer saves a container's current state in the database
func (s *BoltState) SaveContainer(ctr *Container) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	stateJSON, err := json.Marshal(ctr.state)
	if err != nil {
		return fmt.Errorf("marshalling container %s state to JSON: %w", ctr.ID(), err)
	}
	netNSPath := ctr.state.NetNS

	ctrID := []byte(ctr.ID())

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	err = db.Update(func(tx *bolt.Tx) error {
		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		ctrToSave := ctrBucket.Bucket(ctrID)
		if ctrToSave == nil {
			ctr.valid = false
			return fmt.Errorf("container %s does not exist in DB: %w", ctr.ID(), define.ErrNoSuchCtr)
		}

		// Update the state
		if err := ctrToSave.Put(stateKey, stateJSON); err != nil {
			return fmt.Errorf("updating container %s state in DB: %w", ctr.ID(), err)
		}

		if netNSPath == "" {
			// Delete the existing network namespace
			if err := ctrToSave.Delete(netNSKey); err != nil {
				return fmt.Errorf("removing network namespace path for container %s in DB: %w", ctr.ID(), err)
			}
		}

		return nil
	})
	return err
}

// ContainerInUse checks if other containers depend on the given container
// It returns a slice of the IDs of the containers depending on the given
// container. If the slice is empty, no containers depend on the given container
func (s *BoltState) ContainerInUse(ctr *Container) ([]string, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	if !ctr.valid {
		return nil, define.ErrCtrRemoved
	}

	depCtrs := []string{}

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		ctrDB := ctrBucket.Bucket([]byte(ctr.ID()))
		if ctrDB == nil {
			ctr.valid = false
			return fmt.Errorf("no container with ID %q found in DB: %w", ctr.ID(), define.ErrNoSuchCtr)
		}

		dependsBkt := ctrDB.Bucket(dependenciesBkt)
		if dependsBkt == nil {
			return fmt.Errorf("container %s has no dependencies bucket: %w", ctr.ID(), define.ErrInternal)
		}

		// Iterate through and add dependencies
		err = dependsBkt.ForEach(func(id, _ []byte) error {
			depCtrs = append(depCtrs, string(id))

			return nil
		})
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return depCtrs, nil
}

// AllContainers retrieves all the containers in the database
// If `loadState` is set, the containers' state will be loaded as well.
func (s *BoltState) AllContainers(loadState bool) ([]*Container, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	ctrs := []*Container{}

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		allCtrsBucket, err := getAllCtrsBucket(tx)
		if err != nil {
			return err
		}

		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		return allCtrsBucket.ForEach(func(id, _ []byte) error {
			// If performance becomes an issue, this check can be
			// removed. But the error messages that come back will
			// be much less helpful.
			ctrExists := ctrBucket.Bucket(id)
			if ctrExists == nil {
				return fmt.Errorf("state is inconsistent - container ID %s in all containers, but container not found: %w", string(id), define.ErrInternal)
			}

			ctr := new(Container)
			ctr.config = new(ContainerConfig)
			ctr.state = new(ContainerState)

			if err := s.getContainerFromDB(id, ctr, ctrBucket, loadState); err != nil {
				logrus.Errorf("Error retrieving container from database: %v", err)
			} else {
				ctrs = append(ctrs, ctr)
			}

			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	return ctrs, nil
}

// GetNetworks returns the networks this container is a part of.
func (s *BoltState) GetNetworks(ctr *Container) (map[string]types.PerNetworkOptions, error) {
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

	ctrID := []byte(ctr.ID())

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	networks := make(map[string]types.PerNetworkOptions)

	var convertDB bool

	err = db.View(func(tx *bolt.Tx) error {
		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		dbCtr := ctrBucket.Bucket(ctrID)
		if dbCtr == nil {
			ctr.valid = false
			return fmt.Errorf("container %s does not exist in database: %w", ctr.ID(), define.ErrNoSuchCtr)
		}

		ctrNetworkBkt := dbCtr.Bucket(networksBkt)
		if ctrNetworkBkt == nil {
			// convert if needed
			convertDB = true
			return nil
		}

		return ctrNetworkBkt.ForEach(func(network, v []byte) error {
			opts := types.PerNetworkOptions{}
			if err := json.Unmarshal(v, &opts); err != nil {
				// special case for backwards compat
				// earlier version used the container id as value so we set a
				// special error to indicate the we have to migrate the db
				if !bytes.Equal(v, ctrID) {
					return err
				}
				convertDB = true
			}
			networks[string(network)] = opts
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if convertDB {
		err = db.Update(func(tx *bolt.Tx) error {
			ctrBucket, err := getCtrBucket(tx)
			if err != nil {
				return err
			}

			dbCtr := ctrBucket.Bucket(ctrID)
			if dbCtr == nil {
				ctr.valid = false
				return fmt.Errorf("container %s does not exist in database: %w", ctr.ID(), define.ErrNoSuchCtr)
			}

			var networkList []string

			ctrNetworkBkt := dbCtr.Bucket(networksBkt)
			if ctrNetworkBkt == nil {
				ctrNetworkBkt, err = dbCtr.CreateBucket(networksBkt)
				if err != nil {
					return fmt.Errorf("creating networks bucket for container %s: %w", ctr.ID(), err)
				}
				// the container has no networks in the db lookup config and write to the db
				networkList = ctr.config.NetworksDeprecated
				// if there are no networks we have to add the default
				if len(networkList) == 0 {
					networkList = []string{ctr.runtime.config.Network.DefaultNetwork}
				}
			} else {
				err = ctrNetworkBkt.ForEach(func(network, _ []byte) error {
					networkList = append(networkList, string(network))
					return nil
				})
				if err != nil {
					return err
				}
			}

			// the container has no networks in the db lookup config and write to the db
			for i, network := range networkList {
				var intName string
				if ctr.state.NetInterfaceDescriptions != nil {
					eth, exists := ctr.state.NetInterfaceDescriptions.getInterfaceByName(network)
					if !exists {
						return fmt.Errorf("no network interface name for container %s on network %s", ctr.config.ID, network)
					}
					intName = eth
				} else {
					intName = fmt.Sprintf("eth%d", i)
				}
				getAliases := func(network string) []string {
					var aliases []string
					ctrAliasesBkt := dbCtr.Bucket(aliasesBkt)
					if ctrAliasesBkt == nil {
						return nil
					}
					netAliasesBkt := ctrAliasesBkt.Bucket([]byte(network))
					if netAliasesBkt == nil {
						// No aliases for this specific network.
						return nil
					}

					// let's ignore the error here there is nothing we can do
					_ = netAliasesBkt.ForEach(func(alias, _ []byte) error {
						aliases = append(aliases, string(alias))
						return nil
					})
					// also add the short container id as alias
					return aliases
				}

				netOpts := &types.PerNetworkOptions{
					InterfaceName: intName,
					// we have to add the short id as alias for docker compat
					Aliases: append(getAliases(network), ctr.config.ID[:12]),
				}
				// only set the static ip/mac on the first network
				if i == 0 {
					if ctr.config.StaticIP != nil {
						netOpts.StaticIPs = []net.IP{ctr.config.StaticIP}
					}
					netOpts.StaticMAC = ctr.config.StaticMAC
				}

				optsBytes, err := json.Marshal(netOpts)
				if err != nil {
					return err
				}
				// insert into network map because we need to return this
				networks[network] = *netOpts

				err = ctrNetworkBkt.Put([]byte(network), optsBytes)
				if err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return networks, nil
}

// NetworkConnect adds the given container to the given network. If aliases are
// specified, those will be added to the given network.
func (s *BoltState) NetworkConnect(ctr *Container, network string, opts types.PerNetworkOptions) error {
	return s.networkModify(ctr, network, opts, true)
}

// NetworkModify will allow you to set new options on an existing connected network
func (s *BoltState) NetworkModify(ctr *Container, network string, opts types.PerNetworkOptions) error {
	return s.networkModify(ctr, network, opts, false)
}

// networkModify allows you to modify or add a new network, to add a new network use the new bool
func (s *BoltState) networkModify(ctr *Container, network string, opts types.PerNetworkOptions, new bool) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	if network == "" {
		return fmt.Errorf("network names must not be empty: %w", define.ErrInvalidArg)
	}

	optBytes, err := json.Marshal(opts)
	if err != nil {
		return fmt.Errorf("marshalling network options JSON for container %s: %w", ctr.ID(), err)
	}

	ctrID := []byte(ctr.ID())

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	return db.Update(func(tx *bolt.Tx) error {
		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		dbCtr := ctrBucket.Bucket(ctrID)
		if dbCtr == nil {
			ctr.valid = false
			return fmt.Errorf("container %s does not exist in database: %w", ctr.ID(), define.ErrNoSuchCtr)
		}

		ctrNetworksBkt := dbCtr.Bucket(networksBkt)
		if ctrNetworksBkt == nil {
			return fmt.Errorf("container %s does not have a network bucket: %w", ctr.ID(), define.ErrNoSuchNetwork)
		}
		netConnected := ctrNetworksBkt.Get([]byte(network))

		if new && netConnected != nil {
			return fmt.Errorf("container %s is already connected to network %q: %w", ctr.ID(), network, define.ErrNetworkConnected)
		} else if !new && netConnected == nil {
			return fmt.Errorf("container %s is not connected to network %q: %w", ctr.ID(), network, define.ErrNoSuchNetwork)
		}

		// Modify/Add the network
		if err := ctrNetworksBkt.Put([]byte(network), optBytes); err != nil {
			return fmt.Errorf("adding container %s to network %s in DB: %w", ctr.ID(), network, err)
		}

		return nil
	})
}

// NetworkDisconnect disconnects the container from the given network, also
// removing any aliases in the network.
func (s *BoltState) NetworkDisconnect(ctr *Container, network string) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	if network == "" {
		return fmt.Errorf("network names must not be empty: %w", define.ErrInvalidArg)
	}

	ctrID := []byte(ctr.ID())

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	return db.Update(func(tx *bolt.Tx) error {
		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		dbCtr := ctrBucket.Bucket(ctrID)
		if dbCtr == nil {
			ctr.valid = false
			return fmt.Errorf("container %s does not exist in database: %w", ctr.ID(), define.ErrNoSuchCtr)
		}

		ctrAliasesBkt := dbCtr.Bucket(aliasesBkt)
		ctrNetworksBkt := dbCtr.Bucket(networksBkt)
		if ctrNetworksBkt == nil {
			return fmt.Errorf("container %s is not connected to any networks, so cannot disconnect: %w", ctr.ID(), define.ErrNoSuchNetwork)
		}
		netConnected := ctrNetworksBkt.Get([]byte(network))
		if netConnected == nil {
			return fmt.Errorf("container %s is not connected to network %q: %w", ctr.ID(), network, define.ErrNoSuchNetwork)
		}

		if err := ctrNetworksBkt.Delete([]byte(network)); err != nil {
			return fmt.Errorf("removing container %s from network %s: %w", ctr.ID(), network, err)
		}

		if ctrAliasesBkt != nil {
			bktExists := ctrAliasesBkt.Bucket([]byte(network))
			if bktExists == nil {
				return nil
			}

			if err := ctrAliasesBkt.DeleteBucket([]byte(network)); err != nil {
				return fmt.Errorf("removing container %s network aliases for network %s: %w", ctr.ID(), network, err)
			}
		}

		return nil
	})
}

// GetContainerConfig returns a container config from the database by full ID
func (s *BoltState) GetContainerConfig(id string) (*ContainerConfig, error) {
	if len(id) == 0 {
		return nil, define.ErrEmptyID
	}

	if !s.valid {
		return nil, define.ErrDBClosed
	}

	config := new(ContainerConfig)

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		return s.getContainerConfigFromDB([]byte(id), config, ctrBucket)
	})
	if err != nil {
		return nil, err
	}

	return config, nil
}

// AddContainerExitCode adds the exit code for the specified container to the database.
func (s *BoltState) AddContainerExitCode(id string, exitCode int32) error {
	if len(id) == 0 {
		return define.ErrEmptyID
	}

	if !s.valid {
		return define.ErrDBClosed
	}

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	rawID := []byte(id)
	rawExitCode := []byte(strconv.Itoa(int(exitCode)))
	rawTimeStamp, err := time.Now().MarshalText()
	if err != nil {
		return fmt.Errorf("marshalling exit-code time stamp: %w", err)
	}

	return db.Update(func(tx *bolt.Tx) error {
		exitCodeBucket, err := getExitCodeBucket(tx)
		if err != nil {
			return err
		}
		timeStampBucket, err := getExitCodeTimeStampBucket(tx)
		if err != nil {
			return err
		}

		if err := exitCodeBucket.Put(rawID, rawExitCode); err != nil {
			return fmt.Errorf("adding exit code of container %s to DB: %w", id, err)
		}
		if err := timeStampBucket.Put(rawID, rawTimeStamp); err != nil {
			if rmErr := exitCodeBucket.Delete(rawID); rmErr != nil {
				logrus.Errorf("Removing exit code of container %s from DB: %v", id, rmErr)
			}
			return fmt.Errorf("adding exit-code time stamp of container %s to DB: %w", id, err)
		}

		return nil
	})
}

// GetContainerExitCode returns the exit code for the specified container.
func (s *BoltState) GetContainerExitCode(id string) (int32, error) {
	if len(id) == 0 {
		return -1, define.ErrEmptyID
	}

	if !s.valid {
		return -1, define.ErrDBClosed
	}

	db, err := s.getDBCon()
	if err != nil {
		return -1, err
	}
	defer s.deferredCloseDBCon(db)

	rawID := []byte(id)
	result := int32(-1)
	return result, db.View(func(tx *bolt.Tx) error {
		exitCodeBucket, err := getExitCodeBucket(tx)
		if err != nil {
			return err
		}

		rawExitCode := exitCodeBucket.Get(rawID)
		if rawExitCode == nil {
			return fmt.Errorf("getting exit code of container %s from DB: %w", id, define.ErrNoSuchExitCode)
		}

		exitCode, err := strconv.Atoi(string(rawExitCode))
		if err != nil {
			return fmt.Errorf("converting raw exit code %v of container %s: %w", rawExitCode, id, err)
		}

		result = int32(exitCode)
		return nil
	})
}

// GetContainerExitCodeTimeStamp returns the time stamp when the exit code of
// the specified container was added to the database.
func (s *BoltState) GetContainerExitCodeTimeStamp(id string) (*time.Time, error) {
	if len(id) == 0 {
		return nil, define.ErrEmptyID
	}

	if !s.valid {
		return nil, define.ErrDBClosed
	}

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	rawID := []byte(id)
	var result time.Time
	return &result, db.View(func(tx *bolt.Tx) error {
		timeStampBucket, err := getExitCodeTimeStampBucket(tx)
		if err != nil {
			return err
		}

		rawTimeStamp := timeStampBucket.Get(rawID)
		if rawTimeStamp == nil {
			return fmt.Errorf("getting exit-code time stamp of container %s from DB: %w", id, define.ErrNoSuchExitCode)
		}

		if err := result.UnmarshalText(rawTimeStamp); err != nil {
			return fmt.Errorf("converting raw time stamp %v of container %s from DB: %w", rawTimeStamp, id, err)
		}

		return nil
	})
}

// PruneContainerExitCodes removes exit codes older than 5 minutes unless the associated
// container still exists.
func (s *BoltState) PruneContainerExitCodes() error {
	if !s.valid {
		return define.ErrDBClosed
	}

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	toRemoveIDs := []string{}

	threshold := time.Minute * 5
	err = db.View(func(tx *bolt.Tx) error {
		timeStampBucket, err := getExitCodeTimeStampBucket(tx)
		if err != nil {
			return err
		}

		ctrsBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		return timeStampBucket.ForEach(func(rawID, rawTimeStamp []byte) error {
			if ctrsBucket.Bucket(rawID) != nil {
				// If the container still exists, don't prune
				// its exit code since we may still need it.
				return nil
			}
			var timeStamp time.Time
			if err := timeStamp.UnmarshalText(rawTimeStamp); err != nil {
				return fmt.Errorf("converting raw time stamp %v of container %s from DB: %w", rawTimeStamp, string(rawID), err)
			}
			if time.Since(timeStamp) > threshold {
				toRemoveIDs = append(toRemoveIDs, string(rawID))
			}
			return nil
		})
	})
	if err != nil {
		return fmt.Errorf("reading exit codes to prune: %w", err)
	}

	if len(toRemoveIDs) > 0 {
		err = db.Update(func(tx *bolt.Tx) error {
			exitCodeBucket, err := getExitCodeBucket(tx)
			if err != nil {
				return err
			}
			timeStampBucket, err := getExitCodeTimeStampBucket(tx)
			if err != nil {
				return err
			}

			var finalErr error
			for _, id := range toRemoveIDs {
				rawID := []byte(id)
				if err := exitCodeBucket.Delete(rawID); err != nil {
					if finalErr != nil {
						logrus.Error(finalErr)
					}
					finalErr = fmt.Errorf("removing exit code of container %s from DB: %w", id, err)
				}
				if err := timeStampBucket.Delete(rawID); err != nil {
					if finalErr != nil {
						logrus.Error(finalErr)
					}
					finalErr = fmt.Errorf("removing exit code timestamp of container %s from DB: %w", id, err)
				}
			}

			return finalErr
		})
		if err != nil {
			return fmt.Errorf("pruning exit codes: %w", err)
		}
	}

	return nil
}

// AddExecSession adds an exec session to the state.
func (s *BoltState) AddExecSession(ctr *Container, session *ExecSession) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	ctrID := []byte(ctr.ID())
	sessionID := []byte(session.ID())

	err = db.Update(func(tx *bolt.Tx) error {
		execBucket, err := getExecBucket(tx)
		if err != nil {
			return err
		}
		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		dbCtr := ctrBucket.Bucket(ctrID)
		if dbCtr == nil {
			ctr.valid = false
			return fmt.Errorf("container %s is not present in the database: %w", ctr.ID(), define.ErrNoSuchCtr)
		}

		ctrExecSessionBucket, err := dbCtr.CreateBucketIfNotExists(execBkt)
		if err != nil {
			return fmt.Errorf("creating exec sessions bucket for container %s: %w", ctr.ID(), err)
		}

		execExists := execBucket.Get(sessionID)
		if execExists != nil {
			return fmt.Errorf("an exec session with ID %s already exists: %w", session.ID(), define.ErrExecSessionExists)
		}

		if err := execBucket.Put(sessionID, ctrID); err != nil {
			return fmt.Errorf("adding exec session %s to DB: %w", session.ID(), err)
		}

		if err := ctrExecSessionBucket.Put(sessionID, ctrID); err != nil {
			return fmt.Errorf("adding exec session %s to container %s in DB: %w", session.ID(), ctr.ID(), err)
		}

		return nil
	})
	return err
}

// GetExecSession returns the ID of the container an exec session is associated
// with.
func (s *BoltState) GetExecSession(id string) (string, error) {
	if !s.valid {
		return "", define.ErrDBClosed
	}

	if id == "" {
		return "", define.ErrEmptyID
	}

	db, err := s.getDBCon()
	if err != nil {
		return "", err
	}
	defer s.deferredCloseDBCon(db)

	ctrID := ""
	err = db.View(func(tx *bolt.Tx) error {
		execBucket, err := getExecBucket(tx)
		if err != nil {
			return err
		}

		ctr := execBucket.Get([]byte(id))
		if ctr == nil {
			return fmt.Errorf("no exec session with ID %s found: %w", id, define.ErrNoSuchExecSession)
		}
		ctrID = string(ctr)
		return nil
	})
	return ctrID, err
}

// RemoveExecSession removes references to the given exec session in the
// database.
func (s *BoltState) RemoveExecSession(session *ExecSession) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	sessionID := []byte(session.ID())
	containerID := []byte(session.ContainerID())
	err = db.Update(func(tx *bolt.Tx) error {
		execBucket, err := getExecBucket(tx)
		if err != nil {
			return err
		}
		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		sessionExists := execBucket.Get(sessionID)
		if sessionExists == nil {
			return define.ErrNoSuchExecSession
		}
		// Check that container ID matches
		if string(sessionExists) != session.ContainerID() {
			return fmt.Errorf("database inconsistency: exec session %s points to container %s in state but %s in database: %w", session.ID(), session.ContainerID(), string(sessionExists), define.ErrInternal)
		}

		if err := execBucket.Delete(sessionID); err != nil {
			return fmt.Errorf("removing exec session %s from database: %w", session.ID(), err)
		}

		dbCtr := ctrBucket.Bucket(containerID)
		if dbCtr == nil {
			// State is inconsistent. We refer to a container that
			// is no longer in the state.
			// Return without error, to attempt to recover.
			return nil
		}

		ctrExecBucket := dbCtr.Bucket(execBkt)
		if ctrExecBucket == nil {
			// Again, state is inconsistent. We should have an exec
			// bucket, and it should have this session.
			// Again, nothing we can do, so proceed and try to
			// recover.
			return nil
		}

		ctrSessionExists := ctrExecBucket.Get(sessionID)
		if ctrSessionExists != nil {
			if err := ctrExecBucket.Delete(sessionID); err != nil {
				return fmt.Errorf("removing exec session %s from container %s in database: %w", session.ID(), session.ContainerID(), err)
			}
		}

		return nil
	})
	return err
}

// GetContainerExecSessions retrieves the IDs of all exec sessions running in a
// container that the database is aware of (IE, were added via AddExecSession).
func (s *BoltState) GetContainerExecSessions(ctr *Container) ([]string, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	if !ctr.valid {
		return nil, define.ErrCtrRemoved
	}

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	ctrID := []byte(ctr.ID())
	sessions := []string{}
	err = db.View(func(tx *bolt.Tx) error {
		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		dbCtr := ctrBucket.Bucket(ctrID)
		if dbCtr == nil {
			ctr.valid = false
			return define.ErrNoSuchCtr
		}

		ctrExecSessions := dbCtr.Bucket(execBkt)
		if ctrExecSessions == nil {
			return nil
		}

		return ctrExecSessions.ForEach(func(id, _ []byte) error {
			sessions = append(sessions, string(id))
			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	return sessions, nil
}

// RemoveContainerExecSessions removes all exec sessions attached to a given
// container.
func (s *BoltState) RemoveContainerExecSessions(ctr *Container) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	ctrID := []byte(ctr.ID())
	sessions := []string{}

	err = db.Update(func(tx *bolt.Tx) error {
		execBucket, err := getExecBucket(tx)
		if err != nil {
			return err
		}
		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		dbCtr := ctrBucket.Bucket(ctrID)
		if dbCtr == nil {
			ctr.valid = false
			return define.ErrNoSuchCtr
		}

		ctrExecSessions := dbCtr.Bucket(execBkt)
		if ctrExecSessions == nil {
			return nil
		}

		err = ctrExecSessions.ForEach(func(id, _ []byte) error {
			sessions = append(sessions, string(id))
			return nil
		})
		if err != nil {
			return err
		}

		for _, session := range sessions {
			if err := ctrExecSessions.Delete([]byte(session)); err != nil {
				return fmt.Errorf("removing container %s exec session %s from database: %w", ctr.ID(), session, err)
			}
			// Check if the session exists in the global table
			// before removing. It should, but in cases where the DB
			// has become inconsistent, we should try and proceed
			// so we can recover.
			sessionExists := execBucket.Get([]byte(session))
			if sessionExists == nil {
				continue
			}
			if string(sessionExists) != ctr.ID() {
				return fmt.Errorf("database mismatch: exec session %s is associated with containers %s and %s: %w", session, ctr.ID(), string(sessionExists), define.ErrInternal)
			}
			if err := execBucket.Delete([]byte(session)); err != nil {
				return fmt.Errorf("removing container %s exec session %s from exec sessions: %w", ctr.ID(), session, err)
			}
		}

		return nil
	})
	return err
}

// RewriteContainerConfig rewrites a container's configuration.
// WARNING: This function is DANGEROUS. Do not use without reading the full
// comment on this function in state.go.
func (s *BoltState) RewriteContainerConfig(ctr *Container, newCfg *ContainerConfig) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !ctr.valid {
		return define.ErrCtrRemoved
	}

	newCfgJSON, err := json.Marshal(newCfg)
	if err != nil {
		return fmt.Errorf("marshalling new configuration JSON for container %s: %w", ctr.ID(), err)
	}

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	err = db.Update(func(tx *bolt.Tx) error {
		ctrBkt, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		ctrDB := ctrBkt.Bucket([]byte(ctr.ID()))
		if ctrDB == nil {
			ctr.valid = false
			return fmt.Errorf("no container with ID %q found in DB: %w", ctr.ID(), define.ErrNoSuchCtr)
		}

		if err := ctrDB.Put(configKey, newCfgJSON); err != nil {
			return fmt.Errorf("updating container %s config JSON: %w", ctr.ID(), err)
		}

		return nil
	})
	return err
}

// SafeRewriteContainerConfig rewrites a container's configuration in a more
// limited fashion than RewriteContainerConfig. It is marked as safe to use
// under most circumstances, unlike RewriteContainerConfig.
// DO NOT USE TO: Change container dependencies, change pod membership, change
// locks, change container ID.
func (s *BoltState) SafeRewriteContainerConfig(ctr *Container, oldName, newName string, newCfg *ContainerConfig) error {
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

	newCfgJSON, err := json.Marshal(newCfg)
	if err != nil {
		return fmt.Errorf("marshalling new configuration JSON for container %s: %w", ctr.ID(), err)
	}

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	err = db.Update(func(tx *bolt.Tx) error {
		if newName != "" {
			idBkt, err := getIDBucket(tx)
			if err != nil {
				return err
			}
			namesBkt, err := getNamesBucket(tx)
			if err != nil {
				return err
			}
			allCtrsBkt, err := getAllCtrsBucket(tx)
			if err != nil {
				return err
			}

			needsRename := true
			if exists := namesBkt.Get([]byte(newName)); exists != nil {
				if string(exists) == ctr.ID() {
					// Name already associated with the ID
					// of this container. No need for a
					// rename.
					needsRename = false
				} else {
					return fmt.Errorf("name %s already in use, cannot rename container %s: %w", newName, ctr.ID(), define.ErrCtrExists)
				}
			}

			if needsRename {
				// We do have to remove the old name. The other
				// buckets are ID-indexed so we just need to
				// overwrite the values there.
				if err := namesBkt.Delete([]byte(oldName)); err != nil {
					return fmt.Errorf("deleting container %s old name from DB for rename: %w", ctr.ID(), err)
				}
				if err := idBkt.Put([]byte(ctr.ID()), []byte(newName)); err != nil {
					return fmt.Errorf("renaming container %s in ID bucket in DB: %w", ctr.ID(), err)
				}
				if err := namesBkt.Put([]byte(newName), []byte(ctr.ID())); err != nil {
					return fmt.Errorf("adding new name %s for container %s in DB: %w", newName, ctr.ID(), err)
				}
				if err := allCtrsBkt.Put([]byte(ctr.ID()), []byte(newName)); err != nil {
					return fmt.Errorf("renaming container %s in all containers bucket in DB: %w", ctr.ID(), err)
				}
				if ctr.config.Pod != "" {
					podsBkt, err := getPodBucket(tx)
					if err != nil {
						return err
					}
					podBkt := podsBkt.Bucket([]byte(ctr.config.Pod))
					if podBkt == nil {
						return fmt.Errorf("bucket for pod %s does not exist: %w", ctr.config.Pod, define.ErrInternal)
					}
					podCtrBkt := podBkt.Bucket(containersBkt)
					if podCtrBkt == nil {
						return fmt.Errorf("pod %s does not have a containers bucket: %w", ctr.config.Pod, define.ErrInternal)
					}
					if err := podCtrBkt.Put([]byte(ctr.ID()), []byte(newName)); err != nil {
						return fmt.Errorf("renaming container %s in pod %s members bucket: %w", ctr.ID(), ctr.config.Pod, err)
					}
				}
			}
		}

		ctrBkt, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		ctrDB := ctrBkt.Bucket([]byte(ctr.ID()))
		if ctrDB == nil {
			ctr.valid = false
			return fmt.Errorf("no container with ID %q found in DB: %w", ctr.ID(), define.ErrNoSuchCtr)
		}

		if err := ctrDB.Put(configKey, newCfgJSON); err != nil {
			return fmt.Errorf("updating container %s config JSON: %w", ctr.ID(), err)
		}

		return nil
	})
	return err
}

// RewritePodConfig rewrites a pod's configuration.
// WARNING: This function is DANGEROUS. Do not use without reading the full
// comment on this function in state.go.
func (s *BoltState) RewritePodConfig(pod *Pod, newCfg *PodConfig) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !pod.valid {
		return define.ErrPodRemoved
	}

	newCfgJSON, err := json.Marshal(newCfg)
	if err != nil {
		return fmt.Errorf("marshalling new configuration JSON for pod %s: %w", pod.ID(), err)
	}

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	err = db.Update(func(tx *bolt.Tx) error {
		podBkt, err := getPodBucket(tx)
		if err != nil {
			return err
		}

		podDB := podBkt.Bucket([]byte(pod.ID()))
		if podDB == nil {
			pod.valid = false
			return fmt.Errorf("no pod with ID %s found in DB: %w", pod.ID(), define.ErrNoSuchPod)
		}

		if err := podDB.Put(configKey, newCfgJSON); err != nil {
			return fmt.Errorf("updating pod %s config JSON: %w", pod.ID(), err)
		}

		return nil
	})
	return err
}

// RewriteVolumeConfig rewrites a volume's configuration.
// WARNING: This function is DANGEROUS. Do not use without reading the full
// comment on this function in state.go.
func (s *BoltState) RewriteVolumeConfig(volume *Volume, newCfg *VolumeConfig) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !volume.valid {
		return define.ErrVolumeRemoved
	}

	newCfgJSON, err := json.Marshal(newCfg)
	if err != nil {
		return fmt.Errorf("marshalling new configuration JSON for volume %q: %w", volume.Name(), err)
	}

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	err = db.Update(func(tx *bolt.Tx) error {
		volBkt, err := getVolBucket(tx)
		if err != nil {
			return err
		}

		volDB := volBkt.Bucket([]byte(volume.Name()))
		if volDB == nil {
			volume.valid = false
			return fmt.Errorf("no volume with name %q found in DB: %w", volume.Name(), define.ErrNoSuchVolume)
		}

		if err := volDB.Put(configKey, newCfgJSON); err != nil {
			return fmt.Errorf("updating volume %q config JSON: %w", volume.Name(), err)
		}

		return nil
	})
	return err
}

// Pod retrieves a pod given its full ID
func (s *BoltState) Pod(id string) (*Pod, error) {
	if id == "" {
		return nil, define.ErrEmptyID
	}

	if !s.valid {
		return nil, define.ErrDBClosed
	}

	podID := []byte(id)

	pod := new(Pod)
	pod.config = new(PodConfig)
	pod.state = new(podState)

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		podBkt, err := getPodBucket(tx)
		if err != nil {
			return err
		}

		return s.getPodFromDB(podID, pod, podBkt)
	})
	if err != nil {
		return nil, err
	}

	return pod, nil
}

// LookupPod retrieves a pod from full or unique partial ID or name
func (s *BoltState) LookupPod(idOrName string) (*Pod, error) {
	if idOrName == "" {
		return nil, define.ErrEmptyID
	}

	if !s.valid {
		return nil, define.ErrDBClosed
	}

	pod := new(Pod)
	pod.config = new(PodConfig)
	pod.state = new(podState)

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		podBkt, err := getPodBucket(tx)
		if err != nil {
			return err
		}

		namesBkt, err := getNamesBucket(tx)
		if err != nil {
			return err
		}

		// First, check if the ID given was the actual pod ID
		var id []byte
		podExists := podBkt.Bucket([]byte(idOrName))
		if podExists != nil {
			// A full pod ID was given.
			id = []byte(idOrName)
			return s.getPodFromDB(id, pod, podBkt)
		}

		// Next, check if the full name was given
		isCtr := false
		fullID := namesBkt.Get([]byte(idOrName))
		if fullID != nil {
			// The name exists and maps to an ID.
			// However, we aren't yet sure if the ID is a pod.
			podExists = podBkt.Bucket(fullID)
			if podExists != nil {
				// A pod bucket matching the full ID was found.
				return s.getPodFromDB(fullID, pod, podBkt)
			}
			// Don't error if we have a name match but it's not a
			// pod - there's a chance we have a pod with an ID
			// starting with those characters.
			// However, so we can return a good error, note whether
			// this is a container.
			isCtr = true
		}
		// They did not give us a full pod name or ID.
		// Search for partial ID matches.
		exists := false
		err = podBkt.ForEach(func(checkID, _ []byte) error {
			if strings.HasPrefix(string(checkID), idOrName) {
				if exists {
					return fmt.Errorf("more than one result for ID or name %s: %w", idOrName, define.ErrPodExists)
				}
				id = checkID
				exists = true
			}

			return nil
		})
		if err != nil {
			return err
		} else if !exists {
			if isCtr {
				return fmt.Errorf("%s is a container, not a pod: %w", idOrName, define.ErrNoSuchPod)
			}
			return fmt.Errorf("no pod with name or ID %s found: %w", idOrName, define.ErrNoSuchPod)
		}

		// We might have found a container ID, but it's OK
		// We'll just fail in getPodFromDB with ErrNoSuchPod
		return s.getPodFromDB(id, pod, podBkt)
	})
	if err != nil {
		return nil, err
	}

	return pod, nil
}

// HasPod checks if a pod with the given ID exists in the state
func (s *BoltState) HasPod(id string) (bool, error) {
	if id == "" {
		return false, define.ErrEmptyID
	}

	if !s.valid {
		return false, define.ErrDBClosed
	}

	podID := []byte(id)

	exists := false

	db, err := s.getDBCon()
	if err != nil {
		return false, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		podBkt, err := getPodBucket(tx)
		if err != nil {
			return err
		}

		podDB := podBkt.Bucket(podID)
		if podDB != nil {
			exists = true
		}

		return nil
	})
	if err != nil {
		return false, err
	}

	return exists, nil
}

// PodHasContainer checks if the given pod has a container with the given ID
func (s *BoltState) PodHasContainer(pod *Pod, id string) (bool, error) {
	if id == "" {
		return false, define.ErrEmptyID
	}

	if !s.valid {
		return false, define.ErrDBClosed
	}

	if !pod.valid {
		return false, define.ErrPodRemoved
	}

	ctrID := []byte(id)
	podID := []byte(pod.ID())

	exists := false

	db, err := s.getDBCon()
	if err != nil {
		return false, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		podBkt, err := getPodBucket(tx)
		if err != nil {
			return err
		}

		// Get pod itself
		podDB := podBkt.Bucket(podID)
		if podDB == nil {
			pod.valid = false
			return fmt.Errorf("pod %s not found in database: %w", pod.ID(), define.ErrNoSuchPod)
		}

		// Get pod containers bucket
		podCtrs := podDB.Bucket(containersBkt)
		if podCtrs == nil {
			return fmt.Errorf("pod %s missing containers bucket in DB: %w", pod.ID(), define.ErrInternal)
		}

		ctr := podCtrs.Get(ctrID)
		if ctr != nil {
			exists = true
		}

		return nil
	})
	if err != nil {
		return false, err
	}

	return exists, nil
}

// PodContainersByID returns the IDs of all containers present in the given pod
func (s *BoltState) PodContainersByID(pod *Pod) ([]string, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	if !pod.valid {
		return nil, define.ErrPodRemoved
	}

	podID := []byte(pod.ID())

	ctrs := []string{}

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		podBkt, err := getPodBucket(tx)
		if err != nil {
			return err
		}

		// Get pod itself
		podDB := podBkt.Bucket(podID)
		if podDB == nil {
			pod.valid = false
			return fmt.Errorf("pod %s not found in database: %w", pod.ID(), define.ErrNoSuchPod)
		}

		// Get pod containers bucket
		podCtrs := podDB.Bucket(containersBkt)
		if podCtrs == nil {
			return fmt.Errorf("pod %s missing containers bucket in DB: %w", pod.ID(), define.ErrInternal)
		}

		// Iterate through all containers in the pod
		err = podCtrs.ForEach(func(id, _ []byte) error {
			ctrs = append(ctrs, string(id))

			return nil
		})
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return ctrs, nil
}

// PodContainers returns all the containers present in the given pod
func (s *BoltState) PodContainers(pod *Pod) ([]*Container, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	if !pod.valid {
		return nil, define.ErrPodRemoved
	}

	podID := []byte(pod.ID())

	ctrs := []*Container{}

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		podBkt, err := getPodBucket(tx)
		if err != nil {
			return err
		}

		ctrBkt, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		// Get pod itself
		podDB := podBkt.Bucket(podID)
		if podDB == nil {
			pod.valid = false
			return fmt.Errorf("pod %s not found in database: %w", pod.ID(), define.ErrNoSuchPod)
		}

		// Get pod containers bucket
		podCtrs := podDB.Bucket(containersBkt)
		if podCtrs == nil {
			return fmt.Errorf("pod %s missing containers bucket in DB: %w", pod.ID(), define.ErrInternal)
		}

		// Iterate through all containers in the pod
		err = podCtrs.ForEach(func(id, _ []byte) error {
			newCtr := new(Container)
			newCtr.config = new(ContainerConfig)
			newCtr.state = new(ContainerState)
			ctrs = append(ctrs, newCtr)

			return s.getContainerFromDB(id, newCtr, ctrBkt, false)
		})
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return ctrs, nil
}

// AddVolume adds the given volume to the state. It also adds ctrDepID to
// the sub bucket holding the container dependencies that this volume has
func (s *BoltState) AddVolume(volume *Volume) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !volume.valid {
		return define.ErrVolumeRemoved
	}

	volName := []byte(volume.Name())

	volConfigJSON, err := json.Marshal(volume.config)
	if err != nil {
		return fmt.Errorf("marshalling volume %s config to JSON: %w", volume.Name(), err)
	}

	// Volume state is allowed to not exist
	var volStateJSON []byte
	if volume.state != nil {
		volStateJSON, err = json.Marshal(volume.state)
		if err != nil {
			return fmt.Errorf("marshalling volume %s state to JSON: %w", volume.Name(), err)
		}
	}

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	err = db.Update(func(tx *bolt.Tx) error {
		volBkt, err := getVolBucket(tx)
		if err != nil {
			return err
		}

		allVolsBkt, err := getAllVolsBucket(tx)
		if err != nil {
			return err
		}

		volCtrsBkt, err := getVolumeContainersBucket(tx)
		if err != nil {
			return err
		}

		// Check if we already have a volume with the given name
		volExists := allVolsBkt.Get(volName)
		if volExists != nil {
			return fmt.Errorf("name %s is in use: %w", volume.Name(), define.ErrVolumeExists)
		}

		// We are good to add the volume
		// Make a bucket for it
		newVol, err := volBkt.CreateBucket(volName)
		if err != nil {
			return fmt.Errorf("creating bucket for volume %s: %w", volume.Name(), err)
		}

		// Make a subbucket for the containers using the volume. Dependent container IDs will be addedremoved to
		// this bucket in addcontainer/removeContainer
		if _, err := newVol.CreateBucket(volDependenciesBkt); err != nil {
			return fmt.Errorf("creating bucket for containers using volume %s: %w", volume.Name(), err)
		}

		if err := newVol.Put(configKey, volConfigJSON); err != nil {
			return fmt.Errorf("storing volume %s configuration in DB: %w", volume.Name(), err)
		}

		if volStateJSON != nil {
			if err := newVol.Put(stateKey, volStateJSON); err != nil {
				return fmt.Errorf("storing volume %s state in DB: %w", volume.Name(), err)
			}
		}

		if volume.config.StorageID != "" {
			if err := volCtrsBkt.Put([]byte(volume.config.StorageID), volName); err != nil {
				return fmt.Errorf("storing volume %s container ID in DB: %w", volume.Name(), err)
			}
		}

		if err := allVolsBkt.Put(volName, volName); err != nil {
			return fmt.Errorf("storing volume %s in all volumes bucket in DB: %w", volume.Name(), err)
		}

		return nil
	})
	return err
}

// RemoveVolume removes the given volume from the state
func (s *BoltState) RemoveVolume(volume *Volume) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	volName := []byte(volume.Name())

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	err = db.Update(func(tx *bolt.Tx) error {
		volBkt, err := getVolBucket(tx)
		if err != nil {
			return err
		}

		allVolsBkt, err := getAllVolsBucket(tx)
		if err != nil {
			return err
		}

		ctrBkt, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		volCtrIDBkt, err := getVolumeContainersBucket(tx)
		if err != nil {
			return err
		}

		// Check if the volume exists
		volDB := volBkt.Bucket(volName)
		if volDB == nil {
			volume.valid = false
			return fmt.Errorf("volume %s does not exist in DB: %w", volume.Name(), define.ErrNoSuchVolume)
		}

		// Check if volume is not being used by any container
		// This should never be nil
		// But if it is, we can assume that no containers are using
		// the volume.
		volCtrsBkt := volDB.Bucket(volDependenciesBkt)
		if volCtrsBkt != nil {
			var deps []string
			err = volCtrsBkt.ForEach(func(id, _ []byte) error {
				// Alright, this is ugly.
				// But we need it to work around the change in
				// volume dependency handling, to make sure that
				// older Podman versions don't cause DB
				// corruption.
				// Look up all dependencies and see that they
				// still exist before appending.
				ctrExists := ctrBkt.Bucket(id)
				if ctrExists == nil {
					return nil
				}

				deps = append(deps, string(id))
				return nil
			})
			if err != nil {
				return fmt.Errorf("getting list of dependencies from dependencies bucket for volumes %q: %w", volume.Name(), err)
			}
			if len(deps) > 0 {
				return fmt.Errorf("volume %s is being used by container(s) %s: %w", volume.Name(), strings.Join(deps, ","), define.ErrVolumeBeingUsed)
			}
		}

		// volume is ready for removal
		// Let's kick it out
		if err := allVolsBkt.Delete(volName); err != nil {
			return fmt.Errorf("removing volume %s from all volumes bucket in DB: %w", volume.Name(), err)
		}
		if err := volBkt.DeleteBucket(volName); err != nil {
			return fmt.Errorf("removing volume %s from DB: %w", volume.Name(), err)
		}
		if volume.config.StorageID != "" {
			if err := volCtrIDBkt.Delete([]byte(volume.config.StorageID)); err != nil {
				return fmt.Errorf("removing volume %s container ID from DB: %w", volume.Name(), err)
			}
		}

		return nil
	})
	return err
}

// UpdateVolume updates the volume's state from the database.
func (s *BoltState) UpdateVolume(volume *Volume) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !volume.valid {
		return define.ErrVolumeRemoved
	}

	newState := new(VolumeState)
	volumeName := []byte(volume.Name())

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		volBucket, err := getVolBucket(tx)
		if err != nil {
			return err
		}

		volToUpdate := volBucket.Bucket(volumeName)
		if volToUpdate == nil {
			volume.valid = false
			return fmt.Errorf("no volume with name %s found in database: %w", volume.Name(), define.ErrNoSuchVolume)
		}

		stateBytes := volToUpdate.Get(stateKey)
		if stateBytes == nil {
			// Having no state is valid.
			// Return nil, use the empty state.
			return nil
		}

		if err := json.Unmarshal(stateBytes, newState); err != nil {
			return fmt.Errorf("unmarshalling volume %s state: %w", volume.Name(), err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	volume.state = newState

	return nil
}

// SaveVolume saves the volume's state to the database.
func (s *BoltState) SaveVolume(volume *Volume) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !volume.valid {
		return define.ErrVolumeRemoved
	}

	volumeName := []byte(volume.Name())

	var newStateJSON []byte
	if volume.state != nil {
		stateJSON, err := json.Marshal(volume.state)
		if err != nil {
			return fmt.Errorf("marshalling volume %s state to JSON: %w", volume.Name(), err)
		}
		newStateJSON = stateJSON
	}

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	err = db.Update(func(tx *bolt.Tx) error {
		volBucket, err := getVolBucket(tx)
		if err != nil {
			return err
		}

		volToUpdate := volBucket.Bucket(volumeName)
		if volToUpdate == nil {
			volume.valid = false
			return fmt.Errorf("no volume with name %s found in database: %w", volume.Name(), define.ErrNoSuchVolume)
		}

		return volToUpdate.Put(stateKey, newStateJSON)
	})
	return err
}

// AllVolumes returns all volumes present in the state
func (s *BoltState) AllVolumes() ([]*Volume, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	volumes := []*Volume{}

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		allVolsBucket, err := getAllVolsBucket(tx)
		if err != nil {
			return err
		}

		volBucket, err := getVolBucket(tx)
		if err != nil {
			return err
		}
		err = allVolsBucket.ForEach(func(id, _ []byte) error {
			volExists := volBucket.Bucket(id)
			// This check can be removed if performance becomes an
			// issue, but much less helpful errors will be produced
			if volExists == nil {
				return fmt.Errorf("inconsistency in state - volume %s is in all volumes bucket but volume not found: %w", string(id), define.ErrInternal)
			}

			volume := new(Volume)
			volume.config = new(VolumeConfig)
			volume.state = new(VolumeState)

			if err := s.getVolumeFromDB(id, volume, volBucket); err != nil {
				if !errors.Is(err, define.ErrNSMismatch) {
					logrus.Errorf("Retrieving volume %s from the database: %v", string(id), err)
				}
			} else {
				volumes = append(volumes, volume)
			}

			return nil
		})
		return err
	})
	if err != nil {
		return nil, err
	}

	return volumes, nil
}

// Volume retrieves a volume from full name
func (s *BoltState) Volume(name string) (*Volume, error) {
	if name == "" {
		return nil, define.ErrEmptyID
	}

	if !s.valid {
		return nil, define.ErrDBClosed
	}

	volName := []byte(name)

	volume := new(Volume)
	volume.config = new(VolumeConfig)
	volume.state = new(VolumeState)

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		volBkt, err := getVolBucket(tx)
		if err != nil {
			return err
		}

		return s.getVolumeFromDB(volName, volume, volBkt)
	})
	if err != nil {
		return nil, err
	}

	return volume, nil
}

// LookupVolume locates a volume from a partial name.
func (s *BoltState) LookupVolume(name string) (*Volume, error) {
	if name == "" {
		return nil, define.ErrEmptyID
	}

	if !s.valid {
		return nil, define.ErrDBClosed
	}

	volName := []byte(name)

	volume := new(Volume)
	volume.config = new(VolumeConfig)
	volume.state = new(VolumeState)

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		volBkt, err := getVolBucket(tx)
		if err != nil {
			return err
		}

		allVolsBkt, err := getAllVolsBucket(tx)
		if err != nil {
			return err
		}

		// Check for exact match on name
		volDB := volBkt.Bucket(volName)
		if volDB != nil {
			return s.getVolumeFromDB(volName, volume, volBkt)
		}

		// No exact match. Search all names.
		foundMatch := false
		err = allVolsBkt.ForEach(func(checkName, _ []byte) error {
			if strings.HasPrefix(string(checkName), name) {
				if foundMatch {
					return fmt.Errorf("more than one result for volume name %q: %w", name, define.ErrVolumeExists)
				}
				foundMatch = true
				volName = checkName
			}
			return nil
		})
		if err != nil {
			return err
		}

		if !foundMatch {
			return fmt.Errorf("no volume with name %q found: %w", name, define.ErrNoSuchVolume)
		}

		return s.getVolumeFromDB(volName, volume, volBkt)
	})
	if err != nil {
		return nil, err
	}

	return volume, nil
}

// HasVolume returns true if the given volume exists in the state, otherwise it returns false
func (s *BoltState) HasVolume(name string) (bool, error) {
	if name == "" {
		return false, define.ErrEmptyID
	}

	if !s.valid {
		return false, define.ErrDBClosed
	}

	volName := []byte(name)

	exists := false

	db, err := s.getDBCon()
	if err != nil {
		return false, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		volBkt, err := getVolBucket(tx)
		if err != nil {
			return err
		}

		volDB := volBkt.Bucket(volName)
		if volDB != nil {
			exists = true
		}

		return nil
	})
	if err != nil {
		return false, err
	}

	return exists, nil
}

// VolumeInUse checks if any container is using the volume
// It returns a slice of the IDs of the containers using the given
// volume. If the slice is empty, no containers use the given volume
func (s *BoltState) VolumeInUse(volume *Volume) ([]string, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	if !volume.valid {
		return nil, define.ErrVolumeRemoved
	}

	depCtrs := []string{}

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		volBucket, err := getVolBucket(tx)
		if err != nil {
			return err
		}

		ctrBucket, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		volDB := volBucket.Bucket([]byte(volume.Name()))
		if volDB == nil {
			volume.valid = false
			return fmt.Errorf("no volume with name %s found in DB: %w", volume.Name(), define.ErrNoSuchVolume)
		}

		dependsBkt := volDB.Bucket(volDependenciesBkt)
		if dependsBkt == nil {
			return fmt.Errorf("volume %s has no dependencies bucket: %w", volume.Name(), define.ErrInternal)
		}

		// Iterate through and add dependencies
		err = dependsBkt.ForEach(func(id, _ []byte) error {
			// Look up all dependencies and see that they
			// still exist before appending.
			ctrExists := ctrBucket.Bucket(id)
			if ctrExists == nil {
				return nil
			}

			depCtrs = append(depCtrs, string(id))

			return nil
		})
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return depCtrs, nil
}

// AddPod adds the given pod to the state.
func (s *BoltState) AddPod(pod *Pod) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !pod.valid {
		return define.ErrPodRemoved
	}

	podID := []byte(pod.ID())
	podName := []byte(pod.Name())

	podConfigJSON, err := json.Marshal(pod.config)
	if err != nil {
		return fmt.Errorf("marshalling pod %s config to JSON: %w", pod.ID(), err)
	}

	podStateJSON, err := json.Marshal(pod.state)
	if err != nil {
		return fmt.Errorf("marshalling pod %s state to JSON: %w", pod.ID(), err)
	}

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	err = db.Update(func(tx *bolt.Tx) error {
		podBkt, err := getPodBucket(tx)
		if err != nil {
			return err
		}

		allPodsBkt, err := getAllPodsBucket(tx)
		if err != nil {
			return err
		}

		idsBkt, err := getIDBucket(tx)
		if err != nil {
			return err
		}

		namesBkt, err := getNamesBucket(tx)
		if err != nil {
			return err
		}

		// Check if we already have something with the given ID and name
		idExist := idsBkt.Get(podID)
		if idExist != nil {
			err = define.ErrPodExists
			if allPodsBkt.Get(idExist) == nil {
				err = define.ErrCtrExists
			}
			return fmt.Errorf("ID \"%s\" is in use: %w", pod.ID(), err)
		}
		nameExist := namesBkt.Get(podName)
		if nameExist != nil {
			err = define.ErrPodExists
			if allPodsBkt.Get(nameExist) == nil {
				err = define.ErrCtrExists
			}
			return fmt.Errorf("name \"%s\" is in use: %w", pod.Name(), err)
		}

		// We are good to add the pod
		// Make a bucket for it
		newPod, err := podBkt.CreateBucket(podID)
		if err != nil {
			return fmt.Errorf("creating bucket for pod %s: %w", pod.ID(), err)
		}

		// Make a subbucket for pod containers
		if _, err := newPod.CreateBucket(containersBkt); err != nil {
			return fmt.Errorf("creating bucket for pod %s containers: %w", pod.ID(), err)
		}

		if err := newPod.Put(configKey, podConfigJSON); err != nil {
			return fmt.Errorf("storing pod %s configuration in DB: %w", pod.ID(), err)
		}

		if err := newPod.Put(stateKey, podStateJSON); err != nil {
			return fmt.Errorf("storing pod %s state JSON in DB: %w", pod.ID(), err)
		}

		// Add us to the ID and names buckets
		if err := idsBkt.Put(podID, podName); err != nil {
			return fmt.Errorf("storing pod %s ID in DB: %w", pod.ID(), err)
		}
		if err := namesBkt.Put(podName, podID); err != nil {
			return fmt.Errorf("storing pod %s name in DB: %w", pod.Name(), err)
		}
		if err := allPodsBkt.Put(podID, podName); err != nil {
			return fmt.Errorf("storing pod %s in all pods bucket in DB: %w", pod.ID(), err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

// RemovePod removes the given pod from the state
// Only empty pods can be removed
func (s *BoltState) RemovePod(pod *Pod) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !pod.valid {
		return define.ErrPodRemoved
	}

	podID := []byte(pod.ID())
	podName := []byte(pod.Name())

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	err = db.Update(func(tx *bolt.Tx) error {
		podBkt, err := getPodBucket(tx)
		if err != nil {
			return err
		}

		allPodsBkt, err := getAllPodsBucket(tx)
		if err != nil {
			return err
		}

		idsBkt, err := getIDBucket(tx)
		if err != nil {
			return err
		}

		namesBkt, err := getNamesBucket(tx)
		if err != nil {
			return err
		}

		// Check if the pod exists
		podDB := podBkt.Bucket(podID)
		if podDB == nil {
			pod.valid = false
			return fmt.Errorf("pod %s does not exist in DB: %w", pod.ID(), define.ErrNoSuchPod)
		}

		// Check if pod is empty
		// This should never be nil
		// But if it is, we can assume there are no containers in the
		// pod.
		// So let's eject the malformed pod without error.
		podCtrsBkt := podDB.Bucket(containersBkt)
		if podCtrsBkt != nil {
			cursor := podCtrsBkt.Cursor()
			if id, _ := cursor.First(); id != nil {
				return fmt.Errorf("pod %s is not empty: %w", pod.ID(), define.ErrCtrExists)
			}
		}

		// Pod is empty, and ready for removal
		// Let's kick it out
		if err := idsBkt.Delete(podID); err != nil {
			return fmt.Errorf("removing pod %s ID from DB: %w", pod.ID(), err)
		}
		if err := namesBkt.Delete(podName); err != nil {
			return fmt.Errorf("removing pod %s name (%s) from DB: %w", pod.ID(), pod.Name(), err)
		}
		if err := allPodsBkt.Delete(podID); err != nil {
			return fmt.Errorf("removing pod %s ID from all pods bucket in DB: %w", pod.ID(), err)
		}
		if err := podBkt.DeleteBucket(podID); err != nil {
			return fmt.Errorf("removing pod %s from DB: %w", pod.ID(), err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

// RemovePodContainers removes all containers in a pod
func (s *BoltState) RemovePodContainers(pod *Pod) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !pod.valid {
		return define.ErrPodRemoved
	}

	podID := []byte(pod.ID())

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	err = db.Update(func(tx *bolt.Tx) error {
		podBkt, err := getPodBucket(tx)
		if err != nil {
			return err
		}

		ctrBkt, err := getCtrBucket(tx)
		if err != nil {
			return err
		}

		allCtrsBkt, err := getAllCtrsBucket(tx)
		if err != nil {
			return err
		}

		idsBkt, err := getIDBucket(tx)
		if err != nil {
			return err
		}

		namesBkt, err := getNamesBucket(tx)
		if err != nil {
			return err
		}

		// Check if the pod exists
		podDB := podBkt.Bucket(podID)
		if podDB == nil {
			pod.valid = false
			return fmt.Errorf("pod %s does not exist in DB: %w", pod.ID(), define.ErrNoSuchPod)
		}

		podCtrsBkt := podDB.Bucket(containersBkt)
		if podCtrsBkt == nil {
			return fmt.Errorf("pod %s does not have a containers bucket: %w", pod.ID(), define.ErrInternal)
		}

		// Traverse all containers in the pod with a cursor
		// for-each has issues with data mutation
		err = podCtrsBkt.ForEach(func(id, name []byte) error {
			// Get the container so we can check dependencies
			ctr := ctrBkt.Bucket(id)
			if ctr == nil {
				// This should never happen
				// State is inconsistent
				return fmt.Errorf("pod %s referenced nonexistent container %s: %w", pod.ID(), string(id), define.ErrNoSuchCtr)
			}
			ctrDeps := ctr.Bucket(dependenciesBkt)
			// This should never be nil, but if it is, we're
			// removing it anyways, so continue if it is
			if ctrDeps != nil {
				err = ctrDeps.ForEach(func(depID, _ []byte) error {
					exists := podCtrsBkt.Get(depID)
					if exists == nil {
						return fmt.Errorf("container %s has dependency %s outside of pod %s: %w", string(id), string(depID), pod.ID(), define.ErrCtrExists)
					}
					return nil
				})
				if err != nil {
					return err
				}
			}

			// Dependencies are set, we're clear to remove

			if err := ctrBkt.DeleteBucket(id); err != nil {
				return fmt.Errorf("deleting container %s from DB: %w", string(id), define.ErrInternal)
			}

			if err := idsBkt.Delete(id); err != nil {
				return fmt.Errorf("deleting container %s ID in DB: %w", string(id), err)
			}

			if err := namesBkt.Delete(name); err != nil {
				return fmt.Errorf("deleting container %s name in DB: %w", string(id), err)
			}

			if err := allCtrsBkt.Delete(id); err != nil {
				return fmt.Errorf("deleting container %s ID from all containers bucket in DB: %w", string(id), err)
			}

			return nil
		})
		if err != nil {
			return err
		}

		// Delete and recreate the bucket to empty it
		if err := podDB.DeleteBucket(containersBkt); err != nil {
			return fmt.Errorf("removing pod %s containers bucket: %w", pod.ID(), err)
		}
		if _, err := podDB.CreateBucket(containersBkt); err != nil {
			return fmt.Errorf("recreating pod %s containers bucket: %w", pod.ID(), err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

// AddContainerToPod adds the given container to an existing pod
// The container will be added to the state and the pod
func (s *BoltState) AddContainerToPod(pod *Pod, ctr *Container) error {
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

	return s.addContainer(ctr, pod)
}

// RemoveContainerFromPod removes a container from an existing pod
// The container will also be removed from the state
func (s *BoltState) RemoveContainerFromPod(pod *Pod, ctr *Container) error {
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

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	err = db.Update(func(tx *bolt.Tx) error {
		return s.removeContainer(ctr, pod, tx)
	})
	return err
}

// UpdatePod updates a pod's state from the database
func (s *BoltState) UpdatePod(pod *Pod) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !pod.valid {
		return define.ErrPodRemoved
	}

	newState := new(podState)

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	podID := []byte(pod.ID())

	err = db.View(func(tx *bolt.Tx) error {
		podBkt, err := getPodBucket(tx)
		if err != nil {
			return err
		}

		podDB := podBkt.Bucket(podID)
		if podDB == nil {
			pod.valid = false
			return fmt.Errorf("no pod with ID %s found in database: %w", pod.ID(), define.ErrNoSuchPod)
		}

		// Get the pod state JSON
		podStateBytes := podDB.Get(stateKey)
		if podStateBytes == nil {
			return fmt.Errorf("pod %s is missing state key in DB: %w", pod.ID(), define.ErrInternal)
		}

		if err := json.Unmarshal(podStateBytes, newState); err != nil {
			return fmt.Errorf("unmarshalling pod %s state JSON: %w", pod.ID(), err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	pod.state = newState

	return nil
}

// SavePod saves a pod's state to the database
func (s *BoltState) SavePod(pod *Pod) error {
	if !s.valid {
		return define.ErrDBClosed
	}

	if !pod.valid {
		return define.ErrPodRemoved
	}

	stateJSON, err := json.Marshal(pod.state)
	if err != nil {
		return fmt.Errorf("marshalling pod %s state to JSON: %w", pod.ID(), err)
	}

	db, err := s.getDBCon()
	if err != nil {
		return err
	}
	defer s.deferredCloseDBCon(db)

	podID := []byte(pod.ID())

	err = db.Update(func(tx *bolt.Tx) error {
		podBkt, err := getPodBucket(tx)
		if err != nil {
			return err
		}

		podDB := podBkt.Bucket(podID)
		if podDB == nil {
			pod.valid = false
			return fmt.Errorf("no pod with ID %s found in database: %w", pod.ID(), define.ErrNoSuchPod)
		}

		// Set the pod state JSON
		if err := podDB.Put(stateKey, stateJSON); err != nil {
			return fmt.Errorf("updating pod %s state in database: %w", pod.ID(), err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

// AllPods returns all pods present in the state
func (s *BoltState) AllPods() ([]*Pod, error) {
	if !s.valid {
		return nil, define.ErrDBClosed
	}

	pods := []*Pod{}

	db, err := s.getDBCon()
	if err != nil {
		return nil, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		allPodsBucket, err := getAllPodsBucket(tx)
		if err != nil {
			return err
		}

		podBucket, err := getPodBucket(tx)
		if err != nil {
			return err
		}

		err = allPodsBucket.ForEach(func(id, _ []byte) error {
			podExists := podBucket.Bucket(id)
			// This check can be removed if performance becomes an
			// issue, but much less helpful errors will be produced
			if podExists == nil {
				return fmt.Errorf("inconsistency in state - pod %s is in all pods bucket but pod not found: %w", string(id), define.ErrInternal)
			}

			pod := new(Pod)
			pod.config = new(PodConfig)
			pod.state = new(podState)

			if err := s.getPodFromDB(id, pod, podBucket); err != nil {
				if !errors.Is(err, define.ErrNSMismatch) {
					logrus.Errorf("Retrieving pod %s from the database: %v", string(id), err)
				}
			} else {
				pods = append(pods, pod)
			}

			return nil
		})
		return err
	})
	if err != nil {
		return nil, err
	}

	return pods, nil
}

// ContainerIDIsVolume checks if the given c/storage container ID is used as
// backing storage for a volume.
func (s *BoltState) ContainerIDIsVolume(id string) (bool, error) {
	if !s.valid {
		return false, define.ErrDBClosed
	}

	isVol := false

	db, err := s.getDBCon()
	if err != nil {
		return false, err
	}
	defer s.deferredCloseDBCon(db)

	err = db.View(func(tx *bolt.Tx) error {
		volCtrsBkt, err := getVolumeContainersBucket(tx)
		if err != nil {
			return err
		}

		volName := volCtrsBkt.Get([]byte(id))
		if volName != nil {
			isVol = true
		}

		return nil
	})
	return isVol, err
}
