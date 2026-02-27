# Trap Protocol Parser — `snmp/trap`

## Overview

`snmp/trap` handles the **protocol-level** conversion of raw gosnmp
`SnmpPacket` values into `models.SNMPTrap`. It has no knowledge of UDP socket
management — that responsibility belongs to
[`pkg/snmpcollector/trapreceiver`](trapreceiver.md).

```
gosnmp.TrapListener
        │
        ▼ *gosnmp.SnmpPacket + *net.UDPAddr
   snmp/trap.Parse()
        │
        ▼
   models.SNMPTrap  →  chan models.SNMPTrap
```

---

## Entry point

```go
func Parse(pkt *gosnmp.SnmpPacket, remoteAddr *net.UDPAddr) (models.SNMPTrap, error)
```

Dispatches to the appropriate version-specific parser based on `pkt.Version`.
Returns an error only for nil input or a missing mandatory header varbind
(v2c/v3 `snmpTrapOID.0`).

Inform PDUs (`PDUType == InformRequest`) are parsed identically to traps;
the gosnmp `TrapListener` sends the acknowledgement automatically.

---

## Version differences

### SNMPv1 traps

v1 PDUs carry all trap metadata directly in the PDU header fields (embedded in
`gosnmp.SnmpTrap`):

| gosnmp field | Meaning |
|---|---|
| `Enterprise` | Enterprise OID of the trap sender |
| `AgentAddress` | Originating agent IP (may differ from UDP source) |
| `GenericTrap` | Generic trap type (0–6) |
| `SpecificTrap` | Enterprise-specific trap code (when `GenericTrap == 6`) |
| `Timestamp` | Sender's sysUpTime at time of trap |

**TrapOID synthesis (RFC 3584 §3.1)**:

| `GenericTrap` | Synthesised TrapOID |
|---|---|
| 0–5 | `.1.3.6.1.6.3.1.1.5.<GenericTrap+1>` |
| 6 (enterprise-specific) | `<Enterprise>.0.<SpecificTrap>` |

The `Device.IPAddress` is taken from `AgentAddress` (not the UDP source address)
to preserve the original sender identity through NAT.

### SNMPv2c / SNMPv3 traps

v2c and v3 traps carry all metadata in the varbind list. The first two
varbinds are mandatory header varbinds:

| Position | OID | Meaning |
|---|---|---|
| 0 | `.1.3.6.1.2.1.1.3.0` (`sysUpTime.0`) | Sender's sysUpTime |
| 1 | `.1.3.6.1.6.3.1.1.4.1.0` (`snmpTrapOID.0`) | Actual trap OID (ObjectIdentifier value) |

Both header varbinds are stripped before populating `SNMPTrap.Varbinds`; only
the payload varbinds (index 2+) are included.

`Parse` returns an error if `snmpTrapOID.0` is missing.

---

## PDU value type mapping

`convertVarbinds` maps each `gosnmp.SnmpPDU` value to a canonical Go type:

| gosnmp `Asn1BER` | Go type stored in `Metric.Value` |
|---|---|
| `Integer` | `int64` |
| `Counter32`, `Gauge32`, `TimeTicks`, `Opaque` | `uint64` |
| `Counter64` | `uint64` |
| `OctetString` | `string` (printable ASCII); `[]byte` otherwise |
| `ObjectIdentifier` | `string` (normalised, leading dot) |
| `IpAddress` | `string` (e.g. `"192.168.1.1"`) |
| `Boolean` | `bool` |
| `Float` | `float64` |
| `Double` | `float64` |

Error-typed PDUs (`NoSuchObject`, `NoSuchInstance`, `EndOfMibView`, `Null`)
are silently skipped.

---

## OID normalisation

All OIDs stored in `models.SNMPTrap` are normalised:
- Leading dot ensured.
- Trailing dot removed.

---

## Device construction

| SNMP version | `Device.IPAddress` source |
|---|---|
| v1 | `pkt.AgentAddress` (preserves NAT transparency) |
| v2c / v3 | UDP source IP (`remoteAddr.IP.String()`) |

`Device.SNMPVersion` is set to `"1"`, `"2c"`, or `"3"` accordingly.

---

## Error handling

| Condition | Result |
|---|---|
| `pkt == nil` | `error: "trap: nil packet"` |
| `remoteAddr == nil` (v2c/v3) | `Device.IPAddress = ""` (no error) |
| `snmpTrapOID.0` missing in v2c/v3 PDU | `error: "trap: missing snmpTrapOID.0 varbind"` |
| Error-typed PDU in varbind list | Silently skipped |

---

## Tests (11 total)

| Test | What it verifies |
|---|---|
| `TestParse_V1_LinkDown` | v1 generic trap (type 3) → correct TrapOID + Device.IPAddress from AgentAddress |
| `TestParse_V1_EnterpriseSpecific` | v1 enterprise-specific trap (generic=6) → `<enterprise>.0.<specific>` OID |
| `TestParse_V2c_LinkDown` | v2c trap with correct header varbinds → TrapOID extracted, headers stripped |
| `TestParse_V2c_MissingTrapOID` | v2c PDU without `snmpTrapOID.0` → error returned |
| `TestParse_V3_Trap` | v3 trap → identical parse path to v2c |
| `TestParse_InformRequest` | Inform PDU → parsed identically to a trap |
| `TestParse_NilPacket` | nil `pkt` → error returned |
| `TestParse_NilRemoteAddr` | nil `remoteAddr` → no panic, empty IP |
| `TestParse_VarbindTypes` | All value types → correct Go types in `Metric.Value` |
| `TestParse_ErrorPDUsSkipped` | `NoSuchObject` / `NoSuchInstance` / `EndOfMibView` → not included in Varbinds |
| `TestParse_TimestampIsRecent` | `SNMPTrap.Timestamp` is within 5 s of `time.Now()` |
