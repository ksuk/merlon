import { readdirSync, readFileSync, statSync } from "node:fs"
import { join, relative } from "node:path"
import { fileURLToPath } from "node:url"

const CJK_PATTERN = /[぀-ヿ一-鿿㐀-䶿]/
const IGNORE_MARKER = /i18n-ignore/
const LOCALES_PATH = /i18n[\\/]locales/

function stripBlockComments(source) {
  return source.replace(/\/\*[\s\S]*?\*\//g, (match) => match.replace(/[^\n]/g, " "))
}

function stripLineComment(line) {
  for (let i = 0; i < line.length - 1; i++) {
    if (line[i] === "/" && line[i + 1] === "/" && line[i - 1] !== ":") {
      return line.slice(0, i)
    }
  }
  return line
}

export function detectHardcodedCJK(filePath, source) {
  if (LOCALES_PATH.test(filePath)) return []

  const withoutBlockComments = stripBlockComments(source)
  const lines = withoutBlockComments.split("\n")
  const hits = []

  lines.forEach((rawLine, index) => {
    if (IGNORE_MARKER.test(rawLine)) return
    const stripped = stripLineComment(rawLine)
    if (CJK_PATTERN.test(stripped)) {
      hits.push({ file: filePath, line: index + 1, text: stripped.trim() })
    }
  })

  return hits
}

const SOURCE_EXTENSIONS = new Set([".ts", ".tsx"])

// Test files legitimately contain Japanese as sample business data (customer
// names, note content) and as literal assertions against the ja catalog
// (i18n-test-utils.tsx locks the test language to "ja" so existing
// assertions keep passing verbatim). Neither case is hardcoded UI copy
// bypassing i18n, so the CLI excludes *.test.ts(x) from the scan even though
// detectHardcodedCJK() itself does not special-case them.
const TEST_FILE_PATTERN = /\.test\.tsx?$/

function walk(dir, files = []) {
  for (const entry of readdirSync(dir)) {
    const fullPath = join(dir, entry)
    const stat = statSync(fullPath)
    if (stat.isDirectory()) {
      walk(fullPath, files)
    } else if (SOURCE_EXTENSIONS.has(entry.slice(entry.lastIndexOf(".")))) {
      files.push(fullPath)
    }
  }
  return files
}

export function scanDirectory(rootDir) {
  const allHits = []
  for (const filePath of walk(rootDir)) {
    if (TEST_FILE_PATTERN.test(filePath)) continue
    const relPath = relative(rootDir, filePath)
    const source = readFileSync(filePath, "utf-8")
    allHits.push(...detectHardcodedCJK(relPath, source))
  }
  return allHits
}

const isMain = process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]

if (isMain) {
  const srcDir = join(fileURLToPath(new URL(".", import.meta.url)), "..", "src")
  const hits = scanDirectory(srcDir)

  if (hits.length > 0) {
    for (const hit of hits) {
      console.log(`${hit.file}:${hit.line}: ${hit.text}`)
    }
    console.log(`\n${hits.length} hardcoded CJK occurrence(s) found in ui/src.`)
    process.exit(1)
  }

  console.log("No hardcoded CJK found in ui/src.")
  process.exit(0)
}
