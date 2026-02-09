package queue

import (
	"context"
	"errors"
	"sync"

	"git_sonic/internal/controller/webhook"
)

// Handler handles a job.
type Handler func(context.Context, Job) error

// Job wraps a webhook event.
type Job struct {
	Event webhook.Event
}

// Queue runs jobs with worker goroutines.
type Queue struct {
	jobs    chan Job
	handler Handler
	wg      sync.WaitGroup
}

// New creates a new queue.
func New(workerCount int, handler Handler) *Queue {
	if workerCount < 1 {
		workerCount = 1
	}
	return &Queue{
		jobs:    make(chan Job, workerCount*4),
		handler: handler,
	}
}

// Start launches workers.
func (q *Queue) Start(ctx context.Context, workerCount int) {
	if workerCount < 1 {
		workerCount = 1
	}
	for i := 0; i < workerCount; i++ {
		q.wg.Add(1)
		go func() {
			defer q.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-q.jobs:
					_ = q.handler(ctx, job)
				}
			}
		}()
	}
}

// Stop waits for workers to finish.
func (q *Queue) Stop() {
	q.wg.Wait()
}

// Enqueue adds a job to the queue.
func (q *Queue) Enqueue(job Job) error {
	select {
	case q.jobs <- job:
		return nil
	default:
		return errors.New("queue is full")
	}
}
