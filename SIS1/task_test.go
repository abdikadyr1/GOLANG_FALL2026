package trailplanner

import (
	"errors"
	"testing"
)

func testService() *Service {
	return NewService(NewMemoryRepository(), RationPlanner{})
}

func TestCreateValidatesAndAssignsUniqueIDs(t *testing.T) {
	s := testService()
	input := Trip{Trail: "Pine Pass", HighestPointMeters: 2000, Days: 2, Hikers: 3, KcalPerDay: 2500}
	first, err := s.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatal("expected unique IDs")
	}
	if first.State != StateQueued {
		t.Fatalf("state = %q", first.State)
	}

	invalidInputs := []struct {
		name string
		trip Trip
	}{
		{"missing trail", Trip{Days: 2, Hikers: 1, KcalPerDay: 2000}},
		{"zero days", Trip{Trail: "Pine Pass", Days: 0, Hikers: 1, KcalPerDay: 2000}},
		{"invalid elevation", Trip{Trail: "Pine Pass", HighestPointMeters: 9500, Days: 2, Hikers: 1, KcalPerDay: 2000}},
	}
	for _, tc := range invalidInputs {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Create(tc.trip); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("expected invalid input error, got %v", err)
			}
		})
	}
}

func TestProcessStoresResultAndCompletes(t *testing.T) {
	s := testService()
	task, err := s.Create(Trip{Trail: "Pine Pass", HighestPointMeters: 2000, Days: 2, Hikers: 3, KcalPerDay: 2500})
	if err != nil {
		t.Fatal(err)
	}
	processed, err := s.Process(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if processed.State != StateCompleted {
		t.Fatalf("state = %q", processed.State)
	}
	if processed.Result == nil || processed.Result.TotalCalories != 16500 || processed.Result.PerHiker != 5500 || processed.Result.AdjustedCaloriesPerDay != 2750 {
		t.Fatalf("unexpected result: %+v", processed.Result)
	}
	stored, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != StateCompleted || stored.Result == nil || stored.Result.TotalCalories != 16500 {
		t.Fatalf("result was not stored: %+v", stored)
	}
}

func TestMissingTaskAndInvalidTransition(t *testing.T) {
	s := testService()
	if _, err := s.Get("absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	if _, err := s.Process("absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	task, err := s.Create(Trip{Trail: "Pine Pass", Days: 1, Hikers: 1, KcalPerDay: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Process(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Process(task.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected invalid state, got %v", err)
	}
}

func TestMultipleTasksRemainIndependent(t *testing.T) {
	s := testService()
	a, err := s.Create(Trip{Trail: "North Fork", HighestPointMeters: 1000, Days: 1, Hikers: 1, KcalPerDay: 2200})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(Trip{Trail: "South Ridge", HighestPointMeters: 0, Days: 4, Hikers: 2, KcalPerDay: 3000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Process(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Process(a.ID); err != nil {
		t.Fatal(err)
	}
	gotA, err := s.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	gotB, err := s.Get(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotA.Input.Trail != "North Fork" || gotA.Result.TotalCalories != 2310 {
		t.Fatalf("task A mixed: %+v", gotA)
	}
	if gotB.Input.Trail != "South Ridge" || gotB.Result.TotalCalories != 24000 {
		t.Fatalf("task B mixed: %+v", gotB)
	}
}

func TestPlannerRejectsInvalidInput(t *testing.T) {
	_, err := (RationPlanner{}).Plan(Trip{Trail: "Ridge", Days: 0, Hikers: 1, KcalPerDay: 2000})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}
