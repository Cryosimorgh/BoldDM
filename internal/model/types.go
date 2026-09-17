package model

import "time"

type State string

const (
	StateQueued      State = "queued"
	StateDownloading State = "downloading"
	StatePaused      State = "paused"
	StateCompleted   State = "completed"
	StateFailed      State = "failed"
	StateCanceled    State = "canceled"
)

type EngineName string

const (
	EngineAuto   EngineName = "auto"
	EngineNative EngineName = "native"
	EngineAria2  EngineName = "aria2"
)

type SourceKind string

const (
	SourceDirect   SourceKind = "direct"
	SourceFTP      SourceKind = "ftp"
	SourceSFTP     SourceKind = "sftp"
	SourceMagnet   SourceKind = "magnet"
	SourceTorrent  SourceKind = "torrent"
	SourceMetalink SourceKind = "metalink"
)

type ChecksumSpec struct {
	Algorithm string `json:"algorithm"`
	Digest    string `json:"digest"`
}

type Request struct {
	URL         string            `json:"url"`
	Filename    string            `json:"filename,omitempty"`
	DownloadDir string            `json:"downloadDir,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Segments    int               `json:"segments,omitempty"`
	Connections int               `json:"connections,omitempty"`
	Engine      EngineName        `json:"engine,omitempty"`
	StartAt     *time.Time        `json:"startAt,omitempty"`
	Checksum    *ChecksumSpec     `json:"checksum,omitempty"`
}

type BatchRequest struct {
	Downloads []Request `json:"downloads"`
}

type TaskFile struct {
	Index      int    `json:"index"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Downloaded int64  `json:"downloaded"`
	Selected   bool   `json:"selected"`
}

type Task struct {
	ID                     string            `json:"id"`
	URL                    string            `json:"url"`
	Filename               string            `json:"filename"`
	OutputPath             string            `json:"outputPath"`
	OutputRoot             string            `json:"outputRoot,omitempty"`
	Headers                map[string]string `json:"headers,omitempty"`
	SourceKind             SourceKind        `json:"sourceKind,omitempty"`
	Engine                 EngineName        `json:"engine,omitempty"`
	State                  State             `json:"state"`
	Size                   int64             `json:"size"`
	Downloaded             int64             `json:"downloaded"`
	Uploaded               int64             `json:"uploaded,omitempty"`
	SpeedBytesPerSec       int64             `json:"speedBytesPerSec"`
	UploadSpeedBytesPerSec int64             `json:"uploadSpeedBytesPerSec,omitempty"`
	Progress               float64           `json:"progress"`
	Segments               int               `json:"segments"`
	Connections            int               `json:"connections"`
	SegmentSetting         int               `json:"segmentSetting,omitempty"`
	ConnectionSetting      int               `json:"connectionSetting,omitempty"`
	Files                  []TaskFile        `json:"files,omitempty"`
	StartAt                *time.Time         `json:"startAt,omitempty"`
	Checksum               *ChecksumSpec      `json:"checksum,omitempty"`
	ChecksumVerified       bool               `json:"checksumVerified,omitempty"`
	Error                  string             `json:"error,omitempty"`
	CreatedAt              time.Time          `json:"createdAt"`
	UpdatedAt              time.Time          `json:"updatedAt"`
}

type SegmentProgress struct {
	Start      int64 `json:"start"`
	End        int64 `json:"end"`
	Downloaded int64 `json:"downloaded"`
	Done       bool  `json:"done"`
}

type ResumeMeta struct {
	URL          string            `json:"url"`
	Headers      map[string]string `json:"headers,omitempty"`
	Size         int64             `json:"size"`
	ETag         string            `json:"etag,omitempty"`
	LastModified string            `json:"lastModified,omitempty"`
	Segments     []SegmentProgress `json:"segments"`
}
