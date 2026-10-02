// Command hw1 prints the current user name taken from the environment,
// the command-line arguments and the Go version.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
)

func main() {
	run(os.Stdout, os.Args[1:])
}

// run writes the user name, the given arguments and the Go version to w.
func run(w io.Writer, args []string) {
	name, ok := userName()
	if !ok {
		name = "unknown (USER and USERNAME are not set)"
	}
	fmt.Fprintln(w, "User:", name)

	if len(args) == 0 {
		fmt.Fprintln(w, "Arguments: none")
	} else {
		fmt.Fprintf(w, "Arguments (%d):\n", len(args))
		for i, arg := range args {
			fmt.Fprintf(w, "  %d: %q\n", i+1, arg)
		}
	}

	fmt.Fprintln(w, "Go version:", runtime.Version())
}

// userName returns the user name from the environment: USERNAME on Windows
// and USER on other systems, falling back to the other variable.
// The second result is false when neither variable is set.
func userName() (string, bool) {
	keys := []string{"USER", "USERNAME"}
	if runtime.GOOS == "windows" {
		keys = []string{"USERNAME", "USER"}
	}
	for _, key := range keys {
		if name := os.Getenv(key); name != "" {
			return name, true
		}
	}
	return "", false
}
