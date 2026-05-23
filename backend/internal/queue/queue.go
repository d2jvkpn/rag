package queue

// TaskQueue abstracts async task dispatch. Two implementations: goroutine
// channel (no deps) and Asynq (requires Redis).
type TaskQueue interface {
	// Enqueue schedules a document-processing task.
	Enqueue(documentID string, rechunk bool) error
	// Shutdown waits for in-flight tasks and stops the queue.
	Shutdown()
}
