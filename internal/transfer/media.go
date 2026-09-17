package transfer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"boltdm/internal/model"
)

const (
	mediaProgressPrefix = "BOLTDM_PROGRESS:"
	mediaTitlePrefix    = "BOLTDM_TITLE:"
	mediaFilePrefix     = "BOLTDM_FILE:"
)

type MediaEngine struct {
	ytdlp   string
	ffmpeg  string
	version string
}

func NewMediaEngine(ytdlpExecutable, ffmpegExecutable string) (*MediaEngine, error) {
	ytdlp, err := discoverExecutable(ytdlpExecutable, ytDLPNames()...)
	if err != nil {
		return nil, fmt.Errorf("yt-dlp not found: %w", err)
	}
	ffmpeg, _ := discoverExecutable(ffmpegExecutable, ffmpegNames()...)
	engine := &MediaEngine{ytdlp: ytdlp, ffmpeg: ffmpeg}
	engine.version = executableVersion(ytdlp)
	return engine, nil
}

func (e *MediaEngine) Name() model.EngineName { return model.EngineMedia }
func (e *MediaEngine) Version() string        { return e.version }
func (e *MediaEngine) YTDLPPath() string      { return e.ytdlp }
func (e *MediaEngine) FFmpegPath() string     { return e.ffmpeg }
func (e *MediaEngine) HasFFmpeg() bool        { return strings.TrimSpace(e.ffmpeg) != "" }
func (e *MediaEngine) Remove(context.Context, model.Task) error { return nil }

func (e *MediaEngine) Download(ctx context.Context, task model.Task, progress ProgressFunc) error {
	if strings.TrimSpace(e.ytdlp) == "" {
		return errors.New("yt-dlp is unavailable")
	}
	root := strings.TrimSpace(task.OutputRoot)
	if root == "" {
		root = filepath.Dir(task.OutputPath)
	}
	if root == "" || root == "." {
		return errors.New("media download output directory is empty")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create media output directory: %w", err)
	}

	args := e.args(task, root)
	cmd := exec.CommandContext(ctx, e.ytdlp, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("yt-dlp stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("yt-dlp stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start yt-dlp: %w", err)
	}

	var mu sync.Mutex
	finalPath := ""
	title := ""
	lastErrors := make([]string, 0, 8)
	handle := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		if strings.HasPrefix(line, mediaProgressPrefix) {
			if p, ok := parseMediaProgress(line); ok && progress != nil {
				progress(p)
			}
			return
		}
		if strings.HasPrefix(line, mediaTitlePrefix) {
			name := strings.TrimSpace(strings.TrimPrefix(line, mediaTitlePrefix))
			if name != "" {
				mu.Lock()
				title = name
				mu.Unlock()
				if progress != nil {
					progress(Progress{DisplayName: name})
				}
			}
			return
		}
		if strings.HasPrefix(line, mediaFilePrefix) {
			path := strings.TrimSpace(strings.TrimPrefix(line, mediaFilePrefix))
			if path != "" {
				mu.Lock()
				finalPath = path
				mu.Unlock()
				if progress != nil {
					progress(Progress{DisplayName: filepath.Base(path), OutputPath: path})
				}
			}
			return
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") || strings.Contains(lower, "unable") || strings.Contains(lower, "failed") {
			mu.Lock()
			if len(lastErrors) == cap(lastErrors) {
				copy(lastErrors, lastErrors[1:])
				lastErrors = lastErrors[:len(lastErrors)-1]
			}
			lastErrors = append(lastErrors, line)
			mu.Unlock()
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go scanMediaOutput(stdout, handle, &wg)
	go scanMediaOutput(stderr, handle, &wg)
	waitErr := cmd.Wait()
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		mu.Lock()
		detail := strings.Join(lastErrors, "; ")
		mu.Unlock()
		if detail != "" {
			return fmt.Errorf("yt-dlp: %s", detail)
		}
		return fmt.Errorf("yt-dlp exited with an error: %w", waitErr)
	}

	mu.Lock()
	path := finalPath
	finalTitle := title
	mu.Unlock()
	if path != "" && progress != nil {
		p := Progress{DisplayName: filepath.Base(path), OutputPath: path}
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
			p.Downloaded = info.Size()
			p.Total = info.Size()
			p.Files = []model.TaskFile{{Index: 0, Path: path, Size: info.Size(), Downloaded: info.Size(), Selected: true}}
		}
		progress(p)
	} else if finalTitle != "" && progress != nil {
		progress(Progress{DisplayName: finalTitle})
	}
	return nil
}

