import { readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

const thisDir = path.dirname(fileURLToPath(import.meta.url));
const appRoot = path.resolve(thisDir, '..');
const srcRoot = path.join(appRoot, 'src');
const featuresRoot = path.join(srcRoot, 'features');
const layoutRoot = path.join(srcRoot, 'components', 'layout');
const featureRoots = readdirSync(featuresRoot)
  .map((entry) => path.join(featuresRoot, entry))
  .filter((entry) => statSync(entry).isDirectory())
  .sort();

const checks = [
  ...featureRoots.flatMap((fromRoot) =>
    featureRoots
      .filter((candidate) => candidate !== fromRoot)
      .map((forbiddenRoot) => ({
        name: `${path.basename(fromRoot)} must not import ${path.basename(forbiddenRoot)} internals except public api`,
        fromRoot,
        forbiddenRoot,
        allowedRoot: path.join(forbiddenRoot, 'api'),
      })),
  ),
  {
    name: 'layout components must not import feature internals',
    fromRoot: layoutRoot,
    forbiddenRoot: featuresRoot,
  },
];

const violations = checks.flatMap((check) =>
  findResolvedImports(check.fromRoot, check.forbiddenRoot, check.allowedRoot).map(
    (violation) => `${check.name}: ${violation}`,
  ),
);

if (violations.length > 0) {
  console.error('Feature boundary violations found:\n');
  for (const violation of violations) {
    console.error(`- ${violation}`);
  }
  process.exit(1);
}

console.log('Feature boundaries OK');

function findResolvedImports(fromRoot, forbiddenRoot, allowedRoot) {
  const files = walkFiles(fromRoot).filter((file) => /\.(ts|tsx)$/.test(file));
  const found = [];

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
        found.push(`${path.relative(srcRoot, file)} -> ${path.relative(srcRoot, resolved)}`);
      }
    }
  }

  return found.sort();
}

function walkFiles(dir) {
  const entries = readdirSync(dir);
  const files = [];

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

function extractImportSpecifiers(file) {
  const source = readFileSync(file, 'utf8');
  const matches = source.matchAll(/from\s+['"]([^'"]+)['"]/g);
  return Array.from(matches, (match) => match[1]);
}
