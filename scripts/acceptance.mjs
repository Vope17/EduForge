import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { copyFile, mkdir, readFile, writeFile } from 'node:fs/promises'
import { basename, join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'
import { deflateSync } from 'node:zlib'

// Run inside an isolated Compose project. The workspace persists across container replacement.
const [mode, root, fixture] = process.argv.slice(2)
assert.match(root ?? '', /^\/workspace\/acceptance-[a-zA-Z0-9-]+$/)
assert.ok(['seed', 'after-recreate', 'ssh-start', 'ssh-check'].includes(mode))
const base = 'http://127.0.0.1:8080'
const evidence = []
let session

async function request(path, { method = 'GET', body, status = 200, binary = false } = {}) {
  const headers = {}
  if (method !== 'GET') headers['X-EduForge-Token'] = session.csrf_token
  if (body !== undefined && !(body instanceof FormData)) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(body)
  }
  const response = await fetch(base + path, { method, headers, body, signal: AbortSignal.timeout(35000) })
  const data = binary ? Buffer.from(await response.arrayBuffer()) : await response.json()
  assert.equal(response.status, status, `${method} ${path}: ${JSON.stringify(data.error ?? '')}`)
  return data
}

const post = (path, body) => request('/api' + path, { method: 'POST', body })
const put = (path, body) => request('/api' + path, { method: 'PUT', body })
const summary = project => request('/api/summary?project_id=' + project)
const download = path => request('/api' + path, { binary: true })
const log = (cases, observation) => {
  const row = { cases, observation }
  evidence.push(row)
  console.log(JSON.stringify(row))
}

async function waitFor(label, check, timeout = 35000) {
  const expires = Date.now() + timeout
  while (Date.now() < expires) {
    const value = await check()
    if (value) return value
    await delay(300)
  }
  throw new Error('Timed out waiting for ' + label)
}

function command(name, args, cwd = root) {
  const result = spawnSync(name, args, { cwd, encoding: 'utf8', timeout: 25000 })
  assert.equal(result.status, 0, [name, ...args, result.error?.message, result.stderr].join(' '))
  return result.stdout.trim()
}

function pdf(lines, includeFigure = false) {
  const escape = line => line.replaceAll('\\', '\\\\').replaceAll('(', '\\(').replaceAll(')', '\\)')
  const figure = includeFigure ? 'q 180 0 0 95 50 115 cm /Fig Do Q' : ''
  const drawing = Buffer.from(`BT /F 11 Tf 48 750 Td 19 TL ${lines.map(line => `(${escape(line)}) Tj T*`).join(' ')} ET\n${figure}`)
  const stream = (data, options = '') => Buffer.concat([Buffer.from(`<< /Length ${data.length} ${options} >>\nstream\n`), data, Buffer.from('\nendstream')])
  const objects = [
    '<< /Type /Catalog /Pages 2 0 R >>',
    '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F 4 0 R >> ${includeFigure ? '/XObject << /Fig 6 0 R >>' : ''} >> /Contents 5 0 R >>`,
    '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
    stream(drawing),
  ]
  if (includeFigure) {
    const pixels = Buffer.alloc(120 * 90 * 3)
    for (let n = 0; n < pixels.length; n += 3) {
      const x = (n / 3) % 120
      const color = x < 60 ? [20, 68, 142] : [17, 110, 90]
      pixels.set(color, n)
    }
    objects.push(stream(deflateSync(pixels), '/Type /XObject /Subtype /Image /Width 120 /Height 90 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode'))
  }
  const chunks = [Buffer.from('%PDF-1.4\n')]
  const offsets = [0]
  let size = chunks[0].length
  for (const [index, object] of objects.entries()) {
    offsets.push(size)
    const part = Buffer.concat([Buffer.from(`${index + 1} 0 obj\n`), Buffer.from(object), Buffer.from('\nendobj\n')])
    size += part.length
    chunks.push(part)
  }
  chunks.push(Buffer.from(`xref\n0 ${offsets.length}\n0000000000 65535 f \n${offsets.slice(1).map(x => String(x).padStart(10, '0') + ' 00000 n \n').join('')}trailer\n<< /Size ${offsets.length} /Root 1 0 R >>\nstartxref\n${size}\n%%EOF\n`))
  return Buffer.concat(chunks)
}

