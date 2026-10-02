// Command hw1 prints the current user name taken from the environment,
// the command-line arguments and the Go version.
package main

import (
	"fmt"
	"os"
	"runtime"
)

func main() {
	fmt.Println("User:", userName())

	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Println("Arguments: none")
	} else {
		fmt.Printf("Arguments (%d):\n", len(args))
		for i, arg := range args {
			fmt.Printf("  %d: %q\n", i+1, arg)
		}
	}

	fmt.Println("Go version:", runtime.Version())
}

// userName returns the user name from the USER environment variable
// (macOS, Linux) or USERNAME (Windows).
func userName() string {
	for _, key := range []string{"USER", "USERNAME"} {
		if name := os.Getenv(key); name != "" {
			return name
		}
	}
	return "unknown (USER and USERNAME are not set)"
}
