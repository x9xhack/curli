package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/x9xhack/curli/args"
	"github.com/x9xhack/curli/formatter"
	"github.com/x9xhack/curli/internal"
	"golang.org/x/term"
)

func main() {
	// handle `curli version` separately from `curl --version`
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Printf("curli %s (%s)\n", internal.VERSION, internal.DATE)
		os.Exit(0)
		return
	}

	// *nixes use 0, 1, 2
	// Windows uses random numbers
	stdinFd := int(os.Stdin.Fd())
	stdoutFd := int(os.Stdout.Fd())
	stderrFd := int(os.Stderr.Fd())

	// Setting Console mode on windows to allow color output, By default scheme is DefaultColorScheme
	// But in case of any error, it is set to ColorScheme{}.
	scheme := formatter.DefaultColorScheme
	if err := setupWindowsConsole(stdoutFd); err != nil {
		scheme = formatter.ColorScheme{}
	}
	var stdout io.Writer = os.Stdout
	var stderr io.Writer = os.Stderr
	var stdin io.Reader = os.Stdin
	input := &bytes.Buffer{}
	var inputWriter io.Writer = input
	opts := args.Parse(os.Args)

	verbose := opts.Has("verbose") || opts.Has("v")
	quiet := opts.Has("silent") || opts.Has("s")
	pretty := opts.Remove("pretty")
	opts.Remove("i")

	if len(opts) == 0 {
		// Show help if no args
		opts = append(opts, "-h", "all")
	} else {
		// Remove progress bar.
		opts = append(opts, "-s", "-S")
	}

	// Change default method based on binary name.
	switch os.Args[0] {
	case "post", "put", "delete":
		if !opts.Has("X") && !opts.Has("request") {
			opts = append(opts, "-X", os.Args[0])
		}
	case "head":
		if !opts.Has("I") && !opts.Has("head") {
			opts = append(opts, "-I")
		}
	}

	if opts.Has("h") || opts.Has("help") {
		stdout = &formatter.HelpAdapter{Out: stdout, CmdName: os.Args[0]}
	} else {
		isForm := opts.Has("F")
		if pretty || term.IsTerminal(stdoutFd) {
			inputWriter = &formatter.JSON{
				Out:    inputWriter,
				Scheme: scheme,
			}
			// Format/colorize JSON output if stdout is to the terminal.
			stdout = &formatter.JSON{
				Out:    stdout,
				Scheme: scheme,
			}

			// Filter out binary output.
			stdout = &formatter.BinaryFilter{Out: stdout}
		}
		if pretty || term.IsTerminal(stderrFd) {
			// If stderr is not redirected, output headers.
			if !quiet {
				opts = append(opts, "-v")
			}

			stderr = &formatter.HeaderColorizer{
				Out:    stderr,
				Scheme: scheme,
			}
		}
		hasInput := true
		if data := opts.Val("d"); data != "" {
			// If data is provided via -d, read it from there for the verbose mode.
			// XXX handle the @filename case.
			inputWriter.Write([]byte(data))
		} else if !term.IsTerminal(stdinFd) {
			// If something is piped in to the command, tell curl to use it as input.
			opts = append(opts, "-d@-")
			// Tee the stdin to the buffer used show the posted data in verbose mode.
			stdin = io.TeeReader(stdin, inputWriter)
		} else {
			hasInput = false
		}
	}
	if opts.Has("curl") {
		opts.Remove("curl")
		fmt.Print("curl")
		for _, opt := range opts {
			if strings.IndexByte(opt, ' ') != -1 {
				fmt.Printf(" %q", opt)
			} else {
				fmt.Printf(" %s", opt)
			}
		}
		fmt.Println()
		return
	}
	cmd := exec.Command("curl", opts...)
	cmd.Stdin = stdin

	// Buffers for capturing output
	var outBuf, errBuf bytes.Buffer

	// Wrap stderr with HeaderCleaner -> errBuf
	cmd.Stderr = &formatter.HeaderCleaner{
		Out:     &errBuf,
		Verbose: verbose,
		Post:    input,
	}

	// Handle --head with terminal stdout
	if (opts.Has("I") || opts.Has("head")) && term.IsTerminal(stdoutFd) {
		cmd.Stdout = io.Discard
	} else {
		cmd.Stdout = &outBuf
	}

	status := 0
	if err := cmd.Run(); err != nil {
		switch err := err.(type) {
		case *exec.ExitError:
			if ws, ok := err.ProcessState.Sys().(syscall.WaitStatus); ok {
				status = ws.ExitStatus()
			}
		default:
			fmt.Fprint(stderr, err)
		}
	}

	// Print stderr first, then stdout
	io.Copy(stderr, &errBuf)
	io.Copy(stdout, &outBuf)

	os.Exit(status)
}

func headerSupplied(opts args.Opts, header string) bool {
	header = strings.ToLower(header)
	for _, h := range append(opts.Vals("H"), opts.Vals("header")...) {
		if strings.HasPrefix(strings.ToLower(h), header+":") {
			return true
		}
	}
	return false
}
