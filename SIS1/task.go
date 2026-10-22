package trailplanner

import (
	"errors"
	"fmt"
	"strings"
)

type TaskID string

type TaskState string

const (
	StateQueued     TaskState = "queued"
	StateProcessing TaskState = "processing"
	StateCompleted  TaskState = "completed"
	StateFailed     TaskState = "failed"
)

var (
	ErrNotFound      = errors.New("task not found")
	ErrInvalidInput  = errors.New("invalid task input")
	ErrInvalidState  = errors.New("invalid task state transition")
	ErrAlreadyExists = errors.New("task already exists")
)

type Trip struct {
	Trail              string
	HighestPointMeters int
	Days               int
	Hikers             int
	KcalPerDay         int
}

func (t Trip) Validate() error {
	if strings.TrimSpace(t.Trail) == "" {
		return fmt.Errorf("%w: trail name is required", ErrInvalidInput)
	}
	if t.HighestPointMeters < 0 || t.HighestPointMeters > 9000 {
		return fmt.Errorf("%w: highest point must be between 0 and 9000 meters", ErrInvalidInput)
	}
	if t.Days < 1 || t.Days > 30 {
		return fmt.Errorf("%w: days must be between 1 and 30", ErrInvalidInput)
	}
	if t.Hikers < 1 || t.Hikers > 20 {
		return fmt.Errorf("%w: hikers must be between 1 and 20", ErrInvalidInput)
	}
	if t.KcalPerDay < 1000 || t.KcalPerDay > 6000 {
		return fmt.Errorf("%w: daily calories must be between 1000 and 6000", ErrInvalidInput)
	}
	return nil
}

type RationPlan struct {
	Trail                  string
	HighestPointMeters     int
	TotalCalories          int
	PerHiker               int
	BaseCaloriesPerDay     int
	AdjustedCaloriesPerDay int
	Tip                    string
}

type Task struct {
	ID     TaskID
	Input  Trip
	State  TaskState
	Result *RationPlan
	Error  string
}

func newTask(id TaskID, input Trip) Task {
	return Task{ID: id, Input: input, State: StateQueued}
}

func (t *Task) begin() error {
	if t.State != StateQueued {
		return fmt.Errorf("%w: cannot process task in %q state", ErrInvalidState, t.State)
	}
	t.State = StateProcessing
	return nil
}

func (t *Task) complete(plan RationPlan) error {
	if t.State != StateProcessing {
		return fmt.Errorf("%w: cannot complete task in %q state", ErrInvalidState, t.State)
	}
	t.Result = &plan
	t.State = StateCompleted
	return nil
}

func (t *Task) fail(err error) error {
	if t.State != StateProcessing {
		return fmt.Errorf("%w: cannot fail task in %q state", ErrInvalidState, t.State)
	}
	t.Error = err.Error()
	t.State = StateFailed
	return nil
}
