# `producer/metrics` — Metrics Producer

## Overview

The `producer/metrics` package is **Stage 4** of the SNMP Collector pipeline. It consumes
`decoder.DecodedPollResult` values (output from Stage 3 — the SNMP Decoder) and produces
`models.SNMPMetric` values ready for the JSON Formatter.

```
Decoder [Stage 3]
     │  DecodedPollResult
     ▼
producer/metrics.MetricsProducer [Stage 4]
     │  models.SNMPMetric
     ▼
format/json [Stage 5]
```

The package is composed of four source files:

| File | Responsibility |
|---|---|
| `enrich.go` | `EnumRegistry` — translates raw integer/OID values to text labels |
| `normalize.go` | `CounterState` — tracks cumulative counter baselines and emits per-interval deltas |
| `poll.go` | `Build()` — assembles a complete `SNMPMetric` from decoded varbinds |
| `producer.go` | `MetricsProducer` — pipeline-facing interface + orchestration |

---

## Data Flow

```
DecodedPollResult
  ├── Device (forwarded verbatim)
  ├── ObjectDefKey → SNMPMetric.ObjectDefKey
  ├── CollectedAt → SNMPMetric.Timestamp
  ├── PollDurationMs → MetricMetadata.PollDurationMs
  └── Varbinds []DecodedVarbind
        │
        ├─ IsTag=true  →  tagsByInstance map  (per-instance tag map)
        └─ IsTag=false →  metricsByInstance map
              │
              ├─ override resolution  (higher syntaxPriority wins)
              ├─ enum resolution      (EnumRegistry.Resolve, if enabled)
              ├─ counter delta        (CounterState.Delta, if enabled)
              └─ tag copy             (instance tags → Metric.Tags)
                    │
                    ▼
             []models.Metric  →  models.SNMPMetric
```

---

## EnumRegistry (`enrich.go`)

`EnumRegistry` maps raw SNMP values to human-readable labels. It supports three enum
patterns defined in the architecture:

### Integer enum (`EnumInteger` syntax)

```go
r := metrics.NewEnumRegistry()
r.RegisterIntEnum("1.3.6.1.2.1.2.2.1.8", false, map[int64]string{
    1: "up",
    2: "down",
    3: "testing",
})

r.Resolve("1.3.6.1.2.1.2.2.1.8", int64(1))  // → "up"
r.Resolve("1.3.6.1.2.1.2.2.1.8", int64(99)) // → int64(99)  (passthrough)
```

### Bitmap enum (`EnumBitmap` syntax)

Bits are resolved in ascending order and joined with a comma. If **no** bits match, the
raw integer is returned unchanged.

```go
r.RegisterIntEnum("1.3.6.1.2.1.10.166.3.2.10.1.5", true, map[int64]string{
    0: "PDR",
    1: "PBS",
    2: "CDR",
})

// mask = 0b101 (bits 0 and 2)
r.Resolve("1.3.6.1.2.1.10.166.3.2.10.1.5", int64(5)) // → "PDR,CDR"
```

### OID enum (`EnumObjectIdentifier` syntax)

The *value* of the varbind is an OID string. The `oid` key passed to `RegisterOIDEnum`
is the OID value to match; the `label` is the replacement string.

```go
r.RegisterOIDEnum("1.3.6.1.2.1.25.2.1.2", "RAM")

// varbind value = OID string
r.Resolve("any", "1.3.6.1.2.1.25.2.1.2")     // → "RAM"
r.Resolve("any", ".1.3.6.1.2.1.25.2.1.2")    // → "RAM"  (leading dot normalised)
```

### Thread safety

`EnumRegistry` uses `sync.RWMutex`. Register all enums at startup before the producer
workers start; concurrent reads during `Resolve` are fully safe.

### Interface

```go
func NewEnumRegistry() *EnumRegistry
func (r *EnumRegistry) RegisterIntEnum(oid string, isBitmap bool, values map[int64]string)
func (r *EnumRegistry) RegisterOIDEnum(oid, label string)
func (r *EnumRegistry) Resolve(oid string, rawValue interface{}) interface{}
```

---

## CounterState (`normalize.go`)

`CounterState` maintains per-`(Device, Attribute, Instance)` baselines for SNMP
cumulative counters and computes per-interval deltas.

### Baseline seeding

The first call for a given key stores the value but returns `Valid=false`. The second
call computes the delta and returns `Valid=true`. This prevents a spurious large spike
after a restart.

### Wrap detection

If `current < previous`, the counter has wrapped exactly once:

```
delta = (wrapValue − previous) + current + 1
```

| Syntax | Wrap value |
|---|---|
| `Counter32` | `4 294 967 295` (2³²−1) |
| `Counter64` | `18 446 744 073 709 551 615` (2⁶⁴−1) |

### Key type

```go
type CounterKey struct {
    Device    string   // models.Device.Hostname
    Attribute string   // DecodedVarbind.AttributeName
    Instance  string   // DecodedVarbind.Instance
}
```

### DeltaResult

```go
type DeltaResult struct {
    Delta   uint64
    Elapsed time.Duration
    Valid   bool   // false on first observation
}
```

### Lifecycle helpers

```go
func (cs *CounterState) Remove(key CounterKey)           // Remove a single baseline
func (cs *CounterState) Purge(maxAge time.Duration, now time.Time) int
// Purge removes all baselines not updated within maxAge; returns the number removed.
// Call periodically (e.g. every 10× poll interval) to reclaim memory for
// decommissioned interfaces.
```

### Syntax helpers (package-level)

