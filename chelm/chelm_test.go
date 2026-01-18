package main

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"chainguard.dev/tw/chelm/cmd"
	"github.com/rogpeppe/go-internal/testscript"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"chelm": chelmMain,
	})
}

func chelmMain() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var update = flag.Bool("update", false, "update testscript golden files")

func TestChelm(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:           "testdata",
		UpdateScripts: *update,
	})
}
