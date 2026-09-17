package transfer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"boltdm/internal/downloader"
	"boltdm/internal/model"
)

type Progress struct {
	Downloaded        int64
	Total             int64
	Speed             int64
	Uploaded          int64
	UploadSpeed       int64
	Segments          int
	Connections       int
	DisplayName       string
	OutputPath        string
	Files             []model.TaskFile
}

type ProgressFunc func(Progress)

type Engine interface {
	Name() model.EngineName
	Download(context.Context, model.Task, ProgressFunc) error
	Remove(context.Context, model.Task) error
}

type NativeEngine struct {
	dl *downloader.Downloader
}

func NewNativeEngine(dl *downloader.Downloader) *NativeEngine { return &NativeEngine{dl: dl} }
func (e *NativeEngine) Name() model.EngineName                 { return model.EngineNative }
func (e *NativeEngine) Downloader() *downloader.Downloader     { return e.dl }
func (e *NativeEngine) Remove(context.Context, model.Task) error { return nil }

func (e *NativeEngine) Download(ctx context.Context, task model.Task, progress ProgressFunc) error {
	return e.dl.Download(ctx, task, func(done, total, speed int64, segments, connections int) {
		if progress != nil {
			progress(Progress{Downloaded: done, Total: total, Speed: speed, Segments: segments, Connections: connections})
		}
	})
}

func Classify(raw string) (model.SourceKind, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "http", "https":
		if isSupportedMediaHost(u.Hostname()) {
			return model.SourceMedia, nil
		}
		ext := strings.ToLower(filepath.Ext(u.Path))
		switch ext {
		case ".torrent":
			return model.SourceTorrent, nil
		case ".meta4", ".metalink":
			return model.SourceMetalink, nil
		default:
			return model.SourceDirect, nil
		}
	case "ftp":
		return model.SourceFTP, nil
	case "sftp":
		return model.SourceSFTP, nil
	case "magnet":
		return model.SourceMagnet, nil
	default:
		return "", fmt.Errorf("unsupported URL scheme %q", scheme)
	}
}

func isSupportedMediaHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	return host == "youtu.be" || host == "youtube.com" || strings.HasSuffix(host, ".youtube.com")
}

func ResolveEngine(requested model.EngineName, kind model.SourceKind) (model.EngineName, error) {
	if requested == "" || requested == model.EngineAuto {
		switch kind {
		case model.SourceDirect:
			return model.EngineNative, nil
		case model.SourceMedia:
			return model.EngineMedia, nil
		default:
			return model.EngineAria2, nil
		}
	}
	switch requested {
	case model.EngineNative:
		if kind != model.SourceDirect {
			return "", fmt.Errorf("native engine does not support %s sources", kind)
		}
		return model.EngineNative, nil
	case model.EngineAria2:
		if kind == model.SourceMedia {
			return "", errors.New("aria2 cannot extract media from a web page; use the media engine")
		}
		return model.EngineAria2, nil
	case model.EngineMedia:
		if kind != model.SourceMedia {
			return "", fmt.Errorf("media engine does not support %s sources", kind)
		}
		return model.EngineMedia, nil
	default:
		return "", fmt.Errorf("unknown transfer engine %q", requested)
	}
}

func DisplayName(raw string, kind model.SourceKind) string {
	u, err := url.Parse(raw)
	if err == nil {
		if kind == model.SourceMedia {
			return "media-download"
		}
		if kind == model.SourceMagnet {
			if dn := strings.TrimSpace(u.Query().Get("dn")); dn != "" {
				return sanitizeDisplayName(dn)
			}
			if xt := strings.TrimSpace(u.Query().Get("xt")); xt != "" {
				parts := strings.Split(xt, ":")
				if len(parts) > 0 {
					h := parts[len(parts)-1]
					if len(h) > 12 {
						h = h[:12]
					}
					return "magnet-" + sanitizeDisplayName(h)
				}
			}
			return "magnet-download"
		}
		if base := strings.TrimSpace(filepath.Base(u.Path)); base != "" && base != "." && base != "/" {
			return sanitizeDisplayName(base)
		}
	}
	return "download"
}

func sanitizeDisplayName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "download"
	}
	return strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:\"/\\|?*`, r) {
			return '_'
		}
		return r
	}, name)
}
