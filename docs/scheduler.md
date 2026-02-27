# Scheduler — Interval-Based Poll Job Dispatch

## Position in the Pipeline

```
Config → [Scheduler] → Poller → Decoder → Producer → Formatter → Transport
```

The scheduler sits between config and the poller. It resolves the device →
device group → object group → object definition hierarchy into concrete
`PollJob` values, manages per-device timers, and dispatches jobs into the
`WorkerPool` at each device's configured `PollInterval`.

## Package Layout

```
pkg/snmpcollector/scheduler/
├── resolve.go        — ResolveJobs(): config hierarchy → flat PollJob list
├── scheduler.go      — Scheduler loop, timer management, Reload
└── scheduler_test.go — 16 unit tests
```

## Config Hierarchy Resolution

```
DeviceConfig.DeviceGroups []string
  └── DeviceGroup.ObjectGroups []string
        └── ObjectGroup.Objects []string
              └── ObjectDefinition (from LoadedConfig.ObjectDefs)
```

`ResolveJobs()` walks this chain for every device and returns a flat,
deduplicated list of `PollJob` values:

```go
jobs := scheduler.ResolveJobs(cfg, logger)
// → one PollJob per (device, ObjectDefinition) pair
```

- Objects appearing via multiple groups are **deduplicated** per device.
- Missing groups or object definitions are logged and skipped (no panic).
- Output is sorted by hostname for deterministic ordering.

## Key Types

### JobSubmitter (interface)

```go
type JobSubmitter interface {
    Submit(poller.PollJob)
    TrySubmit(poller.PollJob) bool
}
```

Abstracts the `WorkerPool` dependency. The scheduler uses `TrySubmit()`
(non-blocking) so a full job queue causes a warning log rather than blocking
the timer loop.

### Scheduler

```go
s := scheduler.New(cfg, workerPool, logger)

ctx, cancel := context.WithCancel(context.Background())
go s.Start(ctx)    // blocks until ctx is cancelled

s.Reload(newCfg)   // hot reload (atomic)
s.Entries()        // number of active device entries

cancel()
s.Stop()           // waits for loop to exit
```

## Timer Management

The scheduler uses a **sort-to-next** approach:

1. Sort entries by `nextRun` (ascending).
2. Sleep until the earliest entry's `nextRun`.
3. On wake, dispatch all entries where `nextRun ≤ now`.
4. Advance each fired entry by its `interval`.
5. Repeat.

New devices (from init or `Reload`) get `nextRun = now` — they are polled
immediately.

## Hot Reload

```go
s.Reload(newCfg)
```

- **Added devices**: appear with `nextRun = now`.
- **Removed devices**: their entries vanish; no further jobs.
- **Changed intervals**: take effect from the next fire.
- **Changed object groups**: new job lists take effect immediately.

The replacement is protected by a mutex so it's safe to call from any
goroutine (e.g. an HTTP config-reload endpoint).

## Graceful Shutdown

1. Caller cancels the `context.Context`.
2. `Start()` loop exits, closes `done` channel.
3. `Stop()` returns.
4. The scheduler does **not** stop or close the `WorkerPool` — that is the
   app layer's responsibility.

## Tests (16 total)

| Test | What it verifies |
|---|---|
| `TestResolveJobs` | Single device → 1 job with correct fields |
| `TestResolveJobs_MultipleDevices` | Two devices → 2 jobs, sorted by hostname |
| `TestResolveJobs_Dedup` | Same object via two groups → 1 job |
| `TestResolveJobs_MissingGroup` | Unknown group name → 0 jobs, no panic |
| `TestResolveJobs_MissingObjectDef` | Unknown object key → 0 jobs, no panic |
| `TestResolveJobs_NilConfig` | nil config → nil result |
| `TestResolveJobs_MultipleObjects` | Two objects in one group → 2 jobs |
| `TestSchedulerFiresOnInterval` | Jobs dispatched at ~1s cadence |
| `TestSchedulerMultipleIntervals` | Different intervals → different fire rates |
| `TestSchedulerStop` | Context cancel → Start returns promptly |
| `TestSchedulerNoop` | Empty config → no dispatches, no panic |
| `TestSchedulerReload` | Add device mid-run → both devices fire |
| `TestSchedulerReload_RemoveDevice` | Remove device → entry count drops |
| `TestTrySubmitBackpressure` | Full queue → jobs dropped, not blocked |
| `TestSchedulerEntries` | Entries() reports correct count |
| `TestSchedulerConcurrentReload` | Concurrent Reload from 10 goroutines → no panics |
