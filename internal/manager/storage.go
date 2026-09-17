package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"boltdm/internal/model"
)

func (m *Manager) MoveTask(id, destination string) (model.Task, error) {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return model.Task{}, errors.New("destination cannot be empty")
	}
	if abs, err := filepath.Abs(destination); err == nil {
		destination = abs
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return model.Task{}, fmt.Errorf("destination: %w", err)
	}

	m.mu.Lock()
	rt := m.tasks[id]
	if rt == nil {
		m.mu.Unlock()
		return model.Task{}, os.ErrNotExist
	}
	if rt.Task.Engine != model.EngineNative && rt.Task.Engine != "" {
		m.mu.Unlock()
		return model.Task{}, fmt.Errorf("moving %s engine tasks is not supported yet; pause/remove and re-add the transfer in the new location", rt.Task.Engine)
	}
	currentRoot := rt.Task.OutputRoot
	if currentRoot == "" {
		currentRoot = filepath.Dir(rt.Task.OutputPath)
	}
	if filepath.Clean(destination) == filepath.Clean(currentRoot) {
		out := rt.Task
		m.mu.Unlock()
		return out, nil
	}
	oldState := rt.Task.State
	if oldState == model.StateDownloading && rt.cancel != nil {
		rt.Task.State = model.StatePaused
		rt.Task.SpeedBytesPerSec = 0
		rt.Task.UpdatedAt = time.Now()
		rt.cancel()
	}
	done := rt.runDone
	m.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			return model.Task{}, errors.New("timed out waiting for active transfer to stop")
		}
	}

	m.mu.Lock()
	rt = m.tasks[id]
	if rt == nil {
		m.mu.Unlock()
		return model.Task{}, os.ErrNotExist
	}
	oldOutput := rt.Task.OutputPath
	name := uniqueNameLocked(destination, rt.Task.Filename, m.tasks)
	newOutput := filepath.Join(destination, name)
	m.mu.Unlock()

	pairs := [][2]string{{oldOutput, newOutput}, {oldOutput + ".part", newOutput + ".part"}, {oldOutput + ".part.json", newOutput + ".part.json"}}
	moved := make([][2]string, 0, 3)
	for _, pair := range pairs {
		if _, err := os.Stat(pair[0]); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return model.Task{}, err
		}
		moveErr := error(nil)
		if pair[0] == oldOutput+".part" {
			moveErr = movePartialFile(pair[0], pair[1], oldOutput+".part.json")
		} else {
			moveErr = moveFile(pair[0], pair[1])
		}
		if moveErr != nil {
			for i := len(moved) - 1; i >= 0; i-- {
				_ = moveFile(moved[i][1], moved[i][0])
			}
			return model.Task{}, fmt.Errorf("move transfer data: %w", moveErr)
		}
		moved = append(moved, pair)
	}

	m.mu.Lock()
	rt = m.tasks[id]
	if rt == nil {
		m.mu.Unlock()
		return model.Task{}, os.ErrNotExist
	}
	rt.Task.Filename = name
	rt.Task.OutputPath = newOutput
	rt.Task.OutputRoot = destination
	rt.Task.UpdatedAt = time.Now()
	if oldState == model.StateDownloading || oldState == model.StateQueued {
		rt.Task.State = model.StateQueued
	}
	_ = m.saveLocked()
	out := rt.Task
	m.mu.Unlock()
	m.signal()
	return out, nil
}

func movePartialFile(src, dst, metaPath string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	b, err := os.ReadFile(metaPath)
	if err != nil {
		return moveFile(src, dst)
	}
	var meta model.ResumeMeta
	if err := json.Unmarshal(b, &meta); err != nil || meta.Size <= 0 {
		return moveFile(src, dst)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	if err := out.Truncate(meta.Size); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	for _, seg := range meta.Segments {
		n := seg.Downloaded
		if max := seg.End - seg.Start + 1; n > max {
			n = max
		}
		if n <= 0 {
			continue
		}
		if _, err := out.Seek(seg.Start, io.SeekStart); err != nil {
			out.Close()
			_ = os.Remove(dst)
			return err
		}
		if _, err := io.CopyN(out, io.NewSectionReader(in, seg.Start, n), n); err != nil {
			out.Close()
			_ = os.Remove(dst)
			return err
		}
	}
	if err := out.Sync(); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	if err := os.Remove(src); err != nil {
		return err
	}
	return nil
}

func moveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		return copyErr
	}
	if syncErr != nil {
		_ = os.Remove(dst)
		return syncErr
	}
	if closeErr != nil {
		_ = os.Remove(dst)
		return closeErr
	}
	if err := os.Remove(src); err != nil {
		return err
	}
	return nil
}

func (m *Manager) Task(id string) (model.Task, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rt := m.tasks[id]
	if rt == nil {
		return model.Task{}, false
	}
	return rt.Task, true
}
