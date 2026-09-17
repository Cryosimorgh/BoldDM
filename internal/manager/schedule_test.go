package manager

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"boltdm/internal/model"
)

func TestScheduledDownloadDoesNotStartEarly(t *testing.T) {
	requested := make(chan time.Time, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case requested <- time.Now():
		default:
		}
		w.Header().Set("Content-Length", "4")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write([]byte("test"))
	}))
	defer srv.Close()

	root := t.TempDir()
	m, err := New(Config{DownloadDir: filepath.Join(root, "downloads"), MaxActive: 1, Aria2: Aria2Config{Mode: "off"}}, filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	start := time.Now().Add(650 * time.Millisecond)
	task, err := m.Add(model.Request{URL: srv.URL + "/file.bin", StartAt: &start})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	cur, ok := m.Task(task.ID)
	if !ok {
		t.Fatal("scheduled task disappeared")
	}
	if cur.State != model.StateQueued {
		t.Fatalf("state before schedule=%s, want queued", cur.State)
	}
	select {
	case at := <-requested:
		t.Fatalf("request started early at %s; scheduled for %s", at, start)
	default:
	}

	deadline := time.Now().Add(4 * time.Second)
	for {
		cur, _ = m.Task(task.ID)
		if cur.State == model.StateCompleted {
			break
		}
		if cur.State == model.StateFailed {
			t.Fatalf("scheduled task failed: %s", cur.Error)
		}
		if time.Now().After(deadline) {
			t.Fatalf("scheduled task did not complete: %+v", cur)
		}
		time.Sleep(30 * time.Millisecond)
	}
}
