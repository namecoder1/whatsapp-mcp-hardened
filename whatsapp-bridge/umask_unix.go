//go:build unix

package main

import "syscall"

// Make every file the bridge creates (databases, media, token) private to the current user
func restrictFilePermissions() {
	syscall.Umask(0o077)
}
