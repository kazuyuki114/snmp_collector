# SNMP + Trap Collector Architecture

## Overview

An SNMP + Trap Collector is a high-performance network monitoring system written in Go that collects telemetry data from network devices using SNMP polling and receives asynchronous notifications via SNMP traps. It normalizes the collected data into a unified format and forwards it to configured destinations for storage and analysis.

The architecture is designed for:
- **Scalability**: Distributed polling and trap collection across multiple instances
- **High Concurrency**: Leverages Go's goroutines for massive parallelism (3,000-5,000 concurrent operations)
- **Modularity**: Pluggable components for decoders, producers, formatters, and transports
- **Protocol Support**: SNMP v1, v2c, and v3 with trap/inform support
- **Flexibility**: Dynamic MIB loading and custom field mapping
- **Reliability**: Retry logic, failure handling, and persistent state
- **Performance**: Multi-threaded architecture with SO_REUSEPORT, worker pools, and non-blocking pipelines

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Network Devices & Agents                      │
│    (Routers, Switches, Servers, Network Appliances, IoT)       │
└──────────┬────────────────────────────────────────┬─────────────┘
           │                                         │
           │ SNMP Responses                         │ SNMP Traps
           │ (Port 161)                             │ (Port 162)
           ▼                                         ▼
┌─────────────────────────────────────────────────────────────────┐
│                    SNMP + Trap Collector                         │
│                                                                  │
│  ┌──────────────────────────┐    ┌───────────────────────────┐ │
│  │    SNMP Poller           │    │    Trap Listener          │ │
│  │  ┌────────────────┐      │    │  ┌─────────────────┐     │ │
│  │  │  Scheduler     │      │    │  │  UDP Receiver   │     │ │
│  │  │  - Devices     │      │    │  │  (Port 162)     │     │ │
│  │  │  - Intervals   │      │    │  └─────────────────┘     │ │
│  │  │  - Metrics     │      │    │           │               │ │
│  │  └────────────────┘      │    │           ▼               │ │
│  │         │                 │    │  ┌─────────────────┐     │ │
│  │         ▼                 │    │  │  Trap Decoder   │     │ │
│  │  ┌────────────────┐      │    │  │  - v1/v2c/v3    │     │ │
│  │  │  SNMP Client   │      │    │  │  - MIB Lookup   │     │ │
│  │  │  - Walk/Get    │      │    │  └─────────────────┘     │ │
│  │  │  - Bulk        │      │    │           │               │ │
│  │  └────────────────┘      │    └───────────┼───────────────┘
│  │         │                 │                │               │
│  │         ▼                 │                ▼               │
│  │  ┌────────────────┐      │       ┌────────────────┐       │
│  │  │ SNMP Decoder   │──────┼──────▶│   Producer     │       │
│  │  │ - PDU Parser   │      │       │  - Normalize   │       │
│  │  │ - MIB Resolver │      │       │  - Enrich      │       │
│  │  └────────────────┘      │       │  - Transform   │       │
│  └──────────────────────────┘       └────────────────┘       │
│                                              │                 │
│                                              ▼                 │
│                                      ┌────────────────┐        │
│                                      │   Formatter    │        │
│                                      │  - JSON        │        │
│                                      │  - Protobuf    │        │
│                                      │  - OpenMetrics │        │
│                                      └────────────────┘        │
│                                              │                 │
│                                              ▼                 │
│                                      ┌────────────────┐        │
│                                      │   Transport    │        │
│                                      │  - Kafka       │        │
│                                      │  - TimeSeries  │        │
│                                      │  - File        │        │
│                                      └────────────────┘        │
└─────────────────────────────────────────────────────────────────┘
                                              │
                                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Storage & Analytics                           │
│  (Prometheus, InfluxDB, Kafka, ClickHouse, Elasticsearch)       │
└─────────────────────────────────────────────────────────────────┘
```

## Directory Structure

### Application Layer

#### `cmd/` - Command-Line Applications

**`cmd/snmpcollector/`** - Main Collector Binary
```
cmd/snmpcollector/
├── main.go              # Entry point
├── devices.yaml         # Device inventory configuration
├── metrics.yaml         # Metrics to collect configuration
└── mibs/                # MIB files directory
    ├── SNMPv2-MIB.txt
    ├── IF-MIB.txt
    └── ...
```

- `main.go` - Initializes poller, trap listener, and transport
- `devices.yaml` - Target devices with credentials and polling intervals
- `metrics.yaml` - OIDs to poll and their metadata
- Purpose: Collects SNMP metrics and receives traps

**`cmd/mibtool/`** - MIB Management Utility
```
cmd/mibtool/
└── main.go              # MIB compiler and validator
```

- Purpose: Compile MIBs, validate OIDs, generate metric templates

**`cmd/trapd/`** - Standalone Trap Daemon
```
cmd/trapd/
└── main.go              # Lightweight trap-only receiver
```

- Purpose: Dedicated trap collector for high-volume trap environments

#### `pkg/snmpcollector/` - Core Application Components

**`pkg/snmpcollector/app/`** - Application orchestration
- Wires poller, trap listener, producer, and transport
- Manages lifecycle (startup, graceful shutdown)
- Coordinates goroutines and channels

**`pkg/snmpcollector/config/`** - Configuration management
- Device inventory parsing
- Metric definitions
- Credential management (passwords, community strings)
- Validation and defaults

**`pkg/snmpcollector/scheduler/`** - Polling scheduler
- Job queue management
- Interval-based triggering
- Priority queue for different polling frequencies
- Device grouping and batching

**`pkg/snmpcollector/poller/`** - SNMP polling logic
- SNMP Get/GetBulk/Walk operations
- Connection pooling per device
- Retry logic with exponential backoff
- Timeout handling

**`pkg/snmpcollector/trapreceiver/`** - Trap reception
- UDP listener on port 162
- Multi-threaded trap processing
- v1/v2c/v3 trap parsing
- Inform acknowledgment

**`pkg/snmpcollector/credentials/`** - Security management
- Community string management
- SNMPv3 user credentials (authPriv, authNoPriv)
- Key derivation and encryption
- Credential rotation support

**`pkg/snmpcollector/httpserver/`** - Management API
- Prometheus metrics endpoint
- Health checks
- Device status API
- Configuration reload endpoint
- Debug endpoints (active polls, trap stats)

### Protocol Layer

#### `snmp/` - SNMP Protocol Implementation

**`snmp/client/`** - SNMP Client
```go
snmp/client/
├── client.go            # Main client interface
├── v1.go                # SNMPv1 implementation
├── v2c.go               # SNMPv2c implementation
├── v3.go                # SNMPv3 implementation
├── pdu.go               # PDU encoding/decoding
├── transport.go         # UDP transport
└── pool.go              # Connection pooling
```

Features:
- Synchronous and asynchronous operations
- Timeout and retry configuration
- Rate limiting per device
- Context-aware cancellation

**`snmp/decoder/`** - SNMP Response Decoder
```go
snmp/decoder/
├── decoder.go           # Main decoder
├── types.go             # SNMP type conversions
├── ber.go               # BER/DER encoding
└── varbind.go           # Variable binding parser
```

Handles:
- Integer, Counter32/64, Gauge, TimeTicks
- OctetString, ObjectIdentifier
- IpAddress, Opaque
- NoSuchObject, NoSuchInstance, EndOfMibView

**`snmp/trap/`** - Trap Handler
```go
snmp/trap/
├── listener.go          # UDP listener
├── v1trap.go            # v1 trap parsing
├── v2trap.go            # v2c trap/inform parsing
├── v3trap.go            # v3 trap parsing
└── inform.go            # Inform acknowledgment
```

Features:
- Generic trap types (coldStart, warmStart, linkDown, etc.)
- Enterprise-specific trap handling
- Trap filtering and rules
- Duplicate trap detection

#### `mibs/` - MIB Management

**`mibs/parser/`** - MIB Parser
```go
mibs/parser/
├── parser.go            # MIB file parser (SMIv1/SMIv2)
├── lexer.go             # Tokenizer
├── ast.go               # Abstract syntax tree
└── resolver.go          # OID resolution
```

Features:
- Parse MIB files (ASN.1 notation)
- Build OID tree
- Resolve numeric OIDs to names
- Import dependencies between MIBs

**`mibs/registry/`** - MIB Registry
```go
mibs/registry/
├── registry.go          # OID registry
├── cache.go             # Cached lookups
├── loader.go            # Dynamic MIB loading
└── builtin.go           # Built-in essential MIBs
```

Features:
- OID to name mapping
- Name to OID mapping
- Type information
- Description and units metadata

**`mibs/compiler/`** - MIB Compiler
```go
mibs/compiler/
├── compiler.go          # Compile MIBs to internal format
├── validator.go         # Validate MIB syntax
└── generator.go         # Generate metric templates
```

### Production Layer

#### `producer/` - Message Producers

**`producer/metrics/`** - Metrics Producer
```go
producer/metrics/
├── producer.go          # Main producer
├── poll.go              # Poll result conversion
├── trap.go              # Trap conversion
├── normalize.go         # Value normalization
├── enrich.go            # Metadata enrichment
└── aggregate.go         # Optional aggregation
```

Features:
- Convert SNMP varbinds to unified metric format
- Add device metadata (hostname, IP, location, tags)
- Timestamp normalization
- Unit conversion
- Delta calculations for counters

**`producer/timeseries/`** - Time Series Producer
```go
producer/timeseries/
├── producer.go          # Time series format
├── prometheus.go        # Prometheus format
├── influx.go            # InfluxDB line protocol
└── opentsdb.go          # OpenTSDB format
```

**`producer/event/`** - Event Producer
```go
producer/event/
├── producer.go          # Event format for traps
├── severity.go          # Severity mapping
└── alert.go             # Alert format
```

### Serialization Layer

#### `format/` - Output Formatters

**`format/json/`** - JSON Formatter (Primary Implementation)

The JSON formatter outputs the complete `SNMPMetric` structure. SNMP GetBulk operations and table walks return multiple metrics in a single poll cycle:

```json
{
  "timestamp": "2026-02-26T10:30:00.123Z",
  "device": {
    "hostname": "router01.example.com",
    "ip_address": "192.168.1.1",
    "snmp_version": "2c",
    "vendor": "Cisco",
    "model": "ASR1000",
    "sys_descr": "Cisco IOS Software, ASR1000 Software",
    "sys_location": "DC1-Rack5",
    "tags": {
      "environment": "production",
      "role": "edge-router",
      "site": "dc1"
    }
  },
  "metrics": [
    {
      "oid": "1.3.6.1.2.1.2.2.1.10.1",
      "name": "netif.bytes.in",
      "instance": "1",
      "value": 1234567890,
      "type": "Counter64",
      "syntax": "Counter64",
      "tags": {
        "netif.descr": "GigabitEthernet0/0/1",
        "netif.type": "ethernetCsmacd",
        "netif.mac": "00:1a:2b:3c:4d:5e"
      }
    },
    {
      "oid": "1.3.6.1.2.1.2.2.1.16.1",
      "name": "netif.bytes.out",
      "instance": "1",
      "value": 987654321,
      "type": "Counter64",
      "syntax": "Counter64",
      "tags": {
        "netif.descr": "GigabitEthernet0/0/1",
        "netif.type": "ethernetCsmacd"
      }
    },
    {
      "oid": "1.3.6.1.2.1.2.2.1.10.2",
      "name": "netif.bytes.in",
      "instance": "2",
      "value": 5678901234,
      "type": "Counter64",
      "syntax": "Counter64",
      "tags": {
        "netif.descr": "GigabitEthernet0/0/2",
        "netif.type": "ethernetCsmacd"
      }
    },
    {
      "oid": "1.3.6.1.2.1.2.2.1.16.2",
      "name": "netif.bytes.out",
      "instance": "2",
      "value": 4321098765,
      "type": "Counter64",
      "syntax": "Counter64",
      "tags": {
        "netif.descr": "GigabitEthernet0/0/2",
        "netif.type": "ethernetCsmacd"
      }
    },
    {
      "oid": "1.3.6.1.2.1.2.2.1.8.1",
      "name": "netif.state.oper",
      "instance": "1",
      "value": "up",
      "type": "Integer",
      "syntax": "EnumInteger",
      "tags": {
        "netif.descr": "GigabitEthernet0/0/1"
      }
    },
    {
      "oid": "1.3.6.1.2.1.2.2.1.8.2",
      "name": "netif.state.oper",
      "instance": "2",
      "value": "up",
      "type": "Integer",
      "syntax": "EnumInteger",
      "tags": {
        "netif.descr": "GigabitEthernet0/0/2"
      }
    }
  ],
  "metadata": {
    "collector_id": "collector-01",
    "poll_duration_ms": 245,
    "poll_status": "success"
  }
}
```

**Key Features of JSON Output:**
- **Bulk Collection**: Single JSON document contains all metrics from a GetBulk/table walk
- **Tag Enrichment**: Each metric includes contextual tags (interface names, types, MAC addresses)
- **Type Preservation**: Original SNMP types and syntax preserved for downstream processing
- **Metadata**: Poll timing and status for monitoring collector health
- **Indexing**: Instance field contains table index for proper metric identification

**Note**: The architecture is designed to be extensible for multiple formatters. Currently, **only JSON is implemented**. Future formatters can be added by implementing the `Formatter` interface:

**`format/protobuf/`** - Protobuf Formatter (Future)
- Binary serialization for efficiency
- Type-safe schema

**`format/prometheus/`** - Prometheus/OpenMetrics Format (Future)
- Direct integration with Prometheus ecosystem
- Label-based model

**`format/influx/`** - InfluxDB Line Protocol (Future)
- Native InfluxDB format
- Optimized for time-series ingestion

### Transport Layer

#### `transport/` - Message Transports

**`transport/kafka/`** - Kafka Producer
```go
transport/kafka/
├── kafka.go             # Kafka client
├── partitioner.go       # Device-based partitioning
└── serializer.go        # Message serialization
```

Features:
- Per-device partitioning
- Compression (gzip, snappy, lz4, zstd)
- Batching and buffering
- Delivery guarantees

**`transport/timeseries/`** - Time Series Databases
```go
transport/timeseries/
├── prometheus.go        # Prometheus remote write
├── influxdb.go          # InfluxDB client
├── thanos.go            # Thanos receiver
└── victoria.go          # VictoriaMetrics
```

**`transport/elasticsearch/`** - Elasticsearch
```go
transport/elasticsearch/
├── client.go            # ES client
├── bulk.go              # Bulk indexing
└── template.go          # Index templates
```

**`transport/file/`** - File/Stdout Output
```go
transport/file/
├── writer.go            # File writer
└── rotate.go            # Log rotation
```

### Data Models

#### Internal Go Structures (Current Implementation)

The collector uses Go structs as the internal data model, which are then serialized to JSON (currently the only implemented format).

**`models/metric.go`** - Core data structures:

```go
package models

