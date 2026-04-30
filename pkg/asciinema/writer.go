package asciinema

import (
	"encoding/json"
	"io"
	"strconv"
	"time"
)

const (
	version      = 2
	defaultShell = "/bin/bash"
	defaultTerm  = "xterm"
)

var newline = []byte{'\n'}

// Option configures the cast header.
type Option func(*Config)

// Config holds asciicast v2 header fields.
type Config struct {
	Width       int
	Height      int
	Timestamp   time.Time
	Title       string
	EnvShell    string
	EnvTerm     string
	TimestampNS int64 // session start in nanoseconds (for relative event times)
}

// WithWidth sets terminal width (cols).
func WithWidth(w int) Option {
	return func(c *Config) {
		c.Width = w
	}
}

// WithHeight sets terminal height (rows).
func WithHeight(h int) Option {
	return func(c *Config) {
		c.Height = h
	}
}

// WithTimestamp sets session start time (wall clock for header "timestamp" field).
func WithTimestamp(t time.Time) Option {
	return func(c *Config) {
		c.Timestamp = t
		c.TimestampNS = t.UnixNano()
	}
}

// Writer writes asciicast v2 (newline-delimited JSON).
type Writer struct {
	Config
	writer io.Writer
}

// NewWriter creates a writer; default size 80x40 if unset.
func NewWriter(w io.Writer, opts ...Option) *Writer {
	conf := Config{
		Width:     80,
		Height:    40,
		EnvShell:  defaultShell,
		EnvTerm:   defaultTerm,
		Timestamp: time.Now(),
	}
	conf.TimestampNS = conf.Timestamp.UnixNano()
	for _, o := range opts {
		o(&conf)
	}
	if conf.TimestampNS == 0 {
		conf.TimestampNS = conf.Timestamp.UnixNano()
	}
	return &Writer{Config: conf, writer: w}
}

// WriteHeader writes the JSON header line (call once at session start).
func (w *Writer) WriteHeader() error {
	header := map[string]interface{}{
		"version":   version,
		"width":     w.Width,
		"height":    w.Height,
		"timestamp": w.Timestamp.Unix(),
		"env": map[string]string{
			"SHELL": w.EnvShell,
			"TERM":  w.EnvTerm,
		},
	}
	if w.Title != "" {
		header["title"] = w.Title
	}
	raw, err := json.Marshal(header)
	if err != nil {
		return err
	}
	if _, err := w.writer.Write(raw); err != nil {
		return err
	}
	_, err = w.writer.Write(newline)
	return err
}

// WriteStdout appends one output event [t, "o", data] with relative time in seconds.
func (w *Writer) WriteStdout(ts float64, data []byte) error {
	row := []interface{}{ts, "o", string(data)}
	raw, err := json.Marshal(row)
	if err != nil {
		return err
	}
	if _, err := w.writer.Write(raw); err != nil {
		return err
	}
	_, err = w.writer.Write(newline)
	return err
}

// WriteRow writes output using elapsed time since TimestampNS (KoKo-style).
func (w *Writer) WriteRow(p []byte) error {
	now := time.Now().UnixNano()
	ts := float64(now-w.TimestampNS) / 1e9
	return w.WriteStdout(ts, p)
}

// WriteResize emits a resize event [t, "r", "COLSxROWS"] per asciicast v2.
func (w *Writer) WriteResize(ts float64, cols, rows int) error {
	row := []interface{}{ts, "r", fmtDim(cols, rows)}
	raw, err := json.Marshal(row)
	if err != nil {
		return err
	}
	if _, err := w.writer.Write(raw); err != nil {
		return err
	}
	_, err = w.writer.Write(newline)
	return err
}

func fmtDim(cols, rows int) string {
	return strconv.Itoa(cols) + "x" + strconv.Itoa(rows)
}