async function slidesFixture(name, text) {
  const namespaces = {
    office: 'urn:oasis:names:tc:opendocument:xmlns:office:1.0',
    draw: 'urn:oasis:names:tc:opendocument:xmlns:drawing:1.0',
    style: 'urn:oasis:names:tc:opendocument:xmlns:style:1.0',
    text: 'urn:oasis:names:tc:opendocument:xmlns:text:1.0',
    svg: 'urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0',
    fo: 'urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0',
  }
  const xml = `<?xml version="1.0" encoding="UTF-8"?>
<office:document ${Object.entries(namespaces).map(([key, value]) => `xmlns:${key}="${value}"`).join(' ')} office:version="1.2" office:mimetype="application/vnd.oasis.opendocument.presentation">
<office:automatic-styles><style:page-layout style:name="page"><style:page-layout-properties fo:page-width="28cm" fo:page-height="15.75cm"/></style:page-layout></office:automatic-styles>
<office:master-styles><style:master-page style:name="Default" style:page-layout-name="page"/></office:master-styles>
<office:body><office:presentation><draw:page draw:name="Slide 1" draw:master-page-name="Default">
<draw:frame svg:x="1cm" svg:y="1cm" svg:width="25cm" svg:height="3cm"><draw:text-box><text:p>${text}</text:p></draw:text-box></draw:frame>
</draw:page></office:presentation></office:body></office:document>`
  const source = join(root, name + '.fodp')
  await writeFile(source, xml)
  command('libreoffice', [`-env:UserInstallation=file://${root}/lo-fixtures`, '--headless', '--convert-to', 'pptx', '--outdir', root, source])
  return readFile(join(root, name + '.pptx'))
}

async function upload(paper, name, data, role = 'research') {
  const form = new FormData()
  form.append('file', new Blob([data]), name)
  form.append('role', role)
  return post(`/papers/${paper}/sources`, form)
}

async function confirmKnowledge(project, paper, conditions) {
  const data = await summary(project)
  const current = data.paper.find(item => item.id === paper)
  const k = data.knowledge.find(item => item.id === current.data.knowledge_id)
  assert.ok(k)
  const updated = { ...k.data, confirmed: true, conditions, claims: k.data.claims.map(c => ({ ...c, verified: true })) }
  return put(`/papers/${paper}/knowledge`, { version: k.version, knowledge: updated })
}

const conditions = { task: 'classification', dataset: 'synthetic-v1', split: 'heldout-v1', metric: 'accuracy', evaluation: 'nearest-centroid-single-run' }
const researchLines = [
  'Introduction: nearest centroid classification with Euclidean distance.',
  'Method: estimate class centers on a synthetic training dataset.',
  'Results: heldout-v1 accuracy is 1.00 for 4 examples.',
  'Limitations: synthetic data only. Figure 1 shows the two class regions.',
  'Centroid: arithmetic mean of feature vectors for one class.',
  'Distance: Euclidean separation between a point and a centroid.',
  'Label: class assigned to a feature vector.',
  'Accuracy: fraction of predictions matching ground truth.',
  'Feature: measured property of an example.',
  'Inference: prediction on examples omitted from training.',
  'Holdout: fixed subset reserved for evaluation.',
  'Prediction: selected class for a test vector.',
  'Dataset: collection of feature vectors with class labels.',
  'Decision boundary: points where two centroids are equally distant.',
  'Baseline: a simple method used for comparison.',
  'Test example: a held-out point with a known label.',
]

