package trailplanner

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type gatePlanner struct {
	started   chan string
	release   chan struct{}
	active    atomic.Int32
	maxActive atomic.Int32
	once      sync.Once
}

func (g *gatePlanner) open() { g.once.Do(func() { close(g.release) }) }

func newGatePlanner() *gatePlanner {
	return &gatePlanner{started: make(chan string, 100), release: make(chan struct{})}
}

func (g *gatePlanner) Plan(trip Trip) (RationPlan, error) {
	now := g.active.Add(1)
	for {
		old := g.maxActive.Load()
		if now <= old || g.maxActive.CompareAndSwap(old, now) {
			break
		}
	}
	g.started <- trip.Trail
	<-g.release
	g.active.Add(-1)
	return RationPlanner{}.Plan(trip)
}

func (g *gatePlanner) waitStarted(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-g.started:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d tasks started processing", i, n)
		}
	}
}

type failingPlanner struct{}

func (failingPlanner) Plan(Trip) (RationPlan, error) {
	return RationPlan{}, errors.New("weather service unavailable")
}

func trip(name string, days int) Trip {
	return Trip{Trail: name, HighestPointMeters: 2000, Days: days, Hikers: 2, KcalPerDay: 2000}
}

func expectedTotal(days int) int { return 2200 * days * 2 }

func newPool(t *testing.T, planner Planner, workers, capacity int) (*Service, *WorkerPool) {
	t.Helper()
	s := NewService(NewMemoryRepository(), planner)
	p, err := NewWorkerPool(s, workers, capacity)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Shutdown)
	return s, p
}

func mustState(t *testing.T, s *Service, id TaskID, want TaskState) Task {
	t.Helper()
	got, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != want {
		t.Fatalf("task %s state = %q, want %q", id, got.State, want)
	}
	return got
}

func TestNewWorkerPoolValidatesConfig(t *testing.T) {
	s := testService()
	cases := []struct{ workers, capacity int }{{1, 5}, {0, 5}, {2, 0}, {3, -1}}
	for _, c := range cases {
		if _, err := NewWorkerPool(s, c.workers, c.capacity); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("workers=%d capacity=%d: expected config error, got %v", c.workers, c.capacity, err)
		}
	}
	p, err := NewWorkerPool(s, 2, 1)
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	p.Shutdown()
}

func TestPoolProcessesSubmittedTasks(t *testing.T) {
	s, p := newPool(t, RationPlanner{}, 3, 10)

	ids := make(map[TaskID]int)
	for days := 1; days <= 8; days++ {
		task, err := p.CreateAndSubmit(trip(fmt.Sprintf("Trail %d", days), days))
		if err != nil {
			t.Fatal(err)
		}
		if task.State != StateQueued {
			t.Fatalf("submit returned state %q, want queued", task.State)
		}
		ids[task.ID] = days
	}

	p.Shutdown()

	for id, days := range ids {
		got := mustState(t, s, id, StateCompleted)
		if got.Result == nil || got.Result.TotalCalories != expectedTotal(days) {
			t.Fatalf("task %s (days=%d) wrong result: %+v", id, days, got.Result)
		}
		if got.Result.Trail != got.Input.Trail {
			t.Fatalf("task %s got result of another task: %+v", id, got.Result)
		}
	}
}

func TestSubmitDoesNotWaitAndWorkersOverlap(t *testing.T) {
	gate := newGatePlanner()
	s, p := newPool(t, gate, 2, 5)
	t.Cleanup(gate.open)

	var ids []TaskID
	for i := 1; i <= 4; i++ {
		task, err := p.CreateAndSubmit(trip(fmt.Sprintf("T%d", i), i))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, task.ID)
	}

	gate.waitStarted(t, 2)

	counts := map[TaskState]int{}
	for _, id := range ids {
		got, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		counts[got.State]++
	}
	if counts[StateProcessing] != 2 || counts[StateQueued] != 2 {
		t.Fatalf("expected 2 processing and 2 queued, got %v", counts)
	}

	gate.open()
	p.Shutdown()

	if got := gate.maxActive.Load(); got != 2 {
		t.Fatalf("max concurrent plans = %d, want exactly 2 (worker limit)", got)
	}
	for _, id := range ids {
		mustState(t, s, id, StateCompleted)
	}
}

