# Models Package

## Overview

`models/` is the **shared data contract** for the entire collector. Every pipeline stage — decoder, producer, formatter, transport — imports this package and nothing in it imports any other internal package.

All types serialize to JSON using the field tags shown below. This is the wire format output by the formatter stage.

---

## `models/metric.go` — Runtime Data

### `SNMPMetric`

Top-level output payload produced per polling cycle. One document per device per object per poll interval.

```go
type SNMPMetric struct {
    Timestamp time.Time      `json:"timestamp"`
    Device    Device         `json:"device"`
    Metrics   []Metric       `json:"metrics"`
    Metadata  MetricMetadata `json:"metadata,omitempty"`
}
```

**JSON example:**
```json
{
  "timestamp": "2026-02-26T10:30:00.123Z",
  "device": { "hostname": "router01.example.com", "ip_address": "192.168.1.1", "snmp_version": "2c" },
  "metrics": [ ... ],
  "metadata": { "collector_id": "collector-01", "poll_duration_ms": 245, "poll_status": "success" }
}
```

---

### `Device`

Identifies the monitored network device. Populated from device YAML config; optional fields like `SysDescr` are populated after the first `SNMPv2-MIB::system` poll.

| JSON field | Go field | Type | Notes |
|---|---|---|---|
| `hostname` | `Hostname` | `string` | From device config key |
| `ip_address` | `IPAddress` | `string` | From `ip:` in device config |
| `snmp_version` | `SNMPVersion` | `string` | `"1"`, `"2c"`, or `"3"` |
| `vendor` | `Vendor` | `string` | Optional; derived from sysDescr parsing |
| `model` | `Model` | `string` | Optional |
| `sys_descr` | `SysDescr` | `string` | Optional; from `SNMPv2-MIB::sysDescr.0` |
| `sys_location` | `SysLocation` | `string` | Optional |
| `sys_contact` | `SysContact` | `string` | Optional |
| `tags` | `Tags` | `map[string]string` | Static labels from device config |

---

### `Metric`

A single resolved SNMP variable binding. The `Value` field is a native Go type after conversion; downstream consumers should type-switch on it.

| JSON field | Go field | Type | Notes |
|---|---|---|---|
| `oid` | `OID` | `string` | Full numeric OID, no leading dot |
| `name` | `Name` | `string` | Config name, e.g. `"netif.bytes.in"` |
| `instance` | `Instance` | `string` | Table row index, e.g. `"1"`. Empty for scalars |
| `value` | `Value` | `interface{}` | `int64`, `uint64`, `float64`, `string`, `[]byte`, or `bool` |
| `type` | `Type` | `string` | SNMP PDU type, e.g. `"Counter64"` |
| `syntax` | `Syntax` | `string` | Config syntax, e.g. `"Counter64"`, `"BandwidthMBits"` |
| `tags` | `Tags` | `map[string]string` | Dimension attributes, e.g. `{"netif.descr": "Gi0/0/1"}` |

**`Value` type mapping:**

| Syntax category | Go type |
|---|---|
| Integer types (`Integer`, `Integer32`, `EnumInteger`, …) | `int64` |
| Counter/Gauge types (`Counter32`, `Counter64`, `Gauge32`, `TimeTicks`, …) | `uint64` |
| Unit-scaled types (`BandwidthMBits`, `BytesMiB`, `TemperatureDeciC`, …) | `float64` |
| String types (`DisplayString`, `MacAddress`, `IpAddress`, …) | `string` |
| Binary types (non-printable OctetString) | `[]byte` |

---

### `MetricMetadata`

Operational metadata attached to every `SNMPMetric`. Used to monitor collector health.

| JSON field | Go field | Type | Notes |
|---|---|---|---|
| `collector_id` | `CollectorID` | `string` | From `-metrics.addr` config, identifies collector instance |
| `poll_duration_ms` | `PollDurationMs` | `int64` | Round-trip SNMP request latency |
| `poll_status` | `PollStatus` | `string` | `"success"`, `"timeout"`, or `"error"` |

---

### `SNMPTrap`

Top-level payload for a received SNMP trap or inform notification.

```go
type SNMPTrap struct {
    Timestamp time.Time `json:"timestamp"`
    Device    Device    `json:"device"`
    TrapInfo  TrapInfo  `json:"trap_info"`
    Varbinds  []Metric  `json:"varbinds"`
}
```

`Varbinds` reuses `[]Metric` — the same `Name`, `Value`, `Type` fields apply. `Metric.Tags` and `Metric.Instance` are typically empty for trap varbinds.

---

### `TrapInfo`

