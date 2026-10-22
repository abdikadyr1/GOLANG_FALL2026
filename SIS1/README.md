# Trail Ration Planner

A small Go task-processing project that plans calories for hiking trips. It demonstrates a domain model, interfaces, an in-memory repository, lifecycle state transitions, validation, wrapped errors, and unit tests.

## Run

```sh
go run ./cmd/trailplanner
go test ./...
```

The demo prompts for a trail name, highest point elevation, days, hikers, and daily calories. Press Enter at any prompt to use its displayed example value.

## Task lifecycle

`queued → processing → completed`

If a planner returns an error, the task moves from `processing` to `failed`. A task can only be processed while queued, so repeated processing is rejected.

## Domain rules

A trip needs a trail name, a highest point between 0 and 9,000 meters, 1–30 days, 1–20 hikers, and a target of 1,000–6,000 kcal per hiker per day. The planner increases the daily calorie target by 5% per 1,000 meters of highest-point elevation (proportionally, so 500 m adds 2.5%). It multiplies that adjusted daily amount by trip days and hikers to get the group total.

`Repository` and `Planner` are interfaces so the service can use alternate storage or planning implementations. The default `MemoryRepository` stores tasks in a map keyed by unique task IDs. Processing is synchronous; the in-memory map and ID counter are intentionally kept simple for this checkpoint.
