package queue

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/hibiken/asynq"
)

const taskTypeProcess = "document:process"

type asynqPayload struct {
	DocumentID string `json:"document_id"`
	Rechunk    bool   `json:"rechunk"`
}

// AsynqQueue dispatches tasks via Redis-backed Asynq.
type AsynqQueue struct {
	client *asynq.Client
	server *asynq.Server
	wg     sync.WaitGroup
}

func NewAsynqQueue(redisDSN string, concurrency int, handler func(documentID string, rechunk bool)) (*AsynqQueue, error) {
	opt, err := asynq.ParseRedisURI(redisDSN)
	if err != nil {
		return nil, err
	}
	client := asynq.NewClient(opt)
	server := asynq.NewServer(opt, asynq.Config{Concurrency: concurrency})

	mux := asynq.NewServeMux()
	mux.HandleFunc(taskTypeProcess, func(_ context.Context, t *asynq.Task) error {
		var p asynqPayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return err
		}
		handler(p.DocumentID, p.Rechunk)
		return nil
	})

	q := &AsynqQueue{client: client, server: server}
	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		_ = server.Run(mux) // returns after Shutdown() is called
	}()
	return q, nil
}

func (q *AsynqQueue) Enqueue(documentID string, rechunk bool) error {
	payload, err := json.Marshal(asynqPayload{DocumentID: documentID, Rechunk: rechunk})
	if err != nil {
		return err
	}
	_, err = q.client.Enqueue(asynq.NewTask(taskTypeProcess, payload))
	return err
}

func (q *AsynqQueue) Shutdown() {
	q.server.Shutdown()
	_ = q.client.Close()
	q.wg.Wait()
}
