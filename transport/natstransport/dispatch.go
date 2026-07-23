package natstransport

import "sync"

// socketDispatcher fans messages out to per-socket workers, preserving
// per-socket ordering while letting sockets progress independently. Workers
// are spawned on demand and exit once their socket's queue drains.
type socketDispatcher struct {
	queueSize  int
	onOverflow func(socketID string)

	mu     sync.Mutex
	queues map[string][]func()
	wg     sync.WaitGroup
}

func newSocketDispatcher(queueSize int, onOverflow func(socketID string)) *socketDispatcher {
	return &socketDispatcher{
		queueSize:  queueSize,
		onOverflow: onOverflow,
		queues:     map[string][]func(){},
	}
}

// dispatch enqueues work for a socket, preserving per-socket order. It
// returns false if the socket's queue is full, in which case the work is
// dropped and onOverflow is invoked.
func (d *socketDispatcher) dispatch(socketID string, work func()) bool {
	d.mu.Lock()
	queue, active := d.queues[socketID]
	if len(queue) >= d.queueSize {
		d.mu.Unlock()
		transportNatsDebug.Tracef("Dispatch queue overflow for socket %s", socketID)
		if d.onOverflow != nil {
			d.onOverflow(socketID)
		}
		return false
	}
	d.queues[socketID] = append(queue, work)
	if !active {
		d.wg.Add(1)
		go d.run(socketID)
	}
	d.mu.Unlock()
	return true
}

func (d *socketDispatcher) run(socketID string) {
	defer d.wg.Done()
	for {
		d.mu.Lock()
		queue := d.queues[socketID]
		if len(queue) == 0 {
			delete(d.queues, socketID)
			d.mu.Unlock()
			return
		}
		work := queue[0]
		d.queues[socketID] = queue[1:]
		d.mu.Unlock()
		work()
	}
}

// wait blocks until all in-flight work has completed. Callers must stop
// dispatching before calling wait.
func (d *socketDispatcher) wait() {
	d.wg.Wait()
}
