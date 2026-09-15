package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func main() {
	target := flag.String("target", "BoltDM.exe", "application to replace")
	source := flag.String("source", "BoltDM.exe.new", "new application")
	restart := flag.String("restart", "BoltDM.exe", "application to restart")
	flag.Parse()

	backup := *target + ".old"
	_ = os.Remove(backup)

	for i := 0; i < 30; i++ {
		if err := os.Rename(*target, backup); err == nil {
			break
		}
		time.Sleep(time.Second)
	}

	if err := os.Rename(*source, *target); err != nil {
		_ = os.Rename(backup, *target)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if _, err := os.Stat(*target); err != nil {
		_ = os.Rename(backup, *target)
		os.Exit(1)
	}

	if abs, err := filepath.Abs(*restart); err == nil {
		_ = exec.Command(abs).Start()
	}
}
