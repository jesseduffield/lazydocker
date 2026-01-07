package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Stats represents container statistics, returned by /containers/<id>/stats.
//
// See https://goo.gl/Dk3Xio for more details.
type Stats struct {
	Read      time.Time `json:"read,omitempty" yaml:"read,omitempty" toml:"read,omitempty"`
	PreRead   time.Time `json:"preread,omitempty" yaml:"preread,omitempty" toml:"preread,omitempty"`
	NumProcs  uint32    `json:"num_procs" yaml:"num_procs" toml:"num_procs"`
	PidsStats struct {
		Current uint64 `json:"current,omitempty" yaml:"current,omitempty"`
	} `json:"pids_stats,omitempty" yaml:"pids_stats,omitempty" toml:"pids_stats,omitempty"`
	Network     NetworkStats            `json:"network,omitempty" yaml:"network,omitempty" toml:"network,omitempty"`
	Networks    map[string]NetworkStats `json:"networks,omitempty" yaml:"networks,omitempty" toml:"networks,omitempty"`
	MemoryStats struct {
		Stats struct {
			TotalPgmafault          uint64 `json:"total_pgmafault,omitempty" yaml:"total_pgmafault,omitempty" toml:"total_pgmafault,omitempty"`
			Cache                   uint64 `json:"cache,omitempty" yaml:"cache,omitempty" toml:"cache,omitempty"`
			MappedFile              uint64 `json:"mapped_file,omitempty" yaml:"mapped_file,omitempty" toml:"mapped_file,omitempty"`
			TotalInactiveFile       uint64 `json:"total_inactive_file,omitempty" yaml:"total_inactive_file,omitempty" toml:"total_inactive_file,omitempty"`
			Pgpgout                 uint64 `json:"pgpgout,omitempty" yaml:"pgpgout,omitempty" toml:"pgpgout,omitempty"`
			Rss                     uint64 `json:"rss,omitempty" yaml:"rss,omitempty" toml:"rss,omitempty"`
			TotalMappedFile         uint64 `json:"total_mapped_file,omitempty" yaml:"total_mapped_file,omitempty" toml:"total_mapped_file,omitempty"`
			Writeback               uint64 `json:"writeback,omitempty" yaml:"writeback,omitempty" toml:"writeback,omitempty"`
			Unevictable             uint64 `json:"unevictable,omitempty" yaml:"unevictable,omitempty" toml:"unevictable,omitempty"`
			Pgpgin                  uint64 `json:"pgpgin,omitempty" yaml:"pgpgin,omitempty" toml:"pgpgin,omitempty"`
			TotalUnevictable        uint64 `json:"total_unevictable,omitempty" yaml:"total_unevictable,omitempty" toml:"total_unevictable,omitempty"`
			Pgmajfault              uint64 `json:"pgmajfault,omitempty" yaml:"pgmajfault,omitempty" toml:"pgmajfault,omitempty"`
			TotalRss                uint64 `json:"total_rss,omitempty" yaml:"total_rss,omitempty" toml:"total_rss,omitempty"`
			TotalRssHuge            uint64 `json:"total_rss_huge,omitempty" yaml:"total_rss_huge,omitempty" toml:"total_rss_huge,omitempty"`
			TotalWriteback          uint64 `json:"total_writeback,omitempty" yaml:"total_writeback,omitempty" toml:"total_writeback,omitempty"`
			TotalInactiveAnon       uint64 `json:"total_inactive_anon,omitempty" yaml:"total_inactive_anon,omitempty" toml:"total_inactive_anon,omitempty"`
			RssHuge                 uint64 `json:"rss_huge,omitempty" yaml:"rss_huge,omitempty" toml:"rss_huge,omitempty"`
			HierarchicalMemoryLimit uint64 `json:"hierarchical_memory_limit,omitempty" yaml:"hierarchical_memory_limit,omitempty" toml:"hierarchical_memory_limit,omitempty"`
			TotalPgfault            uint64 `json:"total_pgfault,omitempty" yaml:"total_pgfault,omitempty" toml:"total_pgfault,omitempty"`
			TotalActiveFile         uint64 `json:"total_active_file,omitempty" yaml:"total_active_file,omitempty" toml:"total_active_file,omitempty"`
			ActiveAnon              uint64 `json:"active_anon,omitempty" yaml:"active_anon,omitempty" toml:"active_anon,omitempty"`
			TotalActiveAnon         uint64 `json:"total_active_anon,omitempty" yaml:"total_active_anon,omitempty" toml:"total_active_anon,omitempty"`
			TotalPgpgout            uint64 `json:"total_pgpgout,omitempty" yaml:"total_pgpgout,omitempty" toml:"total_pgpgout,omitempty"`
			TotalCache              uint64 `json:"total_cache,omitempty" yaml:"total_cache,omitempty" toml:"total_cache,omitempty"`
			InactiveAnon            uint64 `json:"inactive_anon,omitempty" yaml:"inactive_anon,omitempty" toml:"inactive_anon,omitempty"`
			ActiveFile              uint64 `json:"active_file,omitempty" yaml:"active_file,omitempty" toml:"active_file,omitempty"`
			Pgfault                 uint64 `json:"pgfault,omitempty" yaml:"pgfault,omitempty" toml:"pgfault,omitempty"`
			InactiveFile            uint64 `json:"inactive_file,omitempty" yaml:"inactive_file,omitempty" toml:"inactive_file,omitempty"`
			TotalPgpgin             uint64 `json:"total_pgpgin,omitempty" yaml:"total_pgpgin,omitempty" toml:"total_pgpgin,omitempty"`
			HierarchicalMemswLimit  uint64 `json:"hierarchical_memsw_limit,omitempty" yaml:"hierarchical_memsw_limit,omitempty" toml:"hierarchical_memsw_limit,omitempty"`
			Swap                    uint64 `json:"swap,omitempty" yaml:"swap,omitempty" toml:"swap,omitempty"`
			Anon                    uint64 `json:"anon,omitempty" yaml:"anon,omitempty" toml:"anon,omitempty"`
			AnonThp                 uint64 `json:"anon_thp,omitempty" yaml:"anon_thp,omitempty" toml:"anon_thp,omitempty"`
			File                    uint64 `json:"file,omitempty" yaml:"file,omitempty" toml:"file,omitempty"`
			FileDirty               uint64 `json:"file_dirty,omitempty" yaml:"file_dirty,omitempty" toml:"file_dirty,omitempty"`
			FileMapped              uint64 `json:"file_mapped,omitempty" yaml:"file_mapped,omitempty" toml:"file_mapped,omitempty"`
			FileWriteback           uint64 `json:"file_writeback,omitempty" yaml:"file_writeback,omitempty" toml:"file_writeback,omitempty"`
			KernelStack             uint64 `json:"kernel_stack,omitempty" yaml:"kernel_stack,omitempty" toml:"kernel_stack,omitempty"`
			Pgactivate              uint64 `json:"pgactivate,omitempty" yaml:"pgactivate,omitempty" toml:"pgactivate,omitempty"`
			Pgdeactivate            uint64 `json:"pgdeactivate,omitempty" yaml:"pgdeactivate,omitempty" toml:"pgdeactivate,omitempty"`
			Pglazyfree              uint64 `json:"pglazyfree,omitempty" yaml:"pglazyfree,omitempty" toml:"pglazyfree,omitempty"`
			Pglazyfreed             uint64 `json:"pglazyfreed,omitempty" yaml:"pglazyfreed,omitempty" toml:"pglazyfreed,omitempty"`
			Pgrefill                uint64 `json:"pgrefill,omitempty" yaml:"pgrefill,omitempty" toml:"pgrefill,omitempty"`
			Pgscan                  uint64 `json:"pgscan,omitempty" yaml:"pgscan,omitempty" toml:"pgscan,omitempty"`
			Pgsteal                 uint64 `json:"pgsteal,omitempty" yaml:"pgsteal,omitempty" toml:"pgsteal,omitempty"`
			Shmem                   uint64 `json:"shmem,omitempty" yaml:"shmem,omitempty" toml:"shmem,omitempty"`
			Slab                    uint64 `json:"slab,omitempty" yaml:"slab,omitempty" toml:"slab,omitempty"`
			SlabReclaimable         uint64 `json:"slab_reclaimable,omitempty" yaml:"slab_reclaimable,omitempty" toml:"slab_reclaimable,omitempty"`
			SlabUnreclaimable       uint64 `json:"slab_unreclaimable,omitempty" yaml:"slab_unreclaimable,omitempty" toml:"slab_unreclaimable,omitempty"`
			Sock                    uint64 `json:"sock,omitempty" yaml:"sock,omitempty" toml:"sock,omitempty"`
			ThpCollapseAlloc        uint64 `json:"thp_collapse_alloc,omitempty" yaml:"thp_collapse_alloc,omitempty" toml:"thp_collapse_alloc,omitempty"`
			ThpFaultAlloc           uint64 `json:"thp_fault_alloc,omitempty" yaml:"thp_fault_alloc,omitempty" toml:"thp_fault_alloc,omitempty"`
			WorkingsetActivate      uint64 `json:"workingset_activate,omitempty" yaml:"workingset_activate,omitempty" toml:"workingset_activate,omitempty"`
			WorkingsetNodereclaim   uint64 `json:"workingset_nodereclaim,omitempty" yaml:"workingset_nodereclaim,omitempty" toml:"workingset_nodereclaim,omitempty"`
			WorkingsetRefault       uint64 `json:"workingset_refault,omitempty" yaml:"workingset_refault,omitempty" toml:"workingset_refault,omitempty"`
		} `json:"stats,omitempty" yaml:"stats,omitempty" toml:"stats,omitempty"`
		MaxUsage          uint64 `json:"max_usage,omitempty" yaml:"max_usage,omitempty" toml:"max_usage,omitempty"`
		Usage             uint64 `json:"usage,omitempty" yaml:"usage,omitempty" toml:"usage,omitempty"`
		Failcnt           uint64 `json:"failcnt,omitempty" yaml:"failcnt,omitempty" toml:"failcnt,omitempty"`
		Limit             uint64 `json:"limit,omitempty" yaml:"limit,omitempty" toml:"limit,omitempty"`
		Commit            uint64 `json:"commitbytes,omitempty" yaml:"commitbytes,omitempty" toml:"privateworkingset,omitempty"`
		CommitPeak        uint64 `json:"commitpeakbytes,omitempty" yaml:"commitpeakbytes,omitempty" toml:"commitpeakbytes,omitempty"`
		PrivateWorkingSet uint64 `json:"privateworkingset,omitempty" yaml:"privateworkingset,omitempty" toml:"privateworkingset,omitempty"`
	} `json:"memory_stats,omitempty" yaml:"memory_stats,omitempty" toml:"memory_stats,omitempty"`
	BlkioStats struct {
		IOServiceBytesRecursive []BlkioStatsEntry `json:"io_service_bytes_recursive,omitempty" yaml:"io_service_bytes_recursive,omitempty" toml:"io_service_bytes_recursive,omitempty"`
		IOServicedRecursive     []BlkioStatsEntry `json:"io_serviced_recursive,omitempty" yaml:"io_serviced_recursive,omitempty" toml:"io_serviced_recursive,omitempty"`
		IOQueueRecursive        []BlkioStatsEntry `json:"io_queue_recursive,omitempty" yaml:"io_queue_recursive,omitempty" toml:"io_queue_recursive,omitempty"`
		IOServiceTimeRecursive  []BlkioStatsEntry `json:"io_service_time_recursive,omitempty" yaml:"io_service_time_recursive,omitempty" toml:"io_service_time_recursive,omitempty"`
		IOWaitTimeRecursive     []BlkioStatsEntry `json:"io_wait_time_recursive,omitempty" yaml:"io_wait_time_recursive,omitempty" toml:"io_wait_time_recursive,omitempty"`
		IOMergedRecursive       []BlkioStatsEntry `json:"io_merged_recursive,omitempty" yaml:"io_merged_recursive,omitempty" toml:"io_merged_recursive,omitempty"`
		IOTimeRecursive         []BlkioStatsEntry `json:"io_time_recursive,omitempty" yaml:"io_time_recursive,omitempty" toml:"io_time_recursive,omitempty"`
		SectorsRecursive        []BlkioStatsEntry `json:"sectors_recursive,omitempty" yaml:"sectors_recursive,omitempty" toml:"sectors_recursive,omitempty"`
	} `json:"blkio_stats,omitempty" yaml:"blkio_stats,omitempty" toml:"blkio_stats,omitempty"`
	CPUStats     CPUStats `json:"cpu_stats,omitempty" yaml:"cpu_stats,omitempty" toml:"cpu_stats,omitempty"`
	PreCPUStats  CPUStats `json:"precpu_stats,omitempty"`
	StorageStats struct {
		ReadCountNormalized  uint64 `json:"read_count_normalized,omitempty" yaml:"read_count_normalized,omitempty" toml:"read_count_normalized,omitempty"`
		ReadSizeBytes        uint64 `json:"read_size_bytes,omitempty" yaml:"read_size_bytes,omitempty" toml:"read_size_bytes,omitempty"`
		WriteCountNormalized uint64 `json:"write_count_normalized,omitempty" yaml:"write_count_normalized,omitempty" toml:"write_count_normalized,omitempty"`
		WriteSizeBytes       uint64 `json:"write_size_bytes,omitempty" yaml:"write_size_bytes,omitempty" toml:"write_size_bytes,omitempty"`
	} `json:"storage_stats,omitempty" yaml:"storage_stats,omitempty" toml:"storage_stats,omitempty"`
}

