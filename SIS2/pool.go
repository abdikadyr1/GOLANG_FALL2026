package trailplanner

import (
	"fmt"
	"sync"
)

type WorkerPool struct {
	service *Service
	queue   chan TaskID

	mu     sync.Mutex
	closed bool

	wg           sync.WaitGroup
	shutdownOnce sync.Once
}

func NewWorkerPool(service *Service, workers, capacity int) (*WorkerPool, error) {
	if service == nil {
		return nil, fmt.Errorf("%w: service is required", ErrInvalidConfig)
	}
	if workers < 2 {
		return nil, fmt.Errorf("%w: need at least 2 workers, got %d", ErrInvalidConfig, workers)
	}
	if capacity < 1 {
		return nil, fmt.Errorf("%w: queue capacity must be positive, got %d", ErrInvalidConfig, capacity)
	}
	p := &WorkerPool{service: service, queue: make(chan TaskID, capacity)}
	p.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go p.worker()
	}
	return p, nil
}

func (p *WorkerPool) worker() {
	defer p.wg.Done()
	for id := range p.queue {

		_, _ = p.service.run(id, StateQueued)
	}
}

func (p *WorkerPool) Submit(id TaskID) (Task, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return Task{}, ErrPoolClosed
	}

	task, err := p.service.repo.Modify(id, func(t *Task) error { return t.enqueue() })
	if err != nil {
		return Task{}, err
	}

	select {
	case p.queue <- id:
		return task, nil
	default:

		if _, err := p.service.repo.Modify(id, func(t *Task) error { return t.unqueue() }); err != nil {
			return Task{}, err
		}
		return Task{}, fmt.Errorf("%w: task %s stays pending", ErrQueueFull, id)
	}
}

func (p *WorkerPool) CreateAndSubmit(trip Trip) (Task, error) {
	task, err := p.service.Create(trip)
	if err != nil {
		return Task{}, err
	}
	queued, err := p.Submit(task.ID)
	if err != nil {
		return task, err
	}
	return queued, nil
}

func (p *WorkerPool) Shutdown() {
	p.shutdownOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		close(p.queue)
		p.mu.Unlock()
	})
	p.wg.Wait()
}