import "time"

// SNMPMetric represents a complete SNMP metric collection result
type SNMPMetric struct {
    Timestamp time.Time         `json:"timestamp"`
    Device    Device            `json:"device"`
    Metrics   []Metric          `json:"metrics"`
    Metadata  MetricMetadata    `json:"metadata,omitempty"`
}

// Device contains information about the monitored device
type Device struct {
    Hostname    string            `json:"hostname"`
    IPAddress   string            `json:"ip_address"`
    SNMPVersion string            `json:"snmp_version"` // "1", "2c", "3"
    Vendor      string            `json:"vendor,omitempty"`
    Model       string            `json:"model,omitempty"`
    SysDescr    string            `json:"sys_descr,omitempty"`
    SysLocation string            `json:"sys_location,omitempty"`
    SysContact  string            `json:"sys_contact,omitempty"`
    Tags        map[string]string `json:"tags,omitempty"`
}

// Metric represents a single SNMP metric value
type Metric struct {
    OID      string      `json:"oid"`
    Name     string      `json:"name"`
    Instance string      `json:"instance,omitempty"`
    Value    interface{} `json:"value"` // Can be int64, uint64, float64, string, []byte, bool
    Type     string      `json:"type"`  // "Counter32", "Counter64", "Gauge", "Integer", etc.
    Syntax   string      `json:"syntax"` // Original syntax type from configuration
    Tags     map[string]string `json:"tags,omitempty"` // Tag attributes (e.g., ifDescr, ifType)
}

// MetricMetadata contains collection metadata
type MetricMetadata struct {
    CollectorID    string `json:"collector_id"`
    PollDurationMs int64  `json:"poll_duration_ms"`
    PollStatus     string `json:"poll_status"` // "success", "timeout", "error"
}

// SNMPTrap represents an SNMP trap event
type SNMPTrap struct {
    Timestamp time.Time         `json:"timestamp"`
    Device    Device            `json:"device"`
    TrapInfo  TrapInfo          `json:"trap_info"`
    Varbinds  []Metric          `json:"varbinds"`
}

// TrapInfo contains trap-specific information
type TrapInfo struct {
    Version       string `json:"version"`        // "v1", "v2c", "v3"
    EnterpriseOID string `json:"enterprise_oid,omitempty"` // v1 only
    GenericTrap   int32  `json:"generic_trap,omitempty"`   // v1 only
    SpecificTrap  int32  `json:"specific_trap,omitempty"`  // v1 only
    TrapOID       string `json:"trap_oid"`       // v2c/v3
    TrapName      string `json:"trap_name,omitempty"`
    Severity      string `json:"severity,omitempty"` // "info", "warning", "critical"
}
```

**JSON Output Example (Current Implementation):**

```json
{
  "timestamp": "2026-02-26T10:30:00.123Z",
  "device": {
    "hostname": "router01.example.com",
    "ip_address": "192.168.1.1",
    "snmp_version": "2c",
    "vendor": "Cisco",
    "model": "ASR1000",
    "sys_descr": "Cisco IOS Software, ASR1000 Software",
    "sys_location": "DC1-Rack5",
    "sys_contact": "netops@example.com",
    "tags": {
      "environment": "production",
      "role": "edge-router",
      "site": "dc1"
    }
  },
  "metrics": [
    {
      "oid": "1.3.6.1.2.1.2.2.1.10.1",
      "name": "netif.bytes.in",
      "instance": "1",
      "value": 1234567890,
      "type": "Counter64",
      "syntax": "Counter64",
      "tags": {
        "netif.descr": "GigabitEthernet0/0/1",
        "netif.type": "ethernetCsmacd",
        "netif.mac": "00:1a:2b:3c:4d:5e"
      }
    },
    {
      "oid": "1.3.6.1.2.1.2.2.1.16.1",
      "name": "netif.bytes.out",
      "instance": "1",
      "value": 987654321,
      "type": "Counter64",
      "syntax": "Counter64",
      "tags": {
        "netif.descr": "GigabitEthernet0/0/1",
        "netif.type": "ethernetCsmacd"
      }
    },
    {
      "oid": "1.3.6.1.2.1.2.2.1.8.1",
      "name": "netif.state.oper",
      "instance": "1",
      "value": "up",
      "type": "Integer",
      "syntax": "EnumInteger",
      "tags": {
        "netif.descr": "GigabitEthernet0/0/1"
      }
    }
  ],
  "metadata": {
    "collector_id": "collector-01",
    "poll_duration_ms": 245,
    "poll_status": "success"
  }
}
```

**Trap JSON Output Example:**

```json
{
  "timestamp": "2026-02-26T10:31:15.456Z",
  "device": {
    "hostname": "switch01.example.com",
    "ip_address": "192.168.1.10",
    "snmp_version": "2c"
  },
  "trap_info": {
    "version": "v2c",
    "trap_oid": "1.3.6.1.6.3.1.1.5.3",
    "trap_name": "linkDown",
    "severity": "critical"
  },
  "varbinds": [
    {
      "oid": "1.3.6.1.2.1.2.2.1.1",
      "name": "ifIndex",
      "value": 5,
      "type": "Integer"
    },
    {
      "oid": "1.3.6.1.2.1.2.2.1.8",
      "name": "ifOperStatus",
      "value": "down",
      "type": "Integer"
    },
    {
      "oid": "1.3.6.1.2.1.2.2.1.2",
      "name": "ifDescr",
      "value": "GigabitEthernet0/5",
      "type": "String"
    }
  ]
}
```

#### Future: Protocol Buffers Schema (Design Reference)

For future implementation when Protobuf formatter is added, here's the planned schema:

**`pb/snmp.proto`** - Protobuf schema (not yet implemented):

```protobuf
syntax = "proto3";

package snmpcollector;

import "google/protobuf/timestamp.proto";

// Note: This is a future enhancement. Currently only JSON is implemented.

message SNMPMetric {
  google.protobuf.Timestamp timestamp = 1;
  Device device = 2;
  repeated Metric metrics = 3;
  MetricMetadata metadata = 4;
}

message Device {
  string hostname = 1;
  string ip_address = 2;
  string snmp_version = 3;
  string vendor = 4;
  string model = 5;
  string sys_descr = 6;
  string sys_location = 7;
  string sys_contact = 8;
  map<string, string> tags = 9;
}

message Metric {
  string oid = 1;
  string name = 2;
  string instance = 3;
  oneof value {
    int64 int_value = 4;
    uint64 uint_value = 5;
    double gauge_value = 6;
    string string_value = 7;
    bytes bytes_value = 8;
    bool bool_value = 9;
  }
  string type = 10;
  string syntax = 11;
  map<string, string> tags = 12;
}

