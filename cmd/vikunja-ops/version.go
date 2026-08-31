package main

import (
	"fmt"
	"io"
)

var version = "dev"

func printVersion(out io.Writer) {
	fmt.Fprintf(out, "vikunja-ops %s\n", version)
}
