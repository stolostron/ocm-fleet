import { readdirSync, readFileSync } from "node:fs";
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
  it("starts with no runtime exports", () => {
    expect(Object.keys(shell)).toEqual([]);
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
    expect(manifest.dependencies ?? {}).toEqual({});
    expect(manifest.optionalDependencies ?? {}).toEqual({});
    expect(manifest.peerDependencies).toEqual({
      react: "^19.1.0",
      "react-router-dom": "^7.13.1",
    });
  });

  it("keeps runtime source free of style assets and non-peer imports", () => {
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
            relative || /^(react|react-router-dom)(\/|$)/.test(specifier.text),
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