message MetricMetadata {
  string collector_id = 1;
  int64 poll_duration_ms = 2;
  string poll_status = 3;
}

message SNMPTrap {
  google.protobuf.Timestamp timestamp = 1;
  Device device = 2;
  TrapInfo trap_info = 3;
  repeated Metric varbinds = 4;
}

message TrapInfo {
  string version = 1;
  string enterprise_oid = 2;
  int32 generic_trap = 3;
  int32 specific_trap = 4;
  string trap_oid = 5;
  string trap_name = 6;
  string severity = 7;
}
```

### Observability

#### `metrics/` - Prometheus Metrics

**Poller Metrics:**
```go
// Poll operations
poll_requests_total{device, status}
poll_duration_seconds{device}
poll_errors_total{device, error_type}
poll_oids_total{device}

// Device status
device_status{device, vendor, model}  // 1=up, 0=down
device_last_poll_timestamp{device}

// Scheduler
poll_queue_size
poll_jobs_scheduled_total
poll_jobs_completed_total
poll_jobs_failed_total
```

**Trap Metrics:**
```go
// Trap reception
trap_received_total{device, trap_oid, severity}
trap_processed_total{device, status}
trap_errors_total{device, error_type}
trap_processing_duration_seconds

// Inform acknowledgments
inform_sent_total{device, status}
```

**MIB Metrics:**
```go
// MIB management
mib_loaded_total
mib_load_errors_total
oid_lookups_total{status}  // hit, miss
oid_cache_size
```

**Transport Metrics:**
```go
// Output
messages_sent_total{transport, destination, status}
messages_bytes_total{transport}
transport_errors_total{transport, error_type}
transport_latency_seconds{transport}
```

### Utility Layer

#### `utils/` - Supporting Utilities

**`utils/oid/`** - OID utilities
```go
utils/oid/
├── oid.go               # OID manipulation
├── format.go            # OID formatting
└── validate.go          # OID validation
```

**`utils/snmptest/`** - Testing utilities
```go
utils/snmptest/
├── simulator.go         # SNMP agent simulator
├── mockdevice.go        # Mock device responses
└── fixtures.go          # Test data
```

**`utils/credentials/`** - Credential management
```go
utils/credentials/
├── store.go             # Credential storage
├── vault.go             # HashiCorp Vault integration
└── rotate.go            # Credential rotation
```

### Configuration

The configuration is organized hierarchically: **Devices** → **Device Groups** → **Object Groups** → **Objects** → **Attributes**

Configuration file locations are specified by environment variables with defaults:
- `INPUT_SNMP_DEVICE_DEFINITIONS_DIRECTORY_PATH` (default: `/etc/snmp_collector/snmp/devices`)
- `INPUT_SNMP_DEFAULTS_DIRECTORY_PATH` (default: `/etc/snmp_collector/snmp/defaults`)
- `INPUT_SNMP_DEVICE_GROUP_DEFINITIONS_DIRECTORY_PATH` (default: `/etc/snmp_collector/snmp/device_groups`)
- `INPUT_SNMP_OBJECT_GROUP_DEFINITIONS_DIRECTORY_PATH` (default: `/etc/snmp_collector/snmp/object_groups`)
- `INPUT_SNMP_OBJECT_DEFINITIONS_DIRECTORY_PATH` (default: `/etc/snmp_collector/snmp/objects`)
- `PROCESSOR_SNMP_ENUM_DEFINITIONS_DIRECTORY_PATH` (default: `/etc/snmp_collector/snmp/enums`)

#### Device Configuration

**SNMP v2c Example** - Full configuration:

`/etc/snmp_collector/snmp/devices/router01.yml`:
```yaml
router01.example.com:
  ip: 192.0.2.1
  port: 161
  poll_interval: 60
  timeout: 3000
  retries: 2
  exponential_timeout: false
  version: 2c
  communities:
    - public
  device_groups:
    - cisco_c1000
  max_concurrent_polls: 4
```

**SNMP v2c Example** - When using defaults:
```yaml
router01.example.com:
  ip: 192.0.2.1
  version: 2c
  communities:
    - public
  device_groups:
    - cisco_c1000
```

**SNMP v3 Example** - Full configuration:

`/etc/snmp_collector/snmp/devices/switch01.yml`:
```yaml
switch01.example.com:
  ip: 192.0.2.2
  port: 161
  poll_interval: 60
  timeout: 3000
  retries: 2
  exponential_timeout: false
  version: 3
  v3_credentials:
    - username: snmp_collector
      authentication_protocol: sha
      authentication_passphrase: efauthpassword
      privacy_protocol: des
      privacy_passphrase: efprivpassword
  device_groups:
    - cisco_c1000
  max_concurrent_polls: 4
```

**SNMP v3 Example** - When using defaults:
```yaml
switch01.example.com:
  ip: 192.0.2.2
  version: 3
  v3_credentials:
    - username: snmp_collector
      authentication_protocol: sha
      authentication_passphrase: efauthpassword
      privacy_protocol: des
      privacy_passphrase: efprivpassword
  device_groups:
    - cisco_c1000
```

#### Global Device Defaults

`/etc/snmp_collector/snmp/defaults/device.yml`:
```yaml
default:
  port: 161
  timeout: 3000
  retries: 2
  exponential_timeout: false
  version: 2c
  communities:
    - public
  device_groups:
    - generic
  poll_interval: 60
  max_concurrent_polls: 4
```

#### Device Groups

Device Groups organize Object Groups that may be instrumented by a device model or series.

`/etc/snmp_collector/snmp/device_groups/cisco_c1000.yml`:
```yaml
cisco_c1000:
  object_groups:
    - system
    - host
    - netif
    - ip
    - tcp
    - udp
    - snmp
```

`/etc/snmp_collector/snmp/device_groups/generic.yml`:
```yaml
generic:
  object_groups:
    - system
    - netif
```

#### Object Groups

Object Groups organize Objects that may be implemented by a managed entity.

`/etc/snmp_collector/snmp/object_groups/netif.yml`:
```yaml
netif:
  objects:
    - IF-MIB::system
    - IF-MIB::ifEntry
    - IF-MIB::ifXEntry
    - EtherLike-MIB::dot3StatsEntry
    - EtherLike-MIB::dot3HCStatsEntry
```

`/etc/snmp_collector/snmp/object_groups/system.yml`:
```yaml
system:
  objects:
    - SNMPv2-MIB::system
```

#### Object Definitions

Objects define the SNMP objects and attributes to poll. This is the most detailed configuration layer.

`/etc/snmp_collector/snmp/objects/IF-MIB_ifEntry.yml`:
```yaml
IF-MIB::ifEntry:
  mib: IF-MIB
  object: ifEntry
  index:
    - type: Integer
      oid: .1.3.6.1.2.1.2.2.1.1
      name: netif
      syntax: InterfaceIndex
  discovery_attribute: ifDescr
  attributes:
    ifDescr:
      oid: .1.3.6.1.2.1.2.2.1.2
      tag: true
      name: netif.descr
      syntax: DisplayString
    ifType:
      oid: .1.3.6.1.2.1.2.2.1.3
      tag: true
      name: netif.type
      syntax: IANAifType
    ifSpeed:
      oid: .1.3.6.1.2.1.2.2.1.5
      name: netif.bandwidth.bw
      syntax: Gauge32
    ifPhysAddress:
      oid: .1.3.6.1.2.1.2.2.1.6
      tag: true
      name: netif.mac
      syntax: PhysAddress
    ifAdminStatus:
      oid: .1.3.6.1.2.1.2.2.1.7
      name: netif.state.admin
      syntax: EnumInteger
    ifOperStatus:
      oid: .1.3.6.1.2.1.2.2.1.8
      name: netif.state.oper
      syntax: EnumInteger
    ifInOctets:
      oid: .1.3.6.1.2.1.2.2.1.10
      name: netif.bytes.in
      syntax: Counter32
    ifInUcastPkts:
      oid: .1.3.6.1.2.1.2.2.1.11
      name: netif.packets.ucast.in
      syntax: Counter32
    ifInDiscards:
      oid: .1.3.6.1.2.1.2.2.1.13
      name: netif.packets.discard.in
      syntax: Counter32
    ifInErrors:
      oid: .1.3.6.1.2.1.2.2.1.14
      name: netif.packets.error.in
      syntax: Counter32
    ifOutOctets:
      oid: .1.3.6.1.2.1.2.2.1.16
      name: netif.bytes.out
      syntax: Counter32
    ifOutUcastPkts:
      oid: .1.3.6.1.2.1.2.2.1.17
      name: netif.packets.ucast.out
      syntax: Counter32
    ifOutDiscards:
      oid: .1.3.6.1.2.1.2.2.1.19
      name: netif.packets.discard.out
      syntax: Counter32
    ifOutErrors:
      oid: .1.3.6.1.2.1.2.2.1.20
      name: netif.packets.error.out
      syntax: Counter32
```

**Object with Multi-value Index** - Complex index example:
```yaml
OSPF-MIB::ospfLocalLsdbEntry:
  mib: OSPF-MIB
  object: ospfLocalLsdbEntry
  index:
    - type: IpAddress
      oid: .1.3.6.1.2.1.14.17.1.1
      name: ospf.lsdb.link_local.lsa.netif
      syntax: IpAddress
    - type: Integer32
      oid: .1.3.6.1.2.1.14.17.1.2
      name: ospf.lsdb.link_local.lsa.netif
      syntax: InterfaceIndexOrZero
    - type: Integer
      oid: .1.3.6.1.2.1.14.17.1.3
      name: ospf.lsdb.link_local.lsa.type
      syntax: EnumInteger
    - type: IpAddress
      oid: .1.3.6.1.2.1.14.17.1.4
      name: ospf.lsdb.link_local.lsa.lsid
      syntax: IpAddressNoSuffix
    - type: IpAddress
      oid: .1.3.6.1.2.1.14.17.1.5
      name: ospf.lsdb.link_local.lsa.router.id
      syntax: IpAddressNoSuffix
  discovery_attribute: ospfLocalLsdbSequence
  attributes:
    ospfLocalLsdbSequence:
      oid: .1.3.6.1.2.1.14.17.1.6
      name: ospf.lsdb.link_local.lsa.seq
      syntax: Integer32
```

**Object with Augmentation**:
```yaml
IF-MIB::ifXEntry:
  mib: IF-MIB
  object: ifXEntry
  augments: IF-MIB::ifEntry
  attributes:
    ifHCInOctets:
      oid: .1.3.6.1.2.1.31.1.1.1.6
      name: netif.bytes.in
      syntax: Counter64
      overrides:
        object: IF-MIB::ifEntry
        attribute: ifInOctets
    ifHCOutOctets:
      oid: .1.3.6.1.2.1.31.1.1.1.10
      name: netif.bytes.out
      syntax: Counter64
      overrides:
        object: IF-MIB::ifEntry
        attribute: ifOutOctets
    ifHighSpeed:
      oid: .1.3.6.1.2.1.31.1.1.1.15
      name: netif.bandwidth.bw
      syntax: BandwidthMBits
      overrides:
        object: IF-MIB::ifEntry
        attribute: ifSpeed
