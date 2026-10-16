//go:build !windows

package agent

import (
	"os"
	"syscall"
)

func identity(info os.FileInfo) (uint64, uint64) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, uint64(info.ModTime().UnixNano())
	}
	return uint64(stat.Dev), stat.Ino
}
