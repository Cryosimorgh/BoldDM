package manager

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"boltdm/internal/model"
)

func TestFilenameCannotEscapeDownloadDirectory(t *testing.T) {
	downloads := filepath.Join(t.TempDir(), "downloads")
	m, err := New(Config{DownloadDir: downloads, MaxActive: 1}, filepath.Join(t.TempDir(), "state"))
	if err != nil { t.Fatal(err) }
	defer m.Close()
	task, err := m.Add(model.Request{URL: "https://example.invalid/file", Filename: "../../evil.exe"})
	if err != nil { t.Fatal(err) }
	cleanDir, _ := filepath.Abs(downloads)
	cleanOut, _ := filepath.Abs(task.OutputPath)
	if !strings.HasPrefix(cleanOut, cleanDir+string(filepath.Separator)) { t.Fatalf("output escaped directory: %s", cleanOut) }
	if strings.Contains(task.Filename, "..") { t.Fatalf("unsafe filename: %s", task.Filename) }
}

func TestUpdateConfigPersistsAndNormalizes(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	initial := filepath.Join(t.TempDir(), "initial")
	m, err := New(Config{DownloadDir: initial}, state)
	if err != nil { t.Fatal(err) }
	defer m.Close()
	nextDir := filepath.Join(t.TempDir(), "next")
	cfg, err := m.UpdateConfig(Config{DownloadDir: nextDir, MaxActive: 4, MaxPerHost: 2, SegmentsPerFile: 12, SpeedLimitBytesPerSec: 5 << 20})
	if err != nil { t.Fatal(err) }
	if cfg.MaxActive != 4 || cfg.SegmentsPerFile != 12 || cfg.SpeedLimitBytesPerSec != 5<<20 { t.Fatalf("unexpected config: %+v", cfg) }
	if _, err := filepath.Abs(cfg.DownloadDir); err != nil { t.Fatal(err) }
	if _, err := os.Stat(filepath.Join(state, "config.json")); err != nil { t.Fatalf("config was not persisted: %v", err) }
}

func TestPauseResumeAll(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	m, err := New(Config{DownloadDir: filepath.Join(t.TempDir(), "downloads"), MaxActive: 1}, state)
	if err != nil { t.Fatal(err) }
	defer m.Close()
	m.mu.Lock()
	m.tasks["a"] = &runtimeTask{Task: model.Task{ID: "a", URL: "https://example.invalid/a", Filename: "a", State: model.StateQueued}}
	m.tasks["b"] = &runtimeTask{Task: model.Task{ID: "b", URL: "https://example.invalid/b", Filename: "b", State: model.StatePaused}}
	m.mu.Unlock()
	if n := m.PauseAll(); n != 1 { t.Fatalf("pause all affected %d, want 1", n) }
	if n := m.ResumeAll(); n != 2 { t.Fatalf("resume all affected %d, want 2", n) }
}

func TestMoveTaskMigratesPartialAndMetadata(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	newDir := filepath.Join(root, "new")
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(oldDir, 0o755); err != nil { t.Fatal(err) }
	m, err := New(Config{DownloadDir: oldDir, MaxActive: 1}, state)
	if err != nil { t.Fatal(err) }
	defer m.Close()
	out := filepath.Join(oldDir, "sample.bin")
	if err := os.WriteFile(out+".part", []byte("partial-data"), 0o644); err != nil { t.Fatal(err) }
	if err := os.WriteFile(out+".part.json", []byte(`{"url":"https://example.invalid/sample","size":12,"segments":[{"start":0,"end":11,"downloaded":12,"done":true}]}`), 0o644); err != nil { t.Fatal(err) }
	m.mu.Lock()
	m.tasks["x"] = &runtimeTask{Task: model.Task{ID: "x", URL: "https://example.invalid/sample", Filename: "sample.bin", OutputPath: out, State: model.StatePaused}}
	m.mu.Unlock()
	moved, err := m.MoveTask("x", newDir)
	if err != nil { t.Fatal(err) }
	if filepath.Dir(moved.OutputPath) != newDir { t.Fatalf("moved to %s, want %s", moved.OutputPath, newDir) }
	if moved.State != model.StatePaused { t.Fatalf("state=%s, want paused", moved.State) }
	if _, err := os.Stat(moved.OutputPath + ".part"); err != nil { t.Fatalf("partial missing: %v", err) }
	if _, err := os.Stat(moved.OutputPath + ".part.json"); err != nil { t.Fatalf("metadata missing: %v", err) }
	if _, err := os.Stat(out + ".part"); !os.IsNotExist(err) { t.Fatalf("old partial still exists") }
}

func TestMoveActiveTaskStopsMigratesAndResumes(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), 512*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"move-test"`)
		if r.Method == http.MethodHead { w.Header().Set("Content-Length", strconv.Itoa(len(data))); return }
		start, end := int64(0), int64(len(data)-1)
		if h := r.Header.Get("Range"); h != "" {
			_, _ = fmt.Sscanf(h, "bytes=%d-%d", &start, &end)
			if end >= int64(len(data)) { end = int64(len(data) - 1) }
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
			w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
			w.WriteHeader(http.StatusPartialContent)
		}
		for pos := start; pos <= end; {
			n := int64(16 * 1024)
			if pos+n-1 > end { n = end - pos + 1 }
			_, _ = w.Write(data[pos : pos+n])
			if f, ok := w.(http.Flusher); ok { f.Flush() }
			pos += n
			time.Sleep(4 * time.Millisecond)
		}
	}))
	defer srv.Close()
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	newDir := filepath.Join(root, "new")
	m, err := New(Config{DownloadDir: oldDir, MaxActive: 1, SegmentsPerFile: 4, ConnectionsPerFile: 2, MinSegmentSizeBytes: 256 << 10}, filepath.Join(root, "state"))
	if err != nil { t.Fatal(err) }
	defer m.Close()
	task, err := m.Add(model.Request{URL: srv.URL, Filename: "active.bin"})
	if err != nil { t.Fatal(err) }
	deadline := time.Now().Add(3 * time.Second)
	for {
		cur, _ := m.Task(task.ID)
		if cur.State == model.StateDownloading && cur.Downloaded > 0 { break }
		if time.Now().After(deadline) { t.Fatalf("download never became active: %+v", cur) }
		time.Sleep(10 * time.Millisecond)
	}
	moved, err := m.MoveTask(task.ID, newDir)
	if err != nil { t.Fatal(err) }
	if filepath.Dir(moved.OutputPath) != newDir { t.Fatalf("destination=%s", moved.OutputPath) }
	deadline = time.Now().Add(6 * time.Second)
	for {
		cur, _ := m.Task(task.ID)
		if cur.State == model.StateCompleted {
			got, err := os.ReadFile(cur.OutputPath)
			if err != nil { t.Fatal(err) }
			if !bytes.Equal(got, data) { t.Fatal("migrated download mismatch") }
			if filepath.Dir(cur.OutputPath) != newDir { t.Fatalf("completed in wrong folder: %s", cur.OutputPath) }
			break
		}
		if cur.State == model.StateFailed { t.Fatalf("download failed after migration: %s", cur.Error) }
		if time.Now().After(deadline) { t.Fatalf("download did not finish after migration: %+v", cur) }
		time.Sleep(20 * time.Millisecond)
	}
}