```

#### Enumeration Definitions

Enumerations translate integer and OID values to text. Enable with `PROCESSOR_SNMP_ENUM_ENABLE=true`.

**Integer Enumeration** - `/etc/snmp_collector/snmp/enums/ifOperStatus.yml`:
```yaml
# ifOperStatus - .1.3.6.1.2.1.2.2.1.8
.1.3.6.1.2.1.2.2.1.8:
  1: 'up'
  2: 'down'
  3: 'testing'
  4: 'unknown'
  5: 'dormant'
  6: 'not present'
  7: 'lower-layer down'
```

**Bitmap Enumeration**:
```yaml
# mplsTunnelCRLDPResFlags - .1.3.6.1.2.1.10.166.3.2.10.1.5
.1.3.6.1.2.1.10.166.3.2.10.1.5:
  0: 'PDR'
  1: 'PBS'
  2: 'CDR'
  3: 'CBS'
  4: 'EBS'
  5: 'weight'
```

**OID Enumeration**:
```yaml
# hrStorageType - hrStorageTypes
.1.3.6.1.2.1.25.2.1.1: 'other'
.1.3.6.1.2.1.25.2.1.2: 'RAM'
.1.3.6.1.2.1.25.2.1.3: 'virtual memory'
.1.3.6.1.2.1.25.2.1.4: 'fixed disk'
.1.3.6.1.2.1.25.2.1.5: 'removable disk'
.1.3.6.1.2.1.25.2.1.6: 'floppy disk'
.1.3.6.1.2.1.25.2.1.7: 'compact disc'
.1.3.6.1.2.1.25.2.1.8: 'RAM disk'
.1.3.6.1.2.1.25.2.1.9: 'flash memory'
.1.3.6.1.2.1.25.2.1.10: 'network disk'
```

#### Trap Configuration

**`traps.yaml`** - Trap Configuration
```yaml
trap_config:
  listen_address: 0.0.0.0:162
  workers: 4
  enable_inform_response: true
  duplicate_detection_window: 60s
  
  # Trap filtering
  filters:
    - name: critical_only
      enabled: true
      conditions:
        - trap_oid: 1.3.6.1.6.3.1.1.5.3  # linkDown
          action: accept
          severity: critical
          
        - trap_oid: 1.3.6.1.6.3.1.1.5.4  # linkUp
          action: accept
          severity: info
          
  # Trap enrichment rules
  enrichment:
    - trap_oid: 1.3.6.1.6.3.1.1.5.3  # linkDown
      add_fields:
        alert: true
        runbook: "https://wiki.example.com/linkdown"
        
  # Trap forwarding
  forward_to:
    - type: webhook
      url: https://alerts.example.com/webhook
      method: POST
      headers:
        Authorization: Bearer ${WEBHOOK_TOKEN}
```

## Configuration Attributes Reference

### Device Attributes

| Attribute | Required | Default | Description |
|-----------|----------|---------|-------------|
| `ip` | Yes | - | IP address of the device |
| `port` | No | 161 | UDP port for SNMP requests |
| `poll_interval` | No | 60 | Polling interval in seconds |
| `timeout` | No | 3000 | Request timeout in milliseconds |
| `retries` | No | 2 | Number of retry attempts |
| `exponential_timeout` | No | false | Use exponential backoff for retries |
| `version` | Yes | - | SNMP version: `1`, `2c`, or `3` |
| `communities` | For v1/v2c | - | List of community strings to try |
| `v3_credentials` | For v3 | - | List of SNMPv3 credentials |
| `device_groups` | Yes | - | List of device groups to apply |
| `max_concurrent_polls` | No | 4 | Max concurrent polls to this device |
| `cisco_qos_enabled` | No | false | Enable Cisco QoS MIB enrichment |

### SNMPv3 Credential Attributes

| Attribute | Description |
|-----------|-------------|
| `username` | SNMPv3 username |
| `authentication_protocol` | Auth protocol: `noauth`, `md5`, `sha`, `sha224`, `sha256`, `sha384`, `sha512` |
| `authentication_passphrase` | Authentication passphrase |
| `privacy_protocol` | Privacy protocol: `nopriv`, `des`, `aes`, `aes192`, `aes256`, `aes192c`, `aes256c` |
| `privacy_passphrase` | Privacy passphrase |

### Object Index Types

- `Integer` / `Integer32`
- `OctetString`
- `ImplicitOctetString`
- `ObjectIdentifier`
- `IpAddress`
- `MacAddress`
- `Unsigned32`
- `Opaque`

### Syntax Types

The `syntax` field specifies how raw SNMP values are interpreted. Supports extensive type system including:

**SNMPv2-SMI Types**: `Integer`, `Integer32`, `Unsigned32`, `Gauge32`, `Counter32`, `Counter64`, `OctetString`, `ObjectIdentifier`, `IpAddress`, `TimeTicks`, `Opaque`

**SNMPv2-TC Types**: `DisplayString`, `PhysAddress`, `MacAddress`, `TruthValue`, `TimeStamp`, `TimeInterval`, `DateAndTime`, `RowStatus`

**Enumerated Types**: `EnumBitmap`, `EnumInteger`, `EnumIntegerKeepID`, `EnumObjectIdentifier`, `EnumObjectIdentifierKeepOID`

**Unit Types** (normalized to standard units):
- **Bandwidth**: `BandwidthBits`, `BandwidthKBits`, `BandwidthMBits`, `BandwidthGBits` (normalized to bits/sec)
- **Bytes**: `BytesB`, `BytesKB`, `BytesMB`, `BytesGB`, `BytesTB`, `BytesKiB`, `BytesMiB`, `BytesGiB` (normalized to bytes)
- **Temperature**: `TemperatureC`, `TemperatureDeciC`, `TemperatureCentiC` (normalized to celsius)
- **Power**: `PowerWatt`, `PowerMilliWatt`, `PowerKiloWatt` (normalized to watts)
- **Current**: `CurrentAmp`, `CurrentMilliAmp`, `CurrentMicroAmp` (normalized to amps)
- **Voltage**: `VoltageVolt`, `VoltageMilliVolt`, `VoltageMicroVolt` (normalized to volts)
- **Frequency**: `FreqHz`, `FreqKHz`, `FreqMHz`, `FreqGHz` (normalized to hertz)
- **Duration**: `TicksSec`, `TicksMilliSec`, `TicksMicroSec` (normalized to configured precision)
- **Percentage**: `Percent1`, `Percent100`, `PercentDeci100` (normalized to 0-1 or 0-100)
- **Many more** - See full syntax type reference in configuration documentation

### Attribute Modifiers

- `tag: true` - Mark as dimension/label rather than metric value
- `overrides` - Specify which attribute this should override (e.g., 64-bit version overrides 32-bit)
- `rediscover` - Trigger rediscovery: `OnChange`, `OnReset`

## Command-Line Configuration

```bash
# Input Configuration
-INPUT_SNMP_DEVICE_DEFINITIONS_DIRECTORY_PATH=/etc/snmp_collector/snmp/devices
-INPUT_SNMP_DEFAULTS_DIRECTORY_PATH=/etc/snmp_collector/snmp/defaults
-INPUT_SNMP_DEVICE_GROUP_DEFINITIONS_DIRECTORY_PATH=/etc/snmp_collector/snmp/device_groups
-INPUT_SNMP_OBJECT_GROUP_DEFINITIONS_DIRECTORY_PATH=/etc/snmp_collector/snmp/object_groups
-INPUT_SNMP_OBJECT_DEFINITIONS_DIRECTORY_PATH=/etc/snmp_collector/snmp/objects

# Processor Configuration
-PROCESSOR_SNMP_ENUM_DEFINITIONS_DIRECTORY_PATH=/etc/snmp_collector/snmp/enums
-PROCESSOR_SNMP_ENUM_ENABLE=true
-PROCESSOR_DURATION_PRECISION=ns  # ns, us, ms, s
-PROCESSOR_TIMESTAMP_PRECISION=ms  # s, ms, us, ns
-PROCESSOR_PERCENT_NORM=1  # 1 (0-1 range) or 100 (0-100 range)

# Trap Configuration
-trap.listen=0.0.0.0:162
-trap.workers=4

# Output Settings
-format=json  # Currently only json implemented
-transport=kafka  # kafka, file

# Kafka Settings
-transport.kafka.brokers=localhost:9092
-transport.kafka.topic=snmp-metrics
-transport.kafka.compression=snappy

# Observability
-metrics.addr=:8080
-metrics.path=/metrics
-log.level=info
-log.fmt=json

# Performance Tuning: Worker Thread Configuration
# Adjust these based on your system resources

┌────────────────────────────────────────────────────────────────┐
│         QUICK REFERENCE: Worker Configuration                  │
├────────────────────────────────────────────────────────────────┤
│ System Size │ CPUs │ RAM  │ Poller │ Trap/Socket │ Pipeline  │
│─────────────┼──────┼──────┼────────┼─────────────┼───────────│
│ Small       │  2-4 │  2GB │    100 │          50 │    25-50  │
│ Medium      │    8 │  8GB │    500 │         100 │    50-100 │
│ Large       │   16 │ 16GB │  1,000 │         150 │   100-200 │
│ Extra Large │  32+ │ 32GB │  2,000 │         200 │   150-300 │
└────────────────────────────────────────────────────────────────┘

# CPU and Concurrency
-gomaxprocs=0  # 0 = use all CPU cores (default), or set specific number

# SNMP Poller Workers (concurrent device polling)
-poller.workers=500                    # Default: 500, Range: 10-5000
-poller.max.concurrent.per.device=4    # Max concurrent polls per device

# Trap Listener Workers
-trap.listener.sockets=0               # 0 = auto (num CPU cores), or set specific
-trap.workers.per.socket=100           # Workers per socket, Range: 10-500

# Pipeline Stage Workers (decoder → producer → formatter → transport)
-pipeline.decoder.workers=100          # Default: 100, Range: 10-500
-pipeline.producer.workers=100         # Default: 100, Range: 10-500  
-pipeline.formatter.workers=50         # Default: 50, Range: 10-200
-pipeline.transport.workers=50         # Default: 50, Range: 10-200

# Buffer Sizes (affects memory usage)
-pipeline.buffer.size=10000            # Channel buffer size, Range: 1000-50000
-poller.job.queue.size=10000           # Polling job queue size

