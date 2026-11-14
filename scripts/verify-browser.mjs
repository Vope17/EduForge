import assert from 'node:assert/strict'
import { readFile, writeFile } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'

const entry = process.env.EDUFORGE_PLAYWRIGHT_MODULE
assert.ok(entry, 'Set EDUFORGE_PLAYWRIGHT_MODULE to the absolute path to Playwright index.mjs')
const { chromium } = await import(pathToFileURL(entry))
const base = process.env.EDUFORGE_ACCEPTANCE_URL || 'http://127.0.0.1:18080'
assert.match(base, /^http:\/\/127\.0\.0\.1:\d+$/)
const evidence = new URL('../docs/evidence/compose/', import.meta.url)
const seed = JSON.parse(await readFile(new URL('seed.json', evidence), 'utf8'))
const fixture = seed.find(row => row.observation.originalProject).observation.originalProject
const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
page.setDefaultTimeout(12000)
const errors = []
page.on('pageerror', error => errors.push(error.message))
try {
  await page.goto(base, { waitUntil: 'networkidle' })
  await page.getByRole('button', { name: 'EN', exact: true }).click()
  await page.getByRole('button', { name: 'Settings', exact: true }).waitFor()
  await page.locator('#project-picker').selectOption(fixture)
  await page.locator('.paper-link-main', { hasText: 'Nearest-centroid classification' }).click()
  await page.locator('.stepper').getByRole('tab', { name: /Sources/ }).click()
  await page.getByText('research.pdf', { exact: true }).waitFor()
  await page.getByText('supplement.pptx', { exact: true }).waitFor()
  await page.screenshot({ path: new URL('library-en.png', evidence).pathname })

  await page.locator('.stepper').getByRole('tab', { name: /Outputs/ }).click()
  await page.locator('.subnav').getByRole('tab', { name: /Outputs & versions/ }).click()
  const reader = page.locator('.artifact-card').filter({ has: page.locator('.output-letter', { hasText: /^A$/ }) }).first()
  await reader.getByRole('button', { name: 'Read / edit' }).click()
  const editor = page.locator('.sheet')
  await editor.waitFor()
  await page.frameLocator('.reader-frame').getByRole('heading', { name: 'Nearest-centroid classification', exact: true }).waitFor()
  await editor.getByRole('button', { name: 'Close', exact: true }).click()
  const slide = page.locator('.artifact-card').filter({ has: page.locator('.output-letter', { hasText: /^B$/ }) }).first()
  await slide.getByRole('button', { name: 'Read / edit' }).click()
  await editor.locator('.slide-preview').first().waitFor()
  await editor.getByRole('tab', { name: /Edit/ }).click()
  await editor.locator('textarea').first().waitFor()
  await editor.getByRole('button', { name: 'Close', exact: true }).click()

  await page.getByRole('button', { name: '繁中', exact: true }).click()
  await page.getByRole('button', { name: '設定', exact: true }).waitFor()
  assert.equal(await page.locator('html').getAttribute('lang'), 'zh-TW')
  assert.match(await reader.innerText(), /\ben\b/)
  await page.screenshot({ path: new URL('studio-zh-TW.png', evidence).pathname })
  const projectName = 'Browser acceptance ' + Date.now()
  await page.getByRole('button', { name: '建立專案', exact: true }).click()
  await page.getByLabel('專案名稱', { exact: true }).fill(projectName)
  await page.locator('.modal').getByRole('button', { name: '建立', exact: true }).click()
  await page.locator('#project-picker option:checked').filter({ hasText: projectName }).waitFor({ state: 'attached' })
  await page.locator('.sidebar-new').click()
  await page.getByLabel('研究標題', { exact: true }).fill('Browser-created study')
  await page.locator('.modal').getByRole('button', { name: '建立', exact: true }).click()
  await page.locator('.paper-link-main', { hasText: 'Browser-created study' }).waitFor()
  await page.locator('.brand').click()
  page.once('dialog', dialog => dialog.accept())
  await page.locator('.project-footer').getByRole('button', { name: '刪除', exact: true }).click()
  await page.locator('#project-picker option:checked').filter({ hasText: projectName }).waitFor({ state: 'detached' })
  assert.deepEqual(errors, [])
  const result = { chromium: browser.version(), checks: ['project selection', 'PDF and PPTX source listing', 'reader in-page preview', 'slide preview and editing controls', 'English and Traditional Chinese navigation', 'existing reader language preserved', 'create a project through the browser', 'add a study through the browser', 'delete the browser-created project'], pageErrors: errors }
  await writeFile(new URL('browser.json', evidence), JSON.stringify(result, null, 2) + '\n')
  console.log(JSON.stringify(result))
} finally {
  await browser.close()
}
