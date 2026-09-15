package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"boltdm/internal/manager"
	"boltdm/internal/model"
)

const APIKey = "b7a9f4e6b8634a9bbf95c7a84c2d3e71"

//go:embed web/*
var embedded embed.FS

type Server struct {
	mgr      *manager.Manager
	http     *http.Server
	shutdown chan struct{}
}

func New(addr string, mgr *manager.Manager) *Server {
	s := &Server{mgr: mgr, shutdown: make(chan struct{}, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/downloads", s.auth(s.handleDownloads))
	mux.HandleFunc("/api/v1/tasks", s.auth(s.handleTasks))
	mux.HandleFunc("/api/v1/tasks/", s.auth(s.handleTaskAction))
	mux.HandleFunc("/api/v1/bulk/", s.auth(s.handleBulkAction))
	mux.HandleFunc("/api/v1/settings", s.auth(s.handleSettings))
	mux.HandleFunc("/api/v1/settings/pick-directory", s.auth(s.handlePickDirectory))
	mux.HandleFunc("/api/v1/system/open-downloads", s.auth(s.handleOpenDownloads))
	mux.HandleFunc("/api/v1/system/shutdown", s.auth(s.handleShutdown))
	sub, _ := fs.Sub(embedded, "web")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	s.http = &http.Server{Addr: addr, Handler: cors(mux), ReadHeaderTimeout: 5 * time.Second}
	return s
}

func (s *Server) ListenAndServe() error              { return s.http.ListenAndServe() }
func (s *Server) Close() error                       { return s.http.Close() }
func (s *Server) ShutdownRequested() <-chan struct{} { return s.shutdown }

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-BoltDM-Key") != APIKey {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "bad API key"})
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "name": "BoltDM", "version": "1.1.0"})
}

func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"error": "POST required"})
		return
	}
	var batch model.BatchRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&batch); err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	if len(batch.Downloads) == 0 {
		writeJSON(w, 400, map[string]any{"error": "empty batch"})
		return
	}
	if len(batch.Downloads) > 1000 {
		writeJSON(w, 400, map[string]any{"error": "batch too large"})
		return
	}
	tasks, errs := s.mgr.AddBatch(batch.Downloads)
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	writeJSON(w, 200, map[string]any{"accepted": len(tasks), "failed": len(errs), "errors": msgs, "tasks": tasks})
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]any{"error": "GET required"})
		return
	}
	writeJSON(w, 200, map[string]any{"tasks": s.mgr.List(), "config": s.mgr.Config()})
}

func (s *Server) handleTaskAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"error": "POST required"})
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 2 {
		writeJSON(w, 404, map[string]any{"error": "expected /api/v1/tasks/{id}/{action}"})
		return
	}
	id, action := parts[0], parts[1]
	var err error
	switch action {
	case "pause":
		err = s.mgr.Pause(id)
	case "resume":
		err = s.mgr.Resume(id)
	case "retry":
		err = s.mgr.Retry(id)
	case "cancel":
		err = s.mgr.Cancel(id)
	case "remove":
		deleteFiles, _ := strconv.ParseBool(r.URL.Query().Get("deleteFiles"))
		err = s.mgr.Remove(id, deleteFiles)
	case "move":
		var body struct {
			Destination string `json:"destination"`
		}
		if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); e != nil {
			err = e
		} else {
			_, err = s.mgr.MoveTask(id, body.Destination)
		}
	case "tune":
		var body struct {
			Segments    int  `json:"segments"`
			Connections int  `json:"connections"`
			ApplyNow    bool `json:"applyNow"`
		}
		if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); e != nil {
			err = e
		} else {
			_, err = s.mgr.UpdateTaskTuning(id, body.Segments, body.Connections, body.ApplyNow)
		}
	case "open", "reveal":
		task, ok := s.mgr.Task(id)
		if !ok {
			err = os.ErrNotExist
			break
		}
		if action == "open" {
			err = openPath(task.OutputPath)
		} else {
			err = revealPath(task.OutputPath)
		}
	default:
		writeJSON(w, 404, map[string]any{"error": "unknown action"})
		return
	}
	if err != nil {
		status := http.StatusBadRequest
		if os.IsNotExist(err) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleBulkAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"error": "POST required"})
		return
	}
	action := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/bulk/"), "/")
	var count int
	switch action {
	case "pause":
		count = s.mgr.PauseAll()
	case "resume":
		count = s.mgr.ResumeAll()
	case "retry-failed":
		count = s.mgr.RetryFailed()
	case "cancel":
		count = s.mgr.CancelAll()
	case "restart-active":
		count = s.mgr.RestartActive()
	case "clear-completed":
		count = s.mgr.ClearCompleted()
	default:
		writeJSON(w, 404, map[string]any{"error": "unknown bulk action"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "affected": count})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, map[string]any{"config": s.mgr.Config()})
	case http.MethodPost:
		var cfg manager.Config
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&cfg); err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		updated, err := s.mgr.UpdateConfig(cfg)
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "config": updated})
	default:
		writeJSON(w, 405, map[string]any{"error": "GET or POST required"})
	}
}