async function savePlan(paper, source, repo, commit, commands, resumeCommands = [], host) {
  const payload = {
    target: 'Held-out nearest-centroid accuracy', citation: source, repo, commit,
    host: host ?? { mode: 'container', workdir: root, allowed_root: root, os: 'linux', cpu: process.arch, gpu: 'none', environment: 'direct', concurrency: 1 },
    commands, resume_commands: resumeCommands, data: 'Deterministic synthetic points in experiment.mjs',
    weights: 'No weights file', dependencies: 'Node.js and Git', downloads: 'Local fixture repository',
    output_csv: 'results.csv', metric: 'accuracy', expected: 1, tolerance: 0, conditions,
    estimates: [{ phase: 'train_infer', machine: '0–2 s', human: '0 s', basis: 'Four synthetic test points', confidence: 'high' }],
  }
  const result = await post(`/papers/${paper}/plans`, payload)
  assert.deepEqual(result.record.data.missing, [])
  await request(`/api/plans/${result.record.id}/run`, { method: 'POST', status: 400 })
  await post(`/plans/${result.record.id}/confirm`, { hash: result.hash })
  return result.record
}

async function seed() {
  await mkdir(root)
  assert.equal((await request('/api/health')).status, 'ok')
  assert.equal(session.single_user, true)
  const system = await request('/api/system')
  for (const tool of ['pdftotext', 'libreoffice', 'git', 'ssh', 'codex', 'claude']) assert.equal(system.tools[tool], true)
  const shell = await request('/', { binary: true })
  const entry = shell.toString().match(/src="(\/assets\/[^"]+\.js)"/)?.[1]
  assert.ok(entry)
  assert.ok((await request(entry, { binary: true })).length > 5000)
  log(['AC24', 'AC25', 'AC26'], { health: 'ok', vueBundle: entry, cliVersions: [command('codex', ['--version']), command('claude', ['--version'])] })

  const project = await post('/projects', { name: 'Acceptance ' + basename(root), description: 'Synthetic, reproducible input' })
  const providers = (await summary(project.id)).provider
  const authChecks = []
  for (const [kind, executable, argv, code] of [
    ['codex', 'codex', ['login', 'status'], 'codex_chatgpt_login_required_use_api_source_for_api_billing'],
    ['claude-cli', 'claude', ['auth', 'status'], 'claude_subscription_login_required'],
  ]) {
    if (spawnSync(executable, argv, { cwd: root, timeout: 15000 }).status !== 1) continue
    const provider = providers.find(x => x.data.kind === kind)
    const result = await request(`/api/providers/${provider.id}/test-cli`, { method: 'POST', status: 400 })
    assert.equal(result.error.code, code)
    authChecks.push({ kind, outcome: 'login required' })
  }
  log(['AC26'], { cliAuthChecks: authChecks, liveInference: 'requires a signed-in account' })
  const marker = 'eduforge-test-secret-' + project.id
  for (const provider of providers) {
    await put(`/providers/${provider.id}`, {
      provider: { ...provider.data, enabled: provider.data.kind === 'source-extract' },
      key: provider.data.kind === 'openai' ? marker : '',
    })
  }
  const source = await post(`/projects/${project.id}/papers`, { title: 'Nearest-centroid classification' })
  const related = await post(`/projects/${project.id}/papers`, { title: 'Nearest-centroid baseline comparison' })
  const separate = await post(`/projects/${project.id}/papers`, { title: 'Ceramic thermal conductivity' })
  await upload(source.id, 'research.pdf', pdf(researchLines, true))
  await upload(source.id, 'supplement.pptx', await slidesFixture('supplement', 'Nearest centroid method. 補充投影片。 Four held-out points.'))
  const design = await upload(source.id, 'layout.pptx', await slidesFixture('layout', 'Design placeholder: fake metric 99999'), 'design')
  await upload(related.id, 'comparison.pdf', pdf(researchLines.slice(0, 8)))
  await upload(separate.id, 'ceramics.pdf', pdf(['Thermal conductivity of porous ceramics measured with heat flux.', 'Porous ceramic insulation reduces the flow of heat.']))
  const parse = await Promise.all([source, related, separate].map(p => post(`/papers/${p.id}/parse`)))
  await waitFor('three parsed studies', async () => {
    const data = await summary(project.id)
    return parse.every(task => data.task.some(item => item.id === task.id && item.data.status === 'completed'))
  })
  const knowledge = await confirmKnowledge(project.id, source.id, conditions)
  await confirmKnowledge(project.id, related.id, conditions)
  await confirmKnowledge(project.id, separate.id, { ...conditions, task: 'material characterization', metric: 'conductivity' })
  assert.deepEqual(knowledge.data.units.map(x => [x.kind, x.locator]), [['page', 1], ['slide', 1]])
  assert.equal(knowledge.data.images.length, 1)
  assert.ok(knowledge.data.terms.length >= 10 && knowledge.data.terms.length <= 20)
  assert.ok(!JSON.stringify(knowledge.data).includes('99999'))
  const papers = (await summary(project.id)).pairs
  const pair = papers.find(x => [x.a, x.b].includes(source.id) && [x.a, x.b].includes(related.id))
  assert.equal(pair.numeric_comparable, true)
  const mismatched = papers.find(x => [x.a, x.b].includes(source.id) && [x.a, x.b].includes(separate.id))
  assert.equal(mismatched.numeric_comparable, false)
  await post(`/projects/${project.id}/comparisons`, { ...mismatched, related: false })
  log(['AC01', 'AC02', 'AC05'], { units: knowledge.data.units.map(x => [x.kind, x.locator]), figures: knowledge.data.images.length, terms: knowledge.data.terms.length, matchingConditions: pair.numeric_comparable, incompatible: mismatched.differences.map(d => d.field) })

  // Slide templates are imported PowerPoint files.
  const templateForm = new FormData()
  templateForm.append('file', new Blob([await slidesFixture('theme', 'Template sample slide')]), 'theme.pptx')
  templateForm.append('name', 'Fixture theme')
  const template = await post('/templates', templateForm)
  assert.equal(template.data.confirmed, true)
  assert.ok(template.data.file && template.data.content_layout && template.data.title_box.length === 4)
  await post(`/papers/${source.id}/reading`, { read: true })
  const reqPath = `/projects/${project.id}/requests`
  const generate = async (kind, lang, settings = {}) => post(reqPath, { paper_ids: [source.id], selected: [kind], ui_language: lang, unlimited: true, settings })
  const reader = await generate('A', 'en', { depth: 'deep' })
  await put('/preferences', { key: 'ui_language', value: 'zh-TW' })
  const readerTasks = await post(`/requests/${reader.id}/start`, { ui_language: 'zh-TW', use_defaults: true })
  assert.equal(readerTasks.length, 1)
  assert.equal(readerTasks[0].data.type, 'A')
  assert.equal(readerTasks[0].data.language, 'en')
  assert.equal(readerTasks[0].data.settings.depth, 'deep')
  assert.ok(readerTasks[0].data.read_terms.length >= 10)
  await post(`/papers/${source.id}/reading`, { read: false })
  await post(`/papers/${source.id}/reading`, { read: true })
  const talk = await generate('B', 'zh-TW', { audience: 'advisor', template_id: template.id })
  const talkTasks = await post(`/requests/${talk.id}/start`, { use_defaults: true })
  assert.equal(talkTasks.length, 1)
  assert.equal(talkTasks[0].data.type, 'B')
  assert.equal(talkTasks[0].data.settings.audience, 'advisor')
  assert.equal(talkTasks[0].data.read_terms, null)
  assert.equal(talkTasks[0].data.style_snapshot.resolved.B.template_file, template.data.file)
  log(['AC03', 'AC04', 'AC20', 'AC22'], { readerLanguage: readerTasks[0].data.language, talkLanguage: talkTasks[0].data.language, readerDepth: 'deep', isolatedBranches: ['A', 'B'], slideTemplate: template.id })

  const [a, b] = await waitFor('reader and talk', async () => {
    const data = await summary(project.id)
    const a = data.artifact.find(x => x.data.task_id === readerTasks[0].id)
    const b = data.artifact.find(x => x.data.task_id === talkTasks[0].id)
    return a && b && [a, b]
  })
  const html = await download(`/artifacts/${a.id}/download?format=html`)
  assert.match(html.toString(), /data:image\/png;base64,/)
  assert.match(html.toString(), /accuracy is 1\.00/)
  // Related research is the paper's own related work, never a comparison with other studies in the library.
  assert.ok(!a.data.sections.some(section => section.title.includes('Related research comparison')))
  await writeFile(join(root, 'reader.html'), html)
  const pdfBytes = await download(`/artifacts/${a.id}/download?format=pdf`)
  assert.equal(pdfBytes.subarray(0, 5).toString(), '%PDF-')
  await writeFile(join(root, 'reader.pdf'), pdfBytes)
  assert.match(command('pdftotext', ['-f', '1', '-l', '1', join(root, 'reader.pdf'), '-']), /Nearest-centroid classification/)
  assert.match(command('pdfimages', ['-list', join(root, 'reader.pdf')]), /120\s+90/)
  const pptx = await download(`/artifacts/${b.id}/download?format=pptx`)
  await writeFile(join(root, 'talk.pptx'), pptx)
  command('libreoffice', [`-env:UserInstallation=file://${root}/lo-review`, '--headless', '--convert-to', 'pdf', '--outdir', root, join(root, 'talk.pptx')])
  const pages = command('pdfinfo', [join(root, 'talk.pdf')]).match(/^Pages:\s+(\d+)$/m)?.[1]
  assert.ok(Number(pages) >= 8)
  const restyled = await post(`/artifacts/${b.id}/template`, { template_id: '' })
  assert.deepEqual(restyled.data.sections, b.data.sections)
  const badContent = structuredClone(a.data)
  badContent.editing_seconds = 2.5
  badContent.sections[0].body += ' Unsupported accuracy 765.123%.'
  const bad = await put(`/artifacts/${a.id}`, { version: a.version, artifact: badContent })
  assert.equal(bad.data.check.status, 'draft')
  assert.equal(bad.data.editing_seconds, 2.5)
  assert.ok(bad.data.check.issues.some(x => x.code === 'unsupported_number' && x.detail === '765.123%'))
  log(['AC05', 'AC06', 'AC07', 'AC15', 'AC18'], { outputMode: a.data.mode, pdf: pdfBytes.length, pptx: pptx.length, libreOfficeRenderedSlides: Number(pages), editCount: bad.data.edits, wrongNumber: 'flagged' })

  const savedPapers = (await summary(project.id)).paper
  for (const [paper, kind, lang] of [[source, 'C', 'zh-TW'], [related, 'A', 'en']]) {
    const saved = savedPapers.find(x => x.id === paper.id)
    await put(`/papers/${paper.id}`, { version: saved.version, paper: { ...saved.data, settings: { selected: [kind], languages: { [kind]: lang }, depth: 'brief' } } })
  }
  const batch = await post(reqPath, { paper_ids: [source.id, related.id], use_paper_settings: true, ui_language: 'zh-TW', unlimited: true })
  const batchTasks = await post(`/requests/${batch.id}/start`, { use_defaults: true })
  assert.deepEqual(batchTasks.map(x => [x.paper_id, x.data.type, x.data.language]), [[source.id, 'C', 'zh-TW'], [related.id, 'A', 'en']])
  const broken = await post(`/projects/${project.id}/papers`, { title: 'Initially unreadable source' })
  await upload(broken.id, 'broken.pdf', '%PDF-1.4\n%%EOF\n')
  const parseBroken = await post(`/papers/${broken.id}/parse`)
  await waitFor('isolated parse failure and independent branches', async () => {
    const tasks = (await summary(project.id)).task
    return tasks.some(x => x.id === parseBroken.id && x.data.status === 'failed') &&
      batchTasks.every(task => tasks.some(x => x.id === task.id && x.data.status === 'completed'))
  })
  await upload(broken.id, 'replacement.pdf', pdf(researchLines.slice(0, 4)))
  await post(`/tasks/${parseBroken.id}/control`, { action: 'retry' })
  await waitFor('retry of the failed parse', async () => (await summary(project.id)).task.find(x => x.id === parseBroken.id && x.data.status === 'completed'))
  log(['AC03', 'AC12', 'AC17'], { perStudy: batchTasks.map(x => [x.data.type, x.data.language]), corruptSource: 'failed independently', retryWithReplacement: 'completed' })

  const repo = join(root, 'fixture-repo')
  await mkdir(repo)
  await copyFile(fixture, join(repo, 'experiment.mjs'))
  command('git', ['init', '-q', '-b', 'main'], repo)
  command('git', ['add', 'experiment.mjs'], repo)
  command('git', ['-c', 'user.name=EduForge', '-c', 'user.email=eduforge@localhost', 'commit', '-qm', 'Synthetic nearest-centroid fixture'], repo)
  const commit = command('git', ['rev-parse', 'HEAD'], repo)
  const targetSource = knowledge.data.units[0].id
  const plan = await savePlan(source.id, targetSource, repo, commit, [['node', 'experiment.mjs']])
  const run = await post(`/plans/${plan.id}/run`)
  const completed = await waitFor('experiment completion', async () => (await summary(project.id)).run.find(x => x.id === run.id && x.data.progress === 'finished'))
  assert.equal(completed.data.status, 'completed')
  assert.equal(completed.data.comparison, 'matched')
  assert.equal(completed.data.actual, 1)
  assert.equal(completed.data.original_commit, commit)
  const completedDir = join(root, 'eduforge-' + completed.data.run_id)
  await copyFile(join(completedDir, 'execution.log'), join(root, 'experiment.log'))
  await copyFile(join(completedDir, 'result.csv'), join(root, 'result.csv'))
  await copyFile(join(completedDir, 'status.json'), join(root, 'experiment-status.json'))
  command('git', ['bundle', 'create', join(root, 'experiment.bundle'), '--all'], join(completedDir, 'repo'))
  for (const path of completed.data.evidence) {
    const data = await download(`/files/${completed.id}?path=${encodeURIComponent(path)}`)
    await writeFile(join(root, basename(path)), data)
  }
  log(['AC08', 'AC09'], { runner: completed.data.host.mode, originalCommit: commit, finalCommit: completed.data.git_commit, execution: completed.data.status, comparison: completed.data.comparison, actual: completed.data.actual, evidence: completed.data.evidence })

  for (const lang of ['en', 'zh-TW']) {
    const report = await generate('C', lang)
    const tasks = await post(`/requests/${report.id}/start`, { use_defaults: true })
    assert.equal(tasks.length, 1)
    assert.equal(tasks[0].data.type, 'C')
    const out = await waitFor('C report', async () => (await summary(project.id)).artifact.find(x => x.data.request_id === report.id))
    assert.equal(out.data.language, lang)
    assert.ok(out.data.suggested_target?.citations.length)
    assert.ok(out.data.sections.some(s => s.body.includes(completed.data.git_commit)))
  }
  assert.equal((await summary(project.id)).run.length, 1)
  const exported = await download(`/projects/${project.id}/export?personal=true`)
  const zip = new FormData()
  zip.append('file', new Blob([exported]), 'project.zip')
  zip.append('personal', 'true')
  const imported = await post('/projects/import', zip)
  const migrated = await summary(imported.id)
  assert.ok(migrated.artifact.length >= 5)
  assert.ok(migrated.artifact.every(x => x.owner_user_id === session.owner_user_id))
  assert.equal(migrated.provider.filter(x => x.data.has_credential).length, 1)
  log(['AC14', 'AC19', 'AC23'], { originalProject: project.id, importedProject: imported.id, artifacts: migrated.artifact.length, runCountAfterLanguageChanges: 1, exportSHA256: createHash('sha256').update(exported).digest('hex') })

  const recovery = await savePlan(source.id, targetSource, repo, commit, [
    ['node', '-e', "require('fs').appendFileSync('first-step.txt', 'once\\n')"],
    ['node', '-e', "setTimeout(() => {}, 30000)"],
  ], [[], ['node', 'experiment.mjs']])
  const recovering = await post(`/plans/${recovery.id}/run`)
  const directory = join(root, 'eduforge-' + recovering.data.run_id)
  await waitFor('second command checkpoint', async () => {
    const progress = JSON.parse(await readFile(join(directory, 'checkpoint.json'), 'utf8').catch(() => '{}'))
    return progress.completed_commands === 1 && progress.running_command === 2
  })
  await writeFile(join(root, 'state.json'), JSON.stringify({ project: project.id, owner: session.owner_user_id, source: source.id, citation: targetSource, template: template.id, run: recovering.id, directory, completed: completed.id }))
  await writeFile(join(root, 'seed.json'), JSON.stringify(evidence, null, 2) + '\n')
  console.log('Ready to recreate the isolated container')
}

