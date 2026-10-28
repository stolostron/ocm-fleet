/* eslint playwright/expect-expect: ["error", { "assertFunctionNames": ["doesNotReject", "equal", "match", "rejects"] }] */
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
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

test("reports unchecked production errors with a failing exit status", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(join(directory, "main.go"), uncheckedError);
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(
      error.stdout + error.stderr,
      /main\.go.*os\.Chdir.*not checked.*errcheck/,
    );
    return true;
  });
});

test("reports same-package domain field reads through the custom analyzer", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeDomainFixture(directory, "f.generation");
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(
      error.stdout + error.stderr,
      /access Fulfillment\.generation through Fulfillment methods.*fleetdomain/,
    );
    return true;
  });
});

test("allows the domain's own methods and external getter calls", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeDomainFixture(directory, "f.Generation()");
  await assert.doesNotReject(lint());
});

test("reports unchecked Close errors in production code", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(
    join(directory, "main.go"),
    'package lintfixture\n\nimport "io"\n\nfunc Close(resource io.Closer) {\n\tresource.Close()\n}\n',
  );
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(error.stdout + error.stderr, /main\.go.*Close.*errcheck/);
    return true;
  });
});

test("reports unchecked errors from our own flush methods", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(
    join(directory, "main.go"),
    'package lintfixture\n\nimport "os"\n\ntype writer struct{}\n\nfunc (writer) flush() error {\n\treturn os.Chdir(".")\n}\n\nfunc Flush() {\n\tw := writer{}\n\tw.flush()\n}\n',
  );
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(error.stdout + error.stderr, /main\.go.*flush.*errcheck/);
    return true;
  });
});

test("reports Staticcheck findings in production code", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(join(directory, "main.go"), invalidRegexp);
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(error.stdout + error.stderr, /main\.go.*SA1000/);
    return true;
  });
});

test("reports ineffective breaks that could hide a loop-control bug", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(
    join(directory, "main.go"),
    "package lintfixture\n\nfunc Sum(values []int) int {\n\tvar total int\n\tfor _, value := range values {\n\t\tswitch value {\n\t\tcase 0:\n\t\t\tbreak\n\t\t}\n\t\ttotal += value\n\t}\n\treturn total\n}\n",
  );
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(error.stdout + error.stderr, /main\.go.*SA4011/);
    return true;
  });
});

test("reports Staticcheck findings in tests", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(
    join(directory, "main_test.go"),
    'package lintfixture\n\nimport (\n\t"regexp"\n\t"testing"\n)\n\nfunc TestPattern(t *testing.T) {\n\tregexp.MustCompile("[")\n}\n',
  );
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(error.stdout + error.stderr, /main_test\.go.*SA1000/);
    return true;
  });
});

test("reports Go vet findings in tests", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(
    join(directory, "main_test.go"),
    'package lintfixture\n\nimport (\n\t"fmt"\n\t"testing"\n)\n\nfunc TestFormat(t *testing.T) {\n\tfmt.Printf("%d", "wrong format")\n}\n',
  );
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(error.stdout + error.stderr, /main_test\.go.*govet/);
    return true;
  });
});

test("reports Go vet warnings about unsafe pointer conversions", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(
    join(directory, "main.go"),
    'package lintfixture\n\nimport "unsafe"\n\nfunc Pointer(value uintptr) unsafe.Pointer {\n\treturn unsafe.Pointer(value)\n}\n',
  );
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(
      error.stdout + error.stderr,
      /main\.go.*possible misuse of unsafe\.Pointer.*govet/,
    );
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

test("allows best-effort deferred transaction rollback in tests", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(join(directory, "main_test.go"), rollbackTest);
  await assert.doesNotReject(lint());
});

test("reports unchecked rollback outside deferred test cleanup", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(
    join(directory, "main_test.go"),
    rollbackTest.replace("defer tx.Rollback()", "tx.Rollback()"),
  );
  await assert.rejects(lint(), (error) => {
    assert.equal(error.code, 1);
    assert.match(
      error.stdout + error.stderr,
      /main_test\.go.*Rollback.*errcheck/,
    );
    return true;
  });
});

test("excludes generated code with the standard header", async (t) => {
  const { directory, lint } = await fixture(t);
  await writeFile(
    join(directory, "generated.go"),
    "// Code generated by lint fixture. DO NOT EDIT.\n\n" +
      uncheckedError.replace("Check()", "Generated()"),
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
      /File is not properly formatted \((?:gci|gofmt|goimports)\)/,
    );
    return true;
  });
  assert.equal(await readFile(path, "utf8"), source);
});

// Real modules exercise config paths, formatter checks, exclusions, and exit
// statuses without third-party dependencies or running the fixture's Go code.
async function fixture(t) {
  const directory = await mkdtemp(join(tmpdir(), "ocm-fleet-lint-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
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
    lint: () =>
      run(
        binary,
        ["run", "--config", config, "--allow-serial-runners", "./..."],
        {
          cwd: directory,
          env: { ...process.env, GOWORK: "off" },
        },
      ),
  };
}

const uncheckedError =
  'package lintfixture\n\nimport "os"\n\nfunc Check() {\n\tos.Chdir(".")\n}\n';

async function writeDomainFixture(directory, expression) {
  await writeFile(
    join(directory, "go.mod"),
    "module github.com/stolostron/ocm-fleet/server/internal/domain\n\ngo 1.26.0\n",
  );
  await writeFile(
    join(directory, "main.go"),
    `package domain

type Fulfillment struct{ generation int }

func (f *Fulfillment) Generation() int { return f.generation }

func Read(f *Fulfillment) int {
\treturn ${expression}
}
`,
  );
}

const invalidRegexp =
  'package lintfixture\n\nimport "regexp"\n\nfunc Pattern() *regexp.Regexp {\n\treturn regexp.MustCompile("[")\n}\n';

const rollbackTest =
  'package lintfixture\n\nimport (\n\t"errors"\n\t"testing"\n)\n\ntype transaction struct{}\n\nfunc (transaction) Rollback() error {\n\treturn errors.New("already closed")\n}\n\nfunc TestRollback(t *testing.T) {\n\ttx := transaction{}\n\tdefer tx.Rollback()\n}\n';
