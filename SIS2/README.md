# Trail Ration Planner — Checkpoint 2 (Concurrent Processing)

A Go task-processing application that plans calories for hiking trips. Each task is a trip (trail, highest point, days, hikers, kcal per day). Processing produces a ration plan: the daily target is raised by 5% per 1,000 m of elevation (proportionally), then multiplied by days and hikers.

Checkpoint 2 keeps all Checkpoint 1 behaviour (validation, unique IDs, in-memory storage, retrieval, synchronous `Process`, error handling, tests) and adds concurrent processing with a worker pool.

## Run

```sh
go run ./cmd/trailplanner   
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
```

## Architecture

```text
 CreateAndSubmit / Submit(id)
            |
            v   pending -> queued (atomic, under repo lock)
   +-------------------------+
   | queue: chan TaskID (5)  |   bounded, buffered channel
   +-------------------------+
        |        |        |
        v        v        v
    Worker 1  Worker 2  Worker 3      fixed number of goroutines
        |        |        |
        v        v        v
   queued -> processing -> Plan() -> completed / failed
        \        |        /
         v       v       v
   +-------------------------+
   | MemoryRepository        |   map[TaskID]Task guarded by sync.RWMutex
   +-------------------------+
            ^
            |  Get(id) — works while tasks are being processed
```

- **Creating tasks:** `Service.Create(trip)` validates the trip, assigns a unique ID (`atomic.Uint64` counter) and stores the task as `pending`.
- **Submitting:** `WorkerPool.Submit(id)` (or `CreateAndSubmit(trip)`) moves the task to `queued`, puts its ID in the queue and returns right away. It does not wait for processing.
- **Workers:** `NewWorkerPool(service, workers, capacity)` starts a fixed number of goroutines (the demo uses **3**; at least 2 are required). Each worker loops `for id := range queue`, so it keeps taking tasks until the queue is closed and empty.
- **Queue capacity:** **5** in the demo (configurable, must be ≥ 1). The channel buffer is the only queue, so it cannot grow.
- **Full queue:** submission is **rejected** with `ErrQueueFull` (non-blocking `select` with `default`). The task goes back to `pending`, so it can be submitted again later. It is never left `queued` or `processing`.

## Task lifecycle

```text
pending --Submit--> queued --worker--> processing --> completed
   ^                  |                         \--> failed   (planner error)
   +---- queue full --+
pending --Process (synchronous, CP1)--> processing --> completed / failed
```

| From | To | Trigger |
|---|---|---|
| pending | queued | `Submit` accepted |
| queued | pending | queue was full, submission rolled back |
| queued | processing | a worker takes the task |
| pending | processing | synchronous `Service.Process` |
| processing | completed | plan succeeded, result stored |
| processing | failed | planner returned an error, message stored |

Any other transition returns `ErrInvalidState`. This includes submitting a task that is already queued, processing, completed or failed. Retries are not supported.

## Synchronization

- **Repository:** `MemoryRepository` guards its map with a `sync.RWMutex`: `Get` takes a read lock, writes take the write lock. Tasks are copied on the way in and out (including the `Result` pointer), so no goroutine shares a mutable `Task` with the store.
- **Atomic transitions:** `Repository.Modify(id, fn)` runs check-and-update under one write lock. Because of this, `pending → queued` and `queued → processing` cannot interleave. Exactly one goroutine wins, and a task is never processed by two workers.
- **Locks during planning:** no lock is held while `Plan` runs. A worker locks briefly to mark the task `processing`, plans, then locks briefly again to store the result in that same task.
- **Pool state:** `WorkerPool.mu` guards the `closed` flag. Sending to the queue happens only while holding `mu` with `closed == false`.
- **ID counter:** task IDs come from `atomic.Uint64`.

## Stopping workers

`WorkerPool.Shutdown()`:

1. Locks `mu`, sets `closed = true` and closes the queue channel. Closing happens once, through `sync.Once`. New `Submit` calls now return `ErrPoolClosed`.
2. Workers finish the task they are running, drain everything still in the queue, and exit when `range` ends.
3. `sync.WaitGroup.Wait()` blocks until every worker has exited.

The pool owns the channel and only `Shutdown` closes it. Because sends and the close are serialized by `mu`, nothing can send to a closed channel, so there is no panic. A second call to `Shutdown` is a no-op and still waits. Tasks accepted before shutdown are always completed (or marked failed). They are never dropped.

## Tests

`task_test.go` holds the Checkpoint 1 tests. `pool_test.go` covers:

- **Startup and config:** pool startup and config validation.
- **Processing:** asynchronous submission and processing of many tasks, with correct results per task.
- **Real overlap:** two workers are blocked inside `Plan` at the same time. The test checks exactly 2 `processing` + 2 `queued`, and that peak concurrency equals the worker limit.
- **Invalid submissions:** duplicate or invalid submits (missing, queued, processing, completed).
- **Full queue:** returns `ErrQueueFull` and the task stays `pending`.
- **Planner error:** the task becomes `failed`.
- **Concurrent access:** concurrent `CreateAndSubmit` + `Get` from 100 goroutines, plus concurrent `Process` of one task (runs exactly once).
- **Shutdown:** drains accepted tasks and waits for workers. Later submissions are rejected, double shutdown is safe, and `Submit` racing with `Shutdown` does not panic.

The tests coordinate with channels and `WaitGroup`. Timeouts appear only as a guard against a hanging test.
