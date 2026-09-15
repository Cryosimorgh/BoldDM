package updater

import (
	"encoding/json"
	"net/http"
	"time"
)

const DefaultManifestURL = "https://raw.githubusercontent.com/Cryosimorgh/BoltDM/main/update.json"

type Client struct {
	ManifestURL string
	HTTPClient  *http.Client
}

func NewClient(url string) *Client {
	if url == "" {
		url = DefaultManifestURL
	}
	return &Client{
		ManifestURL: url,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) Check() (*Manifest, error) {
	resp, err := c.HTTPClient.Get(c.ManifestURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var manifest Manifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}
