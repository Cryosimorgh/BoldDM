package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	target := flag.String("target", "BoltDM.exe", "application to replace")
	source := flag.String("source", "BoltDM.exe.new", "new application")
	flag.Parse()

	for i := 0; i < 30; i++ {
		if err := os.Rename(*source, *target); err == nil {
			return
		}
		time.Sleep(time.Second)
	}

	fmt.Fprintln(os.Stderr, "unable to replace application")
	os.Exit(1)
}