func (s *Server) handlePickDirectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"error": "POST required"})
		return
	}
	initial := strings.TrimSpace(r.URL.Query().Get("initial"))
	if initial == "" {
		initial = s.mgr.Config().DownloadDir
	}
	path, err := pickDirectory(initial)
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "path": path})
}

func (s *Server) handleOpenDownloads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"error": "POST required"})
		return
	}
	if err := openDirectory(s.mgr.Config().DownloadDir); err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"error": "POST required"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
	go func() {
		time.Sleep(120 * time.Millisecond)
		select {
		case s.shutdown <- struct{}{}:
		default:
		}
	}()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if strings.HasPrefix(origin, "chrome-extension://") || strings.HasPrefix(origin, "moz-extension://") {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-BoltDM-Key")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			if !strings.HasPrefix(origin, "chrome-extension://") && !strings.HasPrefix(origin, "moz-extension://") {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func pickDirectory(initial string) (string, error) {
	switch runtime.GOOS {
	case "windows":
		script := `$ErrorActionPreference='Stop'; Add-Type -AssemblyName System.Windows.Forms; $d=New-Object System.Windows.Forms.FolderBrowserDialog; $d.Description='Choose BoltDM download folder'; $d.ShowNewFolderButton=$true;` +
			`if ('` + psQuote(initial) + `' -ne '') { $d.SelectedPath='` + psQuote(initial) + `' }; if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($d.SelectedPath) } else { exit 2 }`
		out, err := exec.Command("powershell.exe", "-NoProfile", "-STA", "-Command", script).Output()
		if err != nil {
			return "", fmt.Errorf("folder selection canceled")
		}
		p := strings.TrimSpace(string(out))
		if p == "" {
			return "", fmt.Errorf("no folder selected")
		}
		return p, nil
	case "darwin":
		out, err := exec.Command("osascript", "-e", `POSIX path of (choose folder with prompt "Choose BoltDM download folder")`).Output()
		if err != nil {
			return "", fmt.Errorf("folder selection canceled")
		}
		return strings.TrimSpace(string(out)), nil
	default:
		if _, err := exec.LookPath("zenity"); err != nil {
			return "", fmt.Errorf("native folder picker unavailable; enter the path manually")
		}
		out, err := exec.Command("zenity", "--file-selection", "--directory", "--title=Choose BoltDM download folder").Output()
		if err != nil {
			return "", fmt.Errorf("folder selection canceled")
		}
		return strings.TrimSpace(string(out)), nil
	}
}

func psQuote(s string) string { return strings.ReplaceAll(s, "'", "''") }

func openDirectory(path string) error {
	if path == "" {
		return errorsNew("empty path")
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

func revealPath(path string) error {
	if _, err := os.Stat(path); err != nil {
		partial := path + ".part"
		if _, partialErr := os.Stat(partial); partialErr == nil {
			path = partial
		} else {
			return openDirectory(filepath.Dir(path))
		}
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", "/select,"+path).Start()
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	default:
		return openDirectory(filepath.Dir(path))
	}
}

func openPath(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

func errorsNew(s string) error { return fmt.Errorf("%s", s) }

func (s *Server) URL() string { return fmt.Sprintf("http://%s", s.http.Addr) }
