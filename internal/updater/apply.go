package updater

import "os"

// ApplyFile writes a verified update file to a temporary path.
// Replacement is handled by the updater helper because the running executable
// cannot replace itself on Windows.
func ApplyFile(path string, data []byte) error {
	return os.WriteFile(path+".new", data, 0644)
}