func TestSubmitRejectsInvalidAndDuplicateTasks(t *testing.T) {
	gate := newGatePlanner()
	s, p := newPool(t, gate, 2, 5)
	t.Cleanup(gate.open)

	if _, err := p.Submit("absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	processing, err := p.CreateAndSubmit(trip("Busy", 1))
	if err != nil {
		t.Fatal(err)
	}
	gate.waitStarted(t, 1)
	mustState(t, s, processing.ID, StateProcessing)
	if _, err := p.Submit(processing.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("resubmitting processing task: expected invalid state, got %v", err)
	}

	queued, err := p.CreateAndSubmit(trip("Waiting", 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Submit(queued.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("resubmitting queued/processing task: expected invalid state, got %v", err)
	}

	gate.open()
	p.Shutdown()

	mustState(t, s, processing.ID, StateCompleted)
	mustState(t, s, queued.ID, StateCompleted)
	if _, err := s.Process(processing.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("reprocessing completed task: expected invalid state, got %v", err)
	}
}

func TestResubmitCompletedTaskIsRejected(t *testing.T) {
	s, p := newPool(t, RationPlanner{}, 2, 2)
	task, err := s.Create(trip("Done", 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Process(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Submit(task.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected invalid state for completed task, got %v", err)
	}
}

func TestSubmitWhenQueueIsFull(t *testing.T) {
	gate := newGatePlanner()
	s, p := newPool(t, gate, 2, 1)
	t.Cleanup(gate.open)

	a, err := p.CreateAndSubmit(trip("A", 1))
	if err != nil {
		t.Fatal(err)
	}
	gate.waitStarted(t, 1)
	b, err := p.CreateAndSubmit(trip("B", 2))
	if err != nil {
		t.Fatal(err)
	}
	gate.waitStarted(t, 1)

	c, err := p.CreateAndSubmit(trip("C", 3))
	if err != nil {
		t.Fatal(err)
	}

	d, err := p.CreateAndSubmit(trip("D", 4))
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected queue full, got %v", err)
	}
	mustState(t, s, d.ID, StatePending)

	gate.open()
	p.Shutdown()

	for _, id := range []TaskID{a.ID, b.ID, c.ID} {
		mustState(t, s, id, StateCompleted)
	}
	mustState(t, s, d.ID, StatePending)
}

func TestPlannerFailureMarksTaskFailed(t *testing.T) {
	s, p := newPool(t, failingPlanner{}, 2, 2)
	task, err := p.CreateAndSubmit(trip("Storm", 1))
	if err != nil {
		t.Fatal(err)
	}
	p.Shutdown()
	got := mustState(t, s, task.ID, StateFailed)
	if got.Result != nil || got.Error == "" {
		t.Fatalf("failed task must have error and no result: %+v", got)
	}
}

func TestConcurrentSubmitAndGet(t *testing.T) {
	const n = 100
	s, p := newPool(t, RationPlanner{}, 4, n)

	var wg sync.WaitGroup
	ids := make([]TaskID, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			task, err := p.CreateAndSubmit(trip(fmt.Sprintf("Trail-%d", i), i%30+1))
			if err != nil {
				errs <- err
				return
			}
			ids[i] = task.ID
			for j := 0; j < 5; j++ {
				if _, err := s.Get(task.ID); err != nil {
					errs <- err
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	p.Shutdown()

	seen := make(map[TaskID]bool)
	for i, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate task id %s", id)
		}
		seen[id] = true
		got := mustState(t, s, id, StateCompleted)
		if got.Input.Trail != fmt.Sprintf("Trail-%d", i) || got.Result.TotalCalories != expectedTotal(i%30+1) {
			t.Fatalf("task %s has wrong data: %+v", id, got)
		}
	}
}

func TestConcurrentProcessOfSameTaskRunsOnce(t *testing.T) {
	s := testService()
	task, err := s.Create(trip("Once", 1))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var ok, rejected atomic.Int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Process(task.ID)
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrInvalidState):
				rejected.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || rejected.Load() != 9 {
		t.Fatalf("ok=%d rejected=%d, want 1 and 9", ok.Load(), rejected.Load())
	}
}

func TestShutdownFinishesAcceptedTasksAndStopsWorkers(t *testing.T) {
	gate := newGatePlanner()
	s, p := newPool(t, gate, 2, 3)
	t.Cleanup(gate.open)

	var ids []TaskID
	submit := func(i int) {
		task, err := p.CreateAndSubmit(trip(fmt.Sprintf("S%d", i), i))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, task.ID)
	}
	for i := 1; i <= 2; i++ {
		submit(i)
		gate.waitStarted(t, 1)
	}
	for i := 3; i <= 5; i++ {
		submit(i)
	}

	done := make(chan struct{})
	go func() {
		p.Shutdown()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("shutdown returned while tasks were still processing")
	default:
	}

	gate.open()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("workers did not terminate after shutdown")
	}

	for _, id := range ids {
		mustState(t, s, id, StateCompleted)
	}
}

func TestSubmitAfterShutdownIsRejected(t *testing.T) {
	s, p := newPool(t, RationPlanner{}, 2, 2)
	p.Shutdown()
	p.Shutdown()

	task, err := p.CreateAndSubmit(trip("Late", 1))
	if !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("expected pool closed, got %v", err)
	}
	mustState(t, s, task.ID, StatePending)
}

func TestConcurrentSubmitAndShutdown(t *testing.T) {
	s, p := newPool(t, RationPlanner{}, 3, 20)

	type outcome struct {
		id  TaskID
		err error
	}
	results := make(chan outcome, 50)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			task, err := p.CreateAndSubmit(trip(fmt.Sprintf("R%d", i), 1))
			results <- outcome{task.ID, err}
		}(i)
	}
	p.Shutdown()
	wg.Wait()
	close(results)

	for r := range results {
		switch {
		case r.err == nil:
			mustState(t, s, r.id, StateCompleted)
		case errors.Is(r.err, ErrPoolClosed), errors.Is(r.err, ErrQueueFull):
			mustState(t, s, r.id, StatePending)
		default:
			t.Fatalf("unexpected error: %v", r.err)
		}
	}
}
