package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"boltdm/internal/manager"
	"boltdm/internal/server"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	stateDir := resolveStateDir(home)
	_ = os.MkdirAll(stateDir, 0o755)
	if lf, e := os.OpenFile(filepath.Join(stateDir, "boltdm.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); e == nil {
		defer lf.Close()
		log.SetOutput(lf)
		log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	}
	cfg := loadConfig(filepath.Join(stateDir, "config.json"))
	applyEnv(&cfg)
	mgr, err := manager.New(cfg, stateDir)
	if err != nil {
		log.Fatal(err)
	}
	defer mgr.Close()
	_ = saveConfig(filepath.Join(stateDir, "config.json"), mgr.Config())

	srv := server.New("127.0.0.1:17654", mgr)
	go func() {
		log.Println("BoltDM running at http://127.0.0.1:17654")
		log.Println("Downloads:", mgr.Config().DownloadDir)
		for name, status := range mgr.EngineStatus() {
			if status.Available {
				log.Printf("Transfer engine %s available (version=%s managed=%v)", name, status.Version, status.Managed)
			} else {
				log.Printf("Transfer engine %s unavailable: %s", name, status.Error)
			}
		}
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Println("server:", err)
			os.Exit(1)
		}
	}()
	trayQuit := make(chan struct{}, 1)
	exePath, _ := os.Executable()
	iconPath := filepath.Join(filepath.Dir(exePath), "BoltDM.ico")
	stopTray := startTray(iconPath,
		func() { _ = openBrowser("http://127.0.0.1:17654") },
		func() { mgr.PauseAll() },
		func() { mgr.ResumeAll() },
		func() {
			select {
			case trayQuit <- struct{}{}:
			default:
			}
		},
	)
	defer stopTray()
	time.Sleep(180 * time.Millisecond)
	if os.Getenv("BOLTDM_NO_BROWSER") == "" && shouldOpenDashboard(runtime.GOOS, mgr.Config().LaunchMode) {
		_ = openBrowser("http://127.0.0.1:17654")
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	select {
	case <-ch:
	case <-trayQuit:
	case <-srv.ShutdownRequested():
	}
	_ = srv.Close()
}

func resolveStateDir(home string) string {
	if configured := strings.TrimSpace(os.Getenv("BOLTDM_STATE_DIR")); configured != "" {
		return configured
	}
	return resolveStateDirForOS(home, runtime.GOOS)
}

func resolveStateDirForOS(home, goos string) string {
	legacy := filepath.Join(home, ".boltdm")
	if goos != "linux" {
		return legacy
	}
	// Preserve existing Linux installs before adopting the XDG state location.
	if st, err := os.Stat(legacy); err == nil && st.IsDir() {
		return legacy
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); xdg != "" {
		return filepath.Join(xdg, "boltdm")
	}
	return filepath.Join(home, ".local", "state", "boltdm")
}

func shouldOpenDashboard(goos, launchMode string) bool {
	// Native tray support currently exists on Windows. On Linux/macOS, never let
	// a persisted "tray" preference make the process start with no visible UI.
	return goos != "windows" || launchMode != "tray"
}

func loadConfig(path string) manager.Config {
	var cfg manager.Config
	b, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	return cfg
}
func saveConfig(path string, cfg manager.Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
func applyEnv(cfg *manager.Config) {
	if v := os.Getenv("BOLTDM_DOWNLOAD_DIR"); v != "" {
		cfg.DownloadDir = v
	}
	if v, err := strconv.Atoi(os.Getenv("BOLTDM_MAX_ACTIVE")); err == nil && v > 0 {
		cfg.MaxActive = v
	}
	if v, err := strconv.Atoi(os.Getenv("BOLTDM_MAX_PER_HOST")); err == nil && v > 0 {
		cfg.MaxPerHost = v
	}
	if v, err := strconv.Atoi(os.Getenv("BOLTDM_SEGMENTS")); err == nil && v > 0 {
		cfg.SegmentsPerFile = v
	}
	if v := os.Getenv("BOLTDM_ARIA2_MODE"); v != "" {
		cfg.Aria2.Mode = v
	}
	if v := os.Getenv("BOLTDM_ARIA2_EXECUTABLE"); v != "" {
		cfg.Aria2.Executable = v
	}
	if v := os.Getenv("BOLTDM_ARIA2_RPC_URL"); v != "" {
		cfg.Aria2.RPCURL = v
	}
	if v := os.Getenv("BOLTDM_ARIA2_RPC_SECRET"); v != "" {
		cfg.Aria2.RPCSecret = v
	}
}
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
