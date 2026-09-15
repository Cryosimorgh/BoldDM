package updater

// Manifest describes files available in an application update.
type Manifest struct {
	Version string `json:"version"`
	Channel string `json:"channel"`
	Files []File `json:"files"`
	Notes string `json:"notes"`
}

type File struct {
	Path string `json:"path"`
	SHA256 string `json:"sha256"`
	Size int64 `json:"size"`
}
