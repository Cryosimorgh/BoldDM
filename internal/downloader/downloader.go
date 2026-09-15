package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"boltdm/internal/model"
)

type Options struct {
	Segments       int
	Connections    int
	MinSegmentSize int64
	Retries        int
	Timeout        time.Duration
}

type ProgressFunc func(downloaded, total, speed int64, segments, connections int)

type Downloader struct {
	client      *http.Client
	opts        Options
	segments    atomic.Int64
	connections atomic.Int64
	minSegSize  atomic.Int64
	retries     atomic.Int64
	limiter     *BandwidthLimiter
}

type remoteInfo struct {
	size         int64
	acceptRanges bool
	etag         string
	modified     string
	filename     string
}

func New(opts Options) *Downloader {
	if opts.Segments <= 0 {
		opts.Segments = 8
	}
	if opts.Connections <= 0 {
		opts.Connections = 4
	}
	if opts.MinSegmentSize <= 0 {
		opts.MinSegmentSize = 8 << 20
	}
	if opts.Retries <= 0 {
		opts.Retries = 5
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 45 * time.Second
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          128,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: opts.Timeout,
		ForceAttemptHTTP2:     true,
	}
	d := &Downloader{client: &http.Client{Transport: transport}, opts: opts, limiter: NewBandwidthLimiter(0)}
	d.segments.Store(int64(opts.Segments))
	d.connections.Store(int64(opts.Connections))
	d.minSegSize.Store(opts.MinSegmentSize)
	d.retries.Store(int64(opts.Retries))
	return d
}

func (d *Downloader) SetSegments(segments int) {
	if segments < 1 {
		segments = 1
	}
	if segments > 64 {
		segments = 64
	}
	d.segments.Store(int64(segments))
}

func (d *Downloader) SetConnections(connections int) {
	if connections < 1 {
		connections = 1
	}
	if connections > 32 {
		connections = 32
	}
	d.connections.Store(int64(connections))
}

func (d *Downloader) SetMinSegmentSize(bytes int64) {
	if bytes < 256<<10 {
		bytes = 256 << 10
	}
	d.minSegSize.Store(bytes)
}

func (d *Downloader) SetRetries(retries int) {
	if retries < 1 {
		retries = 1
	}
	if retries > 20 {
		retries = 20
	}
	d.retries.Store(int64(retries))
}

func (d *Downloader) SetSpeedLimit(bytesPerSecond int64) {
	d.limiter.SetLimit(bytesPerSecond)
}

func (d *Downloader) SpeedLimit() int64 { return d.limiter.Limit() }

