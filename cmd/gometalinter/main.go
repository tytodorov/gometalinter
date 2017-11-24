package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/alecthomas/gometalinter"
	kingpin "gopkg.in/alecthomas/kingpin.v3-unstable"
)

var sortKeys = []string{"none", "path", "line", "column", "severity", "message", "linter"}

func setupFlags(app *kingpin.Application) {
	app.Flag("config", "Load JSON configuration from file.").Action(loadConfig).String()
	app.Flag("disable", "Disable previously enabled linters.").PlaceHolder("LINTER").Short('D').Action(disableAction).Strings()
	app.Flag("enable", "Enable previously disabled linters.").PlaceHolder("LINTER").Short('E').Action(enableAction).Strings()
	app.Flag("linter", "Define a linter.").PlaceHolder("NAME:COMMAND:PATTERN").Action(cliLinterOverrides).StringMap()
	app.Flag("message-overrides", "Override message from linter. {message} will be expanded to the original message.").PlaceHolder("LINTER:MESSAGE").StringMapVar(&gometalinter.Configuration.MessageOverride)
	app.Flag("severity", "Map of linter severities.").PlaceHolder("LINTER:SEVERITY").StringMapVar(&gometalinter.Configuration.Severity)
	app.Flag("disable-all", "Disable all linters.").Action(disableAllAction).Bool()
	app.Flag("enable-all", "Enable all linters.").Action(enableAllAction).Bool()
	app.Flag("format", "Output format.").PlaceHolder(gometalinter.Configuration.Format).StringVar(&gometalinter.Configuration.Format)
	app.Flag("vendored-linters", "Use vendored linters (recommended).").BoolVar(&gometalinter.Configuration.VendoredLinters)
	app.Flag("fast", "Only run fast linters.").BoolVar(&gometalinter.Configuration.Fast)
	app.Flag("install", "Attempt to install all known linters.").Short('i').BoolVar(&gometalinter.Configuration.Install)
	app.Flag("update", "Pass -u to go tool when installing.").Short('u').BoolVar(&gometalinter.Configuration.Update)
	app.Flag("force", "Pass -f to go tool when installing.").Short('f').BoolVar(&gometalinter.Configuration.Force)
	app.Flag("download-only", "Pass -d to go tool when installing.").BoolVar(&gometalinter.Configuration.DownloadOnly)
	app.Flag("debug", "Display messages for failed linters, etc.").Short('d').BoolVar(&gometalinter.Configuration.Debug)
	app.Flag("concurrency", "Number of concurrent linters to run.").PlaceHolder(fmt.Sprintf("%d", runtime.NumCPU())).Short('j').IntVar(&gometalinter.Configuration.Concurrency)
	app.Flag("exclude", "Exclude messages matching these regular expressions.").Short('e').PlaceHolder("REGEXP").StringsVar(&gometalinter.Configuration.Exclude)
	app.Flag("include", "Include messages matching these regular expressions.").Short('I').PlaceHolder("REGEXP").StringsVar(&gometalinter.Configuration.Include)
	app.Flag("skip", "Skip directories with this name when expanding '...'.").Short('s').PlaceHolder("DIR...").StringsVar(&gometalinter.Configuration.Skip)
	app.Flag("vendor", "Enable vendoring support (skips 'vendor' directories and sets GO15VENDOREXPERIMENT=1).").BoolVar(&gometalinter.Configuration.Vendor)
	app.Flag("cyclo-over", "Report functions with cyclomatic complexity over N (using gocyclo).").PlaceHolder("10").IntVar(&gometalinter.Configuration.Cyclo)
	app.Flag("line-length", "Report lines longer than N (using lll).").PlaceHolder("80").IntVar(&gometalinter.Configuration.LineLength)
	app.Flag("min-confidence", "Minimum confidence interval to pass to golint.").PlaceHolder(".80").FloatVar(&gometalinter.Configuration.MinConfidence)
	app.Flag("min-occurrences", "Minimum occurrences to pass to goconst.").PlaceHolder("3").IntVar(&gometalinter.Configuration.MinOccurrences)
	app.Flag("min-const-length", "Minimum constant length.").PlaceHolder("3").IntVar(&gometalinter.Configuration.MinConstLength)
	app.Flag("dupl-threshold", "Minimum token sequence as a clone for dupl.").PlaceHolder("50").IntVar(&gometalinter.Configuration.DuplThreshold)
	app.Flag("sort", fmt.Sprintf("Sort output by any of %s.", strings.Join(sortKeys, ", "))).PlaceHolder("none").EnumsVar(&gometalinter.Configuration.Sort, sortKeys...)
	app.Flag("tests", "Include test files for linters that support this option.").Short('t').BoolVar(&gometalinter.Configuration.Test)
	app.Flag("deadline", "Cancel linters if they have not completed within this duration.").PlaceHolder("30s").DurationVar((*time.Duration)(&gometalinter.Configuration.Deadline))
	app.Flag("errors", "Only show errors.").BoolVar(&gometalinter.Configuration.Errors)
	app.Flag("json", "Generate structured JSON rather than standard line-based output.").BoolVar(&gometalinter.Configuration.JSON)
	app.Flag("checkstyle", "Generate checkstyle XML rather than standard line-based output.").BoolVar(&gometalinter.Configuration.Checkstyle)
	app.Flag("enable-gc", "Enable GC for linters (useful on large repositories).").BoolVar(&gometalinter.Configuration.EnableGC)
	app.Flag("aggregate", "Aggregate issues reported by several linters.").BoolVar(&gometalinter.Configuration.Aggregate)
	app.Flag("warn-unmatched-nolint", "Warn if a nolint directive is not matched with an issue.").BoolVar(&gometalinter.Configuration.WarnUnmatchedDirective)
	app.GetFlag("help").Short('h')
}

