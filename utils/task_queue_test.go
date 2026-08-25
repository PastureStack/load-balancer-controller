package controller

import (
	"testing"
	"time"
)

func TestTaskQueueCoalescesPendingKeysAndAllowsLaterReconciliation(t *testing.T) {
	processed := make(chan string, 2)
	stop := make(chan struct{})
	queue := NewTaskQueue(func(key string) { processed <- key })
	queue.Enqueue("service")
	queue.Enqueue("service")
	go queue.Run(0, stop)

	awaitKey := func() {
		t.Helper()
		select {
		case key := <-processed:
			if key != "service" {
				t.Fatalf("processed key = %q", key)
			}
		case <-time.After(time.Second):
			t.Fatal("queue did not process the key")
		}
	}
	awaitKey()
	select {
	case key := <-processed:
		t.Fatalf("duplicate pending key was processed: %q", key)
	case <-time.After(20 * time.Millisecond):
	}

	queue.Enqueue("service")
	awaitKey()
	close(stop)
	select {
	case <-queue.workerDone:
	case <-time.After(time.Second):
		t.Fatal("queue did not stop")
	}
}
