//go:build unix

package main

import (
	"os"
	"syscall"
	"testing"
)

// flock locks are per open file description, so a fresh open behaves like another process
func acquireLockFromOtherProcess(t *testing.T, path string) (bool, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return false, nil
	}
	return true, nil
}