```go
func IsCounterSyntax(syntax string) bool     // Counter32 | Counter64
func WrapForSyntax(syntax string) uint64     // wrap constant for wrap detection
func syntaxPriority(syntax string) int       // used by Build() for override resolution
```

---

## Build() — Poll Assembly (`poll.go`)

`Build` is a pure function (no internal state) that converts a `DecodedPollResult` into
a `models.SNMPMetric` in five logical steps.

```go
func Build(decoded decoder.DecodedPollResult, opts BuildOptions) models.SNMPMetric
```

### BuildOptions

```go
type BuildOptions struct {
    CollectorID string          // written to MetricMetadata.CollectorID
    PollStatus  string          // written to MetricMetadata.PollStatus ("success", "timeout", …)
    Enums       *EnumRegistry   // nil disables enum resolution
    Counters    *CounterState   // nil disables counter delta
}
```

### Assembly steps

**Step 1 — Partition by tag / metric.**
Varbinds with `IsTag=true` are collected into a per-instance `map[string]string`. Varbinds
with `IsTag=false` are grouped into `metricsByInstance`.

**Step 2 — Override resolution.**
When two varbinds share the same `(AttributeName, Instance)` pair (e.g. both `Counter32`
and `Counter64` rows are present), `syntaxPriority` picks the winner:

| Syntax | Priority |
|---|---|
| `Counter64` | 20 |
| `BandwidthGBits` | 15 |
| `BandwidthMBits` | 14 |
| `BandwidthKBits` | 13 |
| `BandwidthBits` | 12 |
| `Gauge32` / `BandwidthBits` | 11 |
| `Counter32` | 10 |
| Everything else | 0 |

**Step 3 — Enum resolution.**
For varbinds with `EnumInteger`, `EnumBitmap`, or `EnumObjectIdentifier` syntax (and
`opts.Enums != nil`), the raw value is replaced with the label returned by
`EnumRegistry.Resolve`. The instance suffix is stripped from the varbind OID before the
lookup so that registrations use the base attribute OID.

**Step 4 — Counter delta.**
For varbinds with `Counter32` or `Counter64` syntax (and `opts.Counters != nil`), the
raw cumulative value is replaced with the delta since the last poll:

- First poll: delta = `uint64(0)`, metric is still emitted so the series is established.
- Subsequent polls: delta = current − previous (or wrap-adjusted).

**Step 5 — Metric assembly.**
Each non-tag varbind (after steps 2-4) becomes a `models.Metric`:

```go
models.Metric{
    OID:      vb.OID,            // full instance OID, e.g. "1.3.6.1.2.1.2.2.1.10.1"
    Name:     vb.AttributeName,  // e.g. "netif.bytes.in"
    Instance: instance,          // e.g. "1"
    Value:    value,             // raw | enum label | counter delta
    Type:     vb.SNMPType,       // e.g. "Counter64"
    Syntax:   vb.Syntax,         // e.g. "Counter64"
    Tags:     instanceTags,      // copy of the per-instance tag map
}
```

---

## MetricsProducer (`producer.go`)

`MetricsProducer` is the component exposed to the pipeline runner.

### Interface

```go
type Producer interface {
    Produce(decoded decoder.DecodedPollResult) (models.SNMPMetric, error)
}
```

### Config

```go
type Config struct {
    CollectorID         string
    EnumEnabled         bool          // false → Enums field ignored
    Enums               *EnumRegistry // pre-populated enum registry
    CounterDeltaEnabled bool          // false → raw cumulative values emitted
}
```

### Construction

```go
func New(cfg Config, logger *slog.Logger) *MetricsProducer
```

- If `logger` is `nil`, a no-op writer is substituted (never panics).
- `CounterState` is allocated only when `CounterDeltaEnabled=true`.
- `EnumRegistry` is used as-is from `cfg.Enums`; ownership is not transferred.

### Usage

```go
reg := metrics.NewEnumRegistry()
// populate reg from YAML config …

producer := metrics.New(metrics.Config{
    CollectorID:         "collector-01",
    EnumEnabled:         true,
    Enums:               reg,
    CounterDeltaEnabled: true,
}, slog.Default())

// In the pipeline worker goroutine:
result, err := producer.Produce(decoded)
if err != nil {
    // err is informational; result is a best-effort partial SNMPMetric
}
```

### Logging

`Produce` emits a single `slog.Debug` entry on success:

```json
{"level":"DEBUG","msg":"produced metrics","collector_id":"collector-01",
 "object_def":"IF-MIB::ifEntry","metric_count":5,"poll_duration_ms":245}
```

---

## Concurrency

`MetricsProducer.Produce` is safe to call from 100+ concurrent goroutines:

- `EnumRegistry` — `sync.RWMutex` for concurrent reads during `Resolve`.
- `CounterState` — `sync.Mutex` per `Delta`/`Remove`/`Purge` call.
- `Build` itself is stateless.

Callers do **not** need external synchronisation.

---

## Edge cases

| Scenario | Behaviour |
|---|---|
| Varbind with unknown enum OID | Raw value returned unchanged (passthrough) |
| Counter first observation | `Value = uint64(0)`, metric emitted, state seeded |
| Counter wrap (current < previous) | Delta computed with wrap arithmetic |
| Two varbinds same name+instance | Higher `syntaxPriority` wins; lower is discarded |
| `Enums = nil` | Enum step skipped entirely for all varbinds |
| `Counters = nil` | Counter step skipped; raw cumulative values forwarded |
| Empty varbind list | Empty `Metrics` slice, metadata still populated |
| `logger = nil` | No-op logger used; no panic |
