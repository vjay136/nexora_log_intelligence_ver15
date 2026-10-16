//go:build windows

package agent

// Windows does not support syncing a directory handle through os.File.Sync.
// The preceding atomic rename remains the durable checkpoint boundary.
func syncDirectory(string) error { return nil }
