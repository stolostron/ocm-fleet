import { readdirSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

import ts from "typescript";
import { describe, expect, it } from "vitest";

import * as shell from "../index";

const packageRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const sourceRoot = join(packageRoot, "src");

function sourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    if (entry.name === "__tests__") return [];
    const path = join(directory, entry.name);
    return entry.isDirectory() ? sourceFiles(path) : [path];
  });
}

describe("shell package boundary", () => {
  it("keeps React and React DOM versions compatible in browser workspaces", () => {
    const workspaceRoot = resolve(packageRoot, "../..");
    for (const project of [
      "client/web",
      "extensions/core/client",
      "extensions/kind/client",
      "sdk/shell",
    ]) {
      const requireFromProject = createRequire(
        join(workspaceRoot, project, "package.json"),
      );
      const react = requireFromProject("react/package.json") as {
        version: string;
      };
      const reactDom = requireFromProject("react-dom/package.json") as {
        version: string;
      };
      expect(reactDom.version, project).toBe(react.version);
    }
  });

  it("emits public declarations without Scalprum types", () => {
    const configFile = ts.readConfigFile(
      join(packageRoot, "tsconfig.build.json"),
      ts.sys.readFile,
    );
    const parsed = ts.parseJsonConfigFileContent(
      configFile.config,
      ts.sys,
      packageRoot,
    );
    const program = ts.createProgram(parsed.fileNames, {
      ...parsed.options,
      emitDeclarationOnly: true,
    });
    const declarations = new Map<string, string>();
    const result = program.emit(undefined, (name, text) => {
      if (name.endsWith(".d.ts")) declarations.set(name, text);
    });
    expect(
      ts.getPreEmitDiagnostics(program).concat(result.diagnostics),
    ).toEqual([]);
    for (const name of ["index.d.ts", "provider.d.ts", "types.d.ts"]) {
      const declaration = [...declarations].find(([path]) =>
        path.endsWith(`/${name}`),
      )?.[1];
      expect(declaration).toBeDefined();
      expect(declaration).not.toMatch(/@scalprum|Scalprum/);
    }
  });
  it("exports only the host-facing runtime API", () => {
    expect(Object.keys(shell).sort()).toEqual([
      "ConsoleProvider",
      "useConsoleConfig",
    ]);
  });

  it("declares only its root export and portable peer dependencies", () => {
    const manifest = JSON.parse(
      readFileSync(join(packageRoot, "package.json"), "utf8"),
    );
    expect(manifest.name).toBe("@ocm-fleet/shell");
    expect(manifest.version).toBe("0.0.0");
    expect(manifest.private).toBe(true);
    expect(Object.keys(manifest.exports)).toEqual(["."]);
    expect(manifest.exports["."]).toEqual({
      types: "./dist/esm/index.d.ts",
      import: "./dist/esm/index.js",
      require: "./dist/cjs/index.js",
    });
    expect(Object.keys(manifest.dependencies).sort()).toEqual([
      "@openshift/dynamic-plugin-sdk",
      "@scalprum/react-core",
      "fast-deep-equal",
      "zod",
    ]);
    expect(manifest.optionalDependencies ?? {}).toEqual({});
    expect(manifest.peerDependencies).toEqual({
      react: "^19.1.0",
      "react-router-dom": "^7.13.1",
    });
  });

  it("keeps runtime source free of style assets and undeclared imports", () => {
    for (const file of sourceFiles(sourceRoot)) {
      expect(file).toMatch(/\.tsx?$/);
      const source = ts.createSourceFile(
        file,
        readFileSync(file, "utf8"),
        ts.ScriptTarget.Latest,
        true,
      );
      function visit(node: ts.Node) {
        let specifier: ts.Expression | undefined;
        if (ts.isImportDeclaration(node) || ts.isExportDeclaration(node))
          specifier = node.moduleSpecifier;
        if (
          ts.isImportEqualsDeclaration(node) &&
          ts.isExternalModuleReference(node.moduleReference)
        )
          specifier = node.moduleReference.expression;
        if (ts.isImportTypeNode(node) && ts.isLiteralTypeNode(node.argument))
          specifier = node.argument.literal;
        if (
          ts.isCallExpression(node) &&
          (node.expression.kind === ts.SyntaxKind.ImportKeyword ||
            (ts.isIdentifier(node.expression) &&
              node.expression.text === "require"))
        ) {
          specifier = node.arguments[0];
          expect(specifier && ts.isStringLiteral(specifier)).toBe(true);
        }
        if (specifier && ts.isStringLiteral(specifier)) {
          const relative = specifier.text.startsWith(".");
          expect(
            relative ||
              /^(react|react-router-dom|@openshift\/dynamic-plugin-sdk|@scalprum\/react-core|fast-deep-equal|zod)(\/|$)/.test(
                specifier.text,
              ),
          ).toBe(true);
          if (relative)
            expect(
              resolve(dirname(file), specifier.text).startsWith(
                sourceRoot + sep,
              ),
            ).toBe(true);
          expect(specifier.text).not.toMatch(/\.(css|scss|sass|less)$/);
        }
        ts.forEachChild(node, visit);
      }
      visit(source);
    }
  });
});
