// Command format-go applies the shared formatter policy with batched goimports.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/golangci/golangci-lint/v2/pkg/config"
	"github.com/golangci/golangci-lint/v2/pkg/goformat"
	"github.com/golangci/golangci-lint/v2/pkg/goformatters/gofmt"
	"github.com/golangci/golangci-lint/v2/pkg/result/processors"
	"github.com/spf13/viper"
)

func main() {
	configPath := flag.String("config", ".golangci.yaml", "Shared golangci-lint configuration")
	flag.Parse()
	if err := run(*configPath, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(configPath string, paths []string) error {
	v := viper.New()
	v.SetConfigFile(configPath)
	if err := v.ReadInConfig(); err != nil {
		return err
	}
	cfg := config.NewDefault()
	if err := v.Unmarshal(cfg); err != nil {
		return err
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	cfg.SetConfigDir(filepath.Dir(abs))
	// Fail explicitly if policy grows beyond the formatters this command supports.
	for _, name := range cfg.Formatters.Enable {
		if name != "gofmt" && name != "goimports" {
			return fmt.Errorf("unsupported formatter %q", name)
		}
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	// Match golangci-lint's compiler selection, including goimports' Go subprocesses.
	if err := os.Setenv("GOTOOLCHAIN", runtime.Version()); err != nil {
		return err
	}
	return formatFiles(cfg, paths, filepath.Join(filepath.Dir(executable), "goimports"))
}

func formatFiles(cfg *config.Config, paths []string, goimports string) error {
	opts, err := goformat.NewRunnerOptions(cfg, false, false, false)
	if err != nil {
		return err
	}
	matcher := processors.NewGeneratedFileMatcher(cfg.Formatters.Exclusions.Generated)
	formatter := gofmt.New(&cfg.Formatters.Settings.GoFmt)
	var files []string
	for _, root := range paths {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			name := entry.Name()
			// Keep directory selection consistent with golangci-lint fmt.
			if entry.IsDir() {
				if name == "vendor" || name == "testdata" || name == "node_modules" || (strings.HasPrefix(name, ".") && name != ".") {
					return fs.SkipDir
				}
				return nil
			}
			if !entry.Type().IsRegular() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".go") {
				return nil
			}
			excluded, err := opts.MatchAnyPattern(path)
			if err != nil || excluded {
				return err
			}
			source, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			generated, err := matcher.IsGeneratedFile(path, source)
			if err != nil || generated {
				return err
			}
			if slices.Contains(cfg.Formatters.Enable, "gofmt") {
				formatted, err := formatter.Format(path, source)
				if err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				if !bytes.Equal(source, formatted) {
					info, err := entry.Info()
					if err != nil {
						return err
					}
					if err := os.WriteFile(path, formatted, info.Mode().Perm()); err != nil {
						return err
					}
				}
			}
			// Absolute paths cannot be interpreted as command-line flags.
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			files = append(files, abs)
			return nil
		})
		if err != nil {
			return err
		}
	}
	if !slices.Contains(cfg.Formatters.Enable, "goimports") {
		return nil
	}
	// The official CLI reuses its import resolver across files. golangci-lint's
	// per-file imports.Process calls create a new resolver for each file instead.
	// Bound argument size while retaining that reuse, including on macOS.
	for len(files) > 0 {
		count, size := 0, 0
		for count < len(files) && size+len(files[count]) < 64*1024 {
			size += len(files[count]) + 1
			count++
		}
		args := []string{"-w", "-local", strings.Join(cfg.Formatters.Settings.GoImports.LocalPrefixes, ",")}
		cmd := exec.Command(goimports, append(args, files[:count]...)...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("goimports: %w", err)
		}
		files = files[count:]
	}
	return nil
}