# Connection Pooling
-snmp.connection.pool.max.idle=100     # Max idle connections per device
-snmp.connection.pool.max.open=1000    # Max open connections total
-snmp.connection.pool.idle.timeout=30s # Idle connection timeout

# Rate Limiting
-poller.rate.limit.per.device=10       # Max requests/sec per device (0=unlimited)
-poller.global.rate.limit=10000        # Global max requests/sec (0=unlimited)
```

### Worker Configuration Guide by System Profile

#### Small Deployment (< 500 devices, 2-4 CPU cores, 2-4GB RAM)
```bash
-gomaxprocs=0  # Use all cores
-poller.workers=100
-trap.workers.per.socket=50
-pipeline.decoder.workers=50
-pipeline.producer.workers=50
-pipeline.formatter.workers=25
-pipeline.transport.workers=25
-pipeline.buffer.size=5000
```

#### Medium Deployment (500-2000 devices, 8 CPU cores, 8GB RAM)
```bash
-gomaxprocs=0
-poller.workers=500
-trap.workers.per.socket=100
-pipeline.decoder.workers=100
-pipeline.producer.workers=100
-pipeline.formatter.workers=50
-pipeline.transport.workers=50
-pipeline.buffer.size=10000
```

#### Large Deployment (2000-5000 devices, 16 CPU cores, 16GB RAM)
```bash
-gomaxprocs=0
-poller.workers=1000
-trap.workers.per.socket=150
-pipeline.decoder.workers=200
-pipeline.producer.workers=200
-pipeline.formatter.workers=100
-pipeline.transport.workers=100
-pipeline.buffer.size=20000
```

#### Extra Large Deployment (5000-10000+ devices, 32+ CPU cores, 32GB+ RAM)
```bash
-gomaxprocs=0
-poller.workers=2000
-trap.workers.per.socket=200
-pipeline.decoder.workers=300
-pipeline.producer.workers=300
-pipeline.formatter.workers=150
-pipeline.transport.workers=150
-pipeline.buffer.size=50000
```

### Dynamic Worker Tuning Based on Metrics

Monitor these metrics to adjust workers:

**Increase Workers When:**
- `poll_queue_size` consistently high → Increase `-poller.workers`
- `trap_queue_size` consistently high → Increase `-trap.workers.per.socket`
- `pipeline_*_queue_size` high → Increase corresponding stage workers
- CPU utilization < 70% → Can add more workers

**Decrease Workers When:**
- CPU utilization > 90% sustained → Reduce workers
- Memory pressure → Reduce `-pipeline.buffer.size` and worker counts
- High context switching → Reduce worker counts

**Monitor Commands:**
```bash
# Check current queue sizes
curl localhost:8080/metrics | grep queue_size

# Check CPU usage
top -p $(pgrep snmpcollector)

# Check goroutine count
curl localhost:8080/debug/vars | jq .goroutines

# Check memory usage
curl localhost:8080/metrics | grep go_memstats
```

### Auto-Tuning (Optional)

Enable automatic worker adjustment based on system load:

```bash
-performance.auto.tune=true             # Enable auto-tuning
-performance.auto.tune.interval=60s     # Check every 60 seconds
-performance.auto.tune.cpu.target=0.75  # Target 75% CPU utilization
-performance.auto.tune.min.workers=50   # Minimum workers per stage
-performance.auto.tune.max.workers=500  # Maximum workers per stage
```

Auto-tuning algorithm:
- If CPU < 70% and queues growing → Increase workers by 10%
- If CPU > 90% → Decrease workers by 10%
- If memory > 85% → Decrease buffer sizes and workers
- Adjustments happen gradually to prevent oscillation

## Data Flow Pipeline

### Concurrency Overview

The collector uses **massive parallelism** at every stage:

```
┌──────────────────────────────────────────────────────────┐
│                    Parallel Execution                     │
├──────────────────────────────────────────────────────────┤
│                                                           │
│  Scheduler (1)                                           │
│      ↓                                                    │
│  ┌─────────────────────────────────┐                    │
│  │  Worker Pool (500 goroutines)   │ → Poll Device 1    │
│  │                                  │ → Poll Device 2    │
│  │  Each polls different device     │ → Poll Device 3    │
│  │  concurrently                    │ → ... (parallel)   │
│  └─────────────────────────────────┘                    │
│                                                           │
│  Trap Listeners (16 sockets)                            │
│      ↓                                                    │
│  ┌─────────────────────────────────┐                    │
│  │  Socket 1 → 100 workers         │ → Process Trap     │
│  │  Socket 2 → 100 workers         │ → Process Trap     │
│  │  Socket 3 → 100 workers         │ → Process Trap     │
│  │  ... (1,600 goroutines total)   │ → ... (parallel)   │
│  └─────────────────────────────────┘                    │
│                                                           │
│  Pipeline Stages (all concurrent)                        │
│      ↓                                                    │
│  ┌─────────────┐   ┌─────────────┐   ┌─────────────┐  │
│  │  Decoders   │→│  Producers  │→│ Formatters  │  │
│  │ (100 workers)│   │ (100 workers)│   │ (50 workers) │  │
│  └─────────────┘   └─────────────┘   └─────────────┘  │
│                                             ↓            │
│                                    ┌─────────────┐      │
│                                    │ Transports  │      │
│                                    │ (50 workers)│      │
│                                    └─────────────┘      │
│                                                           │
│  Total: ~3,000-5,000 concurrent goroutines              │
│  Running on: 8-16 OS threads (CPU cores)                │
└──────────────────────────────────────────────────────────┘
```

### SNMP Polling Flow (Highly Concurrent)

```
1. Scheduler (Single Coordinator)
   └─▶ Job Queue: {device, metrics, interval}
       └─▶ Select ready jobs based on time
       └─▶ Dispatch to worker pool

2. Poller Worker Pool (500 Goroutines - ALL PARALLEL)
   └─▶ For each job (running concurrently):
       ├─▶ Worker 1: Device A polling (goroutine)
       ├─▶ Worker 2: Device B polling (goroutine)
       ├─▶ Worker 3: Device C polling (goroutine) 
       └─▶ Worker N: Device N polling (goroutine)
   
   Each worker independently:
       ├─▶ Get credentials for device
       ├─▶ Get SNMP client from pool (or create)
       ├─▶ Build SNMP request (Get/GetBulk/Walk)
       ├─▶ Send SNMP request (non-blocking)
       └─▶ Wait for response (with timeout)
       └─▶ Send to decoder channel (non-blocking)

3. SNMP Decoder (100 Workers - ALL PARALLEL)
   └─▶ Read from decoder channel
   └─▶ Parse SNMP response PDU (concurrent per response)
       ├─▶ Decode varbinds
       ├─▶ Resolve OIDs to names (cached MIB lookup)
       ├─▶ Convert SNMP types to native types
       └─▶ Extract index from OID
       └─▶ Send to producer channel (non-blocking)

4. Producer (100 Workers - ALL PARALLEL)
   └─▶ Read from producer channel
   └─▶ For each varbind (concurrent processing):
       ├─▶ Create metric message
       ├─▶ Add device metadata
       ├─▶ Add timestamp
       ├─▶ Normalize value (counter delta, gauge as-is)
       └─▶ Enrich with tags and metadata
       └─▶ Send to formatter channel (non-blocking)

5. Formatter (50 Workers - ALL PARALLEL)
   └─▶ Read from formatter channel
   └─▶ Serialize metric message (concurrent per message):
       ├─▶ JSON → JSON bytes
       ├─▶ Protobuf → proto wire format
       └─▶ Prometheus → exposition format
       └─▶ Send to transport channel (non-blocking)

6. Transport (50 Workers - ALL PARALLEL)
   └─▶ Read from transport channel
   └─▶ Send formatted message (concurrent):
       ├─▶ Kafka → Send to topic (async batching)
       ├─▶ TimeSeries → Write to database (async)
       └─▶ File → Write to file (buffered)

7. Scheduler Update (Async)
   └─▶ Schedule next poll (non-blocking)
       └─▶ Update device status and metrics

NOTE: All stages run concurrently via buffered channels.
      No stage blocks another. Total goroutines: ~800+
```

### SNMP Trap Flow (Massively Concurrent)

```
1. Trap Listener (16 UDP Sockets - SO_REUSEPORT)
   └─▶ UDP Socket 1 (Port 162) ──┐
   └─▶ UDP Socket 2 (Port 162) ──┤ All listening on same port
   └─▶ UDP Socket 3 (Port 162) ──┤ Kernel distributes packets
   └─▶ ... Socket N (Port 162) ──┘ across sockets (load balanced)
   
   Each socket (16 total):
       └─▶ Receive trap packet
       └─▶ Dispatch to worker pool (100 workers per socket)
       └─▶ Total: 16 × 100 = 1,600 concurrent trap processors

2. Trap Decoder Workers (1,600 Goroutines - ALL PARALLEL)
   └─▶ Each worker processes one trap concurrently:
       └─▶ Identify SNMP version
           ├─▶ v1: Parse v1 trap PDU
           ├─▶ v2c: Parse v2 trap PDU
           └─▶ v3: Decrypt and parse v3 trap PDU
       └─▶ Extract trap metadata
           ├─▶ Enterprise OID (v1)
           ├─▶ Trap OID (v2c/v3)
           ├─▶ Generic/Specific trap (v1)
           └─▶ Sender address
       └─▶ Parse varbinds
       └─▶ Send to MIB resolution channel (non-blocking)

3. MIB Resolution (100 Workers - PARALLEL)
   └─▶ Lookup trap OID in MIB (cached lookups)
       ├─▶ Get trap name
       ├─▶ Get description
       └─▶ Resolve varbind OIDs
       └─▶ Send to filter channel (non-blocking)

4. Trap Filtering (50 Workers - PARALLEL)
   └─▶ Apply filter rules concurrently
       ├─▶ Accept/Reject based on OID
       ├─▶ Rate limiting per device (lock-free counters)
       └─▶ Duplicate detection (bloom filter)
       └─▶ Send to event producer channel (non-blocking)

5. Event Producer (100 Workers - PARALLEL)
   └─▶ Create trap event message concurrently
       ├─▶ Add device info (from sender IP)
       ├─▶ Add trap metadata
       ├─▶ Map severity lookup tables)
       ├─▶ Add correlation ID
       └─▶ Apply enrichment rules
       └─▶ Send to formatter channel (non-blocking)

6. Formatter & Transport (Same as polling flow - PARALLEL)
   └─▶ 50 formatter workers + 50 transport workers

7. Inform Acknowledgment (if applicable) - BACKGROUND
   └─▶ Send inform response back to sender (async goroutine)
   └─▶ Does not block trap processing

NOTE: Total trap processing goroutines: ~2,000
      Can handle 100K+ traps/sec on modern hardware
      SO_REUSEPORT provides kernel-level load balancing
