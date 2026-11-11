// Command lint-editor retains complete module reports for VS Code's Go extension.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/gofrs/flock"
)

type editor struct {
	workspace string
	binary    string
	cache     string
}

type reports struct {
	Fingerprint string
	Modules     map[string]moduleReport
}

type moduleReport struct {
	Issues   []issue
	Problems []issue
	Error    string
}

type issue struct {
	FromLinter string
	Text       string
	Pos        token.Position
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	workspace := filepath.Dir(filepath.Dir(filepath.Dir(executable)))
	session := fmt.Sprintf("%x", sha256.Sum256([]byte(os.Getenv("VSCODE_PID"))))[:16]
	editor := &editor{
		workspace: workspace,
		binary:    filepath.Join(filepath.Dir(executable), "golangci-lint"),
		cache:     filepath.Join(workspace, ".nx/workspace-data/go-lint-editor", session),
	}
	args := os.Args[1:]
	if len(args) == 0 || args[0] != "run" {
		cmd := exec.CommandContext(ctx, editor.binary, args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				os.Exit(exit.ExitCode())
			}
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	cwd, err := os.Getwd()
	var findings []issue
	if err == nil {
		findings, err = editor.run(ctx, cwd, args[1:])
	}
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		findings = append(findings, failure(filepath.Join(workspace, ".golangci.yaml"), err))
	}
	// The Go extension clears diagnostics on tool errors. Report failures as
	// diagnostics with a successful protocol exit so previous findings survive.
	for _, finding := range findings {
		fmt.Printf("%s:%d:%d: %s (%s)\n", finding.Pos.Filename,
			max(1, finding.Pos.Line), max(1, finding.Pos.Column),
			oneLine(finding.Text), finding.FromLinter)
	}
}

func (e *editor) run(ctx context.Context, cwd string, args []string) (findings []issue, err error) {
	if err := os.MkdirAll(e.cache, 0o755); err != nil {
		return nil, err
	}
	// Serialize analysis and publication together. A canceled process releases the
	// OS lock, and an older invocation cannot overwrite a newer complete report.
	lock := flock.New(filepath.Join(e.cache, "reports.lock"))
	locked, err := lock.TryLockContext(ctx, 20*time.Millisecond)
	if err != nil || !locked {
		return nil, errors.Join(err, ctx.Err())
	}
	defer func() { err = errors.Join(err, lock.Close()) }()

	flags, config := e.arguments(cwd, args)
	state := reports{Modules: make(map[string]moduleReport)}
	data, err := os.ReadFile(filepath.Join(e.cache, "reports.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(data) != 0 {
		var previous reports
		if err := json.Unmarshal(data, &previous); err != nil {
			return nil, fmt.Errorf("read retained lint reports: %w", err)
		}
		if previous.Modules != nil {
			state = previous
		}
	}
	fingerprint, err := e.fingerprint(config, flags)
	if err != nil {
		return collect(state), err
	}
	if state.Fingerprint != fingerprint {
		state = reports{Fingerprint: fingerprint, Modules: make(map[string]moduleReport)}
	}

	modules, err := e.modules(cwd)
	if err != nil {
		return collect(state), err
	}
	for _, module := range modules {
		result, lintErr := e.lint(ctx, module, flags)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if lintErr != nil {
			previous := state.Modules[module]
			previous.Error = oneLine(lintErr.Error())
			previous.Problems = result.Issues
			state.Modules[module] = previous
		} else {
			state.Modules[module] = result
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	for module := range state.Modules {
		if _, err := os.Stat(filepath.Join(module, "go.mod")); errors.Is(err, os.ErrNotExist) {
			delete(state.Modules, module)
		}
	}
	if err := e.save(state); err != nil {
		return collect(state), err
	}
	return collect(state), nil
}

func (e *editor) lint(ctx context.Context, module string, flags []string) (moduleReport, error) {
	report := filepath.Join(e.cache, "result.json")
	if err := os.Remove(report); err != nil && !errors.Is(err, os.ErrNotExist) {
		return moduleReport{}, err
	}
	args := append([]string{"run"}, flags...)
	args = append(args, "--issues-exit-code=0", "--output.json.path="+report,
		"--output.text.path=stdout", "--show-stats=false", "--path-mode=abs", "./...")
	cmd := exec.CommandContext(ctx, e.binary, args...)
	cmd.Dir = module
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return moduleReport{}, fmt.Errorf("lint could not complete; previous findings retained: %w: %s", err, stderr.String())
	}
	data, err := os.ReadFile(report)
	if err != nil {
		return moduleReport{}, err
	}
	var result moduleReport
	if err := json.Unmarshal(data, &result); err != nil {
		return moduleReport{}, fmt.Errorf("decode linter report: %w", err)
	}
	// With --issues-exit-code=0, package-loading failures can also exit zero.
	// They do not constitute a complete replacement for the last good analysis.
	if slices.ContainsFunc(result.Issues, func(finding issue) bool { return finding.FromLinter == "typecheck" }) {
		return result, errors.New("package loading failed; previous findings retained")
	}
	return result, nil
}

func (e *editor) arguments(cwd string, args []string) ([]string, string) {
	flags := slices.DeleteFunc(slices.Clone(args), func(arg string) bool {
		return arg == "." || arg == "./..." || (!strings.HasPrefix(arg, "-") && strings.HasSuffix(arg, ".go"))
	})
	config := filepath.Join(e.workspace, ".golangci.yaml")
	for i, arg := range flags {
		switch {
		case strings.HasPrefix(arg, "--config="):
			config = strings.TrimPrefix(arg, "--config=")
		case (arg == "--config" || arg == "-c") && i+1 < len(flags):
			config = flags[i+1]
		}
	}
	if !filepath.IsAbs(config) {
		config = filepath.Join(cwd, config)
	}
	return append(flags, "--config="+config), config
}

func (e *editor) fingerprint(config string, flags []string) (string, error) {
	hash := sha256.New()
	for _, path := range []string{e.binary, config} {
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(hash, file)
		if err := errors.Join(copyErr, file.Close()); err != nil {
			return "", err
		}
	}
	environment := make(map[string]string)
	for _, name := range []string{"GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOEXPERIMENT"} {
		environment[name] = os.Getenv(name)
	}
	if err := json.NewEncoder(hash).Encode(struct {
		Version     int
		Flags       []string
		Environment map[string]string
	}{1, flags, environment}); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func (e *editor) modules(cwd string) ([]string, error) {
	for directory := cwd; filepath.IsLocal(relative(e.workspace, directory)); directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return []string{directory}, nil
		}
		if directory == e.workspace {
			break
		}
	}
	if cwd != e.workspace {
		return nil, fmt.Errorf("no Go module contains %s", cwd)
	}
	var modules []string
	err := filepath.WalkDir(e.workspace, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		if path != e.workspace && (strings.HasPrefix(entry.Name(), ".") || slices.Contains(
			[]string{"node_modules", "vendor", "bin", "dist", "testdata", "generated", "poc"}, entry.Name())) {
			return fs.SkipDir
		}
		data, err := os.ReadFile(filepath.Join(path, "project.json"))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		var project struct{ Tags []string }
		if err := json.Unmarshal(data, &project); err != nil {
			return err
		}
		if slices.Contains(project.Tags, "type:go-module") {
			modules = append(modules, path)
		}
		return nil
	})
	return modules, err
}

