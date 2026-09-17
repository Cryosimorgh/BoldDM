package manager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"boltdm/internal/aria2rpc"
	"boltdm/internal/downloader"
	"boltdm/internal/model"
	"boltdm/internal/transfer"
)

type AppearanceConfig struct {
	FontFamily      string `json:"fontFamily"`
	FontSizePercent int    `json:"fontSizePercent"`
	FontWeight      int    `json:"fontWeight"`
	FontStyle       string `json:"fontStyle"`
	Density         string `json:"density"`
	AccentColor     string `json:"accentColor"`
	BackgroundColor string `json:"backgroundColor"`
	SurfaceColor    string `json:"surfaceColor"`
	TextColor       string `json:"textColor"`
	CornerRadius    int    `json:"cornerRadius"`
	ReduceMotion    bool   `json:"reduceMotion"`
	CustomCSS       string `json:"customCSS"`
}

type Aria2Config struct {
	Mode       string `json:"mode"`
	Executable string `json:"executable,omitempty"`
	RPCURL     string `json:"rpcUrl,omitempty"`
	RPCSecret  string `json:"rpcSecret,omitempty"`
}

type Config struct {
	DownloadDir           string           `json:"downloadDir"`
	MaxActive             int              `json:"maxActive"`
	MaxPerHost            int              `json:"maxPerHost"`
	SegmentsPerFile       int              `json:"segmentsPerFile"`
	ConnectionsPerFile    int              `json:"connectionsPerFile"`
	MinSegmentSizeBytes   int64            `json:"minSegmentSizeBytes"`
	Retries               int              `json:"retries"`
	SpeedLimitBytesPerSec int64            `json:"speedLimitBytesPerSec"`
	LaunchMode            string           `json:"launchMode"`
	Appearance            AppearanceConfig `json:"appearance"`
	Aria2                 Aria2Config      `json:"aria2"`
}

type EngineInfo struct {
	Available bool     `json:"available"`
	Managed   bool     `json:"managed,omitempty"`
	Version   string   `json:"version,omitempty"`
	Features  []string `json:"features,omitempty"`
	Error     string   `json:"error,omitempty"`
}

type runtimeTask struct {
	Task    model.Task
	cancel  context.CancelFunc
	runDone chan struct{}
}

type Manager struct {
	mu           sync.RWMutex
	tasks        map[string]*runtimeTask
	config       Config
	stateDir     string
	native       *transfer.NativeEngine
	engines      map[model.EngineName]transfer.Engine
	aria2Service *aria2rpc.Service
	aria2Error   string
	wake         chan struct{}
	closed       chan struct{}
	done         chan struct{}
	closeOnce    sync.Once
}

func New(config Config, stateDir string) (*Manager, error) {
	config = normalizeConfig(config)
	if err := os.MkdirAll(config.DownloadDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, err
	}
	dl := downloader.New(downloader.Options{Segments: config.SegmentsPerFile, Connections: config.ConnectionsPerFile, MinSegmentSize: config.MinSegmentSizeBytes, Retries: config.Retries})
	dl.SetSpeedLimit(config.SpeedLimitBytesPerSec)
	native := transfer.NewNativeEngine(dl)
	m := &Manager{
		tasks: make(map[string]*runtimeTask), config: config, stateDir: stateDir,
		native: native, engines: map[model.EngineName]transfer.Engine{model.EngineNative: native},
		wake: make(chan struct{}, 1), closed: make(chan struct{}), done: make(chan struct{}),
	}
	m.openAria2()
	_ = m.load()
	_ = m.saveConfig()
	go m.scheduler()
	m.signal()
	return m, nil
}

func (m *Manager) openAria2() {
	if m.config.Aria2.Mode == "off" {
		m.aria2Error = "aria2 integration is disabled"
		return
	}
	svc, err := aria2rpc.OpenService(aria2rpc.ServiceConfig{
		Mode: m.config.Aria2.Mode, Executable: m.config.Aria2.Executable,
		RPCURL: m.config.Aria2.RPCURL, RPCSecret: m.config.Aria2.RPCSecret,
		StateDir: filepath.Join(m.stateDir, "aria2"),
	})
	if err != nil {
		m.aria2Error = err.Error()
		return
	}
	m.aria2Service = svc
	m.engines[model.EngineAria2] = transfer.NewAria2Engine(svc.Client())
	m.aria2Error = ""
}