```

## Protocol-Specific Considerations

### SNMPv1

**Characteristics:**
- Simple community string authentication
- Limited data types
- 32-bit counters only
- Trap format different from v2c/v3

**Limitations:**
- No bulk operations
- No 64-bit counters (problematic for high-speed interfaces)
- No encryption
- Inefficient for large tables

**Use Cases:**
- Legacy devices
- Simple monitoring scenarios

### SNMPv2c

**Improvements over v1:**
- GetBulk operation for efficient table retrieval
- 64-bit counters (Counter64)
- Better error reporting
- Unified trap/inform format

**Limitations:**
- Still uses community strings (no encryption)

**Use Cases:**
- Most common SNMP version
- Good balance of features and compatibility

### SNMPv3

**Security Features:**
- User-based security model (USM)
- Authentication: MD5, SHA, SHA256, SHA512
- Privacy (encryption): DES, AES128, AES192, AES256, 3DES
- Multiple security levels:
  - noAuthNoPriv: No authentication, no encryption
  - authNoPriv: Authentication, no encryption
  - authPriv: Authentication and encryption

**Additional Features:**
- View-based access control (VACM)
- Context-based access

**Challenges:**
- More complex setup
- Key management
- Engine ID discovery
- Clock synchronization

**Use Cases:**
- Security-sensitive environments
- Compliance requirements (PCI-DSS, HIPAA)
- Production networks

### Trap vs Inform

**Traps (Fire-and-forget):**
- UDP-based, no acknowledgment
- Low overhead
- Possible loss
- v1, v2c, v3 support

**Informs (Acknowledged):**
- Requires acknowledgment
- Guaranteed delivery (with retries)
- Higher overhead
- v2c and v3 only

## MIB Management

### MIB Structure

```
├── iso (1)
    └── org (3)
        └── dod (6)
            └── internet (1)
                ├── mgmt (2)
                │   └── mib-2 (1)
                │       ├── system (1)
                │       ├── interfaces (2)
                │       ├── ip (4)
                │       └── ...
                ├── private (4)
                │   └── enterprises (1)
                │       ├── cisco (9)
                │       ├── hp (11)
                │       ├── juniper (2636)
                │       └── ...
                └── experimental (3)
```

### Essential MIBs

**Standard MIBs:**
- SNMPv2-MIB - System information
- IF-MIB - Interface statistics
- IP-MIB - IP statistics
- TCP-MIB - TCP statistics
- UDP-MIB - UDP statistics
- HOST-RESOURCES-MIB - System resources
- ENTITY-MIB - Physical entity information

**Vendor-Specific MIBs:**
- Cisco: CISCO-PROCESS-MIB, CISCO-MEMORY-POOL-MIB
- Juniper: JUNIPER-MIB
- HP/Aruba: HP-ICF-OID, ARUBA-MIB
- Dell: DELL-VENDOR-MIB

### MIB Loading Strategy

1. **Built-in MIBs**: Core standard MIBs compiled into binary
2. **On-demand Loading**: Load vendor MIBs as needed
3. **MIB Cache**: Cache parsed MIBs for performance
4. **MIB Repository**: Central repository for MIB files

## Polling Strategies

### Polling Methods

**Get** - Retrieve specific OIDs
```go
oids := []string{
    "1.3.6.1.2.1.1.3.0",  // sysUpTime
    "1.3.6.1.2.1.1.5.0",  // sysName
}
result, err := client.Get(oids)
```

**GetBulk** - Efficient retrieval of multiple OIDs
```go
// Retrieve first 50 interface octets
oid := "1.3.6.1.2.1.2.2.1.10"
result, err := client.GetBulk(0, 50, []string{oid})
```

**Walk** - Retrieve entire table
```go
oid := "1.3.6.1.2.1.2.2"  // ifTable
result, err := client.Walk(oid)
```

### Polling Intervals

**Fast Polling (10-30s):**
- Critical interfaces
- CPU/Memory on critical devices
- Use for alerting metrics

**Standard Polling (60-300s):**
- General interface statistics
- Most monitoring metrics
- Balance between granularity and load

**Slow Polling (5-60m):**
- Configuration data
- System information
- Rarely changing metrics

### Optimization Techniques

**Batching:**
```yaml
# Group OIDs by device and poll together
batch_size: 50
batch_timeout: 5s
```

**Parallel Polling:**
```yaml
# Poll multiple devices simultaneously
poller_workers: 100
rate_limit_per_device: 10  # Max 10 requests/sec per device
```

**Adaptive Polling:**
```go
// Reduce frequency if device is slow/unreachable
if pollDuration > timeout*0.8 {
    increaseInterval()
}
if consecutiveErrors > 3 {
    backoff()
}
```

**Delta Calculation:**
```go
// For Counter types, calculate rate
rate := (currentValue - previousValue) / timeDelta
```

## Performance Considerations

The architecture is **heavily optimized for concurrency and parallelism** using Go's goroutines and channels to achieve high throughput and low latency.

### Scalability Targets

- **Devices**: 1,000 - 10,000+ devices per instance
- **Metrics per Device**: 100 - 1,000 OIDs
- **Polling Frequency**: 30s - 300s
- **Traps**: 1,000 - 100,000 traps/sec
- **Latency**: < 1s from poll/trap to transport
- **CPU Cores**: Scales linearly with available cores

### Multi-Threading & Concurrency Architecture

#### 1. **Parallel Device Polling (Massive Concurrency)**

The poller uses a **worker pool pattern** to poll hundreds/thousands of devices simultaneously:

```go
// Configurable worker pool - typically 100-1000 workers
type PollerPool struct {
    workers      int           // e.g., 500 concurrent workers
    jobQueue     chan PollJob  // Buffered queue
    rateLimiter  *RateLimiter  // Per-device rate limiting
}

// Each worker runs in its own goroutine
func (p *PollerPool) worker(id int) {
    for job := range p.jobQueue {
        // Poll device concurrently
        go p.pollDevice(job.Device, job.Metrics)
    }
}

// Main scheduler feeds jobs to worker pool
func (s *Scheduler) Run() {
    // Spawn workers
    for i := 0; i < workers; i++ {
        go pollerPool.worker(i)
    }
    
    // Continuously schedule jobs
    for {
        readyJobs := s.getReadyJobs()
        for _, job := range readyJobs {
            pollerPool.jobQueue <- job  // Non-blocking send
        }
    }
}
```

**Key Features:**
- **N:M concurrency**: 500+ goroutines on 8-16 OS threads
- **Per-device parallelism**: Each device polled independently
- **Connection pooling**: Reuse SNMP connections per device
- **Rate limiting**: Prevent overwhelming individual devices

**Performance Benefit:**
- Can poll 1,000 devices every 60 seconds = ~16 devices/second
- With 500 workers, effective throughput: 500 concurrent requests
- Non-blocking: Slow devices don't block fast ones

#### 2. **Trap Listener Multi-Threading (Kernel-Level Load Balancing)**

Uses **SO_REUSEPORT** for kernel-level distribution across CPU cores:

```go
// One UDP socket per CPU core
func NewTrapListener(addr string, workers int) *TrapListener {
    cpus := runtime.NumCPU()  // e.g., 16 cores
    listeners := make([]*net.UDPConn, cpus)
    
    // Create one socket per core with SO_REUSEPORT
    for i := 0; i < cpus; i++ {
        lc := net.ListenConfig{
            Control: func(network, address string, c syscall.RawConn) error {
                return c.Control(func(fd uintptr) {
                    syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, 
                        syscall.SO_REUSEPORT, 1)
                })
            },
        }
        conn, _ := lc.ListenPacket(ctx, "udp", addr)
        listeners[i] = conn.(*net.UDPConn)
    }
    
    // Spawn receivers for each socket
    for i, conn := range listeners {
        go l.receiveTraps(i, conn)
    }
    
    return l
}

// Each receiver processes traps in parallel
func (l *TrapListener) receiveTraps(id int, conn *net.UDPConn) {
    buf := make([]byte, 65535)
    workerPool := make(chan []byte, 1000)
    
    // Spawn worker goroutines for processing
    for i := 0; i < 100; i++ {
        go l.processTrap(workerPool)
    }
    
    // Receive loop
    for {
        n, addr, _ := conn.ReadFromUDP(buf)
        packet := make([]byte, n)
        copy(packet, buf[:n])
        
        // Non-blocking send to worker pool
        select {
        case workerPool <- packet:
        default:
            // Drop on overflow (metrics will track)
        }
    }
}
```

**Key Features:**
- **SO_REUSEPORT**: Linux kernel distributes incoming UDP packets across sockets
- **Per-core sockets**: One socket per CPU core (16 cores = 16 sockets)
- **Worker pool per socket**: 100 workers × 16 sockets = 1,600 concurrent processors
- **Lock-free**: No shared state between socket receivers

**Performance Benefit:**
- Scales with CPU cores
- 100,000 traps/sec achievable on modern hardware
- No single-threaded bottleneck
- Kernel-level load balancing (zero contention)

#### 3. **Pipeline Parallelism (Non-Blocking Stages)**

Each stage of the pipeline runs concurrently with buffered channels:

```go
// Pipeline stages connected by buffered channels
type Pipeline struct {
    // Stage 1: Receive raw data
    rawData chan RawSNMP           // Buffer: 10,000
    
    // Stage 2: Decode
    decodedData chan DecodedMetric // Buffer: 10,000
    
    // Stage 3: Produce
    producedData chan ProducerMsg  // Buffer: 10,000
    
    // Stage 4: Format
    formattedData chan []byte      // Buffer: 10,000
    
    // Stage 5: Transport
    // (Kafka has internal buffering)
}

// Each stage runs in parallel goroutines
func (p *Pipeline) Start(workers int) {
    // Decoder stage - multiple workers
    for i := 0; i < workers; i++ {
        go p.decodeWorker()
    }
    
    // Producer stage - multiple workers
    for i := 0; i < workers; i++ {
        go p.produceWorker()
    }
    
    // Formatter stage - multiple workers
    for i := 0; i < workers; i++ {
        go p.formatWorker()
    }
    
    // Transport stage - multiple workers
    for i := 0; i < workers; i++ {
        go p.transportWorker()
    }
}

// Backpressure handling
func (p *Pipeline) produceWorker() {
    for decoded := range p.decodedData {
        produced := p.producer.Produce(decoded)
        
        // Non-blocking send with timeout
        select {
        case p.producedData <- produced:
            // Success
        case <-time.After(100 * time.Millisecond):
            // Backpressure - log and drop or retry
            metrics.BackpressureEvents.Inc()
        }
    }
}
```

**Key Features:**
- **Stage parallelism**: All stages run concurrently
- **Worker parallelism**: Multiple workers per stage
- **Buffered channels**: Smooth bursts, prevent blocking
- **Backpressure**: Graceful degradation under overload

**Performance Benefit:**
- No stage blocks others
- CPU utilization across all cores
- Burst tolerance via buffers
- Graceful handling of slow consumers (Kafka)

#### 4. **Connection Pooling & Reuse**

SNMP connections are pooled and reused to avoid overhead:

```go
type SNMPConnectionPool struct {
    pools sync.Map  // device -> connection pool
}

