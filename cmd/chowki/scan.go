package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/852hamza/chowki/internal/buildinfo"
	"github.com/852hamza/chowki/internal/scan"
)

const scanUsage = `Usage:
  chowki scan [--format text|json|sarif] [--output <FILE>] [--pii] [--exit-zero] [<PATH>...]

Finds secrets, such as API keys, private keys and passwords, in files: a
repository, a directory, .env files and MCP configurations. <PATH> is a
file or a directory, the current directory by default. In a git
repository, it reads the files that git tracks or would track.

It never prints the secrets, only where they are. It exits with 1 when it
finds any, 0 otherwise, and 2 on an error.

Flags:
  --format     text (default), json or sarif, for GitHub code scanning
  --output     the file to write the report to, instead of stdout
  --pii        report personal data too, such as email addresses
  --exit-zero  exit with 0 even when there are findings
`

// exitScanFailed is the exit code of a scan that couldn't finish, apart from
// exitError, which means that it found secrets.
const exitScanFailed = 2

func runScan(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki scan", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	format := flags.String("format", "text", "")
	output := flags.String("output", "", "")
	pii := flags.Bool("pii", false, "")
	exitZero := flags.Bool("exit-zero", false, "")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, scanUsage)
			return exitOK
		}
		fmt.Fprintf(stderr, "chowki scan: %v\n\n%s", err, scanUsage)
		return exitUsage
	}
	write := map[string]func(io.Writer, *scan.Result) error{
		"text": scan.WriteText, "json": scan.WriteJSON,
		"sarif": func(w io.Writer, res *scan.Result) error {
			return scan.WriteSARIF(w, res, scan.Tool{Name: "chowki", Version: buildinfo.Version(),
				InformationURI: buildinfo.Project().RepoURL()})
		},
	}[*format]
	if write == nil {
		fmt.Fprintf(stderr, "chowki scan: --format must be text, json or sarif\n\n%s", scanUsage)
		return exitUsage
	}
	paths := flags.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}

	total := &scan.Result{}
	for _, p := range paths {
		res, err := scan.Scan(context.Background(), p, scan.Options{PII: *pii})
		if err != nil {
			fmt.Fprintf(stderr, "chowki scan: %v\n", err)
			return exitScanFailed
		}
		total.Findings = append(total.Findings, res.Findings...)
		total.Files += res.Files
	}
	w := stdout
	if *output != "" {
		f, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			fmt.Fprintf(stderr, "chowki scan: %v\n", err)
			return exitScanFailed
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	if err := write(w, total); err != nil {
		fmt.Fprintf(stderr, "chowki scan: write the report: %v\n", err)
		return exitScanFailed
	}
	if len(total.Findings) > 0 && !*exitZero {
		if *output != "" {
			found := fmt.Sprintf("%d secrets", len(total.Findings))
			if len(total.Findings) == 1 {
				found = "1 secret"
			}
			fmt.Fprintf(stderr, "chowki scan: found %s; see %s\n", found, *output)
		}
		return exitError
	}
	return exitOK
}
