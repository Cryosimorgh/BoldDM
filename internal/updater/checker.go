package updater

import "net/http"

func Check(url string) (*Manifest, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return nil, nil
}
