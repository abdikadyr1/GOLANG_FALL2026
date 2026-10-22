package trailplanner

import (
	"fmt"
)

type Repository interface {
	Save(Task) error
	Get(TaskID) (Task, error)
	Update(Task) error
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
	tasks map[TaskID]Task
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{tasks: make(map[TaskID]Task)}
}

func (r *MemoryRepository) Save(task Task) error {
	if _, exists := r.tasks[task.ID]; exists {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, task.ID)
	}
	r.tasks[task.ID] = cloneTask(task)
	return nil
}

func (r *MemoryRepository) Get(id TaskID) (Task, error) {
	task, ok := r.tasks[id]
	if !ok {
		return Task{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return cloneTask(task), nil
}

func (r *MemoryRepository) Update(task Task) error {
	if _, ok := r.tasks[task.ID]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, task.ID)
	}
	r.tasks[task.ID] = cloneTask(task)
	return nil
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
	nextID  uint64
}

func NewService(repo Repository, planner Planner) *Service {
	return &Service{repo: repo, planner: planner}
}

func (s *Service) Create(input Trip) (Task, error) {
	if err := input.Validate(); err != nil {
		return Task{}, err
	}
	s.nextID++
	id := TaskID(fmt.Sprintf("TR-%06d", s.nextID))
	task := newTask(id, input)
	if err := s.repo.Save(task); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *Service) Get(id TaskID) (Task, error) { return s.repo.Get(id) }

func (s *Service) Process(id TaskID) (Task, error) {
	task, err := s.repo.Get(id)
	if err != nil {
		return Task{}, err
	}
	if err := task.begin(); err != nil {
		return Task{}, err
	}
	if err := s.repo.Update(task); err != nil {
		return Task{}, err
	}
	plan, planErr := s.planner.Plan(task.Input)
	if planErr != nil {
		if err := task.fail(planErr); err != nil {
			return Task{}, err
		}
		if err := s.repo.Update(task); err != nil {
			return Task{}, err
		}
		return task, fmt.Errorf("planning task %s: %w", id, planErr)
	}
	if err := task.complete(plan); err != nil {
		return Task{}, err
	}
	if err := s.repo.Update(task); err != nil {
		return Task{}, err
	}
	return task, nil
}
