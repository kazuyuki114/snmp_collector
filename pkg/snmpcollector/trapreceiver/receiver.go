// Package trapreceiver implements a standalone UDP SNMP trap listener.
//
// This is a completely separate input path from the SNMP poller. The poller
// actively requests data from devices on a schedule; the trap receiver
// passively listens for asynchronous notifications pushed by devices.
//
// Pipeline position:
//
// UDP port 162  →  [TrapReceiver]  →  chan models.SNMPTrap
//
//	         │
//	(parallel with polling path)
//	         ↓
//	format/json  →  transport
//
// The TrapReceiver uses gosnmp's TrapListener as the UDP engine and delegates
// protocol-level parsing (v1/v2c/v3) to the snmp/trap package.
package trapreceiver

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/vpbank/snmp_collector/models"
	snmptrap "github.com/vpbank/snmp_collector/snmp/trap"
)

// ─────────────────────────────────────────────────────────────────────────────
// Configuration
// ─────────────────────────────────────────────────────────────────────────────

// Config controls the TrapReceiver behaviour.
type Config struct {
	// ListenAddr is the UDP address to bind to (default "0.0.0.0:162").
	ListenAddr string

	// OutputBufferSize is the capacity of the output channel (default 10000).
	OutputBufferSize int

	// Community is the SNMP community string for v1/v2c source validation.
	// If empty, all communities are accepted.
	Community string

	// SNMPVersion controls which SNMP version the listener accepts.
	// Defaults to gosnmp.Version2c.
	SNMPVersion gosnmp.SnmpVersion

	// CloseTimeout is the maximum time to wait for the UDP socket to close
	// gracefully (default 3 s, matching gosnmp's default).
	CloseTimeout time.Duration

	// ParseFunc replaces the default snmp/trap.Parse function. Used in tests.
	ParseFunc ParseFunc
}

// ParseFunc is the signature of the trap-parsing function. Callers may inject
// a stub for testing.
type ParseFunc func(pkt *gosnmp.SnmpPacket, addr *net.UDPAddr) (models.SNMPTrap, error)

func (c *Config) withDefaults() Config {
	out := *c
	if out.ListenAddr == "" {
		out.ListenAddr = "0.0.0.0:162"
	}
	if out.OutputBufferSize <= 0 {
		out.OutputBufferSize = 10_000
	}
	if out.SNMPVersion == 0 {
		out.SNMPVersion = gosnmp.Version2c
	}
	if out.CloseTimeout == 0 {
		out.CloseTimeout = 3 * time.Second
	}
	if out.ParseFunc == nil {
		out.ParseFunc = snmptrap.Parse
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// TrapReceiver
// ─────────────────────────────────────────────────────────────────────────────

// TrapReceiver listens on UDP for SNMP traps and informs, converts them into
// models.SNMPTrap values, and sends them on its output channel.
//
// It runs independently of the SNMP poller — the two paths share only the
// downstream format/transport stages.
type TrapReceiver struct {
	cfg    Config
	logger *slog.Logger

	output chan models.SNMPTrap // produced here, consumed downstream

	listener *gosnmp.TrapListener

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// New creates a TrapReceiver with the given configuration.
func New(cfg Config, logger *slog.Logger) *TrapReceiver {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(noopWriter{}, nil))
	}
	c := cfg.withDefaults()
	return &TrapReceiver{
		cfg:    c,
		logger: logger,
		output: make(chan models.SNMPTrap, c.OutputBufferSize),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// Output returns the read-only channel that delivers received traps.
// The channel is closed when the TrapReceiver stops.
func (r *TrapReceiver) Output() <-chan models.SNMPTrap {
	return r.output
}

// ListenAddr returns the address the receiver is (or will be) listening on.
func (r *TrapReceiver) ListenAddr() string {
	return r.cfg.ListenAddr
}

// Start begins listening for traps. It blocks until the listener is ready
// (or until ctx is cancelled). Traps are dispatched to Output() asynchronously.
// Start returns an error if the listener cannot bind to the configured address.
//
// Call Stop (or cancel ctx) to terminate.
func (r *TrapReceiver) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return fmt.Errorf("trapreceiver: already running")
	}
	r.running = true
	r.mu.Unlock()

	tl := gosnmp.NewTrapListener()
	tl.Params = &gosnmp.GoSNMP{
		Version:   r.cfg.SNMPVersion,
		Community: r.cfg.Community,
		Logger:    gosnmp.NewLogger(slogAdapter{r.logger}),
	}
	tl.CloseTimeout = r.cfg.CloseTimeout
	tl.OnNewTrap = r.handleTrap

	r.listener = tl

	// errCh receives the first error from tl.Listen (which blocks).
	errCh := make(chan error, 1)
	go func() {
		defer close(r.doneCh)
		errCh <- tl.Listen(r.cfg.ListenAddr)
	}()

	// Wait for the listener to be ready or for an early bind error.
	select {
	case <-tl.Listening():
		r.logger.Info("trapreceiver: listening", "addr", r.cfg.ListenAddr)
	case err := <-errCh:
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
		return fmt.Errorf("trapreceiver: listen %s: %w", r.cfg.ListenAddr, err)
	case <-ctx.Done():
		tl.Close()
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
		return ctx.Err()
	}

	// Goroutine: stop when ctx is cancelled.
	go func() {
		select {
		case <-ctx.Done():
			r.Stop()
		case <-r.stopCh:
		}
	}()

	return nil
}

// Stop shuts down the UDP listener and closes the output channel. It is safe
// to call Stop multiple times.
func (r *TrapReceiver) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return
	}
	r.running = false

	if r.listener != nil {
		r.listener.Close()
	}
	close(r.stopCh)

	// Wait for the listen goroutine to exit before closing output so that no
	// further writes happen after close.
	<-r.doneCh
	close(r.output)

	r.logger.Info("trapreceiver: stopped")
}

// handleTrap is the gosnmp TrapHandlerFunc callback. It runs in the gosnmp
// internal listener goroutine so it must not block for long.
func (r *TrapReceiver) handleTrap(pkt *gosnmp.SnmpPacket, addr *net.UDPAddr) {
	trap, err := r.cfg.ParseFunc(pkt, addr)
	if err != nil {
		r.logger.Warn("trapreceiver: parse error", "remote", addr, "error", err)
		return
	}

	select {
	case r.output <- trap:
	default:
		r.logger.Warn("trapreceiver: output buffer full — trap dropped",
			"remote", addr,
			"trap_oid", trap.TrapInfo.TrapOID,
		)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Utilities
// ─────────────────────────────────────────────────────────────────────────────

type noopWriter struct{}

func (noopWriter) Write(b []byte) (int, error) { return len(b), nil }

// slogAdapter bridges slog.Logger to gosnmp's Logger interface (Printf-style).
type slogAdapter struct{ l *slog.Logger }

func (a slogAdapter) Print(v ...interface{}) {
	a.l.Debug(fmt.Sprint(v...))
}

func (a slogAdapter) Printf(format string, v ...interface{}) {
	a.l.Debug(fmt.Sprintf(format, v...))
}