async function afterRecreate() {
  const saved = JSON.parse(await readFile(join(root, 'state.json'), 'utf8'))
  assert.equal(session.owner_user_id, saved.owner)
  const done = await waitFor('resumed experiment', async () => (await summary(saved.project)).run.find(x => x.id === saved.run && x.data.progress === 'finished'))
  assert.equal(done.data.status, 'completed')
  assert.equal(done.data.comparison, 'matched')
  assert.equal(done.data.completed_commands, 2)
  assert.equal(await readFile(join(saved.directory, 'repo/first-step.txt'), 'utf8'), 'once\n')
  const executionLog = await readFile(join(saved.directory, 'execution.log'), 'utf8')
  assert.match(executionLog, /Recovering unfinished command 2/)
  const records = await summary(saved.project)
  assert.equal(records.run.length, 2)
  assert.ok(records.template.some(x => x.id === saved.template))
  assert.ok(records.reading.some(x => x.data.paper_id === saved.source))
  assert.ok(records.provider.some(x => x.data.has_credential))
  log(['AC11', 'AC21', 'AC25'], { persisted: ['project', 'reading', 'templates', 'provider metadata', 'tasks'], sameRun: done.data.run_id, completedCommands: done.data.completed_commands, firstCommandInvocations: 1, recovery: 'confirmed resume command', result: done.data.actual })
  const fixturePlan = records.plan.find(x => x.data.commands.length === 1)
  const { repo, commit } = fixturePlan.data
  const controlled = await savePlan(saved.source, saved.citation, repo, commit, [
    ['node', '-e', 'setTimeout(() => {}, 2200)'], ['node', 'experiment.mjs'],
  ])
  const pauseRun = await post(`/plans/${controlled.id}/run`)
  await waitFor('runner ready to pause', async () => (await summary(saved.project)).run.find(x => x.id === pauseRun.id && x.data.progress === 'running'))
  await post(`/runs/${pauseRun.id}/control`, { action: 'pause' })
  await waitFor('paused at a command boundary', async () => (await summary(saved.project)).run.find(x => x.id === pauseRun.id && x.data.progress === 'paused'))
  const queuedPlan = await savePlan(saved.source, saved.citation, repo, commit, [['node', 'experiment.mjs']])
  const queuedRun = await post(`/plans/${queuedPlan.id}/run`)
  await delay(800)
  assert.equal((await summary(saved.project)).run.find(x => x.id === queuedRun.id).data.progress, 'queued')
  const cancelled = await post(`/runs/${queuedRun.id}/control`, { action: 'cancel' })
  assert.equal(cancelled.data.status, 'not_executed')
  assert.equal(cancelled.data.progress, 'finished')
  await post(`/runs/${pauseRun.id}/control`, { action: 'resume' })
  const unpaused = await waitFor('resumed run finished', async () => (await summary(saved.project)).run.find(x => x.id === pauseRun.id && x.data.progress === 'finished'))
  assert.equal(unpaused.data.comparison, 'matched')
  log(['AC13'], { pausedAtCommandBoundary: true, hostConcurrency: 1, queuedWorkCancelledBeforeExecution: true, originalRunResumed: unpaused.data.run_id })
  await writeFile(join(root, 'recovery.log'), executionLog)
  await writeFile(join(root, 'recreate.json'), JSON.stringify(evidence, null, 2) + '\n')
}

