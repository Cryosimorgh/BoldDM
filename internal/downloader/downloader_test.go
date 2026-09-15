package downloader

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"boltdm/internal/model"
)

func rangeServer(data []byte, slow bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"test-etag"`)
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK)
			return
		}
		start, end := int64(0), int64(len(data)-1)
		if h := r.Header.Get("Range"); h != "" {
			var s, e int64
			if _, err := fmt.Sscanf(h, "bytes=%d-%d", &s, &e); err == nil {
				start, end = s, e
			}
			if end >= int64(len(data)) || end < start {
				end = int64(len(data) - 1)
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
			w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		}
		chunk := 32 * 1024
		for pos := start; pos <= end; {
			n := int64(chunk)
			if pos+n-1 > end {
				n = end - pos + 1
			}
			_, _ = w.Write(data[pos : pos+n])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			pos += n
			if slow {
				time.Sleep(3 * time.Millisecond)
			}
		}
	}))
}

func TestSegmentedDownload(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), 256*1024)
	srv := rangeServer(data, false)
	defer srv.Close()
	dir := t.TempDir()
	out := filepath.Join(dir, "file.bin")
	d := New(Options{Segments: 4, MinSegmentSize: 1, Retries: 2, Timeout: 3 * time.Second})
	err := d.Download(context.Background(), model.Task{URL: srv.URL, OutputPath: out}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("download mismatch: got %d want %d", len(got), len(data))
	}
	if _, err := os.Stat(out + ".part.json"); !os.IsNotExist(err) {
		t.Fatalf("resume metadata should be removed")
	}
}

func TestResumeSegmentedDownload(t *testing.T) {
	data := bytes.Repeat([]byte("abcdef0123456789"), 1024*1024)
	srv := rangeServer(data, true)
	defer srv.Close()
	dir := t.TempDir()
	out := filepath.Join(dir, "resume.bin")
	d := New(Options{Segments: 4, MinSegmentSize: 1, Retries: 2, Timeout: 3 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(120 * time.Millisecond); cancel() }()
	_ = d.Download(ctx, model.Task{URL: srv.URL, OutputPath: out}, nil)
	if _, err := os.Stat(out + ".part.json"); err != nil {
		t.Fatalf("expected resume metadata: %v", err)
	}
	if err := d.Download(context.Background(), model.Task{URL: srv.URL, OutputPath: out}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("resumed result mismatch")
	}
}

func TestNoRangeServerFallsBack(t *testing.T) {
	data := []byte(strings.Repeat("x", 1024*1024))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(data)
	}))
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "plain.bin")
	d := New(Options{Segments: 8, MinSegmentSize: 1, Timeout: 3 * time.Second})
	if err := d.Download(context.Background(), model.Task{URL: srv.URL, OutputPath: out}, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	if !bytes.Equal(got, data) {
		t.Fatal("fallback result mismatch")
	}
}

func TestGlobalSpeedLimit(t *testing.T) {
	data := bytes.Repeat([]byte("speed-limit"), 24*1024) // 264 KiB
	srv := rangeServer(data, false)
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "limited.bin")
	d := New(Options{Segments: 4, MinSegmentSize: 1, Retries: 2, Timeout: 3 * time.Second})
	d.SetSpeedLimit(512 * 1024)
	start := time.Now()
	if err := d.Download(context.Background(), model.Task{URL: srv.URL, OutputPath: out}, nil); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < 250*time.Millisecond {
		t.Fatalf("speed limit was not applied; download completed in %v", elapsed)
	}
	got, _ := os.ReadFile(out)
	if !bytes.Equal(got, data) {
		t.Fatal("limited download result mismatch")
	}
}

func TestConnectionsLimitParallelSegmentWorkers(t *testing.T) {
	data := bytes.Repeat([]byte("connection-limit"), 256*1024)
	var active, peak atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if r.Method == http.MethodHead {
			return
		}
		start, end := int64(0), int64(len(data)-1)
		if h := r.Header.Get("Range"); h != "" {
			_, _ = fmt.Sscanf(h, "bytes=%d-%d", &start, &end)
			if end >= int64(len(data)) {
				end = int64(len(data) - 1)
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
			w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
			w.WriteHeader(http.StatusPartialContent)
		}
		cur := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if cur <= old || peak.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write(data[start : end+1])
	}))
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "connections.bin")
	d := New(Options{Segments: 8, Connections: 2, MinSegmentSize: 1, Retries: 2, Timeout: 3 * time.Second})
	if err := d.Download(context.Background(), model.Task{URL: srv.URL, OutputPath: out}, nil); err != nil {
		t.Fatal(err)
	}
	if peak.Load() > 2 {
		t.Fatalf("peak segment workers %d, want <= 2", peak.Load())
	}
}

func TestRepartitionMetaPreservesContiguousDownloadedBytes(t *testing.T) {
	old := model.ResumeMeta{Size: 100, Segments: []model.SegmentProgress{
		{Start: 0, End: 24, Downloaded: 25, Done: true},
		{Start: 25, End: 49, Downloaded: 10},
		{Start: 50, End: 74, Downloaded: 25, Done: true},
		{Start: 75, End: 99, Downloaded: 0},
	}}
	got := repartitionMeta(old, 2)
	if len(got.Segments) != 2 {
		t.Fatalf("segments=%d, want 2", len(got.Segments))
	}
	if got.Segments[0].Downloaded != 35 {
		t.Fatalf("first downloaded=%d, want 35", got.Segments[0].Downloaded)
	}
	if got.Segments[1].Downloaded != 25 {
		t.Fatalf("second downloaded=%d, want 25", got.Segments[1].Downloaded)
	}
}
