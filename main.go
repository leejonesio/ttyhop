// Copyright (c) 2025 Lee Jones
// Licensed under the MIT License. See LICENSE file in the project root for details.

package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

var (
	version = ""
	branch  = ""
	tag     = ""
)

func usage() {
	fmt.Fprintln(os.Stderr, `usage: ttyhop [--check] [-v|--log] [-q|--quiet] [--no-edge] [--edge-steps N] [--wait-ms N] [--version] {left|l|right|r|shell zsh}
  left/l, right/r      hop between terminal windows
  shell zsh            print zsh eval script for keybindings
  --check              print trust + front app info (no focus change)
  -v, --log            enable logging (or set TTYHOP_LOG=1)
  -q, --quiet          disable logging
  --no-edge            don't send C-h/C-l after hop
  --edge-steps N       how many C-h/l to send (default 5)
  --wait-ms N          ms to wait for window focus (default 200, env: TTYHOP_EDGE_WAIT_MS)
  --version            print version and exit`)
	os.Exit(64)
}

func vcsTimeToUnixStr(s string) string {
	t, _ := time.Parse(time.RFC3339, s)
	return fmt.Sprint(t.Unix())
}

func printVersion() {
	var commit, buildTime string
	var isDirtyBuild bool

	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				commit = s.Value
			case "vcs.time":
				buildTime = vcsTimeToUnixStr(s.Value) // e.g. "1765xxxx"
			case "vcs.modified":
				isDirtyBuild = (s.Value == "true")
			}
		}
	}

	// Build the base version string: "v1.0.2" or "v1.0.2.1765xxxx"
	parts := []string{strings.TrimSpace(version)}
	if bt := strings.TrimSpace(buildTime); bt != "" {
		parts = append(parts, bt)
	}
	base := strings.Join(parts, ".")

	// Optional details (only when not dirty)
	var details []string
	if !isDirtyBuild {
		if len(commit) >= 7 {
			details = append(details, fmt.Sprintf("Commit: %s", commit[:7]))
		}
		if tag != "" && tag != "(none)" && !strings.HasPrefix(version, tag) {
			details = append(details, fmt.Sprintf("Tag: %s", tag))
		}
	}

	if branch != "" && branch != "main" {
		details = append(details, fmt.Sprintf("Branch: `%s`", branch))
	}

	detailStr := ""
	if len(details) > 0 {
		detailStr = " (" + strings.Join(details, ", ") + ")"
	}

	fmt.Printf("ttyhop version %s%s\n", base, detailStr)
}

// run is the main application logic, separated for testability.
func run(hopper Hopper, args []string) int {
	var flVerbose, flQuiet, flCheck, flNoEdge, flVersion bool
	var flEdgeSteps string
	var flWaitMs int

	fs := flag.NewFlagSet("ttyhop", flag.ContinueOnError)
	fs.BoolVar(&flVerbose, "v", false, "verbose logging")
	fs.BoolVar(&flVerbose, "log", false, "verbose logging")
	fs.BoolVar(&flQuiet, "q", false, "quiet")
	fs.BoolVar(&flQuiet, "quiet", false, "quiet")
	fs.BoolVar(&flCheck, "check", false, "check only")
	fs.BoolVar(&flNoEdge, "no-edge", false, "disable tmux edge nudge")
	fs.StringVar(&flEdgeSteps, "edge-steps", "5", "number of C-h/l presses after hop")
	fs.IntVar(&flWaitMs, "wait-ms", 0, "ms to wait for window focus")
	fs.BoolVar(&flVersion, "version", false, "print version and exit")

	if err := fs.Parse(args); err != nil {
		usage()
		return 64
	}

	if flVersion {
		printVersion()
		return 0
	}

	envLog := os.Getenv("TTYHOP_LOG") == "1"
	debug := !flQuiet && (flVerbose || envLog)
	hopper.SetDebug(debug)
	hopper.SetWaitMs(flWaitMs)

	if flCheck {
		trusted := hopper.IsTrusted()
		bid, name, source := hopper.GetFrontAppInfo()
		fmt.Printf("trusted=%v front_bid=%q front_name=%q (%s)\n", trusted, bid, name, source)
		return 0
	}

	posArgs := fs.Args()
	if len(posArgs) == 0 {
		usage()
		return 64
	}

	steps := 5
	if n, err := strconv.Atoi(flEdgeSteps); err == nil && n > 0 && n < 50 {
		steps = n
	}
	doEdge := !flNoEdge

	switch posArgs[0] {
	case "left", "l":
		if len(posArgs) != 1 {
			usage()
			return 64
		}
		return hopper.FocusNeighbor(false, debug, doEdge, steps)
	case "right", "r":
		if len(posArgs) != 1 {
			usage()
			return 64
		}
		return hopper.FocusNeighbor(true, debug, doEdge, steps)
	case "shell":
		if len(posArgs) != 2 {
			usage()
			return 64
		}
		switch posArgs[1] {
		case "zsh":
			fmt.Print(zshScript)
			return 0
		default:
			usage()
			return 64
		}
	default:
		usage()
		return 64
	}
}

func main() {
	hopper := newHopper()
	os.Exit(run(hopper, os.Args[1:]))
}

const zshScript = `
# ttyhop with dynamic "passthrough" fallback

# --- ttyhop l (^h) ---
#
# 1. Discover what '^h' is currently bound to.
#    The ` + "`bindkey`" + ` output is like: '"^H" backward-delete-char'. We grab the second part.
original_h_widget=$(bindkey '^h' | awk '{print $2}')

# 2. Define our new function.
ttyhop-l() {
  ttyhop l
  # 3. If ttyhop fails, execute the original command we discovered.
  #    If nothing was bound, default to backward-delete-char just in case.
  if [[ $? -ne 0 ]]; then
    zle "${original_h_widget:-backward-delete-char}"
  fi
}

# 4. Register and bind our new function.
zle -N ttyhop-l
bindkey '^h' ttyhop-l


# --- ttyhop r (^l) ---
#
# 1. Discover what '^l' is currently bound to.
original_l_widget=$(bindkey '^l' | awk '{print $2}')

# 2. Define our new function.
ttyhop-r() {
  ttyhop r
  # 3. If ttyhop fails, execute the original command.
  #    If nothing was bound, default to clear-screen.
  if [[ $? -ne 0 ]]; then
    zle "${original_l_widget:-clear-screen}"
  fi
}

# 4. Register and bind our new function.
zle -N ttyhop-r
bindkey '^l' ttyhop-r

# Clean up the temporary variables
unset original_h_widget original_l_widget
`
