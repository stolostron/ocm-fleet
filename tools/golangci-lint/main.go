// Command golangci-lint runs the upstream linters and OCM Fleet's module plugins.
package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/golangci/golangci-lint/v2/pkg/commands"
	"github.com/golangci/golangci-lint/v2/pkg/exitcodes"

	// Importing the adapter registers our analyzers with golangci-lint.
	_ "github.com/stolostron/ocm-fleet/tools/golangci-lint/plugin"
)

func main() {
	// Config verification needs the upstream version, not this module's version.
	version := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dependency := range info.Deps {
			if dependency.Path == "github.com/golangci/golangci-lint/v2" {
				version = dependency.Version
				break
			}
		}
	}
	identity, err := executableHash()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitcodes.Failure)
	}
	// Upstream uses this version as its analysis-cache salt. The executable hash
	// invalidates cached findings when our analyzers, dependencies, or compiler
	// change, while the upstream prefix keeps config verification working.
	version += "-fleet." + identity
	if err := commands.Execute(commands.BuildInfo{Version: version, GoVersion: runtime.Version()}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitcodes.Failure)
	}
}

func executableHash() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	binary, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		// The executable is read-only; closing it has no buffered writes to flush.
		_ = binary.Close()
	}()
	hash := sha256.New()
	if _, err := io.Copy(hash, binary); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
