package terminalreplay

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/clay-wangzhi/KubePolaris/internal/services"
	"github.com/clay-wangzhi/KubePolaris/pkg/asciinema"
	"github.com/clay-wangzhi/KubePolaris/pkg/logger"
)

// Recorder writes asciicast v2 to disk, gzips on close, and updates audit DB.
type Recorder struct {
	mu         sync.Mutex
	sessionID  uint
	audit      *services.AuditService
	root       string
	absCast    string
	absGz      string
	relGz      string
	writer     *asciinema.Writer
	file       *os.File
	started    bool
	headerOnce sync.Once
	startTime  time.Time
	closed     bool
}

// NewRecorder creates a recorder for a DB terminal session row. replayRoot empty disables recording (returns nil).
func NewRecorder(replayRoot string, audit *services.AuditService, sessionID uint, cols, rows int) (*Recorder, error) {
	if replayRoot == "" || audit == nil || sessionID == 0 {
		return nil, nil
	}
	day := time.Now().UTC().Format("2006-01-02")
	dir := filepath.Join(replayRoot, day)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}
	name := filepath.Join(dir, strconv.FormatUint(uint64(sessionID), 10)+".cast")
	f, err := os.Create(name)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	w := asciinema.NewWriter(f,
		asciinema.WithWidth(cols),
		asciinema.WithHeight(rows),
		asciinema.WithTimestamp(start),
	)
	rel := filepath.Join(day, strconv.FormatUint(uint64(sessionID), 10)+".cast.gz")
	return &Recorder{
		sessionID: sessionID,
		audit:     audit,
		root:      replayRoot,
		absCast:   name,
		absGz:     name + ".gz",
		relGz:     rel,
		writer:    w,
		file:      f,
		startTime: start,
	}, nil
}

// Record appends server output (PTY stdout) to the cast.
func (r *Recorder) Record(p []byte) {
	if r == nil || len(p) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.headerOnce.Do(func() {
		if err := r.writer.WriteHeader(); err != nil {
			logger.Error("replay: write header failed", "error", err)
		}
		r.started = true
	})
	if err := r.writer.WriteRow(p); err != nil {
		logger.Error("replay: write row failed", "error", err)
	}
}

// Resize records a terminal resize event.
func (r *Recorder) Resize(cols, rows int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	now := time.Now().UnixNano()
	ts := float64(now-r.writer.TimestampNS) / 1e9
	r.headerOnce.Do(func() {
		if err := r.writer.WriteHeader(); err != nil {
			logger.Error("replay: write header failed", "error", err)
		}
		r.started = true
	})
	if err := r.writer.WriteResize(ts, cols, rows); err != nil {
		logger.Error("replay: write resize failed", "error", err)
	}
}

// End closes the cast file, gzips, updates DB, and removes temp files.
func (r *Recorder) End() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}
	if !r.started {
		_ = os.Remove(r.absCast)
		return
	}
	if err := gzipFile(r.absCast, r.absGz); err != nil {
		logger.Error("replay: gzip failed", "error", err)
		_ = os.Remove(r.absCast)
		return
	}
	_ = os.Remove(r.absCast)
	info, err := os.Stat(r.absGz)
	if err != nil {
		logger.Error("replay: stat gz failed", "error", err)
		return
	}
	if err := r.audit.SetSessionReplayReady(r.sessionID, r.relGz, info.Size()); err != nil {
		logger.Error("replay: update DB failed", "error", err)
	}
}

func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := gzip.NewWriter(out)
	if _, err := io.Copy(zw, in); err != nil {
		_ = zw.Close()
		return err
	}
	return zw.Close()
}
