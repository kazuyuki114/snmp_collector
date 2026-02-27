package trap_test

import (
	"net"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/vpbank/snmp_collector/snmp/trap"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

var testAddr = &net.UDPAddr{IP: net.ParseIP("192.168.1.50"), Port: 162}

// pdu builds a simple SnmpPDU for test inputs.
func pdu(name string, typ gosnmp.Asn1BER, value interface{}) gosnmp.SnmpPDU {
	return gosnmp.SnmpPDU{Name: name, Type: typ, Value: value}
}

// ─────────────────────────────────────────────────────────────────────────────
// v1 trap
// ─────────────────────────────────────────────────────────────────────────────

func TestParse_V1_LinkDown(t *testing.T) {
	pkt := &gosnmp.SnmpPacket{
		Version:   gosnmp.Version1,
		Community: "public",
		PDUType:   gosnmp.Trap,
		SnmpTrap: gosnmp.SnmpTrap{
			Enterprise:   "1.3.6.1.4.1.9",
			AgentAddress: "10.0.0.1",
			GenericTrap:  2, // linkDown
			SpecificTrap: 0,
			Timestamp:    1234,
		},
		Variables: []gosnmp.SnmpPDU{
			pdu("1.3.6.1.2.1.2.2.1.1.5", gosnmp.Integer, 5),
		},
	}

	result, err := trap.Parse(pkt, testAddr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Version
	if result.TrapInfo.Version != "v1" {
		t.Errorf("Version = %q, want %q", result.TrapInfo.Version, "v1")
	}
	// Enterprise OID normalised
	if result.TrapInfo.EnterpriseOID != ".1.3.6.1.4.1.9" {
		t.Errorf("EnterpriseOID = %q, want %q", result.TrapInfo.EnterpriseOID, ".1.3.6.1.4.1.9")
	}
	// Generic / specific
	if result.TrapInfo.GenericTrap != 2 {
		t.Errorf("GenericTrap = %d, want 2", result.TrapInfo.GenericTrap)
	}
	if result.TrapInfo.SpecificTrap != 0 {
		t.Errorf("SpecificTrap = %d, want 0", result.TrapInfo.SpecificTrap)
	}
	// Synthesised TrapOID: generic 2 → .1.3.6.1.6.3.1.1.5.3
	if result.TrapInfo.TrapOID != ".1.3.6.1.6.3.1.1.5.3" {
		t.Errorf("TrapOID = %q, want %q", result.TrapInfo.TrapOID, ".1.3.6.1.6.3.1.1.5.3")
	}
	// Agent address used over remote addr
	if result.Device.IPAddress != "10.0.0.1" {
		t.Errorf("IPAddress = %q, want %q", result.Device.IPAddress, "10.0.0.1")
	}
	// Varbinds
	if len(result.Varbinds) != 1 {
		t.Fatalf("Varbinds len = %d, want 1", len(result.Varbinds))
	}
	if result.Varbinds[0].OID != ".1.3.6.1.2.1.2.2.1.1.5" {
		t.Errorf("Varbind OID = %q, want normalised", result.Varbinds[0].OID)
	}
	// Timestamp set
	if result.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
	// Device version
	if result.Device.SNMPVersion != "1" {
		t.Errorf("SNMPVersion = %q, want %q", result.Device.SNMPVersion, "1")
	}
}

func TestParse_V1_EnterpriseSpecific(t *testing.T) {
	pkt := &gosnmp.SnmpPacket{
		Version: gosnmp.Version1,
		PDUType: gosnmp.Trap,
		SnmpTrap: gosnmp.SnmpTrap{
			Enterprise:   "1.3.6.1.4.1.9.1",
			AgentAddress: "10.0.0.2",
			GenericTrap:  6, // enterprise-specific
			SpecificTrap: 42,
		},
	}

	result, err := trap.Parse(pkt, testAddr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Enterprise-specific: <enterprise>.0.<specific>
	want := ".1.3.6.1.4.1.9.1.0.42"
	if result.TrapInfo.TrapOID != want {
		t.Errorf("TrapOID = %q, want %q", result.TrapInfo.TrapOID, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// v2c trap
// ─────────────────────────────────────────────────────────────────────────────

func TestParse_V2c_LinkDown(t *testing.T) {
	trapOID := ".1.3.6.1.6.3.1.1.5.3" // linkDown

	pkt := &gosnmp.SnmpPacket{
		Version:   gosnmp.Version2c,
		Community: "public",
		PDUType:   gosnmp.SNMPv2Trap,
		Variables: []gosnmp.SnmpPDU{
			// Standard header: sysUpTime.0
			pdu("1.3.6.1.2.1.1.3.0", gosnmp.TimeTicks, uint32(123456)),
			// Standard header: snmpTrapOID.0
			pdu("1.3.6.1.6.3.1.1.4.1.0", gosnmp.ObjectIdentifier, trapOID),
			// Payload varbinds
			pdu("1.3.6.1.2.1.2.2.1.1.3", gosnmp.Integer, 3),
			pdu("1.3.6.1.2.1.2.2.1.8.3", gosnmp.Integer, 2),
		},
	}

	result, err := trap.Parse(pkt, testAddr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if result.TrapInfo.Version != "v2c" {
		t.Errorf("Version = %q, want %q", result.TrapInfo.Version, "v2c")
	}
	if result.TrapInfo.TrapOID != ".1.3.6.1.6.3.1.1.5.3" {
		t.Errorf("TrapOID = %q, want %q", result.TrapInfo.TrapOID, ".1.3.6.1.6.3.1.1.5.3")
	}
	// Only payload varbinds, not header varbinds
	if len(result.Varbinds) != 2 {
		t.Fatalf("Varbinds len = %d, want 2 (payload only, not header)", len(result.Varbinds))
	}
	// Device IP from remote addr (no AgentAddress in v2c)
	if result.Device.IPAddress != "192.168.1.50" {
		t.Errorf("IPAddress = %q, want %q", result.Device.IPAddress, "192.168.1.50")
	}
	if result.Device.SNMPVersion != "2c" {
		t.Errorf("SNMPVersion = %q, want %q", result.Device.SNMPVersion, "2c")
	}
}

func TestParse_V2c_MissingTrapOID(t *testing.T) {
	// Malformed trap with no snmpTrapOID.0 — should not error, just no trapOID.
	pkt := &gosnmp.SnmpPacket{
		Version: gosnmp.Version2c,
		PDUType: gosnmp.SNMPv2Trap,
		Variables: []gosnmp.SnmpPDU{
			pdu("1.3.6.1.2.1.2.2.1.1.1", gosnmp.Integer, 1),
		},
	}

	result, err := trap.Parse(pkt, testAddr)
	if err != nil {
		t.Fatalf("expected no error on malformed trap, got %v", err)
	}
	if result.TrapInfo.TrapOID != "" {
		t.Errorf("TrapOID should be empty for malformed trap, got %q", result.TrapInfo.TrapOID)
	}
	// All varbinds treated as payload
	if len(result.Varbinds) != 1 {
		t.Errorf("Varbinds = %d, want 1", len(result.Varbinds))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// v3 trap
// ─────────────────────────────────────────────────────────────────────────────

func TestParse_V3_Trap(t *testing.T) {
	trapOID := "1.3.6.1.6.3.1.1.5.4" // linkUp

	pkt := &gosnmp.SnmpPacket{
		Version: gosnmp.Version3,
		PDUType: gosnmp.SNMPv2Trap,
		Variables: []gosnmp.SnmpPDU{
			pdu("1.3.6.1.2.1.1.3.0", gosnmp.TimeTicks, uint32(5000)),
			pdu("1.3.6.1.6.3.1.1.4.1.0", gosnmp.ObjectIdentifier, trapOID),
			pdu("1.3.6.1.2.1.2.2.1.1.2", gosnmp.Integer, 2),
		},
	}

	result, err := trap.Parse(pkt, testAddr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if result.TrapInfo.Version != "v3" {
		t.Errorf("Version = %q, want %q", result.TrapInfo.Version, "v3")
	}
	if result.TrapInfo.TrapOID != ".1.3.6.1.6.3.1.1.5.4" {
		t.Errorf("TrapOID = %q, want normalised %q", result.TrapInfo.TrapOID, ".1.3.6.1.6.3.1.1.5.4")
	}
	if len(result.Varbinds) != 1 {
		t.Fatalf("Varbinds = %d, want 1", len(result.Varbinds))
	}
	if result.Device.SNMPVersion != "3" {
		t.Errorf("SNMPVersion = %q, want %q", result.Device.SNMPVersion, "3")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Inform PDU
// ─────────────────────────────────────────────────────────────────────────────

func TestParse_InformRequest(t *testing.T) {
	// Informs have the same varbind format as v2c traps but with PDUType=InformRequest.
	pkt := &gosnmp.SnmpPacket{
		Version: gosnmp.Version2c,
		PDUType: gosnmp.InformRequest,
		Variables: []gosnmp.SnmpPDU{
			pdu("1.3.6.1.2.1.1.3.0", gosnmp.TimeTicks, uint32(9999)),
			pdu("1.3.6.1.6.3.1.1.4.1.0", gosnmp.ObjectIdentifier, "1.3.6.1.6.3.1.1.5.3"),
			pdu("1.3.6.1.2.1.2.2.1.1.4", gosnmp.Integer, 4),
		},
	}

	result, err := trap.Parse(pkt, testAddr)
	if err != nil {
		t.Fatalf("Parse inform: %v", err)
	}
	// Inform is still parsed identically to a trap.
	if result.TrapInfo.TrapOID != ".1.3.6.1.6.3.1.1.5.3" {
		t.Errorf("TrapOID = %q, want normalised", result.TrapInfo.TrapOID)
	}
	if len(result.Varbinds) != 1 {
		t.Errorf("Varbinds = %d, want 1", len(result.Varbinds))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Nil packet
// ─────────────────────────────────────────────────────────────────────────────

func TestParse_NilPacket(t *testing.T) {
	_, err := trap.Parse(nil, testAddr)
	if err == nil {
		t.Fatal("expected error for nil packet")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Nil remote addr
// ─────────────────────────────────────────────────────────────────────────────

func TestParse_NilRemoteAddr(t *testing.T) {
	pkt := &gosnmp.SnmpPacket{
		Version: gosnmp.Version2c,
		PDUType: gosnmp.SNMPv2Trap,
	}
	result, err := trap.Parse(pkt, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Device.IPAddress != "" {
		t.Errorf("IPAddress = %q, want empty", result.Device.IPAddress)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Varbind type conversion
// ─────────────────────────────────────────────────────────────────────────────

func TestParse_VarbindTypes(t *testing.T) {
	pkt := &gosnmp.SnmpPacket{
		Version: gosnmp.Version2c,
		PDUType: gosnmp.SNMPv2Trap,
		Variables: []gosnmp.SnmpPDU{
			// sysUpTime + trapOID header
			pdu("1.3.6.1.2.1.1.3.0", gosnmp.TimeTicks, uint32(0)),
			pdu("1.3.6.1.6.3.1.1.4.1.0", gosnmp.ObjectIdentifier, "1.3.6.1.6.3.1.1.5.1"),
			// Payload
			pdu("1.1", gosnmp.Integer, 42),
			pdu("1.2", gosnmp.OctetString, []byte("hello")),
			pdu("1.3", gosnmp.Counter32, uint32(9999)),
			pdu("1.4", gosnmp.Counter64, uint64(123456789)),
			pdu("1.5", gosnmp.IPAddress, "10.0.0.1"),
			pdu("1.6", gosnmp.ObjectIdentifier, "1.2.3"),
		},
	}

	result, err := trap.Parse(pkt, testAddr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Varbinds) != 6 {
		t.Fatalf("Varbinds = %d, want 6", len(result.Varbinds))
	}

	tests := []struct {
		idx      int
		wantType string
		wantVal  interface{}
	}{
		{0, "Integer", int64(42)},
		{1, "OctetString", "hello"},
		{2, "Counter32", uint64(9999)},
		{3, "Counter64", uint64(123456789)},
		{4, "IpAddress", "10.0.0.1"},
		{5, "ObjectIdentifier", ".1.2.3"},
	}
	for _, tt := range tests {
		vb := result.Varbinds[tt.idx]
		if vb.Type != tt.wantType {
			t.Errorf("[%d] Type = %q, want %q", tt.idx, vb.Type, tt.wantType)
		}
		if vb.Value != tt.wantVal {
			t.Errorf("[%d] Value = %v (%T), want %v (%T)", tt.idx, vb.Value, vb.Value, tt.wantVal, tt.wantVal)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Error PDU types skipped
// ─────────────────────────────────────────────────────────────────────────────

func TestParse_ErrorPDUsSkipped(t *testing.T) {
	pkt := &gosnmp.SnmpPacket{
		Version: gosnmp.Version2c,
		PDUType: gosnmp.SNMPv2Trap,
		Variables: []gosnmp.SnmpPDU{
			pdu("1.3.6.1.2.1.1.3.0", gosnmp.TimeTicks, uint32(0)),
			pdu("1.3.6.1.6.3.1.1.4.1.0", gosnmp.ObjectIdentifier, "1.3.6.1.6.3.1.1.5.1"),
			// These should be skipped
			pdu("1.1", gosnmp.NoSuchObject, nil),
			pdu("1.2", gosnmp.NoSuchInstance, nil),
			// This should be kept
			pdu("1.3", gosnmp.Integer, 7),
		},
	}

	result, err := trap.Parse(pkt, testAddr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Varbinds) != 1 {
		t.Errorf("Varbinds = %d, want 1 (error PDUs should be skipped)", len(result.Varbinds))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Timestamp is recent
// ─────────────────────────────────────────────────────────────────────────────

func TestParse_TimestampIsRecent(t *testing.T) {
	before := time.Now().UTC()
	pkt := &gosnmp.SnmpPacket{Version: gosnmp.Version2c, PDUType: gosnmp.SNMPv2Trap}
	result, _ := trap.Parse(pkt, testAddr)
	after := time.Now().UTC()

	if result.Timestamp.Before(before) || result.Timestamp.After(after) {
		t.Errorf("Timestamp %v not in range [%v, %v]", result.Timestamp, before, after)
	}
}
