# Poller — SNMP Polling Engine

## Position in the Pipeline

```
Config → Scheduler → [Poller] → Decoder → Producer → Formatter → Transport
```

The poller sits between the scheduler and the decoder. It receives `PollJob`
values, executes the appropriate SNMP operation, and emits `RawPollResult`
messages into the decoder channel.

## Package Layout

```
pkg/snmpcollector/poller/
├── session.go    — gosnmp session factory (DeviceConfig → *gosnmp.GoSNMP)
├── pool.go       — per-device connection pool with concurrency limiting
├── poller.go     — Poller interface + SNMPPoller (Get / Walk / BulkWalk)
├── worker.go     — WorkerPool fan-out dispatcher
└── poller_test.go — 14 unit tests
```

## Key Types

### PollJob

```go
type PollJob struct {
    Hostname     string
    Device       models.Device
    DeviceConfig config.DeviceConfig
    ObjectDef    models.ObjectDefinition
}
```

A unit of work dispatched by the scheduler. One job = one SNMP request for one
object on one device.

### Poller (interface)

```go
type Poller interface {
    Poll(ctx context.Context, job PollJob) (decoder.RawPollResult, error)
}
```

The only method. Returns a `RawPollResult` containing the raw `gosnmp.SnmpPDU`
slice and timing metadata. Implementations must be safe for concurrent use.

### SNMPPoller

Production `Poller` backed by a `ConnectionPool`. Operation selection:

| Condition | SNMP Operation | Method |
|---|---|---|
| Scalar object (no Index) | **Get** | `gosnmp.Get()` with `.0` suffix |
| Table + SNMPv1 | **Walk** | `gosnmp.WalkAll()` |
| Table + v2c / v3 | **BulkWalk** | `gosnmp.BulkWalkAll()` |

The root OID for Walk/BulkWalk is computed as the lowest common prefix of all
attribute OIDs in the object definition.

### ConnectionPool

Per-device pool of `*gosnmp.GoSNMP` sessions.

```go
pool := poller.NewConnectionPool(poller.PoolOptions{
    MaxIdlePerDevice: 2,
    IdleTimeout:      30 * time.Second,
}, logger)

conn, err := pool.Get(ctx, "switch1", deviceCfg)
// ... use conn ...
pool.Put("switch1", conn)      // return to pool
pool.Discard("switch1", conn)  // discard broken connection
pool.Close()                   // drain all sessions
```

Features:

- **Concurrency limiting**: per-device semaphore sized to
  `DeviceConfig.MaxConcurrentPolls` (default 4).
- **LIFO reuse**: most recently returned sessions are reused first, letting
  older idle connections expire.
- **Idle timeout**: connections older than `IdleTimeout` are discarded on next
  Get and a new session is dialled.
- **Custom dialer**: inject `PoolOptions.Dial` for tests.

### WorkerPool

Fan-out dispatcher. N goroutines read `PollJob` from a buffered channel, call
`Poller.Poll()`, and send `RawPollResult` into an output channel.

```go
output := make(chan decoder.RawPollResult, 10000)
wp := poller.NewWorkerPool(500, snmpPoller, output, logger)
wp.Start(ctx)
wp.Submit(job)   // blocking
wp.TrySubmit(job) // non-blocking
wp.Stop()        // close + drain
```

Failed polls with no varbinds are logged but **not** forwarded to the decoder
to avoid flooding with empty messages.

## Session Factory

`NewSession(cfg DeviceConfig)` builds a connected `*gosnmp.GoSNMP`:

| DeviceConfig field | gosnmp mapping |
|---|---|
| IP | Target |
| Port | Port (uint16) |
| Timeout | Timeout (ms → time.Duration) |
| Retries | Retries |
| ExponentialTimeout | ExponentialTimeout |
| Version "1" | Version1 + Community |
| Version "2c" | Version2c + Community |
| Version "3" | Version3 + USM security params |

SNMPv3 message flags are derived from the credential's auth/priv protocols:

- auth + priv → `AuthPriv`
- auth only → `AuthNoPriv`
- neither → `NoAuthNoPriv`

## Concurrency Contract

- `SNMPPoller` and `ConnectionPool` are safe for concurrent use.
- `WorkerPool.Submit()` may be called from any goroutine.
- `WorkerPool.Stop()` must be called exactly once after calling `Start()`.

## Tests (14 total)

| Test | What it verifies |
|---|---|
| `TestLowestCommonOID/*` | 4 subtests: single, two siblings, divergent, empty |
| `TestNewSession_UnsupportedVersion` | Error on version "4" |
| `TestConnectionPool_GetPut` | Session reuse (LIFO) |
| `TestConnectionPool_MaxIdleEviction` | Excess idle connections are closed |
| `TestConnectionPool_ConcurrencyLimit` | Semaphore blocks at max concurrent |
| `TestConnectionPool_IdleTimeout` | Stale sessions are replaced |
| `TestConnectionPool_Close` | Get after Close returns error |
| `TestConnectionPool_DialError` | Dial failure releases semaphore slot |
| `TestSNMPPoller_ScalarUsesGet` | Scalar vs table detection |
| `TestWorkerPool_Dispatch` | N jobs → N results |
| `TestWorkerPool_ContextCancel` | Workers exit on cancellation |
| `TestWorkerPool_TrySubmit_Full` | Non-blocking submit when full |
| `TestWorkerPool_PollError_NoVarbinds` | Empty results not forwarded |
| `TestPollJob_Fields` | PollJob construction |