func (p *SNMPConnectionPool) Get(device string) *SNMPConn {
    pool, _ := p.pools.LoadOrStore(device, &sync.Pool{
        New: func() interface{} {
            return newSNMPConnection(device)
        },
    })
    return pool.(*sync.Pool).Get().(*SNMPConn)
}

func (p *SNMPConnectionPool) Put(device string, conn *SNMPConn) {
    if pool, ok := p.pools.Load(device); ok {
        pool.(*sync.Pool).Put(conn)
    }
}
```

**Performance Benefit:**
- Eliminates connection setup overhead
- Reduces GC pressure
- Better resource utilization

### Concurrency Model Summary

```
┌─────────────────────────────────────────────────────────────┐
│                    SNMP Collector                           │
│                                                             │
│  Scheduler (1 goroutine)                                   │
│       │                                                     │
│       ├──▶ Worker Pool (500 goroutines)                   │
│       │    ├──▶ Device 1 Poll (goroutine)                 │
│       │    ├──▶ Device 2 Poll (goroutine)                 │
│       │    └──▶ Device N Poll (goroutine)                 │
│       │                                                     │
│  Trap Listener (16 sockets × 100 workers = 1,600 goroutines)│
│       │                                                     │
│       ├──▶ Socket 1 → Worker Pool (100 goroutines)        │
│       ├──▶ Socket 2 → Worker Pool (100 goroutines)        │
│       └──▶ Socket N → Worker Pool (100 goroutines)        │
│                                                             │
│  Pipeline Stages (Each with 10-100 workers)               │
│       ├──▶ Decoders (100 goroutines)                      │
│       ├──▶ Producers (100 goroutines)                     │
│       ├──▶ Formatters (50 goroutines)                     │
│       └──▶ Transports (50 goroutines)                     │
│                                                             │
│  Total: ~3,000-5,000 goroutines on 8-16 OS threads       │
└─────────────────────────────────────────────────────────────┘
```

**Concurrency Primitives:**
- **Goroutines**: Lightweight threads (2KB stack vs. 2MB OS thread)
- **Channels**: Type-safe, thread-safe message passing
- **sync.Pool**: Lock-free object pooling
- **sync.Map**: Concurrent map for connection pools
- **Context**: Cancellation and timeout propagation
- **Select**: Non-blocking channel operations

### Performance Tuning Knobs

```bash
# Poller workers (concurrent device polls)
-poller.workers=500

# Trap listener workers per socket
-trap.workers.per.socket=100

# Pipeline stage workers
-pipeline.decoder.workers=100
-pipeline.producer.workers=100
-pipeline.formatter.workers=50
-pipeline.transport.workers=50

# Buffer sizes
-pipeline.buffer.size=10000

# Rate limiting
-poller.rate.limit.per.device=10  # Max 10 req/sec per device
-poller.max.concurrent.per.device=4

# Connection pool
-snmp.connection.pool.max.idle=100
-snmp.connection.pool.max.open=1000
```

### CPU & Memory Scaling

**CPU Utilization:**
- Scales linearly with cores up to ~32 cores
- Beyond 32 cores, network I/O becomes bottleneck
- GOMAXPROCS automatically set to runtime.NumCPU()

**Memory Usage:**
- Base: 100MB
- Per device: ~1KB (metadata + state)
- Per goroutine: ~2KB (stack)
- Per buffer: ~100KB (channel buffers)
- Total for 10K devices: ~500MB-1GB

**Recommended Specs:**
- Small (< 1K devices): 2 cores, 1GB RAM
- Medium (1K-5K): 8 cores, 4GB RAM
- Large (5K-10K): 16 cores, 8GB RAM
- XL (10K+): 32 cores, 16GB RAM

### Worker Thread Calculation Guide

#### How to Calculate Optimal Workers

**Formula for Poller Workers:**
```
poller.workers = (num_devices × polls_per_interval) / avg_poll_duration

Example:
- 1,000 devices
- Poll every 60 seconds
- Average poll takes 0.5 seconds

Required throughput: 1,000 / 60 = ~17 devices/sec
With 0.5s poll time: need 17 × 0.5 = 8.5 concurrent workers
Add buffer (3x): 8.5 × 3 = 25 workers minimum
Recommended: 50-100 workers for burst handling
```

**Formula for Trap Workers:**
```
trap.workers.per.socket = (expected_traps_per_sec / processing_time) / num_cpu_cores

Example:
- 10,000 traps/sec expected
- 1ms processing time per trap
- 16 CPU cores

Per core: 10,000 / 16 = 625 traps/sec
Workers needed: 625 × 0.001 = 0.625 per core
Add buffer (100x): 0.625 × 100 = 62.5
Recommended: 100 workers per socket
```

**Formula for Pipeline Workers:**
```
pipeline_stage.workers = (input_rate × processing_time) × safety_factor

Example for decoder stage:
- 1,000 messages/sec input
- 5ms decode time
- 2x safety factor

Workers: 1,000 × 0.005 × 2 = 10 workers
Recommended: Round up to 50-100 for burst tolerance
```

#### Resource-Based Worker Limits

**CPU-Bound Stages (decoder, formatter):**
```
max_workers = num_cpu_cores × 10 to 20

Rationale: Go scheduler can handle 10-20 goroutines per core efficiently
Example: 8 cores → 80-160 workers maximum
```

**I/O-Bound Stages (poller, transport):**
```
max_workers = num_cpu_cores × 50 to 200

Rationale: I/O waiting allows more goroutines without blocking
Example: 8 cores → 400-1600 workers possible
```

**Memory Constraint:**
```
max_total_goroutines = available_memory / (2KB + buffer_overhead)

Example:
- 4GB available RAM
- 2KB per goroutine
- 1MB channel buffers per stage

Goroutine capacity: 4GB / 2KB = ~2 million goroutines
Practical limit with buffers: ~100,000 goroutines
```

#### Tuning Methodology

**Step 1: Start with Defaults**
```bash
# Use recommended defaults for your size
./snmpcollector -poller.workers=500 -trap.workers.per.socket=100
```

**Step 2: Monitor Key Metrics**
```bash
# Watch for bottlenecks
watch -n 1 'curl -s localhost:8080/metrics | grep -E "(queue_size|goroutines|cpu)"'
```

**Step 3: Identify Bottleneck Stage**
```
If queue_size increasing:
  - poll_queue_size → Increase poller.workers
  - decoder_queue_size → Increase pipeline.decoder.workers
  - producer_queue_size → Increase pipeline.producer.workers
  - formatter_queue_size → Increase pipeline.formatter.workers
  - transport_queue_size → Increase pipeline.transport.workers or check Kafka
```

**Step 4: Incremental Tuning**
```bash
# Increase by 25% at a time
-poller.workers=625  # was 500

# Wait 5 minutes and observe
# If queue still growing, increase again
# If CPU > 90%, you've hit the limit
```

**Step 5: Load Test**
```bash
# Simulate peak load (2x normal)
./snmp-simulator --devices 2000 --trap-rate 20000

# Verify:
# - Queues don't grow unbounded
# - CPU < 85%
# - Memory stable
# - No dropped messages
```

#### Advanced: Per-Stage Tuning Matrix

| Stage | CPU Intensive | I/O Intensive | Recommended Workers | Buffer Size |
|-------|---------------|---------------|---------------------|-------------|
| Poller | Low | High | 100-2000 | 10000 |
| Trap Receiver | Low | High | 50-200 per socket | N/A |
| Decoder | High | Low | 50-200 | 10000 |
| MIB Resolver | Medium | Medium | 50-100 | 5000 |
| Producer | Medium | Low | 50-200 | 10000 |
| Formatter | High | Low | 25-100 | 5000 |
| Transport | Low | High | 25-100 | 5000 |

#### Real-World Tuning Examples

**Example 1: High Device Count, Low Trap Volume**
```bash
# Prioritize polling
-poller.workers=2000
-trap.workers.per.socket=50
-pipeline.decoder.workers=200
-pipeline.producer.workers=150
-pipeline.formatter.workers=100
-pipeline.transport.workers=100
```

**Example 2: Low Device Count, High Trap Volume**
```bash
# Prioritize trap handling
-poller.workers=100
-trap.listener.sockets=16  # All CPU cores
-trap.workers.per.socket=200
-pipeline.decoder.workers=300
-pipeline.producer.workers=200
-pipeline.formatter.workers=100
-pipeline.transport.workers=150
```

**Example 3: CPU-Constrained System**
```bash
# Reduce workers to match available CPU
-gomaxprocs=4  # Limit to 4 cores
-poller.workers=200
-trap.workers.per.socket=50
-pipeline.decoder.workers=40
-pipeline.producer.workers=40
-pipeline.formatter.workers=20
-pipeline.transport.workers=20
-pipeline.buffer.size=5000  # Smaller buffers
```

**Example 4: Memory-Constrained System**
```bash
# Reduce buffers and workers
-poller.workers=200
-trap.workers.per.socket=50
-pipeline.decoder.workers=50
-pipeline.producer.workers=50
-pipeline.formatter.workers=25
-pipeline.transport.workers=25
-pipeline.buffer.size=2000  # Much smaller buffers
-snmp.connection.pool.max.open=500
```

#### Monitoring Worker Efficiency

**Key Metrics:**
```bash
# Worker utilization
goroutines_active / goroutines_total
# Target: > 0.7 (70% actively working)

# Queue efficiency
avg(queue_size) / buffer_size
# Target: < 0.5 (not regularly full)

# Throughput per worker
messages_processed / goroutines_active
# Higher is better

# Context switches (Linux)
cat /proc/$(pgrep snmpcollector)/status | grep voluntary_ctxt_switches
# Lower is better (less contention)
```

### Memory Management

**Connection Pooling:**
```go
// Reuse SNMP connections per device
connPool := NewConnectionPool(
    maxIdleConns: 100,
    maxOpenConns: 1000,
    connTimeout:  30 * time.Second,
)
```

**MIB Cache:**
```go
// LRU cache for MIB lookups
mibCache := NewLRUCache(
    maxEntries: 10000,
    ttl:        1 * time.Hour,
)
```

**Metric Buffering:**
```go
// Bounded buffer for metrics
metricBuffer := make(chan Metric, 10000)
```

### Network Optimization

**UDP Tuning:**
```bash
# Increase receive buffer
net.core.rmem_max = 26214400
net.core.rmem_default = 26214400
```

**SNMP Request Optimization:**
- Use GetBulk instead of Get for tables
- Limit max-repetitions to avoid timeouts
- Adjust PDU size based on network MTU

## Deployment Patterns

### Single Instance

```
        ┌──────────────────┐
        │ SNMP Collector   │
