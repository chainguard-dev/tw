package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/chainguard-dev/sht/internal/control"
)

func main() {
	var op string
	flag.StringVar(&op, "op", "", "Operation to perform")

	var addr string
	flag.StringVar(&addr, "addr", "", "Path to the control socket")

	var testName string
	flag.StringVar(&testName, "test-name", "", "Name of the test to run")

	var exitCode int
	flag.IntVar(&exitCode, "exit-code", 0, "Exit code to use")

	flag.Parse()

	if op == "" || addr == "" || testName == "" {
		fmt.Fprintf(os.Stderr, "Error: op, addr, and test-name flags are required\n")
		flag.Usage()
		os.Exit(1)
	}

	cli := control.NewClient(addr)

	if err := cli.Send(control.Command(op), testName, exitCode); err != nil {
		fmt.Fprintf(os.Stderr, "Error sending command: %v\n", err)
		os.Exit(1)
	}
}
