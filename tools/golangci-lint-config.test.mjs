import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { cp, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const run = promisify(execFile);
const binary = fileURLToPath(
  new URL("../bin/tools/golangci-lint", import.meta.url),
);
const config = fileURLToPath(new URL("../.golangci.yaml", import.meta.url));

test("identifies the exact custom executable in its version for analysis caching", async () => {
  const digest = createHash("sha256")
    .update(await readFile(binary))
    .digest("hex");
  const { stdout } = await run(binary, ["version"]);
  assert.match(
    stdout,
    new RegExp(`version v?2\\.\\d+\\.\\d+-fleet\\.${digest}\\b`),
  );
});

test("validates the shared config with our pinned linter", async () => {
  await assert.doesNotReject(
    run(binary, ["config", "verify", "--config", config]),
  );
});

test("accepts formatted Go code with checked errors", async (t) => {
  const { lint } = await fixture(t);
  await assert.doesNotReject(lint());
});

test("formatting preserves registration comments and produces lint-clean, stable imports", async (t) => {
  const { directory, lint } = await fixture(t);
  // A local dependency reproduces third-party import grouping without downloads.
  await writeFile(
    join(directory, "go.mod"),
    "module github.com/stolostron/ocm-fleet/lintfixture\n\ngo 1.26.0\n\nrequire example.com/dependency v0.0.0\n\nreplace example.com/dependency => ./dependency\n",
  );
  await mkdir(join(directory, "dependency"));
  await writeFile(
    join(directory, "dependency/go.mod"),
    "module example.com/dependency\n\ngo 1.26.0\n",
  );
  for (const name of ["first", "parser", "second", "registration"]) {
    await mkdir(join(directory, "dependency", name));
    await writeFile(
      join(directory, "dependency", name, "value.go"),
      `package ${name}\n\nfunc Value(value int) int { return value }\n`,
    );
  }
  const path = join(directory, "main.go");
  // Two third-party imports before the registration comment trigger the conflict.
  await writeFile(
    path,
    `package lintfixture

import (
    "fmt"
    "os"

    "example.com/dependency/first"
    "example.com/dependency/parser"
    // Register custom handlers for their initialization side effects.
    _ "example.com/dependency/registration"
    "example.com/dependency/second"
)

func Check(value int) string {return fmt.Sprint(first.Value(value) + parser.Value(value) + second.Value(value))}

func Label(value int) string {return strconv.Itoa(value)}
`,
  );
  const format = () =>
    run(binary, ["fmt", "--config", config, "./..."], {
      cwd: directory,
      env: { ...process.env, GOWORK: "off" },
    });

  await format();
  const formatted = await readFile(path, "utf8");
  assert.match(formatted, /\/\/ Register custom handlers/);
  assert.match(formatted, /"strconv"/);
  assert.equal(formatted.includes('"os"'), false);
  await assert.doesNotReject(
    lint().catch((error) => {
      t.diagnostic(error.stdout + error.stderr);
      throw error;
    }),
  );
  await format();
  assert.equal(await readFile(path, "utf8"), formatted);
});

test("loads packages with its build toolchain despite an ambient override", async (t) => {
  const { lint } = await fixture(t);
  await assert.doesNotReject(
    lint({ GOTOOLCHAIN: "invalid-ambient-toolchain" }),
  );
});

test("still rejects modules requiring a newer Go version than the linter supports", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(
    join(directory, "go.mod"),
    "module lintfixture\n\ngo 1.99.0\n",
  );
  await assert.rejects(lint({ GOTOOLCHAIN: "local" }), (error) => {
    assert.match(
      error.stdout + error.stderr,
      /used to build golangci-lint is lower than the targeted Go version \(1\.99\.0\)/,
    );
    return true;
  });
});

test("reports unchecked production errors with a failing exit status and absolute paths", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(join(directory, "main.go"), uncheckedError);
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(
      error.stdout + error.stderr,
      /main\.go.*os\.Chdir.*not checked.*errcheck/,
    );
    assert.match(error.stdout + error.stderr, /^\/[^\n]*\/main\.go:\d+:\d+:/m);
    return true;
  });
});

test("counts post-increments as reads while still reporting unused declarations", async (t) => {
  const { directory, lint } = await fixture(t);
  const path = join(directory, "main.go");
  const source =
    "package lintfixture\n\nfunc Check(limit int) {\n\tfor counter := 0; limit > 0; counter++ {\n\t\tlimit--\n\t}\n}\n";
  await writeFile(path, source);
  await assert.doesNotReject(lint());

  await writeFile(path, source + "\nvar unusedVariable int\n");
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(error.stdout + error.stderr, /unusedVariable.*unused/);
    return true;
  });
});

test("reports unchecked operation errors in tests", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(
    join(directory, "main_test.go"),
    'package lintfixture\n\nimport (\n\t"os"\n\t"testing"\n)\n\nfunc TestCheck(t *testing.T) {\n\tos.Chdir(".")\n}\n',
  );
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(
      error.stdout + error.stderr,
      /main_test\.go.*os\.Chdir.*errcheck/,
    );
    return true;
  });
});

