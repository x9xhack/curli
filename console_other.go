//go:build !windows
// +build !windows

package main

func setupWindowsConsole(stdoutFd int) error {
	return nil
}
