package manager

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"

	"boltdm/internal/model"
)

func normalizeChecksum(spec *model.ChecksumSpec) (*model.ChecksumSpec, error) {
	if spec == nil {
		return nil, nil
	}
	algorithm := strings.ToLower(strings.TrimSpace(spec.Algorithm))
	algorithm = strings.ReplaceAll(algorithm, "-", "")
	digest := strings.ToLower(strings.TrimSpace(spec.Digest))
	digest = strings.ReplaceAll(digest, " ", "")
	if digest == "" {
		return nil, fmt.Errorf("checksum digest cannot be empty")
	}
	var expectedLen int
	switch algorithm {
	case "md5":
		expectedLen = md5.Size * 2
	case "sha1":
		expectedLen = sha1.Size * 2
	case "sha256":
		expectedLen = sha256.Size * 2
	case "sha512":
		expectedLen = sha512.Size * 2
	default:
		return nil, fmt.Errorf("unsupported checksum algorithm %q; use md5, sha1, sha256, or sha512", spec.Algorithm)
	}
	if len(digest) != expectedLen {
		return nil, fmt.Errorf("%s checksum must contain %d hexadecimal characters", algorithm, expectedLen)
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return nil, fmt.Errorf("checksum digest is not valid hexadecimal: %w", err)
	}
	return &model.ChecksumSpec{Algorithm: algorithm, Digest: digest}, nil
}

func verifyTaskChecksum(task model.Task) error {
	if task.Checksum == nil {
		return nil
	}
	path := checksumPath(task)
	if path == "" {
		return fmt.Errorf("checksum verification requires exactly one downloaded file")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("checksum file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("checksum verification requires a file, got directory %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open checksum file: %w", err)
	}
	defer f.Close()

	var h hash.Hash
	switch task.Checksum.Algorithm {
	case "md5":
		h = md5.New()
	case "sha1":
		h = sha1.New()
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	default:
		return fmt.Errorf("unsupported checksum algorithm %q", task.Checksum.Algorithm)
	}
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("calculate checksum: %w", err)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, task.Checksum.Digest) {
		return fmt.Errorf("checksum mismatch: expected %s:%s, got %s:%s", task.Checksum.Algorithm, task.Checksum.Digest, task.Checksum.Algorithm, actual)
	}
	return nil
}

func checksumPath(task model.Task) string {
	selected := ""
	count := 0
	for _, f := range task.Files {
		if !f.Selected {
			continue
		}
		count++
		selected = f.Path
	}
	if count == 1 && strings.TrimSpace(selected) != "" {
		return filepath.Clean(selected)
	}
	if count > 1 {
		return ""
	}
	if strings.TrimSpace(task.OutputPath) == "" {
		return ""
	}
	return filepath.Clean(task.OutputPath)
}
