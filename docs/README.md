# SNMP Collector — Developer Documentation

## Quick Start

### Prerequisites

- Go 1.21+
- `snmpd` (optional, for local testing): `sudo apt install snmpd snmp`

### Build

```bash
go build -o snmpcollector ./cmd/snmpcollector/
```

### Run (polling only)

```bash
./snmpcollector \
  -config.devices=./testdata/devices \
  -config.device.groups=./testdata/device_groups \
  -config.object.groups=./testdata/object_groups \
  -config.objects=./testdata/objects \
  -config.enums=./testdata/enums \
  -log.level=debug \
  -log.fmt=text \
  -format.pretty \
  -poller.workers=2
```

### Run (polling + trap receiver)

Trap receiver requires a UDP listener. Use a port ≥ 1024 to avoid root:

```bash
./snmpcollector \
  -config.devices=./testdata/devices \
  -config.device.groups=./testdata/device_groups \
  -config.object.groups=./testdata/object_groups \
  -config.objects=./testdata/objects \
  -config.enums=./testdata/enums \
  -log.level=debug \
  -log.fmt=text \
  -format.pretty \
  -trap.enabled \
  -trap.listen=0.0.0.0:1620 \
  -poller.workers=2
```

To use the standard trap port 162 without root, grant the binary the capability:

```bash
sudo setcap 'cap_net_bind_service=+ep' ./snmpcollector
./snmpcollector -trap.enabled -trap.listen=0.0.0.0:162 ...
```

### Device configuration

Each device is defined in a YAML file under the devices directory. Example `testdata/devices/localhost.yml`:

```yaml
myswitch.lab:
  ip: 127.0.0.1
  port: 161
  poll_interval: 30
  timeout: 3000
  retries: 2
  version: 2c
  communities:
    - public
  device_groups:
    - generic
  max_concurrent_polls: 2
```

Optional fields fall back to hard-coded defaults: `port=161`, `poll_interval=60`, `timeout=3000`, `retries=2`, `version=2c`, `max_concurrent_polls=4`.

### Sending test traps

Install `snmp` tools if not already available: `sudo apt install snmp`

```bash
# v2c linkDown
snmptrap -v 2c -c public 127.0.0.1:1620 '' \
  1.3.6.1.6.3.1.1.5.3 \
  1.3.6.1.2.1.2.2.1.1 i 2 \
  1.3.6.1.2.1.2.2.1.2 s "GigabitEthernet0/1" \
  1.3.6.1.2.1.2.2.1.8 i 2

# v2c linkUp
snmptrap -v 2c -c public 127.0.0.1:1620 '' \
  1.3.6.1.6.3.1.1.5.4 \
  1.3.6.1.2.1.2.2.1.1 i 2 \
  1.3.6.1.2.1.2.2.1.2 s "GigabitEthernet0/1" \
  1.3.6.1.2.1.2.2.1.8 i 1

# v2c coldStart
snmptrap -v 2c -c public 127.0.0.1:1620 '' \
  1.3.6.1.6.3.1.1.5.1

# v1 enterprise trap
snmptrap -v 1 -c public 127.0.0.1:1620 \
  1.3.6.1.4.1.8072 127.0.0.1 6 1 '' \
  1.3.6.1.2.1.2.2.1.1 i 5
```

### CLI flags reference

| Flag | Default | Description |
|------|---------|-------------|
| `-log.level` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `-log.fmt` | `json` | Log format: `json`, `text` |
| `-collector.id` | hostname | Collector instance ID |
| `-format.pretty` | `false` | Pretty-print JSON output |
| `-poller.workers` | `500` | Concurrent poller workers |
| `-pipeline.buffer.size` | `10000` | Inter-stage channel buffer |
| `-trap.enabled` | `false` | Enable trap receiver |
| `-trap.listen` | `0.0.0.0:162` | Trap listener UDP address |
| `-processor.enum.enable` | `false` | Enable enum resolution |
| `-processor.counter.delta` | `true` | Enable counter delta computation |
| `-snmp.pool.max.idle` | `2` | Max idle connections per device |
| `-snmp.pool.idle.timeout` | `30` | Idle connection timeout (seconds) |
| `-config.devices` | env / `/etc/snmp_collector/snmp/devices` | Devices directory |
| `-config.device.groups` | env / `/etc/snmp_collector/snmp/device_groups` | Device groups directory |
| `-config.object.groups` | env / `/etc/snmp_collector/snmp/object_groups` | Object groups directory |
| `-config.objects` | env / `/etc/snmp_collector/snmp/objects` | Object definitions directory |
| `-config.enums` | env / `/etc/snmp_collector/snmp/enums` | Enum definitions directory |

### Running tests

```bash
go test ./... -count=1              # all tests
go test ./pkg/snmpcollector/config/ -v  # single package, verbose
go test ./... -race -count=1        # with race detector
```

---

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
