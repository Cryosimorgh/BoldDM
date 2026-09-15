package updater

import (
	"crypto/sha256"
	"fmt"
	"os"
)

type ChangeType int

const (
	Unchanged ChangeType = iota
	Added
	Modified
)

type FileChange struct {
	File File
	Type ChangeType
}

func Plan(manifest Manifest, root string) ([]FileChange, error) {
	changes := make([]FileChange, 0)
	for _, file := range manifest.Files {
		path := root + string(os.PathSeparator) + file.Path
		data, err := os.Open(path)
		if err != nil {
			changes = append(changes, FileChange{File: file, Type: Added})
			continue
		}

		h := sha256.New()
		_, copyErr := data.WriteTo(h)
		closeErr := data.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}

		if fmt.Sprintf("%x", h.Sum(nil)) != file.SHA256 {
			changes = append(changes, FileChange{File: file, Type: Modified})
		}
	}
	return changes, nil
}
