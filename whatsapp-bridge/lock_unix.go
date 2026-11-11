//go:build unix

package main

import (
	"fmt"
	"os"
	"syscall"
)

// Kept in a package variable: if the *os.File were garbage collected, its finalizer would close it and drop the lock
var lockHandle *os.File

// Take an exclusive lock on path, held until the process exits. Returns ok=false if another bridge holds it.
func acquireLock(path string) (ok bool, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return false, fmt.Errorf("failed to open %s: %v", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return false, nil
		}
		return false, fmt.Errorf("failed to lock %s: %v", path, err)
	}
	lockHandle = f
	f.Truncate(0)
	fmt.Fprintf(f, "%d\n", os.Getpid())
	return true, nil
}