func cliLinterOverrides(app *kingpin.Application, element *kingpin.ParseElement, ctx *kingpin.ParseContext) error {
	// expected input structure - <name>:<command-spec>
	parts := strings.SplitN(*element.Value, ":", 2)
	if len(parts) < 2 {
		return fmt.Errorf("incorrectly formatted input: %s", *element.Value)
	}
	name := parts[0]
	spec := parts[1]
	conf, err := gometalinter.ParseLinterConfigSpec(name, spec)
	if err != nil {
		return fmt.Errorf("incorrectly formatted input: %s", *element.Value)
	}
	gometalinter.Configuration.Linters[name] = gometalinter.StringOrLinterConfig(conf)
	return nil
}

func loadConfig(app *kingpin.Application, element *kingpin.ParseElement, ctx *kingpin.ParseContext) error {
	r, err := os.Open(*element.Value)
	if err != nil {
		return err
	}
	defer r.Close() // nolint: errcheck
	err = json.NewDecoder(r).Decode(gometalinter.Configuration)
	if err != nil {
		return err
	}
	for _, disable := range gometalinter.Configuration.Disable {
		for i, enable := range gometalinter.Configuration.Enable {
			if enable == disable {
				gometalinter.Configuration.Enable = append(gometalinter.Configuration.Enable[:i], gometalinter.Configuration.Enable[i+1:]...)
				break
			}
		}
	}
	return err
}

func disableAction(app *kingpin.Application, element *kingpin.ParseElement, ctx *kingpin.ParseContext) error {
	out := []string{}
	for _, linter := range gometalinter.Configuration.Enable {
		if linter != *element.Value {
			out = append(out, linter)
		}
	}
	gometalinter.Configuration.Enable = out
	return nil
}

func enableAction(app *kingpin.Application, element *kingpin.ParseElement, ctx *kingpin.ParseContext) error {
	gometalinter.Configuration.Enable = append(gometalinter.Configuration.Enable, *element.Value)
	return nil
}

func disableAllAction(app *kingpin.Application, element *kingpin.ParseElement, ctx *kingpin.ParseContext) error {
	gometalinter.Configuration.Enable = []string{}
	return nil
}

func enableAllAction(app *kingpin.Application, element *kingpin.ParseElement, ctx *kingpin.ParseContext) error {
	for linter := range gometalinter.DefaultLinters {
		gometalinter.Configuration.Enable = append(gometalinter.Configuration.Enable, linter)
	}
	gometalinter.Configuration.EnableAll = true
	return nil
}

func formatLinters() string {
	w := bytes.NewBuffer(nil)
	for _, linter := range gometalinter.GetDefaultLinters() {
		install := "(" + linter.InstallFrom + ")"
		if install == "()" {
			install = ""
		}
		fmt.Fprintf(w, "  %s: %s\n\tcommand: %s\n\tregex: %s\n\tfast: %t\n\tdefault enabled: %t\n\n",
			linter.Name, install, linter.Command, linter.Pattern, linter.IsFast, linter.IsDefaultEnabled())

	}
	return w.String()
}

func formatSeverity() string {
	w := bytes.NewBuffer(nil)
	for name, severity := range gometalinter.Configuration.Severity {
		fmt.Fprintf(w, "  %s -> %s\n", name, severity)
	}
	return w.String()
}

func main() {
	pathsArg := kingpin.Arg("path", "Directories to lint. Defaults to \".\". <path>/... will recurse.").Strings()
	app := kingpin.CommandLine
	setupFlags(app)
	app.Help = fmt.Sprintf(`Aggregate and normalise the output of a whole bunch of Go linters.

	PlaceHolder linters:

	%s

	Severity override map (default is "warning"):

	%s
	`, formatLinters(), formatSeverity())

	kingpin.Parse()
	gometalinter.Run(*pathsArg)
}
