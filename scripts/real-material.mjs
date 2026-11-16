#!/usr/bin/env node
// Phase-4 acceptance with real teaching material (docs/rebuild-plan.md §12–§13).
// Drives a running EduForge over its HTTP API with a real model connection,
// one stage at a time, and prints what a person then checks by hand.
//
//   node scripts/real-material.mjs setup     --course-name N --chapter N --lesson "Title=path.pdf" ...
//   node scripts/real-material.mjs status    --course ID
//   node scripts/real-material.mjs knowledge --course ID     read every page of every lesson
//   node scripts/real-material.mjs confirm   --course ID     confirm knowledge whose coverage is complete
//   node scripts/real-material.mjs generate  --course ID [--kinds A,B,C,D] [--weeks 18]
//   node scripts/real-material.mjs export    --course ID --out DIR
//
// --base defaults to http://127.0.0.1:18081 (the eduforge-next preview).
// Lessons run in order, so later lessons get earlier ones as background.
import { execFileSync } from 'node:child_process'
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { basename, join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'

const [stage, ...rest] = process.argv.slice(2)
const opts = { lesson: [] }
for (let i = 0; i < rest.length; i += 2) {
  const key = rest[i].replace(/^--/, '')
  if (key === 'lesson') opts.lesson.push(rest[i + 1])
  else opts[key] = rest[i + 1]
}
const base = (opts.base || 'http://127.0.0.1:18081') + '/api'
let token

async function api(path, { method = 'GET', body, raw = false } = {}) {
  token ??= (await (await fetch(base + '/session')).json()).csrf_token
  const headers = method === 'GET' ? {} : { 'X-EduForge-Token': token }
  if (body !== undefined && !(body instanceof FormData)) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(body)
  }
  const res = await fetch(base + path, { method, headers, body })
  if (raw) {
    if (!res.ok) throw new Error(`${method} ${path}: ${res.status} ${await res.text()}`)
    return { bytes: Buffer.from(await res.arrayBuffer()), type: res.headers.get('content-type') }
  }
  const data = await res.json()
  if (!res.ok) throw new Error(`${method} ${path}: ${data.error ?? res.status}`)
  return data
}

const summary = () => api('/summary?course_id=' + opts.course)
const byId = list => Object.fromEntries((list || []).map(x => [x.id, x]))
const lessonsOf = s => (s.lesson || []).filter(l => l.course_id === opts.course).sort((a, b) => a.data.position - b.data.position)
const emptySettings = { selected: null, languages: null, depth: '', level: '', audience: '', minutes: 0, template_id: '' }
const say = (...x) => console.log(...x)

// Wait until no task of the given types is queued or running; stop on paused or failed.
async function settle(types, minutes = 60) {
  const end = Date.now() + minutes * 60_000
  let last = ''
  while (Date.now() < end) {
    const tasks = (await summary()).task.filter(t => types.includes(t.data.type))
    const open = tasks.filter(t => ['queued', 'running', 'preparing'].includes(t.data.status))
    const line = tasks.map(t => `${t.data.type}:${t.data.status}${t.data.total ? ` ${t.data.completed}/${t.data.total}` : ''}`).join('  ')
    if (line !== last) say('  ', new Date().toLocaleTimeString('zh-TW'), line), (last = line)
    const stuck = tasks.filter(t => t.data.status === 'paused')
    if (stuck.length) throw new Error('paused: ' + stuck.map(t => t.data.error || t.data.stage).join('; '))
    if (!open.length) return tasks
    await delay(5000)
  }
  throw new Error('timed out')
}

// Refuse to run model stages on the offline test provider alone.
async function requireModel() {
  const real = (await summary()).provider.filter(p => p.data.enabled && p.data.kind !== 'source-extract')
  if (!real.length) throw new Error('no real model connection is enabled (Settings → Model connections)')
  say('using', real.map(p => `${p.data.name} ${p.data.model}`).join(', '))
}

