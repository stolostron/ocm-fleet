// Command golangci-lint runs upstream linters from an isolated tools module.
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
)

func main() {
	// Package loading must use the compiler that built the linter. A newer
	// host Go can expose standard-library syntax this binary cannot parse, and
	// changes config verification's Go-version fallback outside a module.
	goVersion := runtime.Version()
	if err := os.Setenv("GOTOOLCHAIN", goVersion); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitcodes.Failure)
	}

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
	if err := commands.Execute(commands.BuildInfo{Version: version, GoVersion: goVersion}); err != nil {
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
