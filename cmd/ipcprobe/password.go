package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// readPassword reads one line of password from stdin.
//
// Interactive (stdin is a terminal): prints the prompt and disables echo with `stty -echo` on unix (no external
// module needed); on Windows, or if stty is unavailable, it falls back to an echoed read with a warning.
//
// Non-interactive (stdin is a pipe or file — e.g. the macOS app, or `echo pw | ipcprobe set ...`): reads the line
// silently. No prompt, no stty, no warning. This is the supported way for a wrapper to hand the password over
// without it ever appearing on a command line or in a process list.
func readPassword(prompt string) (string, error) {
	if !stdinIsTerminal() {
		return readLine()
	}
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
	return readLine()
}

func readLine() (string, error) {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// stdinIsTerminal reports whether stdin is a character device (a tty) rather than a pipe or file.
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func sttySet(arg string) error {
	cmd := exec.Command("stty", arg)
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
