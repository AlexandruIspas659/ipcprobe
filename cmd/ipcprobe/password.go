package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// readPassword reads a line from the terminal without echoing it. It uses `stty -echo` on unix (no external module
// needed); on Windows, or if stty is unavailable, it falls back to an echoed read with a warning.
func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	restore := func() {}
	if runtime.GOOS != "windows" {
		if err := sttySet("-echo"); err == nil {
			restore = func() { _ = sttySet("echo"); fmt.Fprintln(os.Stderr) }
		} else {
			fmt.Fprint(os.Stderr, "(warning: could not disable echo; input will be visible) ")
		}
	}
	defer restore()

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func sttySet(arg string) error {
	cmd := exec.Command("stty", arg)
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