func (d *Downloader) Download(ctx context.Context, task model.Task, progress ProgressFunc) error {
	info, err := d.probe(ctx, task.URL, task.Headers)
	if err != nil {
		return err
	}
	if info.size <= 0 || !info.acceptRanges {
		return d.downloadStreaming(ctx, task, progress)
	}

	partPath := task.OutputPath + ".part"
	metaPath := partPath + ".json"
	segments := task.SegmentSetting
	if segments <= 0 {
		segments = int(d.segments.Load())
	}
	if segments < 1 {
		segments = 1
	}
	if !info.acceptRanges || info.size < d.minSegSize.Load() {
		segments = 1
	}
	if int64(segments) > info.size {
		segments = 1
	}

	meta := buildMeta(task, info, segments)
	if old, err := loadMeta(metaPath); err == nil && compatible(old, meta) {
		if len(old.Segments) == segments {
			meta = old
		} else {
			meta = repartitionMeta(old, segments)
		}
	} else {
		_ = os.Remove(partPath)
		_ = os.Remove(metaPath)
	}

	if err := os.MkdirAll(filepath.Dir(task.OutputPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Truncate(info.size); err != nil {
		return err
	}

	var totalDownloaded atomic.Int64
	for _, s := range meta.Segments {
		totalDownloaded.Add(s.Downloaded)
	}
	var metaMu sync.Mutex
	var firstErr error
	var errMu sync.Mutex
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stopSaver := make(chan struct{})
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				metaMu.Lock()
				_ = saveMeta(metaPath, meta)
				metaMu.Unlock()
			case <-stopSaver:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	connections := activeConnections(task, d, len(meta.Segments))
	startTime := time.Now()
	startBytes := totalDownloaded.Load()
	stopProgress := make(chan struct{})
	go func() {
		t := time.NewTicker(300 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				cur := totalDownloaded.Load()
				elapsed := time.Since(startTime).Seconds()
				var speed int64
				if elapsed > 0 {
					speed = int64(float64(cur-startBytes) / elapsed)
				}
				if progress != nil {
					progress(cur, info.size, speed, len(meta.Segments), connections)
				}
			case <-stopProgress:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < connections; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if err := d.downloadSegment(ctx, f, task, &meta, &metaMu, idx, &totalDownloaded); err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					errMu.Unlock()
					return
				}
			}
		}()
	}
sendJobs:
	for i := range meta.Segments {
		if meta.Segments[i].Done {
			continue
		}
		select {
		case jobs <- i:
		case <-ctx.Done():
			break sendJobs
		}
	}
	close(jobs)
	wg.Wait()
	close(stopSaver)
	close(stopProgress)
	metaMu.Lock()
	_ = saveMeta(metaPath, meta)
	metaMu.Unlock()
	if firstErr != nil {
		return firstErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if progress != nil {
		progress(info.size, info.size, 0, len(meta.Segments), connections)
	}
	if err := finalize(partPath, task.OutputPath); err != nil {
		return err
	}
	_ = os.Remove(metaPath)
	return nil
}

func (d *Downloader) downloadSegment(ctx context.Context, f *os.File, task model.Task, meta *model.ResumeMeta, metaMu *sync.Mutex, idx int, total *atomic.Int64) error {
	for attempt := 0; attempt < int(d.retries.Load()); attempt++ {
		metaMu.Lock()
		s := meta.Segments[idx]
		metaMu.Unlock()
		if s.Done {
			return nil
		}
		start := s.Start + s.Downloaded
		if start > s.End {
			metaMu.Lock()
			meta.Segments[idx].Done = true
			metaMu.Unlock()
			return nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, task.URL, nil)
		if err != nil {
			return err
		}
		applyHeaders(req, task.Headers)
		if len(meta.Segments) > 1 || start > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, s.End))
		}
		resp, err := d.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			time.Sleep(backoff(attempt))
			continue
		}
		if (len(meta.Segments) > 1 || start > 0) && resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			return fmt.Errorf("server stopped honoring byte ranges: HTTP %d", resp.StatusCode)
		}
		if len(meta.Segments) == 1 && start == 0 && resp.StatusCode/100 != 2 {
			resp.Body.Close()
			return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
		}
		buf := make([]byte, 64*1024)
		offset := start
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				if err := d.limiter.Wait(ctx, n); err != nil {
					resp.Body.Close()
					return err
				}
				if _, err := f.WriteAt(buf[:n], offset); err != nil {
					resp.Body.Close()
					return err
				}
				offset += int64(n)
				total.Add(int64(n))
				metaMu.Lock()
				meta.Segments[idx].Downloaded += int64(n)
				metaMu.Unlock()
			}
			if readErr != nil {
				resp.Body.Close()
				if errors.Is(readErr, io.EOF) {
					metaMu.Lock()
					seg := &meta.Segments[idx]
					seg.Done = seg.Start+seg.Downloaded > seg.End
					complete := seg.Done
					metaMu.Unlock()
					if complete {
						return nil
					}
					break
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				break
			}
		}
		time.Sleep(backoff(attempt))
	}
	return fmt.Errorf("segment %d failed after %d retries", idx+1, d.retries.Load())
}

