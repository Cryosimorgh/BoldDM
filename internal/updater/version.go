package updater

import "strings"

func IsNewer(current string, remote string) bool {
	return normalize(remote) != normalize(current) && strings.Compare(normalize(remote), normalize(current)) > 0
}

func normalize(v string) string {
	return strings.TrimPrefix(v, "v")
}
