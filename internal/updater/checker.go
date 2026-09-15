package updater

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Check downloads and validates an update manifest.
// It intentionally keeps transport separate from update planning.
func Check(url string) (*Manifest, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest request failed: %s", resp.Status)
	}

	var manifest Manifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}
