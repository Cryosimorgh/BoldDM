package updater

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
)

func DownloadFile(url string, destination string, expectedHash string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update download failed: %s", resp.Status)
	}

	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer file.Close()

	hasher := sha256.New()
	writer := io.MultiWriter(file, hasher)
	if _, err := io.Copy(writer, resp.Body); err != nil {
		return err
	}

	if expectedHash != "" && fmt.Sprintf("%x", hasher.Sum(nil)) != expectedHash {
		return fmt.Errorf("update hash mismatch")
	}

	return nil
}