Devices │  - Poller        │ → Kafka/TS
   →    │  - Trap Listener │
        └──────────────────┘
```

**Use Case**: Small environments (< 1K devices)

### High Availability

```
                  ┌──────────────┐
         ┌───────▶│ Collector 1  │───┐
Devices  │        └──────────────┘   │
   →     │                            ├─→ Kafka
         │        ┌──────────────┐   │
         └───────▶│ Collector 2  │───┘
                  └──────────────┘
                  
         (Polling coordination via shared state)
```

**Use Case**: No single point of failure

### Distributed Polling

```
Region 1 Devices → Collector Pod 1 ─┐
                                     ├─→ Central Kafka
Region 2 Devices → Collector Pod 2 ─┘

(Device inventory sharded by region/rack/site)
```

**Use Case**: Geographic distribution

### Dedicated Trap Collectors

```
                   ┌─────────────┐
All Devices ──────▶│  Trap-only  │
(Port 162)         │  Collectors │─→ Kafka
                   │  (Multiple) │
                   └─────────────┘
                   
                   ┌─────────────┐
Selected Devices ─▶│   Pollers   │─→ Kafka
                   └─────────────┘
```

**Use Case**: High trap volume separation

## Monitoring and Alerting

### Health Checks

**Collector Health:**
```
/health/live    → Liveness (process alive)
/health/ready   → Readiness (accepting traffic)
/health/startup → Startup probe
```

**Device Health:**
```
/api/devices              → List all devices and status
/api/devices/:id/status   → Specific device status
/api/devices/:id/metrics  → Latest metrics for device
```

### Key Metrics to Monitor

**Operational Metrics:**
- Poll success rate per device
- Average poll duration
- Trap reception rate
- Queue depths
- Error rates

**Business Metrics:**
- Number of monitored devices
- Metrics per second
- Device availability percentage
- SLA compliance

### Alerting Rules

```yaml
alerts:
  - name: HighPollFailureRate
    expr: rate(poll_errors_total[5m]) > 0.1
    severity: warning
    
  - name: DeviceUnreachable
    expr: device_status == 0
    duration: 5m
    severity: critical
    
  - name: TrapProcessingBacklog
    expr: trap_queue_size > 10000
    severity: warning
```

## Security Considerations

### Authentication Best Practices

**SNMPv1/v2c:**
- Use non-default community strings
- Different communities for read-only and read-write
- Rotate community strings regularly
- Restrict access by source IP

**SNMPv3:**
- Always use authPriv (authentication + encryption)
- Use strong authentication: SHA256 or SHA512
- Use strong encryption: AES256
- Implement password policies
- Regular credential rotation

### Network Security

**Firewall Rules:**
```bash
# Allow SNMP polling (outbound from collector)
Allow TCP/UDP 161 from COLLECTOR_IP to DEVICE_NETWORK

# Allow SNMP traps (inbound to collector)
Allow UDP 162 from DEVICE_NETWORK to COLLECTOR_IP
```

**Segmentation:**
- Dedicated management network for SNMP
- VLANs for device isolation
- Jump hosts for access

### Data Security

**Sensitive Data:**
- Encrypt credentials at rest (HashiCorp Vault, AWS Secrets Manager)
- Mask sensitive OIDs (passwords, keys) in logs
- TLS for transport layer (Kafka, TimeSeries writes)

**Compliance:**
- Audit logging for configuration changes
- Access control for API endpoints
- Data retention policies

## Extensibility

The architecture is designed to be extensible while currently focusing on JSON output. New formatters and components can be added by implementing well-defined interfaces.

### Current Implementation Status

**✅ Implemented:**
- JSON formatter
- Kafka transport
- File transport
- SNMP v1/v2c/v3 client
- MIB parser and registry
- Device/device group/object group/object configuration system
- Enumeration support

**🔄 Extensible (Interface-ready):**
- Additional formatters (Protobuf, Prometheus, InfluxDB)
- Additional transports (TimeSeries DBs, Elasticsearch)
- Custom enrichment plugins
- Alert routing engines

### Adding Custom Formatters

The formatter interface enables new output formats without modifying core code:

```go
// Formatter interface
type Formatter interface {
    Format(metric *SNMPMetric) ([]byte, error)
}

// Example: Adding Prometheus formatter
type PrometheusFormatter struct {
    // configuration
}

func (f *PrometheusFormatter) Format(metric *SNMPMetric) ([]byte, error) {
    // Convert to Prometheus exposition format
    var buf bytes.Buffer
    
    // Build metric name
    metricName := sanitizeMetricName(metric.Name)
    
    // Build labels
    labels := make([]string, 0)
    for k, v := range metric.Tags {
        labels = append(labels, fmt.Sprintf(`%s="%s"`, k, v))
    }
    
    // Format: metric_name{labels} value timestamp
    fmt.Fprintf(&buf, "%s{%s} %v %d\n",
        metricName,
        strings.Join(labels, ","),
        metric.Value,
        metric.Timestamp.UnixMilli(),
    )
    
    return buf.Bytes(), nil
}

// Register formatter
func init() {
    format.Register("prometheus", &PrometheusFormatter{})
}
```

### Adding Custom Transports

Implement the transport interface to send data to new destinations:

```go
// Transport interface
type Transport interface {
    Send(data []byte) error
    Close() error
}

// Example: Adding HTTP webhook transport
type WebhookTransport struct {
    url    string
    client *http.Client
    headers map[string]string
}

func (t *WebhookTransport) Send(data []byte) error {
    req, err := http.NewRequest("POST", t.url, bytes.NewReader(data))
    if err != nil {
        return err
    }
    
    // Add headers
    for k, v := range t.headers {
        req.Header.Set(k, v)
    }
    req.Header.Set("Content-Type", "application/json")
    
    resp, err := t.client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode >= 300 {
        return fmt.Errorf("webhook returned status %d", resp.StatusCode)
    }
    
    return nil
}

func (t *WebhookTransport) Close() error {
    t.client.CloseIdleConnections()
    return nil
}

// Register transport
func init() {
    transport.Register("webhook", NewWebhookTransport)
}
```

### Adding Custom Syntax Types

Extend the syntax type system for custom unit conversions:

```go
// Example: Adding custom bandwidth syntax for non-standard units
type CustomBandwidthKilobitsPerHour struct{}

func (s *CustomBandwidthKilobitsPerHour) Convert(value interface{}) (float64, error) {
    // Convert kilobits/hour to bits/second
    asFloat, err := toFloat64(value)
    if err != nil {
        return 0, err
    }
    
    // Kbps/hour to bps/sec: divide by 3600, multiply by 1000
    return (asFloat * 1000) / 3600, nil
}

// Register syntax type
func init() {
    syntax.Register("BandwidthKilobitsPerHour", &CustomBandwidthKilobitsPerHour{})
}
```

### Plugin Architecture (Future)

For production deployments, consider a plugin system:

```go
// Plugin interface for dynamic loading
type Plugin interface {
    Name() string
    Version() string
    Init(config map[string]interface{}) error
    
    // Optional interfaces that plugins can implement
    Formatter
    Transport
    Enricher
}

// Load plugins from directory
pluginDir := "/opt/snmpcollector/plugins"
plugins, err := loader.LoadPlugins(pluginDir)
```

## Testing

### Unit Tests

```go
// Test SNMP decoder
func TestDecodeGetResponse(t *testing.T) {
    pdu := buildTestPDU()
    result, err := decoder.Decode(pdu)
    assert.NoError(t, err)
    assert.Equal(t, expected, result)
}

// Test MIB resolution
func TestOIDResolution(t *testing.T) {
    name, _ := mibRegistry.OIDToName("1.3.6.1.2.1.1.5.0")
    assert.Equal(t, "sysName.0", name)
}
```

### Integration Tests

```go
// Test with SNMP simulator
func TestPollDevice(t *testing.T) {
    simulator := snmptest.NewSimulator()
    defer simulator.Stop()
    
    poller := NewPoller(config)
    result, err := poller.Poll(simulator.Address())
    assert.NoError(t, err)
    assert.Len(t, result, expectedMetrics)
}
```

### Load Tests

```bash
# Simulate 1000 devices
./snmp-simulator --devices 1000 --port-range 20000-21000

# Run collector and measure
./snmpcollector --devices devices-1k.yaml

# Check metrics
curl localhost:8080/metrics | grep poll_duration
```

## Future Enhancements

### Output Formats
- **Protobuf Binary**: Efficient binary serialization
- **Prometheus Remote Write**: Native Prometheus integration
- **InfluxDB Line Protocol**: Direct InfluxDB ingestion
- **OpenTelemetry**: OTLP metrics format
- **Avro**: Schema-based serialization
- **Parquet**: Columnar format for analytics

### Protocol Support
- NETCONF/RESTCONF for modern devices
- gNMI (gRPC Network Management Interface)
- Streaming telemetry
- YANG models
- NETCONF notifications

### Features
- Machine learning for anomaly detection
- Automatic device discovery (LLDP, CDP, OSPF neighbors)
- Config backup and change detection
- Automatic baseline establishment
- Predictive alerting
- Graph-based topology mapping
- Multi-tenancy support

### Scalability
- Kubernetes operator for auto-scaling
- gRPC for internal communication
- Time series database optimizations
- Edge computing for remote sites
- Distributed polling coordination (etcd, Consul)
- Hot reload of configuration

### Integrations
- ServiceNow for incident management
- PagerDuty/OpsGenie for alerting
- Terraform for device inventory
- GitOps for configuration management
- Webhook integrations
- Custom alerting engines

## References

### Standards
- RFC 1157 - SNMPv1
- RFC 1905 - SNMPv2 Protocol Operations
- RFC 3411-3418 - SNMPv3
- RFC 2578 - SNMPv2 SMI

### Related Projects
- Net-SNMP - Reference SNMP implementation
- Prometheus SNMP Exporter
- Telegraf SNMP plugin
- LibreNMS - Network monitoring platform

### Documentation
- [Device Configuration Guide](device-configuration.md)
- [MIB Management Guide](mib-management.md)
- [Troubleshooting Guide](troubleshooting-snmp.md)
- [Performance Tuning](performance-snmp.md)
- [Security Hardening](security-snmp.md)

## Contributing

For architecture changes, ensure:
1. Backward compatibility with existing device configs
2. MIB compatibility testing
3. Performance benchmarks with large device inventory
4. Security review for credential handling
5. Documentation updates
6. Migration guide for breaking changes

---

**License**: MIT  
**Contact**: network-monitoring-team@example.com  
**Repository**: https://github.com/yourorg/snmpcollector
