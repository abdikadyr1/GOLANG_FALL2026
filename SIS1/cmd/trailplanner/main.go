package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	planner "trailplanner"
)

func main() {
	input, err := readTrip()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	service := planner.NewService(planner.NewMemoryRepository(), planner.RationPlanner{})
	task, err := service.Create(input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	completed, err := service.Process(task.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Task %s: %s\n", completed.ID, completed.State)
	fmt.Printf("Trail: %s | Highest point: %d m\n", completed.Result.Trail, completed.Result.HighestPointMeters)
	fmt.Printf("Daily target: %d kcal → %d kcal after elevation adjustment\n",
		completed.Result.BaseCaloriesPerDay, completed.Result.AdjustedCaloriesPerDay)
	fmt.Printf("Total: %d kcal | Per hiker: %d kcal\n",
		completed.Result.TotalCalories, completed.Result.PerHiker)
	fmt.Println(completed.Result.Tip)
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
