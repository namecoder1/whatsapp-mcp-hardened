//go:build !unix

package main

// File locking is not implemented on this platform: avoid running two bridges at once
func acquireLock(path string) (ok bool, err error) {
	return true, nil
}
