package manager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"boltdm/internal/model"
)

func (m *Manager) scheduler() {
	defer close(m.done)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.wake:
			for m.startOne() {
			}
		case <-ticker.C:
			for m.startOne() {
			}
		case <-m.closed:
			return
		}
	}
}

func (m *Manager) startOne() bool {
	m.mu.Lock()
	active := 0
	perHost := map[string]int{}
	for _, rt := range m.tasks {
		if rt.Task.State == model.StateDownloading {
			active++
			perHost[hostOf(rt.Task.URL)]++
		}
	}
	if active >= m.config.MaxActive {
		m.mu.Unlock()
		return false
	}
	var pick *runtimeTask
	for _, rt := range m.tasks {
		if rt.Task.State != model.StateQueued || rt.runDone != nil {
			continue
		}
		if perHost[hostOf(rt.Task.URL)] >= m.config.MaxPerHost {
			continue
		}
		if pick == nil || rt.Task.CreatedAt.Before(pick.Task.CreatedAt) {
			pick = rt
		}
	}
	if pick == nil {
		m.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	pick.cancel = cancel
	pick.runDone = make(chan struct{})
	pick.Task.State = model.StateDownloading
	pick.Task.Error = ""
	pick.Task.UpdatedAt = time.Now()
	taskCopy := pick.Task
	_ = m.saveLocked()
	m.mu.Unlock()
	go m.run(ctx, taskCopy.ID)
	return true
}

func (m *Manager) run(ctx context.Context, id string) {
	m.mu.RLock()
	rt := m.tasks[id]
	if rt == nil {
		m.mu.RUnlock()
		return
	}
	task := rt.Task
	m.mu.RUnlock()
	err := m.dl.Download(ctx, task, func(done, total, speed int64, segments, connections int) {
		m.mu.Lock()
		if cur := m.tasks[id]; cur != nil && cur.Task.State == model.StateDownloading {
			cur.Task.Downloaded = done
			cur.Task.Size = total
			cur.Task.SpeedBytesPerSec = speed
			cur.Task.Segments = segments
			cur.Task.Connections = connections
			if total > 0 {
				cur.Task.Progress = float64(done) * 100 / float64(total)
			}
			cur.Task.UpdatedAt = time.Now()
		}
		m.mu.Unlock()
	})
	m.mu.Lock()
	cur := m.tasks[id]
	if cur == nil {
		m.mu.Unlock()
		return
	}
	cur.cancel = nil
	if cur.runDone != nil {
		close(cur.runDone)
		cur.runDone = nil
	}
	cur.Task.SpeedBytesPerSec = 0
	cur.Task.UpdatedAt = time.Now()
	if err == nil {
		cur.Task.State = model.StateCompleted
		cur.Task.Progress = 100
		if cur.Task.Size > 0 {
			cur.Task.Downloaded = cur.Task.Size
		}
		cur.Task.Error = ""
	} else if cur.Task.State == model.StatePaused || cur.Task.State == model.StateCanceled || cur.Task.State == model.StateQueued {
		// Preserve explicit user state.
	} else if errors.Is(err, context.Canceled) {
		cur.Task.State = model.StatePaused
	} else {
		cur.Task.State = model.StateFailed
		cur.Task.Error = err.Error()
	}
	_ = m.saveLocked()
	m.mu.Unlock()
	m.signal()
}

func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func hostOf(raw string) string {
	u, _ := url.Parse(raw)
	return strings.ToLower(u.Hostname())
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func safeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(name)
	if name == "." || name == ".." || name == "" {
		return "download.bin"
	}
	bad := `<>:"/\\|?*`
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(bad, r) {
			return '_'
		}
		return r
	}, name)
	if len(name) > 220 {
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		if len(base) > 180 {
			base = base[:180]
		}
		name = base + ext
	}
	return name
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
func validHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func uniqueNameLocked(dir, name string, tasks map[string]*runtimeTask) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	candidate := name
	cleanDir := filepath.Clean(dir)
	used := func(n string) bool {
		if _, e := os.Stat(filepath.Join(cleanDir, n)); e == nil {
			return true
		}
		for _, t := range tasks {
			if filepath.Clean(filepath.Dir(t.Task.OutputPath)) == cleanDir && strings.EqualFold(t.Task.Filename, n) && t.Task.State != model.StateCanceled {
				return true
			}
		}
		return false
	}
	for i := 1; used(candidate); i++ {
		candidate = fmt.Sprintf("%s (%d)%s", base, i, ext)
	}
	return candidate
}

func (m *Manager) statePath() string  { return filepath.Join(m.stateDir, "state.json") }
func (m *Manager) configPath() string { return filepath.Join(m.stateDir, "config.json") }
func (m *Manager) save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.saveReadLocked()
}
func (m *Manager) saveLocked() error { return m.saveReadLocked() }
func (m *Manager) saveReadLocked() error {
	arr := make([]model.Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		arr = append(arr, t.Task)
	}
	b, e := json.MarshalIndent(arr, "", "  ")
	if e != nil {
		return e
	}
	tmp := m.statePath() + ".tmp"
	if e = os.WriteFile(tmp, b, 0o644); e != nil {
		return e
	}
	return os.Rename(tmp, m.statePath())
}
func (m *Manager) saveConfig() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.saveConfigLocked()
}
func (m *Manager) saveConfigLocked() error {
	b, e := json.MarshalIndent(m.config, "", "  ")
	if e != nil {
		return e
	}
	tmp := m.configPath() + ".tmp"
	if e = os.WriteFile(tmp, b, 0o644); e != nil {
		return e
	}
	return os.Rename(tmp, m.configPath())
}
func (m *Manager) load() error {
	b, e := os.ReadFile(m.statePath())
	if e != nil {
		return e
	}
	var arr []model.Task
	if e = json.Unmarshal(b, &arr); e != nil {
		return e
	}
	for _, t := range arr {
		if t.State == model.StateDownloading {
			t.State = model.StateQueued
			t.SpeedBytesPerSec = 0
		}
		tc := t
		m.tasks[t.ID] = &runtimeTask{Task: tc}
	}
	return nil
}