func (e *editor) save(state reports) (err error) {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(e.cache, ".reports-*")
	if err != nil {
		return err
	}
	defer func() {
		if removeErr := os.Remove(file.Name()); !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(e.cache, "reports.json"))
}

func collect(state reports) []issue {
	var findings []issue
	lines := make(map[string][][]byte)
	for module, report := range state.Modules {
		for _, finding := range append(slices.Clone(report.Issues), report.Problems...) {
			if _, ok := lines[finding.Pos.Filename]; !ok {
				source, err := os.ReadFile(finding.Pos.Filename)
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err == nil {
					lines[finding.Pos.Filename] = bytes.Split(source, []byte("\n"))
				}
			}
			if source := lines[finding.Pos.Filename]; len(source) != 0 {
				// Retained positions may outlive edits that remove their source lines.
				finding.Pos.Line = min(max(1, finding.Pos.Line), len(source))
				finding.Pos.Column = min(max(1, finding.Pos.Column), len(source[finding.Pos.Line-1])+1)
			}
			finding.Text = oneLine(finding.Text)
			findings = append(findings, finding)
		}
		if report.Error != "" {
			findings = append(findings, failure(filepath.Join(module, "go.mod"), errors.New(report.Error)))
		}
	}
	slices.SortFunc(findings, func(a, b issue) int {
		return strings.Compare(a.Pos.String()+a.FromLinter+a.Text, b.Pos.String()+b.FromLinter+b.Text)
	})
	return findings
}

func failure(file string, err error) issue {
	return issue{FromLinter: "lint-editor", Text: oneLine(err.Error()), Pos: token.Position{Filename: file, Line: 1, Column: 1}}
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func relative(root, path string) string {
	result, err := filepath.Rel(root, path)
	if err != nil {
		return ".."
	}
	return result
}
