import { spawn } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { basename, dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// The Go analyzer uses native related locations. Its golangci-lint adapter keeps
// them within one issue because upstream otherwise emits independent findings.
// Decode explicit records here, after normal exclusions and nolint rules. Never
// derive a rule, certainty, or reason from the English headline or explanation.
export function domainFinding(issue) {
  if (!issue.FromLinter?.startsWith("domain")) return undefined;
  const [message, metadata = "", context = "", ...records] = issue.Text.split(
    /\s+\| (?=[a-z][a-z-]*: )/,
  );
  const identity = metadata
    .trim()
    .match(
      /^rule: ([a-z][a-z0-9-]*\.[a-z][a-z0-9-]*); certainty: (known|possible); reason: ([a-z][a-z0-9-]*)$/,
    );
  const quoted = '"(?:[^"\\\\]|\\\\.)*"';
  const fields = context
    .trim()
    .match(
      new RegExp(
        `^operation: ([a-z][a-z-]*); value: (${quoted}); type: (${quoted}); field-path: (${quoted})$`,
      ),
    );
  if (!identity || !fields || !identity[1].startsWith(issue.FromLinter + "."))
    return undefined;
  const provenance = [];
  try {
    for (const record of records) {
      const match = record
        .trim()
        .match(
          new RegExp(
            `^([a-z][a-z-]*): (.*) \\((${quoted}), line (\\d+), column (\\d+)\\)$`,
          ),
        );
      if (!match) return undefined;
      provenance.push({
        Kind: match[1],
        Pos: {
          Filename: JSON.parse(match[3]),
          Line: Number(match[4]),
          Column: Number(match[5]),
        },
        Message: match[2].replace(
          /\\([\\|nr])/g,
          (_, escape) => ({ n: "\n", r: "\r" })[escape] ?? escape,
        ),
      });
    }
    const [rule, certainty, reason] = identity.slice(1);
    const [operation, value, type, fieldPath] = fields.slice(1);
    const category =
      rule === "domainzero.invalid-zero"
        ? `${certainty}-zero`
        : rule === "domainzero.zero-comparison"
          ? "zero-comparison"
          : issue.FromLinter === "domainconstruct"
            ? "construction"
            : issue.FromLinter === "domainserialization"
              ? "serialization"
              : "contract";
    return {
      Rule: rule,
      Certainty: certainty,
      Operation: operation,
      Value: JSON.parse(value),
      Type: JSON.parse(type),
      FieldPath: JSON.parse(fieldPath),
      Reason: reason,
      Pos: issue.Pos,
      Category: category,
      Message: message.trim(),
      Provenance: provenance,
    };
  } catch (error) {
    if (error instanceof SyntaxError) return undefined;
    throw error;
  }
}

export async function writeDomainReports({
  workspace,
  moduleRoot = ".",
  reportPath,
  report,
}) {
  const generatedAt = new Date().toISOString();
  const moduleDirectory = resolve(workspace, moduleRoot);
  const position = (pos) => ({
    ...pos,
    Filename: resolve(moduleDirectory, pos.Filename),
  });
  const enriched = {
    ...report,
    GeneratedAt: generatedAt,
    Issues: (report.Issues ?? []).map((issue) => {
      const domain = domainFinding(issue);
      const pos = position(issue.Pos);
      if (domain) {
        domain.Pos = pos;
        domain.Provenance = domain.Provenance.map((step) => ({
          ...step,
          Pos: position(step.Pos),
        }));
      }
      return { ...issue, Pos: pos, Domain: domain };
    }),
  };
  const testCode = (issue) => {
    const path = relative(workspace, issue.Pos.Filename).replaceAll("\\", "/");
    return (
      path.endsWith("_test.go") ||
      /(?:^|\/)(?:testdata|testutil|testserver|sqltxtest|oidctest)(?:\/|$)/.test(
        path,
      ) ||
      /\/domain\/[^/]+test\//.test(path) ||
      /\/infrastructure\/(?:postgres|sqlite)\/testdb\.go$/.test(path)
    );
  };
  const counts = (issues) =>
    ["domainconstruct", "domainzero", "domainserialization"].map(
      (name) => issues.filter((issue) => issue.FromLinter === name).length,
    );
  const file = (suffix) => reportPath.replace(/\.json$/, suffix);
  const link = (path, label = relative(workspace, path)) => {
    const target = relative(dirname(reportPath), path).replaceAll("\\", "/");
    return `[${label}](${target.split("/").map(encodeURIComponent).join("/")})`;
  };
  const location = (pos) =>
    `${link(pos.Filename)} (line ${pos.Line}, column ${pos.Column})`;
  const escape = (text) => text.replaceAll("|", "\\|").replaceAll("\n", " ");
  const scopes = [
    {
      name: "main",
      title: "main code",
      issues: enriched.Issues.filter((issue) => !testCode(issue)),
    },
    {
      name: "test",
      title: "test code",
      issues: enriched.Issues.filter(testCode),
    },
  ];
  await mkdir(dirname(reportPath), { recursive: true });
  await writeFile(reportPath, JSON.stringify(enriched, null, 2) + "\n");
  for (const scope of scopes) {
    await writeFile(
      file(`-${scope.name}.json`),
      JSON.stringify({ ...enriched, Issues: scope.issues }, null, 2) + "\n",
    );
    const [construction, zero, serialization] = counts(scope.issues);
    const lines = [
      `# Domain audit — ${scope.title}`,
      "",
      `Generated ${generatedAt}. ${scope.issues.length} findings: ${construction} construction, ${zero} zero-value, ${serialization} serialization.`,
      "",
      `[Summary](${basename(file(".md"))}) · [Structured JSON](${basename(file(`-${scope.name}.json`))})`,
      "",
    ];
    for (const issue of scope.issues) {
      lines.push(
        `- ${location(issue.Pos)} — ${issue.Domain?.Rule ?? issue.FromLinter}, ${issue.Domain?.Category ?? "finding"}: ${escape(issue.Domain?.Message ?? issue.Text)}`,
        "",
      );
      if (issue.Domain?.Provenance.length) {
        lines.push(
          "  <details><summary>Provenance</summary>",
          "",
          "  | Step | Location | Detail |",
          "  | --- | --- | --- |",
        );
        for (const step of issue.Domain.Provenance)
          lines.push(
            `  | ${step.Kind} | ${location(step.Pos)} | ${escape(step.Message)} |`,
          );
        lines.push("", "  </details>", "");
      }
    }
    await writeFile(file(`-${scope.name}.md`), lines.join("\n"));
  }
  const summary = [
    "# Domain audit",
    "",
    `Generated ${generatedAt}.`,
    "",
    "| Scope | Construction | Zero-value | Serialization | Total |",
    "| --- | ---: | ---: | ---: | ---: |",
    ...scopes.map((scope) => {
      const [construction, zero, serialization] = counts(scope.issues);
      return `| [${scope.title}](${basename(file(`-${scope.name}.md`))}) | ${construction} | ${zero} | ${serialization} | ${scope.issues.length} |`;
    }),
    "",
    `[Combined structured JSON](${basename(reportPath)})`,
    "",
    "Test scope includes Go test files, domain repository contract packages, and test support packages. Other files count as main code, including mixed-purpose files.",
    "",
    "Known-zero and possible-zero findings describe flow evidence; zero-comparison findings enforce the optionality convention. Construction and serialization contracts remain separate checks. Provenance lists the traced source, transformations, and use; it is not a complete execution trace.",
    "",
  ];
  await writeFile(file(".md"), summary.join("\n"));
  return enriched;
}

async function runDomainAudit(project, moduleRoot) {
  if (!project || !moduleRoot)
    throw new Error("Provide an Nx project name and module root");
  const workspace = fileURLToPath(new URL("../", import.meta.url));
  const reportPath = join(
    workspace,
    ".nx/reports/domainconstruct",
    `${project}.json`,
  );
  await mkdir(dirname(reportPath), { recursive: true });
  const temporary = await mkdtemp(join(dirname(reportPath), ".audit-"));
  try {
    const raw = join(temporary, "issues.json");
    const child = spawn(
      join(workspace, "bin/tools/golangci-lint"),
      [
        "run",
        "--enable-only=domainconstruct,domainzero,domainserialization",
        "--config",
        join(workspace, ".golangci.yaml"),
        `--output.json.path=${raw}`,
        "./...",
      ],
      {
        cwd: resolve(workspace, moduleRoot),
        env: { ...process.env, GOWORK: "off" },
        stdio: "inherit",
      },
    );
    const status = await new Promise((done, reject) => {
      child.once("error", reject);
      // A terminated process has no complete findings report to publish.
      child.once("close", (code) => done(code ?? 2));
    });
    if (status === 0 || status === 1) {
      await writeDomainReports({
        workspace,
        moduleRoot,
        reportPath,
        report: JSON.parse(await readFile(raw, "utf8")),
      });
    }
    return status;
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  try {
    process.exitCode = await runDomainAudit(...process.argv.slice(2));
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
