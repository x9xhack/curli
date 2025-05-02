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
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Printf("curli %s (%s)\n", internal.VERSION, internal.DATE)
		os.Exit(0)
	}

	stdinFd := int(os.Stdin.Fd())
	stdoutFd := int(os.Stdout.Fd())
	stderrFd := int(os.Stderr.Fd())

	scheme := formatter.DefaultColorScheme
	if err := setupWindowsConsole(stdoutFd); err != nil {
		scheme = formatter.ColorScheme{}
	}

	opts := args.Parse(os.Args)
	pretty := opts.Remove("pretty")
	quiet := opts.Has("silent") || opts.Has("s")
	verbose := opts.Has("v") || opts.Has("verbose")

	opts.Remove("i")

	var (
		stdin       io.Reader = os.Stdin
		stdout      io.Writer = os.Stdout
		stderr      io.Writer = os.Stderr
		input                 = &bytes.Buffer{}
		inputWriter io.Writer = input
	)

	if len(opts) == 0 {
		opts = append(opts, "-h", "all")
	} else {
		opts = append(opts, "-s", "-S")
	}

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
		if verbose && headerSupplied(opts, "Content-Type") {
			for _, h := range append(opts.Vals("H"), opts.Vals("header")...) {
				if strings.HasPrefix(strings.ToLower(h), "content-type:") &&
					strings.Contains(strings.ToLower(h), "application/json") {
					inputWriter = &formatter.JSON{Out: inputWriter, Scheme: scheme}
					break
				}
			}
		}

		if term.IsTerminal(stdoutFd) {
			stdout = &formatter.BinaryFilter{Out: stdout}
		}

		if term.IsTerminal(stderrFd) && !quiet {
			opts = append(opts, "-v")
		}

		if pretty || term.IsTerminal(stderrFd) {
			stderr = &formatter.HeaderColorizer{Out: stderr, Scheme: scheme}
		}

		if data := opts.Val("d"); data != "" {
			inputWriter.Write([]byte(data))
		} else if !term.IsTerminal(stdinFd) {
			opts = append(opts, "-d@-")
			stdin = io.TeeReader(stdin, inputWriter)
		}
	}

	if opts.Has("curl") {
		opts.Remove("curl")
		fmt.Print("curl")
		for _, opt := range opts {
			if strings.Contains(opt, " ") {
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

	var outBuf, errBuf bytes.Buffer

	cmd.Stderr = &formatter.HeaderCleaner{
		Out:     &errBuf,
		Verbose: verbose,
		Post:    input,
	}

	if (opts.Has("I") || opts.Has("head")) && term.IsTerminal(stdoutFd) {
		cmd.Stdout = io.Discard
	} else {
		cmd.Stdout = &outBuf
	}

	status := 0
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if ws, ok := exitErr.ProcessState.Sys().(syscall.WaitStatus); ok {
				status = ws.ExitStatus()
			}
		} else {
			fmt.Fprint(stderr, err)
		}
	}

	// Extract content-type from headers
	contentType := ""
	for _, line := range strings.Split(errBuf.String(), "\n") {
		if strings.HasPrefix(strings.ToLower(line), "content-type:") {
			contentType = strings.ToLower(strings.TrimSpace(strings.SplitN(line, ":", 2)[1]))
			break
		}
	}

	io.Copy(stderr, &errBuf)

	if term.IsTerminal(stdoutFd) && strings.Contains(contentType, "application/json") {
		var prettyBuf bytes.Buffer
		jsonFormatter := &formatter.JSON{Out: &prettyBuf, Scheme: scheme}
		io.Copy(jsonFormatter, &outBuf)
		io.Copy(stdout, &prettyBuf)
	} else {
		io.Copy(stdout, &outBuf)
	}

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
