# SNMP Collector — Developer Documentation

## Module docs

| Document | What it covers |
|---|---|
| [models.md](models.md) | Core data structures (`SNMPMetric`, `Device`, `Metric`, `SNMPTrap`), configuration types (`ObjectDefinition`, `AttributeDefinition`), design constraints |
| [decoder.md](decoder.md) | SNMP response decoder — pipeline position, `RawPollResult` / `DecodedPollResult` channel types, `VarbindParser` OID matching, `ConvertValue` syntax-to-Go-type table, error handling, usage examples |
| [producer.md](producer.md) | Metrics producer — `EnumRegistry` (integer / bitmap / OID enums), `CounterState` (delta + wrap detection), `Build()` assembly steps, `MetricsProducer` interface, concurrency contract |
| [formatter.md](formatter.md) | JSON formatter — `Formatter` interface, `Config`, `Format()` schema, timestamp format, value type preservation, pretty-print, concurrency contract |
| [poller.md](poller.md) | SNMP poller — `Poller` interface, `ConnectionPool`, `WorkerPool`, session factory, operation selection (Get/Walk/BulkWalk), concurrency contract |
| [scheduler.md](scheduler.md) | Polling scheduler — `Scheduler`, `JobSubmitter` interface, `ResolveJobs()` config hierarchy resolution, timer management, hot reload |
| [trap.md](trap.md) | SNMP trap protocol parser — v1/v2c/v3 PDU → `models.SNMPTrap`, RFC 3584 TrapOID synthesis, varbind value type mapping, error PDU handling |
| [trapreceiver.md](trapreceiver.md) | Trap receiver — `TrapReceiver` lifecycle (`Start`/`Stop`/`Output`), `Config`, injectable `ParseFunc`, concurrency contract |

## Pipeline stages (build order)

```
[1] models/                    ← done  — shared data contract (no deps)
[2] snmp/decoder/              ← done  — PDU → DecodedVarbind
[3] producer/metrics/          ← done  — DecodedVarbind → SNMPMetric
[4] format/json/               ← done  — SNMPMetric → []byte (JSON)
[5] transport/file/            ← done  — stdout/file writer (Kafka deferred)
[6] pkg/snmpcollector/config/  ← done  — YAML config loader
[7] pkg/snmpcollector/poller/  ← done  — SNMP Get/Bulk/Walk + connection pool
[8] pkg/snmpcollector/scheduler/ ← done — polling job queue
[ ] transport/kafka/                   — deferred — Kafka producer
[9] snmp/trap/                      ← done  — raw SnmpPacket → models.SNMPTrap (v1/v2c/v3)
[10] pkg/snmpcollector/trapreceiver/ ← done  — UDP TrapListener + output channel
[11] pkg/snmpcollector/app/         ← done  — wiring + lifecycle
[12] cmd/snmpcollector/             ← done  — binary entry point
```
