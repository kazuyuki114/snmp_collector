// Package trap converts received SNMP trap and inform PDUs into the canonical
// models.SNMPTrap representation. It is responsible for the protocol-level
// differences between v1, v2c, and v3 traps but has no knowledge of UDP
// socket management — that is handled separately by the trapreceiver package.
package trap

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/vpbank/snmp_collector/models"
)

// ─────────────────────────────────────────────────────────────────────────────
// Well-known OID constants
// ─────────────────────────────────────────────────────────────────────────────

const (
	// oidSysUpTime is the OID for sysUpTime.0 — the first standard varbind
	// in v2c/v3 trap PDUs.
	oidSysUpTime = ".1.3.6.1.2.1.1.3.0"

	// oidSnmpTrapOID is the OID for snmpTrapOID.0 — the second standard
	// varbind in v2c/v3 trap PDUs; its value is the actual trap OID.
	oidSnmpTrapOID = ".1.3.6.1.6.3.1.1.4.1.0"
)

// ─────────────────────────────────────────────────────────────────────────────
// Parse — main entry point
// ─────────────────────────────────────────────────────────────────────────────

// Parse converts a raw gosnmp SnmpPacket received by a TrapListener into a
// models.SNMPTrap. The remoteAddr is the source UDP address of the sender.
//
// Inform PDUs are treated identically to traps for the purpose of building the
// SNMPTrap payload — their acknowledgement is the responsibility of the caller.
func Parse(pkt *gosnmp.SnmpPacket, remoteAddr *net.UDPAddr) (models.SNMPTrap, error) {
	if pkt == nil {
		return models.SNMPTrap{}, fmt.Errorf("trap: nil packet")
	}

	trap := models.SNMPTrap{
		Timestamp: time.Now().UTC(),
		Device:    deviceFromPacket(pkt, remoteAddr),
	}

	switch pkt.Version {
	case gosnmp.Version1:
		trap.TrapInfo = parsev1Info(pkt)
		trap.Varbinds = convertVarbinds(pkt.Variables)
	case gosnmp.Version2c, gosnmp.Version3:
		info, remaining, err := parsev2Info(pkt)
		if err != nil {
			return trap, err
		}
		trap.TrapInfo = info
		trap.Varbinds = convertVarbinds(remaining)
	default:
		return trap, fmt.Errorf("trap: unsupported SNMP version %v", pkt.Version)
	}

	return trap, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Device extraction
// ─────────────────────────────────────────────────────────────────────────────

// deviceFromPacket builds a models.Device from the packet and sender address.
// For v1, the agent address field in the PDU is used when available.
func deviceFromPacket(pkt *gosnmp.SnmpPacket, remoteAddr *net.UDPAddr) models.Device {
	ip := ""
	if remoteAddr != nil {
		ip = remoteAddr.IP.String()
	}

	// v1 traps carry an explicit AgentAddress field.
	if pkt.Version == gosnmp.Version1 && pkt.AgentAddress != "" {
		ip = pkt.AgentAddress
	}

	version := snmpVersionString(pkt.Version)

	return models.Device{
		IPAddress:   ip,
		SNMPVersion: version,
	}
}

func snmpVersionString(v gosnmp.SnmpVersion) string {
	switch v {
	case gosnmp.Version1:
		return "1"
	case gosnmp.Version2c:
		return "2c"
	case gosnmp.Version3:
		return "3"
	default:
		return "unknown"
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// v1 trap parsing
// ─────────────────────────────────────────────────────────────────────────────

// parsev1Info extracts the TrapInfo from a v1 trap PDU.
// v1 traps use a dedicated PDU format rather than embedding the trap OID as a
// varbind. The TrapOID is synthesised from the enterprise and trap codes.
func parsev1Info(pkt *gosnmp.SnmpPacket) models.TrapInfo {
	info := models.TrapInfo{
		Version:       "v1",
		EnterpriseOID: normaliseOID(pkt.Enterprise),
		GenericTrap:   int32(pkt.GenericTrap),  //nolint:gosec
		SpecificTrap:  int32(pkt.SpecificTrap), //nolint:gosec
	}

	// Synthesise a TrapOID following the v1-to-v2 mapping from RFC 3584 §3.1:
	//   generic 0-5  → standard OID: .1.3.6.1.6.3.1.1.5.<generic+1>
	//   generic 6    → enterprise-specific: <enterprise>.0.<specific>
	if pkt.GenericTrap >= 0 && pkt.GenericTrap < 6 {
		info.TrapOID = fmt.Sprintf(".1.3.6.1.6.3.1.1.5.%d", pkt.GenericTrap+1)
	} else {
		// Enterprise-specific trap.
		ent := strings.TrimSuffix(normaliseOID(pkt.Enterprise), ".")
		info.TrapOID = fmt.Sprintf("%s.0.%d", ent, pkt.SpecificTrap)
	}

	return info
}

// ─────────────────────────────────────────────────────────────────────────────
// v2c / v3 trap parsing
// ─────────────────────────────────────────────────────────────────────────────

// parsev2Info extracts TrapInfo from a v2c or v3 trap PDU.
// The first two varbinds are always sysUpTime.0 and snmpTrapOID.0; the
// remainder are the actual payload varbinds returned as `remaining`.
func parsev2Info(pkt *gosnmp.SnmpPacket) (models.TrapInfo, []gosnmp.SnmpPDU, error) {
	info := models.TrapInfo{
		Version: "v" + snmpVersionString(pkt.Version),
	}

	vars := pkt.Variables
	remaining := vars

	// Locate snmpTrapOID.0 — it should be the second varbind but we search to
	// be tolerant of agents that omit sysUpTime.0.
	trapOIDIdx := -1
	for i, v := range vars {
		n := normaliseOID(v.Name)
		if n == oidSnmpTrapOID {
			trapOIDIdx = i
			break
		}
	}

	if trapOIDIdx < 0 {
		// Not a well-formed trap, but don't error — treat all varbinds as payload.
		return info, remaining, nil
	}

	// Extract the trap OID value.
	rawOID := fmt.Sprintf("%v", vars[trapOIDIdx].Value)
	info.TrapOID = normaliseOID(rawOID)

	// Payload is everything after snmpTrapOID.0 (skip sysUpTime + trapOID).
	remaining = vars[trapOIDIdx+1:]

	return info, remaining, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Varbind conversion
// ─────────────────────────────────────────────────────────────────────────────

// isErrorPDU returns true for PDU types that signal a missing/error value.
func isErrorPDU(t gosnmp.Asn1BER) bool {
	return t == gosnmp.NoSuchObject || t == gosnmp.NoSuchInstance ||
		t == gosnmp.EndOfMibView || t == gosnmp.Null
}

// convertVarbinds converts a slice of gosnmp SnmpPDUs into models.Metric
// values. Error PDU types (NoSuchObject, etc.) are silently skipped.
func convertVarbinds(pdus []gosnmp.SnmpPDU) []models.Metric {
	out := make([]models.Metric, 0, len(pdus))
	for _, pdu := range pdus {
		if isErrorPDU(pdu.Type) {
			continue
		}
		out = append(out, models.Metric{
			OID:   normaliseOID(pdu.Name),
			Name:  pdu.Name, // Raw OID as name — enrichment is a higher-level concern.
			Value: convertPDUValue(pdu),
			Type:  pduTypeString(pdu.Type),
		})
	}
	return out
}

// convertPDUValue converts a gosnmp PDU value to a canonical Go type matching
// the models.Metric.Value contract.
func convertPDUValue(pdu gosnmp.SnmpPDU) interface{} {
	switch pdu.Type {
	case gosnmp.OctetString:
		if b, ok := pdu.Value.([]byte); ok {
			// Try to return a printable string; fall back to hex otherwise.
			if isPrintable(b) {
				return string(b)
			}
			return b
		}
		return fmt.Sprintf("%v", pdu.Value)

	case gosnmp.ObjectIdentifier:
		s := fmt.Sprintf("%v", pdu.Value)
		return normaliseOID(s)

	case gosnmp.IPAddress:
		return fmt.Sprintf("%v", pdu.Value)

	case gosnmp.Integer:
		return gosnmp.ToBigInt(pdu.Value).Int64()

	case gosnmp.Counter32, gosnmp.Gauge32, gosnmp.TimeTicks, gosnmp.Uinteger32:
		return uint64(gosnmp.ToBigInt(pdu.Value).Uint64())

	case gosnmp.Counter64:
		return gosnmp.ToBigInt(pdu.Value).Uint64()

	default:
		return fmt.Sprintf("%v", pdu.Value)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// normaliseOID ensures an OID string starts with a leading dot and has no
// trailing dots.
func normaliseOID(oid string) string {
	oid = strings.TrimSpace(oid)
	if oid == "" {
		return ""
	}
	if !strings.HasPrefix(oid, ".") {
		oid = "." + oid
	}
	return strings.TrimSuffix(oid, ".")
}

// isPrintable returns true if all bytes in b are printable ASCII (0x20–0x7e)
// or common whitespace.
func isPrintable(b []byte) bool {
	for _, c := range b {
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			return false
		}
		if c > 0x7e {
			return false
		}
	}
	return true
}

// pduTypeString returns the human-readable name for a gosnmp Asn1BER type.
// Kept local rather than depending on snmp/decoder to avoid a cycle.
func pduTypeString(t gosnmp.Asn1BER) string {
	switch t {
	case gosnmp.Integer:
		return "Integer"
	case gosnmp.OctetString:
		return "OctetString"
	case gosnmp.ObjectIdentifier:
		return "ObjectIdentifier"
	case gosnmp.IPAddress:
		return "IpAddress"
	case gosnmp.Counter32:
		return "Counter32"
	case gosnmp.Gauge32:
		return "Gauge32"
	case gosnmp.TimeTicks:
		return "TimeTicks"
	case gosnmp.Counter64:
		return "Counter64"
	case gosnmp.Uinteger32:
		return "Unsigned32"
	case gosnmp.BitString:
		return "BitString"
	default:
		return fmt.Sprintf("Unknown(0x%02X)", uint8(t))
	}
}