func (d *Downloader) downloadStreaming(ctx context.Context, task model.Task, progress ProgressFunc) error {
	if err := os.MkdirAll(filepath.Dir(task.OutputPath), 0o755); err != nil {
		return err
	}
	partPath := task.OutputPath + ".part"
	var existing int64
	if st, err := os.Stat(partPath); err == nil {
		existing = st.Size()
	}
	for attempt := 0; attempt < int(d.retries.Load()); attempt++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, task.URL, nil)
		applyHeaders(req, task.Headers)
		if existing > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existing))
		}
		resp, err := d.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			time.Sleep(backoff(attempt))
			continue
		}
		if existing > 0 && resp.StatusCode != http.StatusPartialContent {
			existing = 0
		}
		flags := os.O_CREATE | os.O_WRONLY
		if existing > 0 {
			flags |= os.O_APPEND
		} else {
			flags |= os.O_TRUNC
		}
		f, err := os.OpenFile(partPath, flags, 0o644)
		if err != nil {
			resp.Body.Close()
			return err
		}
		buf := make([]byte, 64*1024)
		start := time.Now()
		base := existing
		for {
			n, er := resp.Body.Read(buf)
			if n > 0 {
				if e := d.limiter.Wait(ctx, n); e != nil {
					f.Close()
					resp.Body.Close()
					return e
				}
				if _, e := f.Write(buf[:n]); e != nil {
					f.Close()
					resp.Body.Close()
					return e
				}
				existing += int64(n)
				if progress != nil {
					speed := int64(float64(existing-base) / max(time.Since(start).Seconds(), 0.001))
					progress(existing, 0, speed, 1, 1)
				}
			}
			if er != nil {
				f.Close()
				resp.Body.Close()
				if errors.Is(er, io.EOF) {
					return finalize(partPath, task.OutputPath)
				}
				break
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(backoff(attempt))
	}
	return fmt.Errorf("streaming download failed after %d retries", d.retries.Load())
}

func (d *Downloader) probe(ctx context.Context, rawURL string, headers map[string]string) (remoteInfo, error) {
	var info remoteInfo
	req, _ := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	applyHeaders(req, headers)
	resp, err := d.client.Do(req)
	if err == nil && resp.StatusCode/100 == 2 {
		info = infoFromResponse(resp)
		resp.Body.Close()
		if info.size > 0 {
			return info, nil
		}
	} else if resp != nil {
		resp.Body.Close()
	}

	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	applyHeaders(req, headers)
	req.Header.Set("Range", "bytes=0-0")
	resp, err = d.client.Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode/100 != 2 {
		return info, fmt.Errorf("probe failed: HTTP %d", resp.StatusCode)
	}
	info = infoFromResponse(resp)
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if slash := strings.LastIndex(cr, "/"); slash >= 0 {
			if n, e := strconv.ParseInt(cr[slash+1:], 10, 64); e == nil {
				info.size = n
				info.acceptRanges = true
			}
		}
	}
	return info, nil
}

func infoFromResponse(resp *http.Response) remoteInfo {
	info := remoteInfo{size: resp.ContentLength, etag: resp.Header.Get("ETag"), modified: resp.Header.Get("Last-Modified")}
	info.acceptRanges = strings.Contains(strings.ToLower(resp.Header.Get("Accept-Ranges")), "bytes") || resp.StatusCode == http.StatusPartialContent
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, p, e := mime.ParseMediaType(cd); e == nil {
			info.filename = p["filename"]
		}
	}
	return info
}

func buildMeta(task model.Task, info remoteInfo, count int) model.ResumeMeta {
	m := model.ResumeMeta{URL: task.URL, Headers: task.Headers, Size: info.size, ETag: info.etag, LastModified: info.modified}
	chunk := info.size / int64(count)
	for i := 0; i < count; i++ {
		start := int64(i) * chunk
		end := start + chunk - 1
		if i == count-1 {
			end = info.size - 1
		}
		m.Segments = append(m.Segments, model.SegmentProgress{Start: start, End: end})
	}
	return m
}

