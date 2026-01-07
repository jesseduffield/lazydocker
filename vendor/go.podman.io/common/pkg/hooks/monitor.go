package hooks

import (
	"context"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
	current "go.podman.io/common/pkg/hooks/1.0.0"
)

// Monitor dynamically monitors hook directories for additions,
// updates, and removals.
//
// This function writes two empty structs to the sync channel: the
// first is written after the watchers are established and the second
// when this function exits.  The expected usage is:
//
//	ctx, cancel := context.WithCancel(context.Background())
//	sync := make(chan error, 2)
//	go m.Monitor(ctx, sync)
//	err := <-sync // block until writers are established
//	if err != nil {
//	  return err // failed to establish watchers
//	}
//	// do stuff
//	cancel()
//	err = <-sync // block until monitor finishes
func (m *Manager) Monitor(ctx context.Context, sync chan<- error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		sync <- err
		return
	}
	defer watcher.Close()

	for _, dir := range m.directories {
		err = watcher.Add(dir)
		if err != nil {
			logrus.Errorf("Failed to watch %q for hooks", dir)
			sync <- err
			return
		}
		logrus.Debugf("monitoring %q for hooks", dir)
	}

	sync <- nil

	for {
		select {
		case event := <-watcher.Events:
			m.lock.Lock()
			m.hooks = make(map[string]*current.Hook)
			for _, dir := range m.directories {
				err = ReadDir(dir, m.extensionStages, m.hooks)
				if err != nil {
					logrus.Errorf("Failed loading hooks for %s: %v", event.Name, err)
				}
			}
			m.lock.Unlock()
		case <-ctx.Done():
			err = ctx.Err()
			logrus.Debugf("hook monitoring canceled: %v", err)
			sync <- err
			close(sync)
			return
		}
	}
}