func (e *MediaEngine) args(task model.Task, root string) []string {
	template := "%(title).180B [%(id)s].%(ext)s"
	if name := strings.TrimSpace(task.Filename); name != "" && name != "media-download" {
		base := strings.TrimSuffix(name, filepath.Ext(name))
		if base != "" {
			template = base + ".%(ext)s"
		}
	}
	args := []string{
		"--no-playlist",
		"--newline",
		"--no-color",
		"--continue",
		"--progress",
		"--progress-template", "download:" + mediaProgressPrefix + "%(progress.downloaded_bytes)s|%(progress.total_bytes)s|%(progress.total_bytes_estimate)s|%(progress.speed)s",
		"--print", "before_dl:" + mediaTitlePrefix + "%(title)s",
		"--print", "after_move:" + mediaFilePrefix + "%(filepath)s",
		"-o", filepath.Join(root, template),
	}
	if e.HasFFmpeg() {
		args = append(args, "--ffmpeg-location", e.ffmpeg, "-f", "bv*+ba/b")
	} else {
		args = append(args, "-f", "b")
	}
	for key, value := range task.Headers {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" || strings.ContainsAny(key+value, "\r\n") {
			continue
		}
		args = append(args, "--add-header", key+":"+value)
	}
	args = append(args, task.URL)
	return args
}

func parseMediaProgress(line string) (Progress, bool) {
	if !strings.HasPrefix(strings.TrimSpace(line), mediaProgressPrefix) {
		return Progress{}, false
	}
	payload := strings.TrimPrefix(strings.TrimSpace(line), mediaProgressPrefix)
	parts := strings.Split(payload, "|")
	if len(parts) < 4 {
		return Progress{}, false
	}
	done := parseMediaNumber(parts[0])
	total := parseMediaNumber(parts[1])
	if total <= 0 {
		total = parseMediaNumber(parts[2])
	}
	speed := parseMediaNumber(parts[3])
	return Progress{Downloaded: done, Total: total, Speed: speed}, true
}

func parseMediaNumber(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "NA") || strings.EqualFold(value, "none") {
		return 0
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil || f < 0 {
		return 0
	}
	return int64(f)
}

func scanMediaOutput(r io.Reader, handle func(string), wg *sync.WaitGroup) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		handle(scanner.Text())
	}
}

func discoverExecutable(configured string, names ...string) (string, error) {
	if value := strings.TrimSpace(configured); value != "" {
		path, err := filepath.Abs(value)
		if err != nil {
			path = value
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
		return "", fmt.Errorf("configured executable %q does not exist", value)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, name := range names {
			candidate := filepath.Join(dir, name)
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", errors.New("not found next to BoltDM or on PATH")
}

func executableVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "unknown"
	}
	version := strings.TrimSpace(string(out))
	if i := strings.IndexByte(version, '\n'); i >= 0 {
		version = strings.TrimSpace(version[:i])
	}
	if version == "" {
		return "unknown"
	}
	return version
}

func ytDLPNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"yt-dlp.exe", "yt-dlp"}
	}
	return []string{"yt-dlp", "yt-dlp_linux", "yt-dlp_linux_aarch64"}
}

func ffmpegNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"ffmpeg.exe", "ffmpeg"}
	}
	return []string{"ffmpeg"}
}