func repartitionMeta(old model.ResumeMeta, count int) model.ResumeMeta {
	if count < 1 {
		count = 1
	}
	fresh := old
	fresh.Segments = nil
	chunk := old.Size / int64(count)
	if chunk < 1 {
		count = 1
		chunk = old.Size
	}
	for i := 0; i < count; i++ {
		start := int64(i) * chunk
		end := start + chunk - 1
		if i == count-1 {
			end = old.Size - 1
		}
		fresh.Segments = append(fresh.Segments, model.SegmentProgress{Start: start, End: end})
	}
	type interval struct{ start, end int64 }
	var merged []interval
	for _, seg := range old.Segments {
		if seg.Downloaded <= 0 {
			continue
		}
		end := seg.Start + seg.Downloaded - 1
		if end > seg.End {
			end = seg.End
		}
		if end < seg.Start {
			continue
		}
		if len(merged) > 0 && seg.Start <= merged[len(merged)-1].end+1 {
			if end > merged[len(merged)-1].end {
				merged[len(merged)-1].end = end
			}
		} else {
			merged = append(merged, interval{seg.Start, end})
		}
	}
	for i := range fresh.Segments {
		ns := &fresh.Segments[i]
		for _, iv := range merged {
			if iv.start > ns.Start {
				break
			}
			if iv.start <= ns.Start && iv.end >= ns.Start {
				end := iv.end
				if end > ns.End {
					end = ns.End
				}
				ns.Downloaded = end - ns.Start + 1
				ns.Done = ns.Start+ns.Downloaded > ns.End
				break
			}
		}
	}
	return fresh
}

func compatible(a, b model.ResumeMeta) bool {
	if a.URL != b.URL || a.Size != b.Size {
		return false
	}
	if a.ETag != "" && b.ETag != "" && a.ETag != b.ETag {
		return false
	}
	if a.LastModified != "" && b.LastModified != "" && a.LastModified != b.LastModified {
		return false
	}
	return true
}

func activeConnections(task model.Task, d *Downloader, segments int) int {
	c := task.ConnectionSetting
	if c <= 0 {
		c = int(d.connections.Load())
	}
	if c < 1 {
		c = 1
	}
	if c > 32 {
		c = 32
	}
	if segments > 0 && c > segments {
		c = segments
	}
	return c
}

func loadMeta(path string) (model.ResumeMeta, error) {
	var m model.ResumeMeta
	b, e := os.ReadFile(path)
	if e != nil {
		return m, e
	}
	e = json.Unmarshal(b, &m)
	return m, e
}
func saveMeta(path string, m model.ResumeMeta) error {
	b, e := json.MarshalIndent(m, "", "  ")
	if e != nil {
		return e
	}
	tmp := path + ".tmp"
	if e = os.WriteFile(tmp, b, 0o644); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}
func finalize(part, dst string) error { _ = os.Remove(dst); return os.Rename(part, dst) }
func applyHeaders(req *http.Request, headers map[string]string) {
	for k, v := range headers {
		if strings.TrimSpace(v) != "" {
			req.Header.Set(k, v)
		}
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "*/*")
	}
}
func backoff(attempt int) time.Duration {
	d := time.Duration(1<<min(attempt, 5)) * 350 * time.Millisecond
	return d
}

func FilenameFromURL(raw string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return "download.bin"
	}
	name := filepath.Base(strings.TrimSuffix(u.Path, "/"))
	if name == "." || name == "/" || name == "" {
		return "download.bin"
	}
	if dec, e := url.PathUnescape(name); e == nil {
		name = dec
	}
	return sanitize(name)
}
func sanitize(name string) string {
	bad := `<>:"/\\|?*`
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(bad, r) || r < 32 {
			return '_'
		}
		return r
	}, name)
}
