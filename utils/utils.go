package controller

import (
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// TaskQueue serializes controller reconciliation and coalesces duplicate keys.
// It replaces the Kubernetes 1.4 workqueue previously used by every controller,
// without carrying the obsolete Kubernetes client into the HAProxy runtime.
type TaskQueue struct {
	mu         sync.Mutex
	items      []string
	pending    map[string]struct{}
	wake       chan struct{}
	workerDone chan struct{}
	stopOnce   sync.Once
	stopped    bool
	sync       func(string)
}

func (t *TaskQueue) pop() (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.items) == 0 {
		return "", false
	}
	key := t.items[0]
	t.items = t.items[1:]
	delete(t.pending, key)
	return key, true
}

// Run processes queued keys until stopCh is closed. period remains in the
// signature for source compatibility; wakeups are event-driven.
func (t *TaskQueue) Run(_ time.Duration, stopCh <-chan struct{}) {
	defer close(t.workerDone)
	for {
		if key, ok := t.pop(); ok {
			log.Debugf("syncing %v", key)
			t.sync(key)
			continue
		}
		select {
		case <-stopCh:
			t.stopOnce.Do(func() {
				t.mu.Lock()
				t.stopped = true
				t.mu.Unlock()
			})
			return
		case <-t.wake:
		}
	}
}

// Enqueue schedules a key once until it is picked up by the worker.
func (t *TaskQueue) Enqueue(key string) {
	if key == "" {
		return
	}
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	if _, exists := t.pending[key]; exists {
		t.mu.Unlock()
		return
	}
	t.pending[key] = struct{}{}
	t.items = append(t.items, key)
	t.mu.Unlock()
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

func (t *TaskQueue) Requeue(key string, err error) {
	log.Debugf("Requeuing %v, err %v", key, err)
	t.Enqueue(key)
}

// Shutdown prevents new work. The owning controller closes stopCh to stop Run.
func (t *TaskQueue) Shutdown() {
	t.stopOnce.Do(func() {
		t.mu.Lock()
		t.stopped = true
		t.mu.Unlock()
		select {
		case t.wake <- struct{}{}:
		default:
		}
	})
}

func NewTaskQueue(syncFn func(string)) *TaskQueue {
	return &TaskQueue{
		pending:    make(map[string]struct{}),
		wake:       make(chan struct{}, 1),
		workerDone: make(chan struct{}),
		sync:       syncFn,
	}
}
