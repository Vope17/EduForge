// Generates frontend/src/api/domain.ts from the Go structs that the API returns,
// so the frontend's types follow the backend's JSON contract.
//
//   node scripts/gen-types.mjs           write the file
//   node scripts/gen-types.mjs --check   exit 1 if the file is out of date
//
// It reads the Go source directly (no Go toolchain needed) and understands the
// subset of Go used by these files: struct types with `json` tags, map/slice/
// pointer field types and named types such as `type Style map[string]any`.
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const SOURCES = [
  { file: 'internal/domain/model.go' },
  { file: 'internal/research/research.go', only: ['Pair', 'Difference'] },
  { file: 'internal/research/edit.go', only: ['EditBlock', 'EditResult'] },
  { file: 'internal/app/template_ai.go', only: ['SpecChange'] },
  { file: 'internal/mathtex/mathtex.go', only: ['Formula', 'Rendered'] },
  { file: 'internal/providers/connections.go', only: ['ModelOption', 'LoginState'] },
  { file: 'internal/artifacts/template_spec.go', only: ['TemplateSpec', 'WordStyleSpec', 'Margins', 'HeaderFooter'] },
]
const OUTPUT = 'frontend/src/api/domain.ts'
// Go names that would clash with TypeScript built-ins.
const RENAME = { Record: 'DomainRecord' }

const SCALARS = {
  string: 'string', bool: 'boolean', any: 'unknown', 'interface{}': 'unknown', 'json.RawMessage': 'unknown',
  int: 'number', int32: 'number', int64: 'number', float32: 'number', float64: 'number',
}

// Returns the TypeScript type and whether Go may encode the value as null.
// Collections nested in collections are typed as never null to keep the types
// readable; the backend does not store nil there.
function tsType(go) {
  if (go.startsWith('*')) return { type: tsType(go.slice(1)).type, nullable: true }
  if (go.startsWith('[]')) {
    const inner = tsType(go.slice(2))
    const t = inner.nullable && !inner.collection ? `(${inner.type} | null)` : inner.type
    return { type: `${t}[]`, nullable: true, collection: true }
  }
  const map = go.match(/^map\[string\](.+)$/)
  if (map) {
    const inner = tsType(map[1])
    return { type: `Record<string, ${inner.type}${inner.nullable && !inner.collection ? ' | null' : ''}>`, nullable: true, collection: true }
  }
  if (SCALARS[go]) return { type: SCALARS[go], nullable: false }
  const name = go.replace(/^domain\./, '')
  if (!/^[A-Z]\w*$/.test(name)) throw new Error(`gen-types: unsupported Go type ${go}`)
  return { type: RENAME[name] || name, nullable: false }
}

function doc(lines, indent = '') {
  const text = lines.map(line => line.replace(/^\s*\/\/\s?/, '')).filter(Boolean)
  if (!text.length) return ''
  if (text.length === 1) return `${indent}/** ${text[0]} */\n`
  return `${indent}/**\n${text.map(line => `${indent} * ${line}`).join('\n')}\n${indent} */\n`
}

// A field: `Name Type `json:"tag"`` with an optional trailing comment.
const FIELD = /^\s*([A-Z]\w*)\s+(\S+)\s+`json:"([^"]*)"`\s*(?:\/\/\s*(.*))?$/

function parse(file, only) {
  const lines = readFileSync(join(root, file), 'utf8').split('\n')
  const out = []
  let comments = []
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    if (/^\/\//.test(line)) { comments.push(line); continue }
    const alias = line.match(/^type ([A-Z]\w*) ((?:map|\[\]|\*)\S+)$/)
    const struct = line.match(/^type ([A-Z]\w*) struct \{$/)
    const goName = alias?.[1] || struct?.[1]
    if (!goName || (only && !only.includes(goName))) { comments = []; continue }
    const name = RENAME[goName] || goName
    let body = ''
    if (alias) {
      body = `export type ${name} = ${tsType(alias[2]).type}\n`
    } else {
      const fields = []
      let fieldDoc = []
      for (i++; lines[i] !== '}'; i++) {
        const fieldLine = lines[i]
        if (/^\s*\/\//.test(fieldLine)) { fieldDoc.push(fieldLine); continue }
        if (!fieldLine.trim()) { fieldDoc = []; continue }
        const m = fieldLine.match(FIELD)
        if (!m) throw new Error(`gen-types: ${file}:${i + 1}: cannot read field: ${fieldLine.trim()}`)
        const [, , goType, tag, trailing] = m
        const [key, ...options] = tag.split(',')
        if (key === '-') { fieldDoc = []; continue }
        const omitempty = options.includes('omitempty')
        const t = tsType(goType)
        // omitempty drops nil and empty values; otherwise Go writes nil as null.
        const type = !omitempty && t.nullable ? `${t.type} | null` : t.type
        const prop = /^[A-Za-z_]\w*$/.test(key) ? key : JSON.stringify(key)
        fields.push(doc([...fieldDoc, ...(trailing ? [trailing] : [])], '  ') + `  ${prop}${omitempty ? '?' : ''}: ${type}`)
        fieldDoc = []
      }
      body = `export interface ${name} {\n${fields.join('\n')}\n}\n`
    }
    out.push(doc(comments) + body)
    comments = []
  }
  return out
}

const header = `// Code generated by scripts/gen-types.mjs from ${SOURCES.map(s => s.file).join(', ')}. DO NOT EDIT.
// Regenerate with \`make types\` after changing those Go structs.
`
const content = header + '\n' + SOURCES.flatMap(s => parse(s.file, s.only)).join('\n')

if (process.argv.includes('--check')) {
  let current = ''
  try { current = readFileSync(join(root, OUTPUT), 'utf8') } catch {}
  if (current !== content) {
    console.error(`${OUTPUT} is out of date with the Go structs. Run: make types`)
    process.exit(1)
  }
  console.log(`PASS: ${OUTPUT} matches the Go structs.`)
} else {
  mkdirSync(dirname(join(root, OUTPUT)), { recursive: true })
  writeFileSync(join(root, OUTPUT), content)
  console.log(`wrote ${OUTPUT}`)
}
