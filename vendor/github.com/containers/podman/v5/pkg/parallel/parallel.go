package parallel

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"
)

var (
	// Semaphore to control thread creation and ensure numThreads is
	// respected.
	jobControl *semaphore.Weighted
	// Lock to control changing the semaphore - we don't want to do it
	// while anyone is using it.
	jobControlLock sync.RWMutex
)

// SetMaxThreads sets the number of threads that will be used for parallel jobs.
func SetMaxThreads(threads uint) error {
	if threads == 0 {
		return errors.New("must give a non-zero number of threads to execute with")
	}

	jobControlLock.Lock()
	defer jobControlLock.Unlock()

	jobControl = semaphore.NewWeighted(int64(threads))
	logrus.Infof("Setting parallel job count to %d", threads)

	return nil
}

// Enqueue adds a single function to the parallel jobs queue. This function will
// be run when an unused thread is available.
// Returns a receive-only error channel that will return the error (if any) from
// the provided function fn when fn has finished executing. The channel will be
// closed after this.
func Enqueue(ctx context.Context, fn func() error) <-chan error {
	retChan := make(chan error)

	go func() {
		jobControlLock.RLock()
		defer jobControlLock.RUnlock()

		defer close(retChan)

		if err := jobControl.Acquire(ctx, 1); err != nil {
			retChan <- fmt.Errorf("acquiring job control semaphore: %w", err)
			return
		}

		err := fn()

		jobControl.Release(1)

		retChan <- err
	}()

	return retChan
}
