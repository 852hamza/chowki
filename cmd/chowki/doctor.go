package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/852hamza/chowki/internal/doctor"
)

const doctorUsage = `Usage:
  chowki doctor [--config <FILE>]

Checks that chowki serve can start and work with the configuration: the .env
file, the configuration, the master key, the providers' keys and prices, the
model catalog, the database and the listen address. It changes nothing and
calls no provider. It exits with 1 when a check fails.

Flags:
  --config  the configuration file; by default $CHOWKI_CONFIG, or chowki.yaml
`

func runDoctor(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("chowki doctor", flag.ContinueOnError)
	configPath := flags.String("config", defaultConfig(), "")
	if code, ok := parseCommand(flags, args, doctorUsage, stdout, stderr); !ok {
		return code
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	results := doctor.Run(ctx, doctor.Options{ConfigPath: *configPath, EnvPath: ".env", Now: time.Now()})
	return printDoctor(stdout, results)
}

// printDoctor prints one line for each check, and the lines of a longer
// message under its first, then a summary. It returns the exit code.
func printDoctor(w io.Writer, results []doctor.Result) int {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	var failed, warned int
	for _, r := range results {
		lines := strings.Split(r.Message, "\n")
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Status, r.Check, lines[0])
		for _, line := range lines[1:] {
			fmt.Fprintf(tw, "\t\t%s\n", line)
		}
		switch r.Status {
		case doctor.Fail:
			failed++
		case doctor.Warn:
			warned++
		}
	}
	_ = tw.Flush() // w is stdout; there is nothing useful to do on failure
	switch {
	case failed > 0 && warned > 0:
		fmt.Fprintf(w, "\n%s failed and %d warned. Fix what failed before you start chowki serve.\n",
			plural(failed, "check"), warned)
	case failed > 0:
		fmt.Fprintf(w, "\n%s failed. Fix what failed before you start chowki serve.\n", plural(failed, "check"))
	case warned > 0:
		fmt.Fprintf(w, "\n%s warned; none failed.\n", plural(warned, "check"))
	default:
		fmt.Fprintln(w, "\nEvery check passed.")
	}
	if failed > 0 {
		return exitError
	}
	return exitOK
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