test("temporary cleanup exclusions require both an existing project and a matching operation", async (t) => {
  const existing = await fixture(t, { project: "server" });
  const close =
    'package lintfixture\n\nimport "io"\n\nfunc Close(resource io.Closer) {\n\tresource.Close()\n}\n';
  await writeFile(join(existing.directory, "main.go"), close);
  await assert.doesNotReject(existing.lint());

  await writeFile(
    join(existing.directory, "main.go"),
    close.replace('import "io"', 'import (\n\t"io"\n\t"os"\n)') +
      '\nfunc ChangeDirectory() {\n\tos.Chdir(".")\n}\n',
  );
  await assert.rejects(existing.lint(), { code: 1 });
  const issues = await existing.issues();
  assert.equal(issues.length, 1);
  assert.match(issues[0].Text, /os\.Chdir/);

  const added = await fixture(t, { project: "another-go-module" });
  await writeFile(join(added.directory, "main.go"), close);
  await assert.rejects(added.lint(), { code: 1 });
  assert.match((await added.issues())[0].Text, /Close.*not checked/);
});

test("temporary test exclusions leave bug checks active", async (t) => {
  const { directory, lint, issues } = await fixture(t, { project: "server" });
  const path = join(directory, "main_test.go");
  const source =
    'package lintfixture\n\nimport (\n\t"os"\n\t"testing"\n)\n\nfunc TestCheck(t *testing.T) {\n\tos.Chdir(".")\n}\n';
  await writeFile(path, source);
  await assert.doesNotReject(lint());

  await writeFile(
    path,
    source
      .replace('"os"', '"os"\n\t"regexp"')
      .replace('os.Chdir(".")', 'os.Chdir(".")\n\tregexp.MustCompile("[")'),
  );
  await assert.rejects(lint(), { code: 1 });
  const findings = await issues();
  assert.equal(findings.length, 1);
  assert.equal(findings[0].FromLinter, "staticcheck");
  assert.match(findings[0].Text, /SA1000/);
});

test("inventory decoding exclusions leave other errors in the same file active", async (t) => {
  const { directory, lint, issues } = await fixture(t, {
    project: "server/internal/infrastructure/sqlite",
  });
  const path = join(directory, "extension_resource_repo.go");
  const source =
    'package lintfixture\n\nimport "encoding/json"\n\nfunc Decode(data []byte) map[string]string {\n\tlabels := map[string]string{}\n\tjson.Unmarshal(data, &labels)\n\treturn labels\n}\n';
  await writeFile(path, source);
  await assert.doesNotReject(lint());

  await writeFile(
    path,
    source
      .replace(
        'import "encoding/json"',
        'import (\n\t"encoding/json"\n\t"os"\n)',
      )
      .replace("return labels", 'os.Chdir(".")\n\treturn labels'),
  );
  await assert.rejects(lint(), { code: 1 });
  const findings = await issues();
  assert.equal(findings.length, 1);
  assert.equal(findings[0].FromLinter, "errcheck");
  assert.match(findings[0].Text, /os\.Chdir/);
});

test("excludes generated code only with the standard header", async (t) => {
  const { directory, lint } = await fixture(t);
  const path = join(directory, "generated.go");
  const source = uncheckedError.replace("Check()", "Generated()");
  for (const header of ["", "// Code generated by lint fixture.\n\n"]) {
    await writeFile(path, header + source);
    await assert.rejects(lint(), (error) => {
      assert.equal(error.code, 1);
      assert.match(
        error.stdout + error.stderr,
        /generated\.go.*os\.Chdir.*errcheck/,
      );
      return true;
    });
  }
  await writeFile(
    path,
    "// Code generated by lint fixture. DO NOT EDIT.\n\n" + source,
  );
  await assert.doesNotReject(lint());
});

test("reports formatting issues without modifying sources", async (t) => {
  const { directory, lint } = await fixture(t);
  const path = join(directory, "main.go");
  const source = "package lintfixture\n\nfunc Check() int {return 1}\n";
  await writeFile(path, source);
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(
      error.stdout + error.stderr,
      /File is not properly formatted \((?:gofmt|goimports)\)/,
    );
    return true;
  });
  assert.equal(await readFile(path, "utf8"), source);
});

// Real modules exercise config paths, formatter checks, exclusions, and exit
// statuses without third-party dependencies or running the fixture's Go code.
async function fixture(t, { project = "" } = {}) {
  const root = await mkdtemp(join(tmpdir(), "ocm-fleet-lint-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const directory = join(root, project);
  await mkdir(directory, { recursive: true });
  const configuration = join(root, ".golangci.yaml");
  const report = join(root, "issues.json");
  await cp(config, configuration);
  await writeFile(
    join(directory, "go.mod"),
    "module github.com/stolostron/ocm-fleet/lintfixture\n\ngo 1.26.0\n",
  );
  await writeFile(
    join(directory, "main.go"),
    'package lintfixture\n\nimport "os"\n\nfunc Check() error {\n\treturn os.Chdir(".")\n}\n',
  );
  return {
    directory,
    issues: async () => JSON.parse(await readFile(report, "utf8")).Issues ?? [],
    lint: (env = {}) =>
      run(
        binary,
        [
          "run",
          "--config",
          configuration,
          "--output.text.path=stdout",
          `--output.json.path=${report}`,
          "./...",
        ],
        {
          cwd: directory,
          env: { ...process.env, ...env, GOWORK: "off" },
        },
      ),
  };
}

const uncheckedError =
  'package lintfixture\n\nimport "os"\n\nfunc Check() {\n\tos.Chdir(".")\n}\n';
