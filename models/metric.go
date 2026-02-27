// Package models defines the core data structures shared across all layers of
// the SNMP Collector. These types represent the canonical in-memory form of all
// collected data; every other package depends on this package and nothing here
// depends on any other internal package.
package models

import "time"

// SNMPMetric is the top-level payload produced per polling cycle.
// It contains everything the downstream pipeline (formatter → transport) needs:
// the originating device, all collected metric values, and collection metadata.
type SNMPMetric struct {
	Timestamp time.Time      `json:"timestamp"`
	Device    Device         `json:"device"`
	Metrics   []Metric       `json:"metrics"`
	Metadata  MetricMetadata `json:"metadata,omitempty"`
}

// Device carries identifying information about the monitored network device.
// Optional fields are populated as they become known (e.g. from sysDescr polling).
type Device struct {
	Hostname    string            `json:"hostname"`
	IPAddress   string            `json:"ip_address"`
	SNMPVersion string            `json:"snmp_version"`           // "1", "2c", or "3"
	Vendor      string            `json:"vendor,omitempty"`
	Model       string            `json:"model,omitempty"`
	SysDescr    string            `json:"sys_descr,omitempty"`
	SysLocation string            `json:"sys_location,omitempty"`
	SysContact  string            `json:"sys_contact,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"` // Static labels from device config
}

// Metric represents a single resolved SNMP variable binding. The Value field is
// already converted to a native Go type by the decoder (int64, uint64, float64,
// string, []byte, or bool). Tags carry dimension attributes (e.g. ifDescr).
type Metric struct {
	OID      string            `json:"oid"`
	Name     string            `json:"name"`
	Instance string            `json:"instance,omitempty"` // Table row index, e.g. "1" for ifIndex 1
	Value    interface{}       `json:"value"`              // int64 | uint64 | float64 | string | []byte | bool
	Type     string            `json:"type"`               // SNMP PDU type: "Counter64", "Integer", etc.
	Syntax   string            `json:"syntax"`             // Config syntax: "Counter64", "BandwidthMBits", etc.
	Tags     map[string]string `json:"tags,omitempty"`     // Dimension attributes keyed by attribute name
}

// MetricMetadata carries operational metadata about the collection cycle.
// It is used to monitor the health and performance of the collector itself.
type MetricMetadata struct {
	CollectorID    string `json:"collector_id"`
	PollDurationMs int64  `json:"poll_duration_ms"`
	PollStatus     string `json:"poll_status"` // "success" | "timeout" | "error"
}

// SNMPTrap is the top-level payload for a received SNMP trap or inform.
type SNMPTrap struct {
	Timestamp time.Time `json:"timestamp"`
	Device    Device    `json:"device"`
	TrapInfo  TrapInfo  `json:"trap_info"`
	Varbinds  []Metric  `json:"varbinds"`
}

// TrapInfo carries trap-specific header fields that are not present in regular polls.
type TrapInfo struct {
	Version       string `json:"version"`                  // "v1", "v2c", "v3"
	EnterpriseOID string `json:"enterprise_oid,omitempty"` // v1 only
	GenericTrap   int32  `json:"generic_trap,omitempty"`   // v1 only (0–6)
	SpecificTrap  int32  `json:"specific_trap,omitempty"`  // v1 only
	TrapOID       string `json:"trap_oid"`                 // v2c / v3 SNMPv2-MIB::snmpTrapOID.0
	TrapName      string `json:"trap_name,omitempty"`      // Resolved MIB name, e.g. "linkDown"
	Severity      string `json:"severity,omitempty"`       // "info" | "warning" | "critical"
}