async function sshStart() {
  const saved = JSON.parse(await readFile(join(root, 'state.json'), 'utf8'))
  const { commit, known_hosts } = JSON.parse(await readFile(join(root, 'ssh-fixture.json'), 'utf8'))
  const key = await readFile('/secrets/acceptance-ssh-key', 'utf8')
  await put('/connections/ssh', {
    config: 'Host fixture\n  HostName ssh-fixture\n  User experiment\n',
    known_hosts,
    private_key: key,
  })
  const host = { mode: 'ssh', ssh_alias: 'fixture', workdir: '/home/experiment/workspace', allowed_root: '/home/experiment/workspace', os: 'linux', cpu: process.arch, gpu: 'none', environment: 'direct', concurrency: 1 }
  const probe = await post('/hosts/probe', host)
  assert.equal(probe.runner_compatible, true)
  const plan = await savePlan(saved.source, saved.citation, host.workdir + '/fixture-repo', commit, [
    ['node', '-e', "console.log('SSH command one');"],
    ['node', '-e', "setTimeout(() => import('./experiment.mjs'), 18000)"],
  ], [], host)
  const run = await post(`/plans/${plan.id}/run`)
  await waitFor('remote runner started its second command', async () => {
    const active = (await summary(saved.project)).run.find(x => x.id === run.id)
    if (active?.data.progress === 'finished') throw new Error(JSON.stringify(active.data))
    return active?.data.progress === 'running' && active.data.completed_commands === 1
  })
  await writeFile(join(root, 'ssh-state.json'), JSON.stringify({ id: run.id, runID: run.data.run_id, commit, probe }))
}

