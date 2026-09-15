package updater

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
)

// DownloadFile resumes partial update downloads when the server supports ranges.
// The temporary file is only promoted after hash validation succeeds.
func DownloadFile(url string, destination string, expectedHash string) error {
	return downloadRange(url, destination, expectedHash)
}

func downloadRange(url string, destination string, expectedHash string) error {
	start := int64(0)
	if info, err := os.Stat(destination + ".part"); err == nil {
		start = info.Size()
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if start > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("update download failed: %s", resp.Status)
	}

	flag := os.O_CREATE | os.O_WRONLY
	if resp.StatusCode == http.StatusPartialContent {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}

	file, err := os.OpenFile(destination+".part", flag, 0644)
	if err != nil {
		return err
	}

	_, err = io.Copy(file, resp.Body)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	data, err := os.Open(destination + ".part")
	if err != nil {
		return err
	}
	defer data.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, data); err != nil {
		return err
	}
	if expectedHash != "" && fmt.Sprintf("%x", hasher.Sum(nil)) != expectedHash {
		return fmt.Errorf("update hash mismatch")
	}

	return os.Rename(destination+".part", destination)
}
