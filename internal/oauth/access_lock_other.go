//go:build !linux

package oauth

func withAccessStoreFileLock(_ string, fn func() error) error {
	return fn()
}
