import { readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const thisDir = path.dirname(fileURLToPath(import.meta.url));
const srcRoot = path.resolve(thisDir, '..');
const featuresRoot = path.join(srcRoot, 'features');
const layoutRoot = path.join(srcRoot, 'components', 'layout');
const featureRoots = readdirSync(featuresRoot)
  .map((entry) => path.join(featuresRoot, entry))
  .filter((entry) => statSync(entry).isDirectory())
  .sort();

describe('feature boundaries', () => {
  for (const featureRoot of featureRoots) {
    const featureName = path.basename(featureRoot);
    it(`keeps ${featureName} isolated from other feature internals except public api`, () => {
      const violations = featureRoots
        .filter((candidate) => candidate !== featureRoot)
        .flatMap((otherFeatureRoot) =>
          findResolvedImports(featureRoot, otherFeatureRoot, path.join(otherFeatureRoot, 'api')),
        );

      expect(violations).toEqual([]);
    });
  }

  it('keeps layout components free from feature coupling', () => {
    const violations = findResolvedImports(layoutRoot, featuresRoot);
    expect(violations).toEqual([]);
  });
});

function findResolvedImports(fromRoot: string, forbiddenRoot: string, allowedRoot?: string): string[] {
  const files = walkFiles(fromRoot).filter((file) => /\.(ts|tsx)$/.test(file));
  const violations: string[] = [];

  for (const file of files) {
    for (const specifier of extractImportSpecifiers(file)) {
      if (!specifier.startsWith('.')) {
        continue;
      }
      const resolved = path.resolve(path.dirname(file), specifier);
      if (resolved.startsWith(forbiddenRoot)) {
        if (allowedRoot && resolved.startsWith(allowedRoot)) {
          continue;
        }
        violations.push(`${path.relative(srcRoot, file)} -> ${path.relative(srcRoot, resolved)}`);
      }
    }
  }

  return violations.sort();
}

function walkFiles(dir: string): string[] {
  const entries = readdirSync(dir);
  const files: string[] = [];

  for (const entry of entries) {
    const fullPath = path.join(dir, entry);
    const stats = statSync(fullPath);
    if (stats.isDirectory()) {
      files.push(...walkFiles(fullPath));
      continue;
    }
    files.push(fullPath);
  }

  return files;
}

function extractImportSpecifiers(file: string): string[] {
  const source = readFileSync(file, 'utf8');
  const matches = source.matchAll(/from\s+['"]([^'"]+)['"]/g);
  return Array.from(matches, (match) => match[1]);
}
