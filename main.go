package gometalinter

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"text/template"
	"time"

	kingpin "gopkg.in/alecthomas/kingpin.v3-unstable"
)

var (
	// Locations to look for vendored linters.
	vendoredSearchPaths = [][]string{
		{"github.com", "alecthomas", "gometalinter", "_linters"},
		{"gopkg.in", "alecthomas", "gometalinter.v1", "_linters"},
	}
)

type debugFunction func(format string, args ...interface{})

func debug(format string, args ...interface{}) {
	if Configuration.Debug {
		fmt.Fprintf(os.Stderr, "DEBUG: "+format+"\n", args...)
	}
}

func namespacedDebug(prefix string) debugFunction {
	return func(format string, args ...interface{}) {
		debug(prefix+format, args...)
	}
}

func warning(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "WARNING: "+format+"\n", args...)
}

func Run(paths []string) {
	if Configuration.Install {
		if Configuration.VendoredLinters {
			configureEnvironmentForInstall()
		}
		installLinters()
		return
	}

	configureEnvironment()
	include, exclude := processConfig(Configuration)

	start := time.Now()
	resolvedPaths := resolvePaths(paths, Configuration.Skip)

	linters := lintersFromConfig(Configuration)
	err := validateLinters(linters, Configuration)
	kingpin.FatalIfError(err, "")

	issues, errch := runLinters(linters, resolvedPaths, Configuration.Concurrency, exclude, include)
	status := 0
	if Configuration.JSON {
		status |= outputToJSON(issues)
	} else if Configuration.Checkstyle {
		status |= outputToCheckstyle(issues)
	} else {
		status |= outputToConsole(issues)
	}
	for err := range errch {
		warning("%s", err)
		status |= 2
	}
	elapsed := time.Since(start)
	debug("total elapsed time %s", elapsed)
	os.Exit(status)
}

// nolint: gocyclo
func processConfig(config *Config) (include *regexp.Regexp, exclude *regexp.Regexp) {
	tmpl, err := template.New("output").Parse(config.Format)
	kingpin.FatalIfError(err, "invalid format %q", config.Format)
	config.formatTemplate = tmpl

	// Linters are by their very nature, short lived, so disable GC.
	// Reduced (user) linting time on kingpin from 0.97s to 0.64s.
	if !config.EnableGC {
		_ = os.Setenv("GOGC", "off")
	}
	if config.VendoredLinters && config.Install && config.Update {
		warning(`Linters are now vendored by default, --update ignored. The original
behaviour can be re-enabled with --no-vendored-linters.

To request an update for a vendored linter file an issue at:
https://github.com/alecthomas/gometalinter/issues/new
`)
		config.Update = false
	}
	// Force sorting by path if checkstyle mode is selected
	// !jsonFlag check is required to handle:
	// 	gometalinter --json --checkstyle --sort=severity
	if config.Checkstyle && !config.JSON {
		config.Sort = []string{"path"}
	}

	// PlaceHolder to skipping "vendor" directory if GO15VENDOREXPERIMENT=1 is enabled.
	// TODO(alec): This will probably need to be enabled by default at a later time.
	if os.Getenv("GO15VENDOREXPERIMENT") == "1" || config.Vendor {
		if err := os.Setenv("GO15VENDOREXPERIMENT", "1"); err != nil {
			warning("setenv GO15VENDOREXPERIMENT: %s", err)
		}
		config.Skip = append(config.Skip, "vendor")
		config.Vendor = true
	}
	if len(config.Exclude) > 0 {
		exclude = regexp.MustCompile(strings.Join(config.Exclude, "|"))
	}

	if len(config.Include) > 0 {
		include = regexp.MustCompile(strings.Join(config.Include, "|"))
	}

	runtime.GOMAXPROCS(config.Concurrency)
	return include, exclude
}

func outputToConsole(issues chan *Issue) int {
	status := 0
	for issue := range issues {
		if Configuration.Errors && issue.Severity != Error {
			continue
		}
		fmt.Println(issue.String())
		status = 1
	}
	return status
}

func outputToJSON(issues chan *Issue) int {
	fmt.Println("[")
	status := 0
	for issue := range issues {
		if Configuration.Errors && issue.Severity != Error {
			continue
		}
		if status != 0 {
			fmt.Printf(",\n")
		}
		d, err := json.Marshal(issue)
		kingpin.FatalIfError(err, "")
		fmt.Printf("  %s", d)
		status = 1
	}
	fmt.Printf("\n]\n")
	return status
}

func resolvePaths(paths, skip []string) []string {
	if len(paths) == 0 {
		return []string{"."}
	}

	skipPath := newPathFilter(skip)
	dirs := newStringSet()
	for _, path := range paths {
		if strings.HasSuffix(path, "/...") {
			root := filepath.Dir(path)
			_ = filepath.Walk(root, func(p string, i os.FileInfo, err error) error {
				if err != nil {
					warning("invalid path %q: %s", p, err)
					return err
				}

				skip := skipPath(p)
				switch {
				case i.IsDir() && skip:
					return filepath.SkipDir
				case !i.IsDir() && !skip && strings.HasSuffix(p, ".go"):
					dirs.add(filepath.Clean(filepath.Dir(p)))
				}
				return nil
			})
		} else {
			dirs.add(filepath.Clean(path))
		}
	}
	out := make([]string, 0, dirs.size())
	for _, d := range dirs.asSlice() {
		out = append(out, relativePackagePath(d))
	}
	sort.Strings(out)
	for _, d := range out {
		debug("linting path %s", d)
	}
	return out
}

func newPathFilter(skip []string) func(string) bool {
	filter := map[string]bool{}
	for _, name := range skip {
		filter[name] = true
	}

	return func(path string) bool {
		base := filepath.Base(path)
		if filter[base] || filter[path] {
			return true
		}
		return base != "." && base != ".." && strings.ContainsAny(base[0:1], "_.")
	}
}

func relativePackagePath(dir string) string {
	if filepath.IsAbs(dir) || strings.HasPrefix(dir, ".") {
		return dir
	}
	// package names must start with a ./
	return "./" + dir
}

func lintersFromConfig(config *Config) map[string]*Linter {
	out := map[string]*Linter{}
	config.Enable = replaceWithMegacheck(config.Enable, config.EnableAll)
	for _, name := range config.Enable {
		linter := getLinterByName(name, LinterConfig(config.Linters[name]))
		if config.Fast && !linter.IsFast {
			continue
		}
		out[name] = linter
	}
	for _, linter := range config.Disable {
		delete(out, linter)
	}
	return out
}

// replaceWithMegacheck checks enabled linters if they duplicate megacheck and
// returns a either a revised list removing those and adding megacheck or an
// unchanged slice. Emits a warning if linters were removed and swapped with
// megacheck.
func replaceWithMegacheck(enabled []string, enableAll bool) []string {
	var (
		staticcheck,
		gosimple,
		unused bool
		revised []string
	)
	for _, linter := range enabled {
		switch linter {
		case "staticcheck":
			staticcheck = true
		case "gosimple":
			gosimple = true
		case "unused":
			unused = true
		case "megacheck":
			// Don't add to revised slice, we'll add it later
		default:
			revised = append(revised, linter)
		}
	}
	if staticcheck && gosimple && unused {
		if !enableAll {
			warning("staticcheck, gosimple and unused are all set, using megacheck instead")
		}
		return append(revised, "megacheck")
	}
	return enabled
}

func findVendoredLinters() string {
	gopaths := getGoPathList()
	for _, home := range vendoredSearchPaths {
		for _, p := range gopaths {
			joined := append([]string{p, "src"}, home...)
			vendorRoot := filepath.Join(joined...)
			if _, err := os.Stat(vendorRoot); err == nil {
				return vendorRoot
			}
		}
	}
	return ""
}

// Go 1.8 compatible GOPATH.
func getGoPath() string {
	path := os.Getenv("GOPATH")
	if path == "" {
		user, err := user.Current()
		kingpin.FatalIfError(err, "")
		path = filepath.Join(user.HomeDir, "go")
	}
	return path
}

func getGoPathList() []string {
	return strings.Split(getGoPath(), string(os.PathListSeparator))
}

// addPath appends path to paths if path does not already exist in paths. Returns
// the new paths.
func addPath(paths []string, path string) []string {
	for _, existingpath := range paths {
		if path == existingpath {
			return paths
		}
	}
	return append(paths, path)
}

// configureEnvironment adds all `bin/` directories from $GOPATH to $PATH
func configureEnvironment() {
	paths := addGoBinsToPath(getGoPathList())
	setEnv("PATH", strings.Join(paths, string(os.PathListSeparator)))
	debugPrintEnv()
}

func addGoBinsToPath(gopaths []string) []string {
	paths := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))
	for _, p := range gopaths {
		paths = addPath(paths, filepath.Join(p, "bin"))
	}
	gobin := os.Getenv("GOBIN")
	if gobin != "" {
		paths = addPath(paths, gobin)
	}
	return paths
}

// configureEnvironmentForInstall sets GOPATH and GOBIN so that vendored linters
// can be installed
func configureEnvironmentForInstall() {
	gopaths := getGoPathList()
	vendorRoot := findVendoredLinters()
	if vendorRoot == "" {
		kingpin.Fatalf("could not find vendored linters in GOPATH=%q", getGoPath())
	}
	debug("found vendored linters at %s, updating environment", vendorRoot)

	gobin := os.Getenv("GOBIN")
	if gobin == "" {
		gobin = filepath.Join(gopaths[0], "bin")
	}
	setEnv("GOBIN", gobin)

	// "go install" panics when one GOPATH element is beneath another, so set
	// GOPATH to the vendor root
	setEnv("GOPATH", vendorRoot)
	debugPrintEnv()
}

func setEnv(key string, value string) {
	if err := os.Setenv(key, value); err != nil {
		warning("setenv %s: %s", key, err)
	}
}

func debugPrintEnv() {
	debug("PATH=%s", os.Getenv("PATH"))
	debug("GOPATH=%s", os.Getenv("GOPATH"))
	debug("GOBIN=%s", os.Getenv("GOBIN"))
}
