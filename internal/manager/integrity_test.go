package manager

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"boltdm/internal/model"
)

func TestNormalizeChecksum(t *testing.T) {
	digest := strings.Repeat("AB", sha256.Size)
	spec, err := normalizeChecksum(&model.ChecksumSpec{Algorithm: "SHA-256", Digest: digest})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Algorithm != "sha256" {
		t.Fatalf("algorithm=%q", spec.Algorithm)
	}
	if spec.Digest != strings.ToLower(digest) {
		t.Fatalf("digest was not normalized: %q", spec.Digest)
	}
}

func TestNormalizeChecksumRejectsMalformedDigest(t *testing.T) {
	if _, err := normalizeChecksum(&model.ChecksumSpec{Algorithm: "sha256", Digest: "xyz"}); err == nil {
		t.Fatal("expected malformed digest to fail")
	}
	if _, err := normalizeChecksum(&model.ChecksumSpec{Algorithm: "crc32", Digest: "00000000"}); err == nil {
		t.Fatal("expected unsupported algorithm to fail")
	}
}

func TestVerifyTaskChecksum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payload.bin")
	data := []byte("BoltDM integrity test payload")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(data)
	task := model.Task{
		OutputPath: path,
		Checksum: &model.ChecksumSpec{Algorithm: "sha256", Digest: hex.EncodeToString(h[:])},
	}
	if err := verifyTaskChecksum(task); err != nil {
		t.Fatalf("valid checksum failed: %v", err)
	}
	task.Checksum.Digest = strings.Repeat("0", sha256.Size*2)
	if err := verifyTaskChecksum(task); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestVerifyTaskChecksumRejectsMultiFileTask(t *testing.T) {
	task := model.Task{
		OutputPath: t.TempDir(),
		Files: []model.TaskFile{
			{Path: "one", Selected: true},
			{Path: "two", Selected: true},
		},
		Checksum: &model.ChecksumSpec{Algorithm: "sha256", Digest: strings.Repeat("0", sha256.Size*2)},
	}
	if err := verifyTaskChecksum(task); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected multi-file rejection, got %v", err)
	}
}
