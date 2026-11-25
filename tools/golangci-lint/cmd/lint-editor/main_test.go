package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gofrs/flock"
)

func TestReportsAccumulateAcrossModulesAndReplaceFixedFindings(t *testing.T) {
	editor, root := fixture(t)
	first := addModule(t, root, "first", unchecked)
	second := addModule(t, root, "second", unchecked)
	nested := filepath.Join(first, "nested")
	write(t, filepath.Join(nested, "value.go"), "package nested\n\nfunc Value() int { return 1 }\n")

	findings := lint(t, editor, nested)
	requireFinding(t, findings, filepath.Join(first, "main.go"), "errcheck")
	if len(findings) != 1 {
		t.Fatalf("first module: got %v", findings)
	}

	findings = lint(t, editor, second)
	if len(findings) != 2 {
		t.Fatalf("both modules: got %v", findings)
	}
	requireFinding(t, findings, filepath.Join(first, "main.go"), "errcheck")
	requireFinding(t, findings, filepath.Join(second, "main.go"), "errcheck")

	// An edit in another module must not trigger analysis of that module.
	write(t, filepath.Join(second, "main.go"), "package fixture\ninvalid Go\n")
	write(t, filepath.Join(first, "main.go"), clean)
	findings = lint(t, editor, nested)
	if len(findings) != 1 {
		t.Fatalf("fixed first module: got %v", findings)
	}
	requireFinding(t, findings, filepath.Join(second, "main.go"), "errcheck")

	write(t, filepath.Join(second, "main.go"), clean)
	if findings := lint(t, editor, second); len(findings) != 0 {
		t.Fatalf("fixed modules: got %v", findings)
	}
}

func TestReportsSurviveFailedRuns(t *testing.T) {
	editor, root := fixture(t)
	module := addModule(t, root, "first", unchecked)
	findings := lint(t, editor, module)
	requireFinding(t, findings, filepath.Join(module, "main.go"), "errcheck")

	write(t, filepath.Join(module, "main.go"), "package fixture\ninvalid Go\n")
	findings = lint(t, editor, module)
	requireFinding(t, findings, filepath.Join(module, "main.go"), "errcheck")
	requireFinding(t, findings, filepath.Join(module, "go.mod"), "lint-editor")
	requireFinding(t, findings, filepath.Join(module, "main.go"), "typecheck")
	for _, finding := range findings {
		if finding.Pos.Filename == filepath.Join(module, "main.go") && finding.Pos.Line > 3 {
			t.Fatalf("retained location exceeds the edited file: %v", finding)
		}
	}

	write(t, filepath.Join(module, "go.mod"), "module example.com/first\n\ngo 1.99.0\n")
	findings = lint(t, editor, module)
	requireFinding(t, findings, filepath.Join(module, "main.go"), "errcheck")
	requireFinding(t, findings, filepath.Join(module, "go.mod"), "lint-editor")

	write(t, filepath.Join(module, "go.mod"), "module example.com/first\n\ngo 1.26.0\n")
	write(t, filepath.Join(module, "main.go"), clean)
	if findings := lint(t, editor, module); len(findings) != 0 {
		t.Fatalf("recovered module: got %v", findings)
	}
}

func TestReportsIncludeCustomAnalyzersAndSurviveFailedRuns(t *testing.T) {
	editor, root := fixture(t)
	module := addModule(t, root, "first", `package fixture

// ID requires construction.
//domain:constructors NewID
type ID string

func NewID(raw string) ID { return ID(raw) }

func Invalid(raw string) ID { return ID(raw) }

func Zero() ID {
	var id ID
	return id
}
`)
	audit := func() []issue {
		t.Helper()
		return lint(t, editor, module, "--enable-only=domainconstruct,domainzero")
	}
	findings := audit()
	requireFinding(t, findings, filepath.Join(module, "main.go"), "domainconstruct")
	requireFinding(t, findings, filepath.Join(module, "main.go"), "domainzero")

	write(t, filepath.Join(module, "main.go"), "package fixture\ninvalid Go\n")
	findings = audit()
	requireFinding(t, findings, filepath.Join(module, "main.go"), "domainconstruct")
	requireFinding(t, findings, filepath.Join(module, "go.mod"), "lint-editor")
	requireFinding(t, findings, filepath.Join(module, "main.go"), "typecheck")
	for _, finding := range findings {
		if finding.Pos.Filename == filepath.Join(module, "main.go") && finding.Pos.Line > 3 {
			t.Fatalf("retained location exceeds the edited file: %v", finding)
		}
	}

	write(t, filepath.Join(module, "go.mod"), "module example.com/first\n\ngo 1.99.0\n")
	findings = audit()
	requireFinding(t, findings, filepath.Join(module, "main.go"), "domainzero")
	requireFinding(t, findings, filepath.Join(module, "go.mod"), "lint-editor")

	write(t, filepath.Join(module, "go.mod"), "module example.com/first\n\ngo 1.26.0\n")
	write(t, filepath.Join(module, "main.go"), clean)
	if findings := audit(); len(findings) != 0 {
		t.Fatalf("recovered module: got %v", findings)
	}
}

