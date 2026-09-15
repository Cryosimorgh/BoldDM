package manager

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"boltdm/internal/downloader"
	"boltdm/internal/model"
)

func (m *Manager) Add(req model.Request) (model.Task, error) {
	if strings.TrimSpace(req.URL) == "" {
		return model.Task{}, errors.New("empty URL")
	}
	u, err := url.Parse(req.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return model.Task{}, errors.New("only http/https URLs are supported")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rt := range m.tasks {
		if rt.Task.URL == req.URL && rt.Task.State != model.StateCanceled && rt.Task.State != model.StateFailed {
			return rt.Task, nil
		}
	}

	dir := strings.TrimSpace(req.DownloadDir)
	if dir == "" {
		dir = m.config.DownloadDir
	}
	if abs, e := filepath.Abs(dir); e == nil {
		dir = abs
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return model.Task{}, fmt.Errorf("download directory: %w", err)
	}
	name := safeFilename(strings.TrimSpace(req.Filename))
	if name == "" {
		name = safeFilename(downloader.FilenameFromURL(req.URL))
	}
	name = uniqueNameLocked(dir, name, m.tasks)
	now := time.Now()
	t := model.Task{ID: newID(), URL: req.URL, Filename: name, OutputPath: filepath.Join(dir, name), Headers: req.Headers, State: model.StateQueued, SegmentSetting: clamp(req.Segments, 0, 64), ConnectionSetting: clamp(req.Connections, 0, 32), CreatedAt: now, UpdatedAt: now}
	m.tasks[t.ID] = &runtimeTask{Task: t}
	_ = m.saveLocked()
	m.signal()
	return t, nil
}

func (m *Manager) AddBatch(reqs []model.Request) ([]model.Task, []error) {
	out := make([]model.Task, 0, len(reqs))
	errs := make([]error, 0)
	for _, r := range reqs {
		t, e := m.Add(r)
		if e != nil {
			errs = append(errs, e)
			continue
		}
		out = append(out, t)
	}
	return out, errs
}

func (m *Manager) List() []model.Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		out = append(out, t.Task)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (m *Manager) Pause(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt := m.tasks[id]
	if rt == nil {
		return os.ErrNotExist
	}
	if rt.Task.State == model.StateDownloading && rt.cancel != nil {
		rt.cancel()
	}
	if rt.Task.State == model.StateQueued || rt.Task.State == model.StateDownloading || rt.Task.State == model.StateFailed {
		rt.Task.State = model.StatePaused
		rt.Task.Error = ""
		rt.Task.SpeedBytesPerSec = 0
		rt.Task.UpdatedAt = time.Now()
	}
	return m.saveLocked()
}

func (m *Manager) Resume(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt := m.tasks[id]
	if rt == nil {
		return os.ErrNotExist
	}
	if rt.Task.State == model.StatePaused || rt.Task.State == model.StateFailed || rt.Task.State == model.StateCanceled {
		rt.Task.State = model.StateQueued
		rt.Task.Error = ""
		rt.Task.UpdatedAt = time.Now()
		_ = m.saveLocked()
		m.signal()
	}
	return nil
}

func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt := m.tasks[id]
	if rt == nil {
		return os.ErrNotExist
	}
	if rt.cancel != nil {
		rt.cancel()
	}
	rt.Task.State = model.StateCanceled
	rt.Task.SpeedBytesPerSec = 0
	rt.Task.UpdatedAt = time.Now()
	return m.saveLocked()
}

func (m *Manager) Retry(id string) error { return m.Resume(id) }

func (m *Manager) PauseAll() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, rt := range m.tasks {
		if rt.Task.State == model.StateQueued || rt.Task.State == model.StateDownloading {
			if rt.cancel != nil {
				rt.cancel()
			}
			rt.Task.State = model.StatePaused
			rt.Task.SpeedBytesPerSec = 0
			rt.Task.UpdatedAt = time.Now()
			count++
		}
	}
	_ = m.saveLocked()
	return count
}

func (m *Manager) ResumeAll() int {
	m.mu.Lock()
	count := 0
	for _, rt := range m.tasks {
		if rt.Task.State == model.StatePaused {
			rt.Task.State = model.StateQueued
			rt.Task.Error = ""
			rt.Task.UpdatedAt = time.Now()
			count++
		}
	}
	_ = m.saveLocked()
	m.mu.Unlock()
	if count > 0 {
		m.signal()
	}
	return count
}

func (m *Manager) RetryFailed() int {
	m.mu.Lock()
	count := 0
	for _, rt := range m.tasks {
		if rt.Task.State == model.StateFailed {
			rt.Task.State = model.StateQueued
			rt.Task.Error = ""
			rt.Task.UpdatedAt = time.Now()
			count++
		}
	}
	_ = m.saveLocked()
	m.mu.Unlock()
	if count > 0 {
		m.signal()
	}
	return count
}

func (m *Manager) RestartActive() int {
	m.mu.Lock()
	count := 0
	for _, rt := range m.tasks {
		if rt.Task.State == model.StateDownloading && rt.cancel != nil {
			rt.Task.State = model.StateQueued
			rt.Task.SpeedBytesPerSec = 0
			rt.Task.UpdatedAt = time.Now()
			rt.cancel()
			count++
		}
	}
	_ = m.saveLocked()
	m.mu.Unlock()
	if count > 0 {
		m.signal()
	}
	return count
}

func (m *Manager) CancelAll() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, rt := range m.tasks {
		if rt.Task.State == model.StateDownloading || rt.Task.State == model.StateQueued || rt.Task.State == model.StatePaused {
			if rt.cancel != nil {
				rt.cancel()
			}
			rt.Task.State = model.StateCanceled
			rt.Task.SpeedBytesPerSec = 0
			rt.Task.UpdatedAt = time.Now()
			count++
		}
	}
	_ = m.saveLocked()
	return count
}

func (m *Manager) ClearCompleted() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for id, rt := range m.tasks {
		if rt.Task.State == model.StateCompleted {
			delete(m.tasks, id)
			count++
		}
	}
	_ = m.saveLocked()
	return count
}

func (m *Manager) Remove(id string, deleteFiles bool) error {
	m.mu.Lock()
	rt := m.tasks[id]
	if rt == nil {
		m.mu.Unlock()
		return os.ErrNotExist
	}
	if rt.cancel != nil {
		rt.cancel()
	}
	output := rt.Task.OutputPath
	delete(m.tasks, id)
	err := m.saveLocked()
	m.mu.Unlock()
	if deleteFiles {
		_ = os.Remove(output)
		_ = os.Remove(output + ".part")
		_ = os.Remove(output + ".part.json")
	}
	m.signal()
	return err
}

func (m *Manager) UpdateTaskTuning(id string, segments, connections int, applyNow bool) (model.Task, error) {
	segments = clamp(segments, 0, 64)
	connections = clamp(connections, 0, 32)
	m.mu.Lock()
	rt := m.tasks[id]
	if rt == nil {
		m.mu.Unlock()
		return model.Task{}, os.ErrNotExist
	}
	rt.Task.SegmentSetting = segments
	rt.Task.ConnectionSetting = connections
	rt.Task.UpdatedAt = time.Now()
	if applyNow && rt.Task.State == model.StateDownloading && rt.cancel != nil {
		rt.Task.State = model.StateQueued
		rt.Task.SpeedBytesPerSec = 0
		rt.cancel()
	}
	_ = m.saveLocked()
	out := rt.Task
	m.mu.Unlock()
	m.signal()
	return out, nil
}