async function sshCheck() {
  const saved = JSON.parse(await readFile(join(root, 'state.json'), 'utf8'))
  const remote = JSON.parse(await readFile(join(root, 'ssh-state.json'), 'utf8'))
  const done = await waitFor('SSH experiment after service restart', async () => (await summary(saved.project)).run.find(x => x.id === remote.id && x.data.progress === 'finished'))
  assert.equal(done.data.run_id, remote.runID)
  assert.equal(done.data.original_commit, remote.commit)
  assert.equal(done.data.status, 'completed')
  assert.equal(done.data.comparison, 'matched')
  assert.equal(done.data.actual, 1)
  const logPath = done.data.evidence.find(path => path.endsWith('.log'))
  const execution = await download(`/files/${done.id}?path=${encodeURIComponent(logPath)}`)
  assert.equal(execution.toString().match(/^SSH command one$/mg)?.length, 1)
  assert.ok(!execution.toString().includes('Recovering unfinished'))
  log(['AC08', 'AC11'], { host: 'separate OpenSSH container', sameRun: remote.runID, originalCommit: remote.commit, finalCommit: done.data.git_commit, observed: remote.probe.observed, firstCommandInvocations: 1, execution: done.data.status, comparison: done.data.comparison })
  await writeFile(join(root, 'ssh.log'), execution)
  await writeFile(join(root, 'ssh.json'), JSON.stringify(evidence, null, 2) + '\n')
}

session = await request('/api/session')
await { seed, 'after-recreate': afterRecreate, 'ssh-start': sshStart, 'ssh-check': sshCheck }[mode]()
