package transfer

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"boltdm/internal/aria2rpc"
	"boltdm/internal/model"
)

type Aria2Engine struct {
	client *aria2rpc.Client
}

func NewAria2Engine(client *aria2rpc.Client) *Aria2Engine { return &Aria2Engine{client: client} }
func (e *Aria2Engine) Name() model.EngineName             { return model.EngineAria2 }

func (e *Aria2Engine) Download(ctx context.Context, task model.Task, progress ProgressFunc) error {
	if e == nil || e.client == nil {
		return fmt.Errorf("aria2 engine is unavailable")
	}
	if err := e.ensureRoot(ctx, task); err != nil {
		return err
	}

	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	for {
		leaves, err := e.leafStatuses(ctx, task.ID)
		if err != nil {
			return err
		}
		if len(leaves) == 0 {
			return fmt.Errorf("aria2 returned no transfer for %s", task.ID)
		}

		allComplete := true
		var total, downloaded, speed, uploaded, uploadSpeed int64
		connections := 0
		files := make([]model.TaskFile, 0)
		displayName := ""
		outputPath := ""
		for _, st := range leaves {
			if st.Status == "paused" {
				if err := e.client.Unpause(ctx, st.GID); err != nil && !aria2rpc.IsNotFound(err) {
					return err
				}
				allComplete = false
			} else if st.Status != "complete" {
				allComplete = false
			}
			if st.Status == "error" {
				msg := strings.TrimSpace(st.ErrorMessage)
				if msg == "" {
					msg = "unknown aria2 error"
				}
				return fmt.Errorf("aria2 transfer failed (%s): %s", st.ErrorCode, msg)
			}
			if st.Status == "removed" {
				return fmt.Errorf("aria2 transfer was removed")
			}
			total += parseInt64(st.TotalLength)
			downloaded += parseInt64(st.CompletedLength)
			speed += parseInt64(st.DownloadSpeed)
			uploaded += parseInt64(st.UploadLength)
			uploadSpeed += parseInt64(st.UploadSpeed)
			connections += int(parseInt64(st.Connections))
			if displayName == "" && strings.TrimSpace(st.BitTorrent.Info.Name) != "" {
				displayName = st.BitTorrent.Info.Name
			}
			for _, f := range st.Files {
				idx, _ := strconv.Atoi(f.Index)
				files = append(files, model.TaskFile{
					Index: idx, Path: f.Path, Size: parseInt64(f.Length), Downloaded: parseInt64(f.CompletedLength), Selected: f.Selected != "false",
				})
			}
		}

		if len(files) == 1 {
			outputPath = files[0].Path
			if displayName == "" {
				displayName = filepath.Base(files[0].Path)
			}
		} else if displayName != "" && task.OutputRoot != "" {
			outputPath = filepath.Join(task.OutputRoot, displayName)
		} else if task.OutputRoot != "" {
			outputPath = task.OutputRoot
		}
		if progress != nil {
			progress(Progress{
				Downloaded: downloaded, Total: total, Speed: speed, Uploaded: uploaded, UploadSpeed: uploadSpeed,
				Segments: task.SegmentSetting, Connections: connections, DisplayName: displayName, OutputPath: outputPath, Files: files,
			})
		}
		if allComplete {
			return nil
		}

		select {
		case <-ctx.Done():
			pauseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = e.pauseTree(pauseCtx, task.ID)
			cancel()
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (e *Aria2Engine) Remove(ctx context.Context, task model.Task) error {
	if e == nil || e.client == nil {
		return nil
	}
	statuses, err := e.treeStatuses(ctx, task.ID)
	if err != nil && !aria2rpc.IsNotFound(err) {
		return err
	}
	for i := len(statuses) - 1; i >= 0; i-- {
		if err := e.client.Remove(ctx, statuses[i].GID); err != nil && !aria2rpc.IsNotFound(err) {
			return err
		}
	}
	if len(statuses) == 0 {
		return e.client.Remove(ctx, task.ID)
	}
	return nil
}

func (e *Aria2Engine) ensureRoot(ctx context.Context, task model.Task) error {
	st, err := e.client.TellStatus(ctx, task.ID)
	if err == nil {
		if st.Status == "paused" {
			return e.client.Unpause(ctx, task.ID)
		}
		return nil
	}
	if !aria2rpc.IsNotFound(err) {
		return err
	}

	dir := task.OutputRoot
	if dir == "" && task.OutputPath != "" {
		dir = filepath.Dir(task.OutputPath)
	}
	options := map[string]any{
		"gid":                 task.ID,
		"dir":                 dir,
		"continue":            "true",
		"allow-overwrite":     "false",
		"auto-file-renaming":  "false",
		"check-integrity":      "true",
		"follow-torrent":       "true",
		"follow-metalink":      "true",
	}
	if task.SourceKind == model.SourceDirect || task.SourceKind == model.SourceFTP || task.SourceKind == model.SourceSFTP {
		if strings.TrimSpace(task.Filename) != "" {
			options["out"] = task.Filename
		}
	}
	segments := task.SegmentSetting
	if segments <= 0 {
		segments = 8
	}
	connections := task.ConnectionSetting
	if connections <= 0 {
		connections = 4
	}
	options["split"] = strconv.Itoa(segments)
	options["max-connection-per-server"] = strconv.Itoa(connections)
	if task.SourceKind == model.SourceMagnet || task.SourceKind == model.SourceTorrent {
		// BoltDM does not expose seeding policy yet. Do not leave an invisible background seed running.
		options["seed-time"] = "0"
	}
	if len(task.Headers) > 0 {
		keys := make([]string, 0, len(task.Headers))
		for k := range task.Headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		headers := make([]string, 0, len(keys))
		for _, k := range keys {
			headers = append(headers, k+": "+task.Headers[k])
		}
		options["header"] = headers
	}
	gid, err := e.client.AddURI(ctx, []string{task.URL}, options)
	if err != nil {
		return err
	}
	if !strings.EqualFold(gid, task.ID) {
		return fmt.Errorf("aria2 returned unexpected GID %s for task %s", gid, task.ID)
	}
	return nil
}

func (e *Aria2Engine) pauseTree(ctx context.Context, root string) error {
	statuses, err := e.treeStatuses(ctx, root)
	if err != nil && !aria2rpc.IsNotFound(err) {
		return err
	}
	for i := len(statuses) - 1; i >= 0; i-- {
		if statuses[i].Status == "active" || statuses[i].Status == "waiting" {
			if err := e.client.Pause(ctx, statuses[i].GID); err != nil && !aria2rpc.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

func (e *Aria2Engine) leafStatuses(ctx context.Context, root string) ([]aria2rpc.Status, error) {
	all, err := e.treeStatuses(ctx, root)
	if err != nil {
		return nil, err
	}
	parents := make(map[string]bool)
	for _, st := range all {
		if len(st.FollowedBy) > 0 {
			parents[st.GID] = true
		}
	}
	out := make([]aria2rpc.Status, 0, len(all))
	for _, st := range all {
		if !parents[st.GID] {
			out = append(out, st)
		}
	}
	return out, nil
}

func (e *Aria2Engine) treeStatuses(ctx context.Context, root string) ([]aria2rpc.Status, error) {
	queue := []string{root}
	seen := map[string]bool{}
	out := make([]aria2rpc.Status, 0, 4)
	for len(queue) > 0 {
		gid := queue[0]
		queue = queue[1:]
		if gid == "" || seen[gid] {
			continue
		}
		seen[gid] = true
		st, err := e.client.TellStatus(ctx, gid)
		if err != nil {
			if gid != root && aria2rpc.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		out = append(out, st)
		queue = append(queue, st.FollowedBy...)
		if len(seen) > 128 {
			return nil, fmt.Errorf("aria2 metadata chain exceeded safety limit")
		}
	}
	return out, nil
}

func parseInt64(s string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return v
}