func normalizeConfig(config Config) Config {
	if config.DownloadDir == "" {
		home, _ := os.UserHomeDir()
		config.DownloadDir = filepath.Join(home, "Downloads", "BoltDM")
	}
	if abs, err := filepath.Abs(config.DownloadDir); err == nil {
		config.DownloadDir = abs
	}
	if config.MaxActive <= 0 {
		config.MaxActive = 3
	}
	if config.MaxActive > 32 {
		config.MaxActive = 32
	}
	if config.MaxPerHost <= 0 {
		config.MaxPerHost = 2
	}
	if config.MaxPerHost > 32 {
		config.MaxPerHost = 32
	}
	if config.SegmentsPerFile <= 0 {
		config.SegmentsPerFile = 8
	}
	if config.SegmentsPerFile > 64 {
		config.SegmentsPerFile = 64
	}
	if config.ConnectionsPerFile <= 0 {
		config.ConnectionsPerFile = 4
	}
	if config.ConnectionsPerFile > 32 {
		config.ConnectionsPerFile = 32
	}
	if config.MinSegmentSizeBytes <= 0 {
		config.MinSegmentSizeBytes = 8 << 20
	}
	if config.MinSegmentSizeBytes < 256<<10 {
		config.MinSegmentSizeBytes = 256 << 10
	}
	if config.Retries <= 0 {
		config.Retries = 6
	}
	if config.Retries > 20 {
		config.Retries = 20
	}
	if config.LaunchMode != "tray" {
		config.LaunchMode = "dashboard"
	}
	if strings.TrimSpace(config.Appearance.FontFamily) == "" {
		config.Appearance.FontFamily = `"Segoe UI Variable","Segoe UI",Inter,system-ui,sans-serif`
	}
	if config.Appearance.FontSizePercent <= 0 {
		config.Appearance.FontSizePercent = 100
	}
	if config.Appearance.FontSizePercent < 75 {
		config.Appearance.FontSizePercent = 75
	}
	if config.Appearance.FontSizePercent > 150 {
		config.Appearance.FontSizePercent = 150
	}
	if config.Appearance.FontWeight < 300 {
		config.Appearance.FontWeight = 400
	}
	if config.Appearance.FontWeight > 800 {
		config.Appearance.FontWeight = 800
	}
	if config.Appearance.FontStyle != "italic" {
		config.Appearance.FontStyle = "normal"
	}
	if config.Appearance.Density != "compact" && config.Appearance.Density != "spacious" {
		config.Appearance.Density = "balanced"
	}
	if !validHexColor(config.Appearance.AccentColor) {
		config.Appearance.AccentColor = "#c9a46a"
	}
	if !validHexColor(config.Appearance.BackgroundColor) {
		config.Appearance.BackgroundColor = "#070708"
	}
	if !validHexColor(config.Appearance.SurfaceColor) {
		config.Appearance.SurfaceColor = "#0e0e11"
	}
	if !validHexColor(config.Appearance.TextColor) {
		config.Appearance.TextColor = "#f2efe8"
	}
	if config.Appearance.CornerRadius <= 0 {
		config.Appearance.CornerRadius = 14
	}
	if config.Appearance.CornerRadius > 28 {
		config.Appearance.CornerRadius = 28
	}
	if config.SpeedLimitBytesPerSec < 0 {
		config.SpeedLimitBytesPerSec = 0
	}
	mode := strings.ToLower(strings.TrimSpace(config.Aria2.Mode))
	if mode != "off" && mode != "external" {
		mode = "auto"
	}
	config.Aria2.Mode = mode
	return config
}

func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		close(m.closed)
		m.mu.Lock()
		dones := make([]chan struct{}, 0)
		for _, t := range m.tasks {
			if t.cancel != nil {
				t.cancel()
			}
			if t.runDone != nil {
				dones = append(dones, t.runDone)
			}
		}
		m.mu.Unlock()
		<-m.done
		deadline := time.After(5 * time.Second)
		for _, done := range dones {
			select {
			case <-done:
			case <-deadline:
				break
			}
		}
		_ = m.save()
		_ = m.saveConfig()
		if m.aria2Service != nil {
			_ = m.aria2Service.Close()
		}
	})
}

func (m *Manager) Config() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

func (m *Manager) EngineStatus() map[string]EngineInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[string]EngineInfo{
		string(model.EngineNative): {Available: true, Version: "builtin", Features: []string{"http", "https", "segmented-resume"}},
	}
	aria := EngineInfo{Available: m.aria2Service != nil, Error: m.aria2Error}
	if m.aria2Service != nil {
		v := m.aria2Service.Version()
		aria.Managed = m.aria2Service.Managed()
		aria.Version = v.Version
		aria.Features = append([]string(nil), v.EnabledFeatures...)
	}
	out[string(model.EngineAria2)] = aria
	return out
}

func (m *Manager) UpdateConfig(config Config) (Config, error) {
	config = normalizeConfig(config)
	if strings.TrimSpace(config.DownloadDir) == "" {
		return Config{}, errors.New("download directory cannot be empty")
	}
	if err := os.MkdirAll(config.DownloadDir, 0o755); err != nil {
		return Config{}, fmt.Errorf("download directory: %w", err)
	}
	m.mu.Lock()
	m.config = config
	dl := m.native.Downloader()
	dl.SetSegments(config.SegmentsPerFile)
	dl.SetConnections(config.ConnectionsPerFile)
	dl.SetMinSegmentSize(config.MinSegmentSizeBytes)
	dl.SetRetries(config.Retries)
	dl.SetSpeedLimit(config.SpeedLimitBytesPerSec)
	err := m.saveConfigLocked()
	m.mu.Unlock()
	m.signal()
	return config, err
}
