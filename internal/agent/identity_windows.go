//go:build windows

package agent

import (
	"hash/fnv"
	"os"
)

// Windows FileInfo does not expose the Unix device/inode pair. A stable hash
// of the available identity data still detects replacements and truncations;
// the existing size check independently handles copy-truncate rotation.
func identity(info os.FileInfo) (uint64, uint64) {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(info.Name()))
	_, _ = hash.Write([]byte(info.ModTime().UTC().String()))
	return 0, hash.Sum64()
}
