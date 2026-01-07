package copy

import (
	"io"
	"time"

	"go.podman.io/image/v5/types"
)

// progressReader is a reader that reports its progress to a types.ProgressProperties channel on an interval.
type progressReader struct {
	source       io.Reader
	channel      chan<- types.ProgressProperties
	interval     time.Duration
	artifact     types.BlobInfo
	lastUpdate   time.Time
	offset       uint64
	offsetUpdate uint64
}

// newProgressReader creates a new progress reader for:
// `source`:   The source when internally reading bytes
// `channel`:  The reporter channel to which the progress will be sent
// `interval`: The update interval to indicate how often the progress should update
// `artifact`: The blob metadata which is currently being progressed
func newProgressReader(
	source io.Reader,
	channel chan<- types.ProgressProperties,
	interval time.Duration,
	artifact types.BlobInfo,
) *progressReader {
	// The progress reader constructor informs the progress channel
	// that a new artifact will be read
	channel <- types.ProgressProperties{
		Event:    types.ProgressEventNewArtifact,
		Artifact: artifact,
	}
	return &progressReader{
		source:       source,
		channel:      channel,
		interval:     interval,
		artifact:     artifact,
		lastUpdate:   time.Now(),
		offset:       0,
		offsetUpdate: 0,
	}
}

// reportDone indicates to the internal channel that the progress has been
// finished
func (r *progressReader) reportDone() {
	r.channel <- types.ProgressProperties{
		Event:        types.ProgressEventDone,
		Artifact:     r.artifact,
		Offset:       r.offset,
		OffsetUpdate: r.offsetUpdate,
	}
}

// Read continuously reads bytes into the progress reader and reports the
// status via the internal channel
func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	r.offset += uint64(n)
	r.offsetUpdate += uint64(n)

	// Fire the progress reader in the provided interval
	if time.Since(r.lastUpdate) > r.interval {
		r.channel <- types.ProgressProperties{
			Event:        types.ProgressEventRead,
			Artifact:     r.artifact,
			Offset:       r.offset,
			OffsetUpdate: r.offsetUpdate,
		}
		r.lastUpdate = time.Now()
		r.offsetUpdate = 0
	}
	return n, err
}
