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
	stateDir := filepath.Join(home, ".boltdm")
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
	if os.Getenv("BOLTDM_NO_BROWSER") == "" && mgr.Config().LaunchMode != "tray" {
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
