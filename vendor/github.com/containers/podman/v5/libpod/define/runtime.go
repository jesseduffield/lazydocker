package define

import "time"

// RuntimeStateStore is a constant indicating which state store implementation
// should be used by libpod
type RuntimeStateStore int

const (
	// InvalidStateStore is an invalid state store
	InvalidStateStore RuntimeStateStore = iota
	// InMemoryStateStore is an in-memory state that will not persist data
	// on containers and pods between libpod instances or after system
	// reboot
	InMemoryStateStore RuntimeStateStore = iota
	// SQLiteStateStore is a state backed by a SQLite database
	// It is presently disabled
	SQLiteStateStore RuntimeStateStore = iota
	// BoltDBStateStore is a state backed by a BoltDB database
	BoltDBStateStore RuntimeStateStore = iota
	// ContainerCreateTimeout is the timeout before we decide we've failed
	// to create a container.
	// TODO: Make this generic - all OCI runtime operations should use the
	// same timeout, this one.
	// TODO: Consider dropping from 240 to 60 seconds. I don't think waiting
	// 4 minutes versus 1 minute makes a real difference.
	ContainerCreateTimeout = 240 * time.Second
)
