// Package file implements a Transport that writes formatted messages to any
// io.Writer — typically os.Stdout (default) or a file.
//
// Pipeline position:
//
//	format/json [Stage 5] → transport/file [Stage 6]
//
// This transport is the development/debugging alternative to the Kafka
// transport.  Each call to Send writes one JSON record followed by a newline.
package file

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

// ─────────────────────────────────────────────────────────────────────────────
// Transport interface
// ─────────────────────────────────────────────────────────────────────────────

// Transport is the pipeline contract for all transport implementations.
// Send delivers one pre-formatted message (JSON bytes from format/json).
// Close flushes and releases resources.
type Transport interface {
	Send(data []byte) error
	Close() error
}

// ─────────────────────────────────────────────────────────────────────────────
// Config
// ─────────────────────────────────────────────────────────────────────────────

// Config controls WriterTransport behaviour.
type Config struct {
	// Writer is the destination.  nil defaults to os.Stdout.
	Writer io.Writer

	// Newline appended after each message.  Default "\n".
	Newline string
}

// ─────────────────────────────────────────────────────────────────────────────
// WriterTransport
// ─────────────────────────────────────────────────────────────────────────────

// WriterTransport implements Transport by writing each message to an io.Writer
// followed by a configurable newline.  It is safe for concurrent use.
type WriterTransport struct {
	mu     sync.Mutex
	w      io.Writer
	nl     []byte
	logger *slog.Logger
}

// New constructs a WriterTransport.
//
//   - cfg.Writer defaults to os.Stdout when nil.
//   - cfg.Newline defaults to "\n" when empty.
//   - logger defaults to a no-op writer when nil.
func New(cfg Config, logger *slog.Logger) *WriterTransport {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(noopWriter{}, nil))
	}
	w := cfg.Writer
	if w == nil {
		w = os.Stdout
	}
	nl := cfg.Newline
	if nl == "" {
		nl = "\n"
	}
	return &WriterTransport{
		w:      w,
		nl:     []byte(nl),
		logger: logger,
	}
}

// Send writes data followed by the configured newline to the underlying
// io.Writer.  It holds a mutex so concurrent goroutines produce un-interleaved
// output (important when w == os.Stdout).
func (t *WriterTransport) Send(data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, err := t.w.Write(data); err != nil {
		t.logger.Error("transport/file: write failed", "error", err.Error(), "bytes", len(data))
		return fmt.Errorf("transport/file: write: %w", err)
	}
	if _, err := t.w.Write(t.nl); err != nil {
		t.logger.Error("transport/file: newline write failed", "error", err.Error())
		return fmt.Errorf("transport/file: write newline: %w", err)
	}

	t.logger.Debug("transport/file: sent message", "bytes", len(data))
	return nil
}

// Close is a no-op for WriterTransport.  If the underlying writer must be
// closed (e.g. a file), the caller is responsible for closing it; this
// follows the principle that the writer's lifetime is managed by whoever
// created it.
func (t *WriterTransport) Close() error {
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// no-op logger writer
// ─────────────────────────────────────────────────────────────────────────────

type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }
