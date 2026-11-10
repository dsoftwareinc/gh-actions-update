// Command gh-actions-update is a GitHub CLI extension that finds outdated actions in GitHub Actions workflows and
// updates them, in local clones or remote repositories. Install it with:
//
//	gh extension install dsoftwareinc/gh-actions-update
package main

import (
	"fmt"
	"os"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/cli/go-gh/v2/pkg/term"
)

// currentVersion is the version of the extension. Bump it, and add a CHANGELOG entry, when releasing.
const currentVersion = "v2.0.0"

// version overrides currentVersion when set at build time, as release builds do with
// -ldflags "-X main.version=<release tag>".
var version string

var releaseVersionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

func buildVersion() string {
	if version != "" {
		return version
	}
	// `go install github.com/dsoftwareinc/gh-actions-update/v2@vX.Y.Z` records the version it installed.
	if info, ok := debug.ReadBuildInfo(); ok && releaseVersionPattern.MatchString(info.Main.Version) {
		return info.Main.Version
	}
	return currentVersion
}

func main() {
	t := term.FromEnv()
	errColor := term.IsColorForced() || (!term.IsColorDisabled() && term.IsTerminal(os.Stderr))
	a := newApp(t.Out(), t.ErrOut(), t.IsColorEnabled(), errColor)
	if err := a.execute(normalizeLegacyArgs(os.Args[1:])); err != nil {
		fmt.Fprintln(a.stderr, a.errColor.style("Error: "+err.Error(), red))
		os.Exit(1)
	}
}

// normalizeLegacyArgs accepts -commit-msg, the single-dash spelling of --commit-msg used by the Python version.
func normalizeLegacyArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i, arg := range args {
		if arg == "--" {
			return append(out, args[i:]...)
		}
		if arg == "-commit-msg" || strings.HasPrefix(arg, "-commit-msg=") {
			arg = "-" + arg
		}
		out = append(out, arg)
	}
	return out
}
