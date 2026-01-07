package define

// This contains values reported by CRIU during
// checkpointing or restoring.
// All names are the same as reported by CRIU.
type CRIUCheckpointRestoreStatistics struct {
	// Checkpoint values
	// Time required to freeze/pause/quiesce the processes
	FreezingTime uint32 `json:"freezing_time,omitempty"`
	// Time the processes are actually not running during checkpointing
	FrozenTime uint32 `json:"frozen_time,omitempty"`
	// Time required to extract memory pages from the processes
	MemdumpTime uint32 `json:"memdump_time,omitempty"`
	// Time required to write memory pages to disk
	MemwriteTime uint32 `json:"memwrite_time,omitempty"`
	// Number of memory pages CRIU analyzed
	PagesScanned uint64 `json:"pages_scanned,omitempty"`
	// Number of memory pages written
	PagesWritten uint64 `json:"pages_written,omitempty"`

	// Restore values
	// Number of pages compared during restore
	PagesCompared uint64 `json:"pages_compared,omitempty"`
	// Number of COW pages skipped during restore
	PagesSkippedCow uint64 `json:"pages_skipped_cow,omitempty"`
	// Time required to fork processes
	ForkingTime uint32 `json:"forking_time,omitempty"`
	// Time required to restore
	RestoreTime uint32 `json:"restore_time,omitempty"`
	// Number of memory pages restored
	PagesRestored uint64 `json:"pages_restored,omitempty"`
}
