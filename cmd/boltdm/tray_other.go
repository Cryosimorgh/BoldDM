//go:build !windows

package main

func startTray(iconPath string, onOpen, onPause, onResume, onExit func()) func() { return func() {} }