Trap-specific header fields. Some fields are version-specific.

| JSON field | Go field | Type | Present in |
|---|---|---|---|
| `version` | `Version` | `string` | All — `"v1"`, `"v2c"`, `"v3"` |
| `enterprise_oid` | `EnterpriseOID` | `string` | v1 only |
| `generic_trap` | `GenericTrap` | `int32` | v1 only (0–6) |
| `specific_trap` | `SpecificTrap` | `int32` | v1 only |
| `trap_oid` | `TrapOID` | `string` | v2c / v3 — `SNMPv2-MIB::snmpTrapOID.0` value |
| `trap_name` | `TrapName` | `string` | All — resolved MIB name, e.g. `"linkDown"` |
| `severity` | `Severity` | `string` | All — `"info"`, `"warning"`, `"critical"` |

---

## `models/config.go` — Configuration Types

These types are the in-memory parsed form of the YAML object definition files. They are produced by the config loader and consumed by the decoder (and later the producer).

### `ObjectDefinition`

Represents one object YAML file, e.g. `IF-MIB_ifEntry.yml`.

| Field | Type | Source YAML key | Notes |
|---|---|---|---|
| `Key` | `string` | (file-level map key) | e.g. `"IF-MIB::ifEntry"` |
| `MIB` | `string` | `mib:` | e.g. `"IF-MIB"` |
| `Object` | `string` | `object:` | e.g. `"ifEntry"` |
| `Augments` | `string` | `augments:` | Key of augmented object |
| `Index` | `[]IndexDefinition` | `index:` | Empty for scalars |
| `DiscoveryAttribute` | `string` | `discovery_attribute:` | Attribute name used for row detection |
| `Attributes` | `map[string]AttributeDefinition` | `attributes:` | SNMP attribute name → definition |

**YAML → struct mapping example:**
```yaml
IF-MIB::ifEntry:
  mib: IF-MIB
  object: ifEntry
  discovery_attribute: ifDescr
  attributes:
    ifInOctets:                          # → map key
      oid: .1.3.6.1.2.1.2.2.1.10        # → AttributeDefinition.OID
      name: netif.bytes.in               # → AttributeDefinition.Name
      syntax: Counter32                  # → AttributeDefinition.Syntax
```

---

### `AttributeDefinition`

One column/field within an SNMP table object.

| Field | Type | Source YAML key | Notes |
|---|---|---|---|
| `OID` | `string` | `oid:` | Numeric OID, leading dot optional |
| `Name` | `string` | `name:` | Output metric name |
| `Syntax` | `string` | `syntax:` | Conversion hint; see [decoder syntax table](decoder.md#conversion-table) |
| `IsTag` | `bool` | `tag: true` | Stores value in `Metric.Tags` rather than `Metric.Value` |
| `Overrides` | `*OverrideReference` | `overrides:` | Declares this supersedes an older attribute |
| `Rediscover` | `string` | `rediscover:` | `""`, `"OnChange"`, or `"OnReset"` |

---

### `IndexDefinition`

One component of a table's composite OID index. The order in the slice matches the OID arc order.

| Field | Type | Source YAML key | Notes |
|---|---|---|---|
| `Type` | `string` | `type:` | Encoding type: `Integer`, `IpAddress`, `OctetString`, `MacAddress`, etc. |
| `OID` | `string` | `oid:` | OID of the index column |
| `Name` | `string` | `name:` | Semantic name for this index component |
| `Syntax` | `string` | `syntax:` | Display hint for the index value |

---

### `OverrideReference`

Identifies the attribute that a newer (typically higher-precision) attribute supersedes.

```yaml
# IF-MIB::ifXEntry
ifHCInOctets:
  oid: .1.3.6.1.2.1.31.1.1.1.6
  name: netif.bytes.in
  syntax: Counter64
  overrides:
    object: IF-MIB::ifEntry     # → OverrideReference.Object
    attribute: ifInOctets       # → OverrideReference.Attribute
```

The Producer uses `OverrideReference` to drop the 32-bit `ifInOctets` value when the 64-bit `ifHCInOctets` is available for the same instance.

---

## Design Constraints

- **`models/` imports only the Go standard library.** No external or internal dependencies. This ensures all pipeline stages can import it without creating import cycles.
- **`Metric.Value` is `interface{}`**, not a `oneof`. This keeps the JSON formatter simple and avoids protobuf dependency in the hot path. The Protobuf formatter (future) will type-switch on the value when serialising.
- **Config types live in `models/`**, not in `pkg/snmpcollector/config/`, because both the config loader *and* the decoder need them. Putting them in the decoder would create an import cycle when the producer also needs them.
