//go:build !unix

package main

// File modes are not enforced via umask on this platform
func restrictFilePermissions() {}
