package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	planner "trailplanner"
)

const (
	workerCount   = 3
	queueCapacity = 5
)

func main() {
	input, err := readTrip()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	service := planner.NewService(planner.NewMemoryRepository(), planner.RationPlanner{})
	pool, err := planner.NewWorkerPool(service, workerCount, queueCapacity)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	trips := []planner.Trip{
		input,
		{Trail: "Big Almaty Lake", HighestPointMeters: 2500, Days: 1, Hikers: 4, KcalPerDay: 2400},
		{Trail: "Kok-Zhailau", HighestPointMeters: 1800, Days: 2, Hikers: 3, KcalPerDay: 2600},
		{Trail: "Furmanov Peak", HighestPointMeters: 3050, Days: 3, Hikers: 2, KcalPerDay: 3000},
	}

	var ids []planner.TaskID
	for _, trip := range trips {
		task, err := pool.CreateAndSubmit(trip)
		if err != nil {
			fmt.Fprintf(os.Stderr, "submit %q: %v\n", trip.Trail, err)
			continue
		}
		fmt.Printf("Submitted %s (%s): %s\n", task.ID, trip.Trail, task.State)
		ids = append(ids, task.ID)
	}

	pool.Shutdown()
	fmt.Println()

	for _, id := range ids {
		task, err := service.Get(id)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		printTask(task)
	}
}

func printTask(task planner.Task) {
	fmt.Printf("Task %s: %s\n", task.ID, task.State)
	if task.Result == nil {
		fmt.Printf("  Error: %s\n\n", task.Error)
		return
	}
	r := task.Result
	fmt.Printf("  Trail: %s | Highest point: %d m\n", r.Trail, r.HighestPointMeters)
	fmt.Printf("  Daily target: %d kcal → %d kcal after elevation adjustment\n",
		r.BaseCaloriesPerDay, r.AdjustedCaloriesPerDay)
	fmt.Printf("  Total: %d kcal | Per hiker: %d kcal\n", r.TotalCalories, r.PerHiker)
	fmt.Printf("  %s\n\n", r.Tip)
}

func readTrip() (planner.Trip, error) {
	reader := bufio.NewReader(os.Stdin)
	trail, err := readString(reader, "Trail name", "Eagle Ridge Loop")
	if err != nil {
		return planner.Trip{}, err
	}
	height, err := readInt(reader, "Highest point elevation in meters", 2000)
	if err != nil {
		return planner.Trip{}, err
	}
	days, err := readInt(reader, "Trip length in days", 3)
	if err != nil {
		return planner.Trip{}, err
	}
	hikers, err := readInt(reader, "Number of hikers", 2)
	if err != nil {
		return planner.Trip{}, err
	}
	kcal, err := readInt(reader, "Calories per hiker per day", 2800)
	if err != nil {
		return planner.Trip{}, err
	}
	return planner.Trip{Trail: trail, HighestPointMeters: height, Days: days, Hikers: hikers, KcalPerDay: kcal}, nil
}

func readString(reader *bufio.Reader, label, defaultValue string) (string, error) {
	fmt.Printf("%s [%s]: ", label, defaultValue)
	value, err := reader.ReadString('\n')
	if err != nil && len(value) == 0 {
		return "", fmt.Errorf("read %s: %w", label, err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultValue, nil
	}
	return value, nil
}

func readInt(reader *bufio.Reader, label string, defaultValue int) (int, error) {
	value, err := readString(reader, label, strconv.Itoa(defaultValue))
	if err != nil {
		return 0, err
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a whole number: %w", label, err)
	}
	return number, nil
}
