# Trap Receiver — `pkg/snmpcollector/trapreceiver`

## Overview

`trapreceiver` is a **standalone UDP SNMP trap input**. It is completely
independent of the polling path — where the poller actively requests data from
devices on a schedule, the trap receiver passively listens for asynchronous
notifications pushed by devices.

Both inputs converge only at the downstream format/transport stages.

```
UDP port 162  →  [TrapReceiver]  →  chan models.SNMPTrap
                                             │
                     (parallel with polling path)
                                             ↓
                              format/json  →  transport
```

Protocol-level parsing (v1/v2c/v3 PDU differences) is delegated to the
`snmp/trap` package — see [trap.md](trap.md).

---

## Config

```go
type Config struct {
    ListenAddr       string             // default "0.0.0.0:162"
    OutputBufferSize int                // default 10 000
    Community        string             // v1/v2c community (empty = accept all)
    SNMPVersion      gosnmp.SnmpVersion // default Version2c
    CloseTimeout     time.Duration      // default 3 s
    ParseFunc        ParseFunc          // nil = snmptrap.Parse (injectable for tests)
}
```

### Defaults applied by `New()`

| Field | Default |
|---|---|
| `ListenAddr` | `"0.0.0.0:162"` |
| `OutputBufferSize` | `10 000` |
| `SNMPVersion` | `gosnmp.Version2c` |
| `CloseTimeout` | `3 s` |
| `ParseFunc` | `snmptrap.Parse` |

---

## TrapReceiver API

### Constructor

```go
func New(cfg Config, logger *slog.Logger) *TrapReceiver
```

`logger` may be `nil`; a no-op logger is used in that case.

### Methods

| Method | Description |
|---|---|
| `Start(ctx context.Context) error` | Binds the UDP socket and begins accepting traps. Blocks until the socket is ready or an error occurs. Cancelling `ctx` calls `Stop()` automatically. |
| `Stop()` | Closes the UDP listener and the output channel. Safe to call multiple times (idempotent). Waits for the listen goroutine to exit before closing the channel. |
| `Output() <-chan models.SNMPTrap` | Read-only channel of parsed traps. Closed when `Stop()` completes. |
| `ListenAddr() string` | Returns the configured listen address. |

### ParseFunc

```go
type ParseFunc func(pkt *gosnmp.SnmpPacket, addr *net.UDPAddr) (models.SNMPTrap, error)
```

The injectable parse function allows tests to bypass real UDP parsing. The
default implementation is `snmptrap.Parse` (see [trap.md](trap.md)).

---

## Lifecycle

```
New(cfg, logger)
        │
        ▼
Start(ctx) ─── binds UDP ──► Listening() signal ──► returns nil
        │                              │
        │                    goroutine: ctx.Done() → Stop()
        ▼
  traps arrive → handleTrap() → ParseFunc → Output() channel
        │
Stop() ─── Close listener ──► wait doneCh ──► close(output)
```

- `Start` returns an error if the address is already in use or invalid.
- `Stop` waits for the gosnmp listen goroutine to exit before closing the
  output channel, ensuring no write-after-close panic.
- When the output buffer is full, the trap is dropped and a warning is logged
  (no blocking in the handler goroutine).

---

## Error handling

| Situation | Behaviour |
|---|---|
| Bad listen address | `Start()` returns error; receiver is not started |
| Double `Start()` | Returns `"trapreceiver: already running"` |
| Parse error from `ParseFunc` | Warning logged; trap not emitted |
| Output buffer full | Warning logged; trap dropped (non-blocking) |
| UDP socket error after start | gosnmp closes; context watcher calls `Stop()` |

---

## Concurrency contract

- `New()`, `Start()`, `Stop()`, `Output()`, `ListenAddr()` are safe to call
  from any goroutine.
- `Start()` and `Stop()` are guarded by a mutex; double-call of either is safe.
- The output channel is closed exactly once (after `Stop()` confirms the listen
  goroutine has exited).
- `handleTrap` runs in the gosnmp internal goroutine; it must not block.

---

## Usage example

```go
cfg := trapreceiver.Config{
    ListenAddr: "0.0.0.0:162",
    Community:  "public",
}

r := trapreceiver.New(cfg, slog.Default())
if err := r.Start(ctx); err != nil {
    log.Fatal(err)
}

for trap := range r.Output() {
    // forward to formatter
    data, _ := formatter.Format(trap)
    writer.Write(data)
}
```

---

## Tests (12 total)

| Test | What it verifies |
|---|---|
| `TestNew_NonNil` | `New()` returns non-nil with valid output channel |
| `TestNew_DefaultListenAddr` | Default listen address is applied |
| `TestStart_BindsAndReturnsNil` | Happy-path `Start()` on a free port |
| `TestStop_ClosesOutputChannel` | `Stop()` closes the output channel |
| `TestStop_Idempotent` | Double `Stop()` does not panic or deadlock |
| `TestStart_AlreadyRunning_ReturnsError` | Second `Start()` returns an error |
| `TestStart_BadAddr_ReturnsError` | Invalid address returns error immediately |
| `TestContextCancel_ClosesOutput` | Cancelling `ctx` shuts the receiver down |
| `TestParseError_NoEmit` | Parse errors produce nothing on the output channel |
| `TestOutputBufferSize_Capacity` | `OutputBufferSize` config is honoured |
| `TestListenAddr_MatchesConfig` | `ListenAddr()` returns the configured address |
| `TestRealUDP_V2c_TrapDelivered` | Full UDP round-trip: send v2c trap → receive on output channel |
