// Syntax-only checks available without installing Vue or Go. This is not a build;
// `npm run typecheck` in frontend/ checks the types.
import { readFileSync, readdirSync, existsSync } from 'node:fs'
import { join, resolve, dirname } from 'node:path'
import vm from 'node:vm'
import { stripTypeScriptTypes } from 'node:module'

function walk(path) { return readdirSync(path, { withFileTypes: true }).flatMap(entry => entry.isDirectory() ? walk(join(path, entry.name)) : [join(path, entry.name)]) }
const files = walk('frontend').filter(file => !file.includes('node_modules') && /\.(vue|js|mjs|ts)$/.test(file) && !file.endsWith('.d.ts'))
if (!vm.SourceTextModule) throw Error('Run with node --experimental-vm-modules scripts/check-source.mjs')
let scripts = 0
{
  for (const file of files) {
    const text = readFileSync(file, 'utf8')
    const block = file.endsWith('.vue') ? text.match(/<script\s+setup([^>]*)>([\s\S]*?)<\/script>/) : null
    const source = file.endsWith('.vue') ? block?.[2] : text
    if (!source) continue
    const typed = file.endsWith('.ts') || /lang=["']ts["']/.test(block?.[1] || '')
    const code = typed ? stripTypeScriptTypes(source) : source
    new vm.SourceTextModule(code, { identifier: file })
    scripts++
    if (file.endsWith('.vue')) {
      if (!text.includes('<template>') || !text.trimEnd().endsWith('</template>')) throw Error('Missing Vue template: ' + file)
    }
    for (const match of code.matchAll(/from\s+['"](\.[^'"]+)['"]/g)) if (!existsSync(resolve(dirname(file), match[1]))) throw Error('Missing local import: ' + file + ': ' + match[1])
  }
  console.log(`PASS: ${scripts} TypeScript / JavaScript / Vue script blocks parse; local imports resolve.`)
}