const stages = {
  async setup() {
    const course = await api('/courses', { method: 'POST', body: { name: opts['course-name'], description: opts.description || '', style: {}, settings: emptySettings } })
    const chapter = await api(`/courses/${course.id}/chapters`, { method: 'POST', body: { name: opts.chapter } })
    for (const [position, spec] of opts.lesson.entries()) {
      const [title, file] = spec.split('=')
      const lesson = await api(`/courses/${course.id}/lessons`, { method: 'POST', body: { title, chapter: chapter.id, position, knowledge_id: '', settings: emptySettings } })
      const form = new FormData()
      form.append('file', new Blob([readFileSync(file)]), basename(file))
      form.append('role', 'research')
      await api(`/lessons/${lesson.id}/sources`, { method: 'POST', body: form })
      await api(`/lessons/${lesson.id}/parse`, { method: 'POST' })
    }
    opts.course = course.id
    await settle(['parse'])
    say('course', course.id)
    await stages.status()
  },

  async status() {
    const s = await summary()
    const knowledge = byId(s.knowledge)
    for (const p of s.provider || []) say(`provider ${p.data.name}: enabled=${p.data.enabled} model=${p.data.model || '-'} credential=${p.data.has_credential}`)
    for (const l of lessonsOf(s)) {
      say(`\n${l.data.title}  (${l.id})`)
      for (const src of (s.source || []).filter(x => x.lesson_id === l.id)) {
        const parsed = src.data.parsed || {}
        const vision = (parsed.units || []).filter(u => u.mode === 'vision').map(u => u.locator)
        say(`  source ${parsed.title || src.data.name}: ${(parsed.units || []).length} pages, vision pages [${vision}], figures ${(parsed.images || []).length}, issues [${(parsed.issues || []).map(i => i.code + '@' + i.citation.split(':')[1])}]`)
      }
      const k = knowledge[l.data.knowledge_id]?.data
      if (k) {
        const c = k.coverage
        say(`  knowledge: author=${k.author || 'rules'} confirmed=${k.confirmed} claims=${k.claims.length} terms=${k.terms.length}` +
          (c ? ` coverage ${c.read}/${c.pages} (vision ${c.vision}, batches ${c.batches}${c.missed?.length ? ', missed ' + c.missed : ''})` : '') +
          (k.ai_error ? ` ai_error=${k.ai_error}` : ''))
        const uncited = k.claims.filter(c => !c.citations?.length).length
        if (uncited) say(`  !! ${uncited} claims without a citation`)
      }
      const outs = (s.artifact || []).filter(a => a.data.knowledge_id === l.data.knowledge_id)
      for (const a of outs) say(`  artifact ${a.data.type} ${a.id} ${a.data.language} ${a.data.title || ''}`)
    }
    for (const a of (s.artifact || []).filter(a => a.data.type === 'S')) say(`\nsyllabus ${a.id} ${a.data.title || ''}`)
    const failed = (s.task || []).filter(t => t.data.status === 'failed')
    for (const t of failed) say(`failed task ${t.data.type}: ${t.data.error}`)
  },

  async knowledge() {
    await requireModel()
    for (const l of lessonsOf(await summary())) {
      const k = byId((await summary()).knowledge)[l.data.knowledge_id]
      say('reading', l.data.title)
      await api(`/lessons/${l.id}/knowledge/ai`, { method: 'POST', body: { version: k.version, missed_only: false } })
      await settle(['knowledge'], 90)
    }
    await stages.status()
  },

  async confirm() {
    const s = await summary()
    const knowledge = byId(s.knowledge)
    for (const l of lessonsOf(s)) {
      const rec = knowledge[l.data.knowledge_id]
      const c = rec.data.coverage
      if (!c || c.read < c.pages || c.missed?.length) {
        say(`skip ${l.data.title}: coverage incomplete`)
        continue
      }
      await api(`/lessons/${l.id}/knowledge`, { method: 'PUT', body: { version: rec.version, knowledge: { ...rec.data, confirmed: true } } })
      say('confirmed', l.data.title)
    }
  },

  async generate() {
    await requireModel()
    const kinds = (opts.kinds || 'A,B,C,D').split(',')
    for (const l of lessonsOf(await summary())) {
      say('generating', kinds.join(''), 'for', l.data.title)
      const request = await api(`/courses/${opts.course}/requests`, {
        method: 'POST',
        body: {
          selected: kinds, use_lesson_settings: false, languages: {}, profile_id: '', overrides: {},
          settings: { ...emptySettings, depth: 'standard', level: 'intermediate', minutes: 50, questions: { choice: 4, fill: 3, short: 3, calc: 2, practice: 0 } },
          lesson_ids: [l.id], ui_language: 'zh-TW',
        },
      })
      await api(`/requests/${request.id}/start`, { method: 'POST', body: { ui_language: 'zh-TW', use_defaults: true } })
      await settle(kinds, 120)
    }
    if (opts.weeks !== '0') {
      say('generating syllabus')
      await api(`/courses/${opts.course}/syllabus`, { method: 'POST', body: { weeks: Number(opts.weeks || 18), language: 'zh-TW' } })
      await settle(['syllabus'], 30)
    }
    await stages.status()
  },

  async export() {
    const s = await summary()
    const lessons = Object.fromEntries(lessonsOf(s).map(l => [l.data.knowledge_id, l]))
    mkdirSync(opts.out, { recursive: true })
    const formats = { A: ['docx', 'pdf'], B: ['pptx', 'pdf', 'notes'], C: ['docx', 'pdf'], D: ['docx', 'pdf', 'docx:teacher', 'pdf:teacher'], S: ['docx', 'pdf', 'tsv'] }
    const report = []
    for (const a of s.artifact || []) {
      const lesson = lessons[a.data.knowledge_id]
      const prefix = (lesson ? `${lesson.data.position + 1}-` : '0-') + a.data.type
      for (const spec of formats[a.data.type] || []) {
        const [format, version] = spec.split(':')
        const query = `format=${format}` + (version ? `&version=${version}` : '')
        const ext = format === 'notes' ? 'notes.pdf' : format
        const file = join(opts.out, `${prefix}${version ? '-' + version : ''}.${ext}`)
        try {
          const { bytes } = await api(`/artifacts/${a.id}/download?${query}`, { raw: true })
          writeFileSync(file, bytes)
          report.push({ file: basename(file), bytes: bytes.length, check: check(file, format) })
        } catch (err) {
          report.push({ file: basename(file), error: err.message })
        }
      }
    }
    for (const r of report) say(r.error ? `FAIL ${r.file}: ${r.error}` : `ok   ${r.file}  ${(r.bytes / 1024).toFixed(0)} KB  ${r.check}`)
    writeFileSync(join(opts.out, 'report.json'), JSON.stringify(report, null, 2))
  },
}

// A cheap structural look at each file; opening it in Word or PowerPoint is still the real check.
function check(file, format) {
  const run = (cmd, args) => { try { return execFileSync(cmd, args, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }) } catch { return '' } }
  if (format === 'pdf' || format === 'notes') return 'pages=' + (run('pdfinfo', [file]).match(/Pages:\s+(\d+)/)?.[1] ?? '?')
  if (format === 'docx' || format === 'pptx') {
    const names = run('unzip', ['-Z1', file]).split('\n')
    const parts = format === 'pptx' ? names.filter(n => /^ppt\/slides\/slide\d+\.xml$/.test(n)).length + ' slides' : names.includes('word/document.xml') ? 'document.xml' : 'NO document.xml'
    return parts + ', media=' + names.filter(n => n.includes('/media/')).length
  }
  if (format === 'tsv') return readFileSync(file, 'utf8').split('\n').length + ' lines'
  return ''
}

if (!stages[stage]) {
  console.error('stages: ' + Object.keys(stages).join(', '))
  process.exit(2)
}
if (stage !== 'setup' && !opts.course) {
  console.error('--course is required')
  process.exit(2)
}
stages[stage]().catch(err => {
  console.error('error:', err.message)
  process.exit(1)
})
