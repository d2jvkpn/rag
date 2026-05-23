package queue

import "sync"

// GoroutineQueue dispatches tasks to a pool of goroutines via a buffered channel.
type GoroutineQueue struct {
	ch      chan task
	wg      sync.WaitGroup
}

type task struct {
	documentID string
	rechunk    bool
}

func NewGoroutineQueue(concurrency int, handler func(documentID string, rechunk bool)) *GoroutineQueue {
	q := &GoroutineQueue{ch: make(chan task, 32)}
	for range concurrency {
		q.wg.Add(1)
		go func() {
			defer q.wg.Done()
			for t := range q.ch {
				handler(t.documentID, t.rechunk)
			}
		}()
	}
	return q
}

func (q *GoroutineQueue) Enqueue(documentID string, rechunk bool) error {
	q.ch <- task{documentID: documentID, rechunk: rechunk}
	return nil
}

func (q *GoroutineQueue) Shutdown() {
	close(q.ch)
	q.wg.Wait()
}
