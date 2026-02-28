// Package file — split.go provides a Transport that writes SNMP poll metrics
// and SNMP trap events to separate destinations (files).
//
// Pipeline position:
//
//	format/json [Stage 5] → transport/file/split [Stage 6]
//
// Routing logic:
//   - JSON payloads containing a "trap_info" key → trap writer
//   - Everything else (poll metrics) → metric writer
//
// Both writers can be plain io.Writers (os.Stdout, *os.File) or RotatingFile
// instances for automatic size-based rotation.
package file

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

// ─────────────────────────────────────────────────────────────────────────────
// SplitConfig
// ─────────────────────────────────────────────────────────────────────────────

// SplitConfig controls SplitWriterTransport behaviour.
type SplitConfig struct {
	// MetricWriter receives SNMP poll metric payloads.
	// nil defaults to os.Stdout.
	MetricWriter io.Writer

	// TrapWriter receives SNMP trap payloads.
	// nil defaults to os.Stderr.
	TrapWriter io.Writer

	// Newline appended after each message.  Default "\n".
	Newline string
}

// ─────────────────────────────────────────────────────────────────────────────
// SplitWriterTransport
// ─────────────────────────────────────────────────────────────────────────────

// SplitWriterTransport implements Transport by routing each JSON message to one
// of two io.Writers based on its content type.  It is safe for concurrent use.
//
// Detection: a fast bytes.Contains check for the `"trap_info"` key is used
// instead of full JSON unmarshalling to keep the hot path allocation-free.
type SplitWriterTransport struct {
	metricMu sync.Mutex
	trapMu   sync.Mutex
	metricW  io.Writer
	trapW    io.Writer
	nl       []byte
	closers  []io.Closer
	logger   *slog.Logger
}

// trapMarker is the byte sequence used to identify trap payloads.
// Every SNMPTrap JSON object contains this key.
var trapMarker = []byte(`"trap_info"`)

// NewSplit constructs a SplitWriterTransport.
//
//   - cfg.MetricWriter defaults to os.Stdout when nil.
//   - cfg.TrapWriter defaults to os.Stderr when nil.
//   - cfg.Newline defaults to "\n" when empty.
//   - logger defaults to a no-op logger when nil.
func NewSplit(cfg SplitConfig, logger *slog.Logger) *SplitWriterTransport {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(noopWriter{}, nil))
	}

	mw := cfg.MetricWriter
	if mw == nil {
		mw = os.Stdout
	}
	tw := cfg.TrapWriter
	if tw == nil {
		tw = os.Stderr
	}
	nl := cfg.Newline
	if nl == "" {
		nl = "\n"
	}

	st := &SplitWriterTransport{
		metricW: mw,
		trapW:   tw,
		nl:      []byte(nl),
		logger:  logger,
	}

	// Track io.Closers so Close() can clean up RotatingFile instances.
	if c, ok := mw.(io.Closer); ok && mw != os.Stdout && mw != os.Stderr {
		st.closers = append(st.closers, c)
	}
	if c, ok := tw.(io.Closer); ok && tw != os.Stdout && tw != os.Stderr {
		st.closers = append(st.closers, c)
	}

	return st
}

// Send inspects data for the trap marker and routes to the appropriate writer.
func (st *SplitWriterTransport) Send(data []byte) error {
	if bytes.Contains(data, trapMarker) {
		return st.writeTrap(data)
	}
	return st.writeMetric(data)
}

// Close flushes and closes any io.Closer writers (e.g. RotatingFile).
// Plain os.Stdout / os.Stderr are never closed.
func (st *SplitWriterTransport) Close() error {
	var firstErr error
	for _, c := range st.closers {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

func (st *SplitWriterTransport) writeMetric(data []byte) error {
	st.metricMu.Lock()
	defer st.metricMu.Unlock()

	if _, err := st.metricW.Write(data); err != nil {
		st.logger.Error("transport/file: metric write failed",
			"error", err.Error(), "bytes", len(data),
		)
		return fmt.Errorf("transport/file: metric write: %w", err)
	}
	if _, err := st.metricW.Write(st.nl); err != nil {
		st.logger.Error("transport/file: metric newline write failed",
			"error", err.Error(),
		)
		return fmt.Errorf("transport/file: metric write newline: %w", err)
	}

	st.logger.Debug("transport/file: sent metric message", "bytes", len(data))
	return nil
}

func (st *SplitWriterTransport) writeTrap(data []byte) error {
	st.trapMu.Lock()
	defer st.trapMu.Unlock()

	if _, err := st.trapW.Write(data); err != nil {
		st.logger.Error("transport/file: trap write failed",
			"error", err.Error(), "bytes", len(data),
		)
		return fmt.Errorf("transport/file: trap write: %w", err)
	}
	if _, err := st.trapW.Write(st.nl); err != nil {
		st.logger.Error("transport/file: trap newline write failed",
			"error", err.Error(),
		)
		return fmt.Errorf("transport/file: trap write newline: %w", err)
	}

	st.logger.Debug("transport/file: sent trap message", "bytes", len(data))
	return nil
}