// NetworkStats is a stats entry for network stats
type NetworkStats struct {
	RxDropped uint64 `json:"rx_dropped,omitempty" yaml:"rx_dropped,omitempty" toml:"rx_dropped,omitempty"`
	RxBytes   uint64 `json:"rx_bytes,omitempty" yaml:"rx_bytes,omitempty" toml:"rx_bytes,omitempty"`
	RxErrors  uint64 `json:"rx_errors,omitempty" yaml:"rx_errors,omitempty" toml:"rx_errors,omitempty"`
	TxPackets uint64 `json:"tx_packets,omitempty" yaml:"tx_packets,omitempty" toml:"tx_packets,omitempty"`
	TxDropped uint64 `json:"tx_dropped,omitempty" yaml:"tx_dropped,omitempty" toml:"tx_dropped,omitempty"`
	RxPackets uint64 `json:"rx_packets,omitempty" yaml:"rx_packets,omitempty" toml:"rx_packets,omitempty"`
	TxErrors  uint64 `json:"tx_errors,omitempty" yaml:"tx_errors,omitempty" toml:"tx_errors,omitempty"`
	TxBytes   uint64 `json:"tx_bytes,omitempty" yaml:"tx_bytes,omitempty" toml:"tx_bytes,omitempty"`
}

// CPUStats is a stats entry for cpu stats
type CPUStats struct {
	CPUUsage struct {
		PercpuUsage       []uint64 `json:"percpu_usage,omitempty" yaml:"percpu_usage,omitempty" toml:"percpu_usage,omitempty"`
		UsageInUsermode   uint64   `json:"usage_in_usermode,omitempty" yaml:"usage_in_usermode,omitempty" toml:"usage_in_usermode,omitempty"`
		TotalUsage        uint64   `json:"total_usage,omitempty" yaml:"total_usage,omitempty" toml:"total_usage,omitempty"`
		UsageInKernelmode uint64   `json:"usage_in_kernelmode,omitempty" yaml:"usage_in_kernelmode,omitempty" toml:"usage_in_kernelmode,omitempty"`
	} `json:"cpu_usage,omitempty" yaml:"cpu_usage,omitempty" toml:"cpu_usage,omitempty"`
	SystemCPUUsage uint64 `json:"system_cpu_usage,omitempty" yaml:"system_cpu_usage,omitempty" toml:"system_cpu_usage,omitempty"`
	OnlineCPUs     uint64 `json:"online_cpus,omitempty" yaml:"online_cpus,omitempty" toml:"online_cpus,omitempty"`
	ThrottlingData struct {
		Periods          uint64 `json:"periods,omitempty"`
		ThrottledPeriods uint64 `json:"throttled_periods,omitempty"`
		ThrottledTime    uint64 `json:"throttled_time,omitempty"`
	} `json:"throttling_data,omitempty" yaml:"throttling_data,omitempty" toml:"throttling_data,omitempty"`
}