func TestChangedConfigurationInvalidatesRetainedReports(t *testing.T) {
	editor, root := fixture(t)
	first := addModule(t, root, "first", unchecked)
	second := addModule(t, root, "second", unchecked)
	_ = lint(t, editor, first)
	_ = lint(t, editor, second)

	config := filepath.Join(root, ".golangci.yaml")
	source, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	write(t, config, string(source)+"\n# Changed lint policy.\n")
	findings := lint(t, editor, first)
	if len(findings) != 1 {
		t.Fatalf("old configuration retained findings: %v", findings)
	}
	requireFinding(t, findings, filepath.Join(first, "main.go"), "errcheck")
}

func TestDeletedSourcesDoNotRetainFindings(t *testing.T) {
	editor, root := fixture(t)
	first := addModule(t, root, "first", unchecked)
	second := addModule(t, root, "second", unchecked)
	_ = lint(t, editor, first)
	_ = lint(t, editor, second)
	if err := os.Remove(filepath.Join(second, "main.go")); err != nil {
		t.Fatal(err)
	}
	findings := lint(t, editor, first)
	if len(findings) != 1 {
		t.Fatalf("deleted file retained findings: %v", findings)
	}
}

func TestWorkspaceLintScansRegisteredGoModules(t *testing.T) {
	editor, root := fixture(t)
	first := addModule(t, root, "first", unchecked)
	second := addModule(t, root, "second", unchecked)
	addModule(t, root, "poc/ignored", "package fixture\ninvalid Go\n")
	findings := lint(t, editor, root)
	if len(findings) != 2 {
		t.Fatalf("workspace: got %v", findings)
	}
	requireFinding(t, findings, filepath.Join(first, "main.go"), "errcheck")
	requireFinding(t, findings, filepath.Join(second, "main.go"), "errcheck")
}

func TestCanceledRunDoesNotChangeRetainedReports(t *testing.T) {
	editor, root := fixture(t)
	module := addModule(t, root, "first", unchecked)
	_ = lint(t, editor, module)
	before, err := os.ReadFile(filepath.Join(editor.cache, "reports.json"))
	if err != nil {
		t.Fatal(err)
	}
	lock := flock.New(filepath.Join(editor.cache, "reports.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Unlock(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = editor.run(ctx, module, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled run: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(editor.cache, "reports.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("canceled run modified retained reports")
	}
}

func TestConcurrentModuleRunsDoNotLoseReports(t *testing.T) {
	editor, root := fixture(t)
	first := addModule(t, root, "first", unchecked)
	second := addModule(t, root, "second", unchecked)
	done := make(chan error, 2)
	for _, module := range []string{first, second} {
		go func() {
			_, err := editor.run(t.Context(), module, nil)
			done <- err
		}()
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	findings := lint(t, editor, first)
	if len(findings) != 2 {
		t.Fatalf("concurrent updates lost a report: %v", findings)
	}
}

func TestCommandAcceptsGoExtensionFlagsAndPublishesDiagnosticLines(t *testing.T) {
	editor, root := fixture(t)
	for _, name := range []string{"golangci-lint", "golangci-lint-editor"} {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(editor.binary), name))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "bin/tools", name)
		write(t, path, string(data))
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	first := addModule(t, root, "first", unchecked)
	second := addModule(t, root, "second", unchecked)
	cmd := exec.CommandContext(t.Context(), filepath.Join(root, "bin/tools/golangci-lint-editor"),
		"run", "--config="+filepath.Join(root, ".golangci.yaml"), "--issues-exit-code=0",
		"--output.text.print-issued-lines=false", "--output.text.path=stdout",
		"--path-mode=abs", "--show-stats=false", "./...")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != 2 {
		t.Fatalf("unexpected diagnostic output: %s", output)
	}
	for _, module := range []string{first, second} {
		pattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(filepath.Join(module, "main.go")) + `:\d+:\d+: .* \(errcheck\)$`)
		if !pattern.Match(output) {
			t.Fatalf("missing diagnostic for %s: %s", module, output)
		}
	}
}

func fixture(t *testing.T) (*editor, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binary, err := filepath.Abs("../../../../bin/tools/golangci-lint")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../../../../.golangci.yaml")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, ".golangci.yaml"), string(source))
	t.Setenv("GOLANGCI_LINT_CACHE", filepath.Join(root, "analysis-cache"))
	return &editor{workspace: root, binary: binary, cache: filepath.Join(root, ".nx/editor")}, root
}

func addModule(t *testing.T, root, name, source string) string {
	t.Helper()
	module := filepath.Join(root, name)
	write(t, filepath.Join(module, "go.mod"), "module example.com/"+filepath.Base(name)+"\n\ngo 1.26.0\n")
	write(t, filepath.Join(module, "project.json"), `{"tags":["type:go-module"]}`)
	write(t, filepath.Join(module, "main.go"), source)
	return module
}

func lint(t *testing.T, editor *editor, cwd string, flags ...string) []issue {
	t.Helper()
	findings, err := editor.run(t.Context(), cwd, flags)
	if err != nil {
		t.Fatal(err)
	}
	return findings
}

func requireFinding(t *testing.T, findings []issue, file, linter string) {
	t.Helper()
	for _, finding := range findings {
		if finding.Pos.Filename == file && finding.FromLinter == linter {
			if strings.Contains(finding.Text, "\n") {
				t.Fatalf("diagnostic must fit one output line: %v", finding)
			}
			return
		}
	}
	t.Fatalf("missing %s finding in %s: %v", linter, file, findings)
}

func write(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

const unchecked = "package fixture\n\nimport \"os\"\n\nfunc Check() { os.Chdir(\".\") }\n"
const clean = "package fixture\n\nfunc Check() {}\n"
