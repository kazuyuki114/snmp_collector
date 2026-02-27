package trapreceiver_test

import (
"context"
"fmt"
"net"
"testing"
"time"

"github.com/gosnmp/gosnmp"
"github.com/vpbank/snmp_collector/models"
"github.com/vpbank/snmp_collector/pkg/snmpcollector/trapreceiver"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// mkTrap produces a minimal pre-built models.SNMPTrap.
func mkTrap(trapOID, ip string) models.SNMPTrap {
return models.SNMPTrap{
Timestamp: time.Now().UTC(),
Device:    models.Device{IPAddress: ip, SNMPVersion: "2c"},
TrapInfo:  models.TrapInfo{Version: "v2c", TrapOID: trapOID},
Varbinds:  []models.Metric{{OID: ".1.2.3", Name: ".1.2.3", Value: int64(1), Type: "Integer"}},
}
}

// stubParseFunc returns a ParseFunc that always produces a fixed trap.
func stubParseFunc(trap models.SNMPTrap) trapreceiver.ParseFunc {
return func(_ *gosnmp.SnmpPacket, _ *net.UDPAddr) (models.SNMPTrap, error) {
return trap, nil
}
}

// errorParseFunc returns a ParseFunc that always errors.
func errorParseFunc() trapreceiver.ParseFunc {
return func(_ *gosnmp.SnmpPacket, _ *net.UDPAddr) (models.SNMPTrap, error) {
return models.SNMPTrap{}, fmt.Errorf("parse error")
}
}

// freePort finds a free UDP port on localhost.
func freePort(t *testing.T) int {
t.Helper()
conn, err := net.ListenPacket("udp", "127.0.0.1:0")
if err != nil {
t.Fatalf("freePort: %v", err)
}
defer conn.Close()
return conn.LocalAddr().(*net.UDPAddr).Port
}

// startReceiver starts a TrapReceiver using the given config and returns it
// together with a cancel function.
func startReceiver(t *testing.T, cfg trapreceiver.Config) (*trapreceiver.TrapReceiver, context.CancelFunc) {
t.Helper()
ctx, cancel := context.WithCancel(context.Background())
r := trapreceiver.New(cfg, nil)
if err := r.Start(ctx); err != nil {
cancel()
t.Fatalf("Start: %v", err)
}
return r, cancel
}

// ─────────────────────────────────────────────────────────────────────────────
// Config / constructor
// ─────────────────────────────────────────────────────────────────────────────

func TestNew_NonNil(t *testing.T) {
r := trapreceiver.New(trapreceiver.Config{}, nil)
if r == nil {
t.Fatal("New returned nil")
}
if r.Output() == nil {
t.Fatal("Output() returned nil channel")
}
}

func TestNew_DefaultListenAddr(t *testing.T) {
r := trapreceiver.New(trapreceiver.Config{}, nil)
if r.ListenAddr() == "" {
t.Error("ListenAddr() should not be empty after defaults are applied")
}
}

// ─────────────────────────────────────────────────────────────────────────────
// Start / Stop lifecycle
// ─────────────────────────────────────────────────────────────────────────────

func TestStart_BindsAndReturnsNil(t *testing.T) {
port := freePort(t)
cfg := trapreceiver.Config{
ListenAddr: fmt.Sprintf("127.0.0.1:%d", port),
ParseFunc:  stubParseFunc(mkTrap(".1.2.3", "10.0.0.1")),
}
r, cancel := startReceiver(t, cfg)
defer cancel()
defer r.Stop()
}

func TestStop_ClosesOutputChannel(t *testing.T) {
port := freePort(t)
cfg := trapreceiver.Config{
ListenAddr: fmt.Sprintf("127.0.0.1:%d", port),
ParseFunc:  stubParseFunc(mkTrap(".1.2.3", "10.0.0.1")),
}
r, cancel := startReceiver(t, cfg)
defer cancel()

r.Stop()

select {
case _, ok := <-r.Output():
if ok {
t.Error("expected output channel to be closed")
}
case <-time.After(2 * time.Second):
t.Error("output channel not closed within 2s")
}
}

func TestStop_Idempotent(t *testing.T) {
port := freePort(t)
cfg := trapreceiver.Config{
ListenAddr: fmt.Sprintf("127.0.0.1:%d", port),
ParseFunc:  stubParseFunc(mkTrap(".1.2.3", "10.0.0.1")),
}
r, cancel := startReceiver(t, cfg)
defer cancel()

r.Stop()
r.Stop() // must not panic or deadlock
}

func TestStart_AlreadyRunning_ReturnsError(t *testing.T) {
port := freePort(t)
cfg := trapreceiver.Config{
ListenAddr: fmt.Sprintf("127.0.0.1:%d", port),
ParseFunc:  stubParseFunc(mkTrap(".1.2.3", "10.0.0.1")),
}
r, cancel := startReceiver(t, cfg)
defer cancel()
defer r.Stop()

err := r.Start(context.Background())
if err == nil {
t.Fatal("expected error on second Start, got nil")
}
}

func TestStart_BadAddr_ReturnsError(t *testing.T) {
cfg := trapreceiver.Config{
ListenAddr: "999.999.999.999:9999",
ParseFunc:  stubParseFunc(mkTrap(".1.2.3", "10.0.0.1")),
}
r := trapreceiver.New(cfg, nil)
err := r.Start(context.Background())
if err == nil {
r.Stop()
t.Fatal("expected error for bad address, got nil")
}
}

// ─────────────────────────────────────────────────────────────────────────────
// Context cancellation
// ─────────────────────────────────────────────────────────────────────────────

func TestContextCancel_ClosesOutput(t *testing.T) {
port := freePort(t)
cfg := trapreceiver.Config{
ListenAddr: fmt.Sprintf("127.0.0.1:%d", port),
ParseFunc:  stubParseFunc(mkTrap(".1.2.3", "10.0.0.1")),
}
ctx, cancel := context.WithCancel(context.Background())
r := trapreceiver.New(cfg, nil)
if err := r.Start(ctx); err != nil {
cancel()
t.Fatalf("Start: %v", err)
}

cancel() // signal context done

select {
case _, ok := <-r.Output():
if ok {
t.Error("expected closed channel after context cancel")
}
case <-time.After(3 * time.Second):
t.Error("output channel not closed within 3s after ctx cancel")
}
}

// ─────────────────────────────────────────────────────────────────────────────
// Parse error → no emission
// ─────────────────────────────────────────────────────────────────────────────

func TestParseError_NoEmit(t *testing.T) {
port := freePort(t)
cfg := trapreceiver.Config{
ListenAddr: fmt.Sprintf("127.0.0.1:%d", port),
ParseFunc:  errorParseFunc(),
}
r, cancel := startReceiver(t, cfg)
defer cancel()
defer r.Stop()

// Nothing should be on the channel immediately.
select {
case v, ok := <-r.Output():
if ok {
t.Errorf("unexpected trap emitted: %+v", v)
}
case <-time.After(20 * time.Millisecond):
// expected — nothing queued
}
}

// ─────────────────────────────────────────────────────────────────────────────
// Output buffer capacity is honoured
// ─────────────────────────────────────────────────────────────────────────────

func TestOutputBufferSize_Capacity(t *testing.T) {
port := freePort(t)
cfg := trapreceiver.Config{
ListenAddr:       fmt.Sprintf("127.0.0.1:%d", port),
OutputBufferSize: 3,
ParseFunc:        stubParseFunc(mkTrap(".1.2.3", "10.0.0.1")),
}
r, cancel := startReceiver(t, cfg)
defer cancel()
defer r.Stop()

if cap(r.Output()) != 3 {
t.Errorf("output buf cap = %d, want 3", cap(r.Output()))
}
}

// ─────────────────────────────────────────────────────────────────────────────
// ListenAddr
// ─────────────────────────────────────────────────────────────────────────────

func TestListenAddr_MatchesConfig(t *testing.T) {
port := freePort(t)
addr := fmt.Sprintf("127.0.0.1:%d", port)
cfg := trapreceiver.Config{
ListenAddr: addr,
ParseFunc:  stubParseFunc(mkTrap(".1.2.3", "10.0.0.1")),
}
r, cancel := startReceiver(t, cfg)
defer cancel()
defer r.Stop()

if r.ListenAddr() != addr {
t.Errorf("ListenAddr = %q, want %q", r.ListenAddr(), addr)
}
}

// ─────────────────────────────────────────────────────────────────────────────
// Real UDP round-trip (v2c trap on loopback)
// ─────────────────────────────────────────────────────────────────────────────

func TestRealUDP_V2c_TrapDelivered(t *testing.T) {
port := freePort(t)

cfg := trapreceiver.Config{
ListenAddr:  fmt.Sprintf("127.0.0.1:%d", port),
SNMPVersion: gosnmp.Version2c,
Community:   "public",
// No ParseFunc → defaults to trap.Parse
}
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

r := trapreceiver.New(cfg, nil)
if err := r.Start(ctx); err != nil {
t.Fatalf("Start: %v", err)
}
defer r.Stop()

// Small sleep to ensure the UDP socket is ready.
time.Sleep(50 * time.Millisecond)

// Send a v2c trap to the listener.
sender := &gosnmp.GoSNMP{
Target:    "127.0.0.1",
Port:      uint16(port),
Version:   gosnmp.Version2c,
Community: "public",
Timeout:   2 * time.Second,
Retries:   0,
}
if err := sender.Connect(); err != nil {
t.Fatalf("sender.Connect: %v", err)
}
defer sender.Conn.Close()

trap := gosnmp.SnmpTrap{
Variables: []gosnmp.SnmpPDU{
{
Name:  ".1.3.6.1.2.1.1.3.0",
Type:  gosnmp.TimeTicks,
Value: uint32(12345),
},
{
Name:  ".1.3.6.1.6.3.1.1.4.1.0",
Type:  gosnmp.ObjectIdentifier,
Value: "1.3.6.1.6.3.1.1.5.3",
},
},
}
if _, err := sender.SendTrap(trap); err != nil {
t.Fatalf("SendTrap: %v", err)
}

select {
case got, ok := <-r.Output():
if !ok {
t.Fatal("output channel closed unexpectedly")
}
if got.TrapInfo.TrapOID == "" {
t.Errorf("TrapOID is empty, got trap: %+v", got)
}
if got.Device.IPAddress == "" {
t.Errorf("Device.IPAddress is empty")
}
case <-time.After(3 * time.Second):
t.Fatal("timed out waiting for trap on output channel")
}
}
