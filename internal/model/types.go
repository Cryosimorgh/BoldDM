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

type Request struct {
	URL         string            `json:"url"`
	Filename    string            `json:"filename,omitempty"`
	DownloadDir string            `json:"downloadDir,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Segments    int               `json:"segments,omitempty"`
	Connections int               `json:"connections,omitempty"`
}

type BatchRequest struct {
	Downloads []Request `json:"downloads"`
}

type Task struct {
	ID                string            `json:"id"`
	URL               string            `json:"url"`
	Filename          string            `json:"filename"`
	OutputPath        string            `json:"outputPath"`
	Headers           map[string]string `json:"headers,omitempty"`
	State             State             `json:"state"`
	Size              int64             `json:"size"`
	Downloaded        int64             `json:"downloaded"`
	SpeedBytesPerSec  int64             `json:"speedBytesPerSec"`
	Progress          float64           `json:"progress"`
	Segments          int               `json:"segments"`
	Connections       int               `json:"connections"`
	SegmentSetting    int               `json:"segmentSetting,omitempty"`
	ConnectionSetting int               `json:"connectionSetting,omitempty"`
	Error             string            `json:"error,omitempty"`
	CreatedAt         time.Time         `json:"createdAt"`
	UpdatedAt         time.Time         `json:"updatedAt"`
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