// BlkioStatsEntry is a stats entry for blkio_stats
type BlkioStatsEntry struct {
	Major uint64 `json:"major,omitempty" yaml:"major,omitempty" toml:"major,omitempty"`
	Minor uint64 `json:"minor,omitempty" yaml:"minor,omitempty" toml:"minor,omitempty"`
	Op    string `json:"op,omitempty" yaml:"op,omitempty" toml:"op,omitempty"`
	Value uint64 `json:"value,omitempty" yaml:"value,omitempty" toml:"value,omitempty"`
}

// StatsOptions specify parameters to the Stats function.
//
// See https://goo.gl/Dk3Xio for more details.
type StatsOptions struct {
	ID     string
	Stats  chan<- *Stats
	Stream bool
	// A flag that enables stopping the stats operation
	Done <-chan bool
	// Initial connection timeout
	Timeout time.Duration
	// Timeout with no data is received, it's reset every time new data
	// arrives
	InactivityTimeout time.Duration `qs:"-"`
	Context           context.Context
}

// Stats sends container statistics for the given container to the given channel.
//
// This function is blocking, similar to a streaming call for logs, and should be run
// on a separate goroutine from the caller. Note that this function will block until
// the given container is removed, not just exited. When finished, this function
// will close the given channel. Alternatively, function can be stopped by
// signaling on the Done channel.
//
// See https://goo.gl/Dk3Xio for more details.
func (c *Client) Stats(opts StatsOptions) (retErr error) {
	errC := make(chan error, 1)
	readCloser, writeCloser := io.Pipe()

	defer func() {
		close(opts.Stats)

		if err := <-errC; err != nil && retErr == nil {
			retErr = err
		}

		if err := readCloser.Close(); err != nil && retErr == nil {
			retErr = err
		}
	}()

	reqSent := make(chan struct{})
	go func() {
		defer close(errC)
		err := c.stream(http.MethodGet, fmt.Sprintf("/containers/%s/stats?stream=%v", opts.ID, opts.Stream), streamOptions{
			rawJSONStream:     true,
			useJSONDecoder:    true,
			stdout:            writeCloser,
			timeout:           opts.Timeout,
			inactivityTimeout: opts.InactivityTimeout,
			context:           opts.Context,
			reqSent:           reqSent,
		})
		if err != nil {
			var dockerError *Error
			if errors.As(err, &dockerError) {
				if dockerError.Status == http.StatusNotFound {
					err = &NoSuchContainer{ID: opts.ID}
				}
			}
		}
		if closeErr := writeCloser.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		errC <- err
	}()

	quit := make(chan struct{})
	defer close(quit)
	go func() {
		// block here waiting for the signal to stop function
		select {
		case <-opts.Done:
			readCloser.Close()
		case <-quit:
			return
		}
	}()

	decoder := json.NewDecoder(readCloser)
	stats := new(Stats)
	<-reqSent
	for err := decoder.Decode(stats); !errors.Is(err, io.EOF); err = decoder.Decode(stats) {
		if err != nil {
			return err
		}
		opts.Stats <- stats
		stats = new(Stats)
	}
	return nil
}
