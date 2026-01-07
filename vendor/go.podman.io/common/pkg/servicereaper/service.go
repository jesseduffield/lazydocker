//go:build linux || freebsd

package servicereaper

import (
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"
)

type service struct {
	pidMap map[int]bool
	mutex  *sync.Mutex
}

var s = service{
	pidMap: map[int]bool{},
	mutex:  &sync.Mutex{},
}

func AddPID(pid int) {
	s.mutex.Lock()
	s.pidMap[pid] = true
	s.mutex.Unlock()
}

func Start() {
	// create signal channel and only wait for SIGCHLD
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGCHLD)
	// wait and reap in an  extra goroutine
	go reaper(sigc)
}

func reaper(sigc chan os.Signal) {
	for {
		// block until we receive SIGCHLD
		<-sigc
		s.mutex.Lock()
		for pid := range s.pidMap {
			var status syscall.WaitStatus
			waitpid, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
			if err != nil {
				// do not log error for ECHILD
				if err != syscall.ECHILD {
					logrus.Warnf("Wait for pid %d failed: %v ", pid, err)
				}
				delete(s.pidMap, pid)
				continue
			}
			// if pid == 0 nothing happened
			if waitpid == 0 {
				continue
			}
			if status.Exited() || status.Signaled() {
				delete(s.pidMap, pid)
			}
		}
		s.mutex.Unlock()
	}
}
