package transfer

import (
	"testing"

	"boltdm/internal/model"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		url  string
		want model.SourceKind
	}{
		{"https://example.com/file.iso", model.SourceDirect},
		{"http://example.com/archive.torrent", model.SourceTorrent},
		{"https://example.com/list.meta4?token=x", model.SourceMetalink},
		{"ftp://example.com/file.zip", model.SourceFTP},
		{"sftp://example.com/file.zip", model.SourceSFTP},
		{"magnet:?xt=urn:btih:0123456789abcdef", model.SourceMagnet},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", model.SourceMedia},
		{"https://youtu.be/dQw4w9WgXcQ", model.SourceMedia},
		{"https://music.youtube.com/watch?v=dQw4w9WgXcQ", model.SourceMedia},
	}
	for _, tt := range tests {
		t.Run(string(tt.want)+tt.url, func(t *testing.T) {
			got, err := Classify(tt.url)
			if err != nil {
				t.Fatalf("Classify(%q): %v", tt.url, err)
			}
			if got != tt.want {
				t.Fatalf("Classify(%q)=%q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestClassifyRejectsUnsupportedScheme(t *testing.T) {
	if _, err := Classify("gopher://example.com/file"); err == nil {
		t.Fatal("expected unsupported scheme error")
	}
}

func TestResolveEngine(t *testing.T) {
	tests := []struct {
		requested model.EngineName
		kind      model.SourceKind
		want      model.EngineName
		wantErr   bool
	}{
		{"", model.SourceDirect, model.EngineNative, false},
		{model.EngineAuto, model.SourceMagnet, model.EngineAria2, false},
		{model.EngineAuto, model.SourceMedia, model.EngineMedia, false},
		{model.EngineAria2, model.SourceDirect, model.EngineAria2, false},
		{model.EngineNative, model.SourceFTP, "", true},
		{model.EngineAria2, model.SourceMedia, "", true},
		{model.EngineMedia, model.SourceDirect, "", true},
		{model.EngineMedia, model.SourceMedia, model.EngineMedia, false},
	}
	for _, tt := range tests {
		got, err := ResolveEngine(tt.requested, tt.kind)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("ResolveEngine(%q,%q) expected error", tt.requested, tt.kind)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ResolveEngine(%q,%q): %v", tt.requested, tt.kind, err)
		}
		if got != tt.want {
			t.Fatalf("ResolveEngine(%q,%q)=%q, want %q", tt.requested, tt.kind, got, tt.want)
		}
	}
}

func TestDisplayNameMagnet(t *testing.T) {
	got := DisplayName("magnet:?xt=urn:btih:0123456789abcdef&dn=Example%20Release", model.SourceMagnet)
	if got != "Example Release" {
		t.Fatalf("DisplayName magnet=%q", got)
	}
}

func TestParseMediaProgress(t *testing.T) {
	p, ok := parseMediaProgress("BOLTDM_PROGRESS:1048576|2097152|0|524288")
	if !ok {
		t.Fatal("expected progress line")
	}
	if p.Downloaded != 1048576 || p.Total != 2097152 || p.Speed != 524288 {
		t.Fatalf("unexpected progress: %+v", p)
	}

	p, ok = parseMediaProgress("BOLTDM_PROGRESS:100|NA|250|NA")
	if !ok || p.Downloaded != 100 || p.Total != 250 || p.Speed != 0 {
		t.Fatalf("estimated progress parse failed: %+v, ok=%v", p, ok)
	}
}

func TestMediaArgsChooseSafeFallbackWithoutFFmpeg(t *testing.T) {
	e := &MediaEngine{ytdlp: "yt-dlp"}
	args := e.args(model.Task{URL: "https://youtu.be/test", Filename: "media-download"}, t.TempDir())
	if !containsPair(args, "-f", "b") {
		t.Fatalf("expected single-file fallback format, args=%v", args)
	}
	if containsArg(args, "--ffmpeg-location") {
		t.Fatalf("unexpected ffmpeg argument, args=%v", args)
	}
}

func TestMediaArgsUseMergedBestWithFFmpeg(t *testing.T) {
	e := &MediaEngine{ytdlp: "yt-dlp", ffmpeg: "ffmpeg"}
	args := e.args(model.Task{URL: "https://youtu.be/test", Filename: "media-download"}, t.TempDir())
	if !containsPair(args, "-f", "bv*+ba/b") || !containsArg(args, "--ffmpeg-location") {
		t.Fatalf("expected merged best format, args=%v", args)
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func containsPair(args []string, key, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}
