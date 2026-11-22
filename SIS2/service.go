package trailplanner

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type Repository interface {
	Save(Task) error
	Get(TaskID) (Task, error)
	Update(Task) error

	Modify(TaskID, func(*Task) error) (Task, error)
}

type Planner interface {
	Plan(Trip) (RationPlan, error)
}

type RationPlanner struct{}

func (RationPlanner) Plan(trip Trip) (RationPlan, error) {
	if err := trip.Validate(); err != nil {
		return RationPlan{}, err
	}
	adjustedPerDay := trip.KcalPerDay + trip.KcalPerDay*trip.HighestPointMeters/20000
	perHiker := trip.Days * adjustedPerDay
	total := perHiker * trip.Hikers
	return RationPlan{
		Trail:                  trip.Trail,
		HighestPointMeters:     trip.HighestPointMeters,
		TotalCalories:          total,
		PerHiker:               perHiker,
		BaseCaloriesPerDay:     trip.KcalPerDay,
		AdjustedCaloriesPerDay: adjustedPerDay,
		Tip:                    "Pack roughly 10% extra calories for cold weather or a slower pace.",
	}, nil
}

type MemoryRepository struct {
	mu    sync.RWMutex
	tasks map[TaskID]Task
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{tasks: make(map[TaskID]Task)}
}

func (r *MemoryRepository) Save(task Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tasks[task.ID]; exists {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, task.ID)
	}
	r.tasks[task.ID] = cloneTask(task)
	return nil
}

func (r *MemoryRepository) Get(id TaskID) (Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	task, ok := r.tasks[id]
	if !ok {
		return Task{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return cloneTask(task), nil
}

func (r *MemoryRepository) Update(task Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tasks[task.ID]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, task.ID)
	}
	r.tasks[task.ID] = cloneTask(task)
	return nil
}

func (r *MemoryRepository) Modify(id TaskID, fn func(*Task) error) (Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[id]
	if !ok {
		return Task{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	task = cloneTask(task)
	if err := fn(&task); err != nil {
		return Task{}, err
	}
	r.tasks[id] = cloneTask(task)
	return task, nil
}

func cloneTask(task Task) Task {
	if task.Result != nil {
		result := *task.Result
		task.Result = &result
	}
	return task
}

type Service struct {
	repo    Repository
	planner Planner
	nextID  atomic.Uint64
}

func NewService(repo Repository, planner Planner) *Service {
	return &Service{repo: repo, planner: planner}
}

func (s *Service) Create(input Trip) (Task, error) {
	if err := input.Validate(); err != nil {
		return Task{}, err
	}
	id := TaskID(fmt.Sprintf("TR-%06d", s.nextID.Add(1)))
	task := newTask(id, input)
	if err := s.repo.Save(task); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *Service) Get(id TaskID) (Task, error) { return s.repo.Get(id) }

func (s *Service) Process(id TaskID) (Task, error) {
	return s.run(id, StatePending)
}

func (s *Service) run(id TaskID, from TaskState) (Task, error) {
	task, err := s.repo.Modify(id, func(t *Task) error { return t.begin(from) })
	if err != nil {
		return Task{}, err
	}

	plan, planErr := s.planner.Plan(task.Input)

	if planErr != nil {
		failed, err := s.repo.Modify(id, func(t *Task) error { return t.fail(planErr) })
		if err != nil {
			return Task{}, err
		}
		return failed, fmt.Errorf("planning task %s: %w", id, planErr)
	}
	return s.repo.Modify(id, func(t *Task) error { return t.complete(plan) })
}
