//go:build !remote

package libpod

func (r *Runtime) setupWorkerQueue() {
	r.workerChannel = make(chan func(), 10)
}

func (r *Runtime) startWorker() {
	go func() {
		for w := range r.workerChannel {
			w()
			r.workerGroup.Done()
		}
	}()
}

func (r *Runtime) queueWork(f func()) {
	r.workerGroup.Add(1)
	go func() {
		r.workerChannel <- f
	}()
}
