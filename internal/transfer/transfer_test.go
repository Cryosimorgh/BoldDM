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
	}
	for _, tt := range tests {
		t.Run(string(tt.want), func(t *testing.T) {
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
		{model.EngineAria2, model.SourceDirect, model.EngineAria2, false},
		{model.EngineNative, model.SourceFTP, "", true},
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
