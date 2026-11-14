import test from 'node:test'
import assert from 'node:assert/strict'
import { t, labels, errorText, outputLanguage, move } from '../frontend/src/lib/i18n.ts'

test('all registered UI labels have distinct nonempty English and Traditional Chinese values', () => {
  assert.ok(Object.keys(labels).length > 150)
  for (const [key, values] of Object.entries(labels)) {
    assert.equal(values.length, 2, key)
    assert.ok(values.every(value => typeof value === 'string' && value.length), key)
    assert.equal(t(key, 'zh-TW'), values[0], key)
    assert.equal(t(key, 'en'), values[1], key)
  }
})
test('explicit output language wins over interface language', () => {
  assert.equal(outputLanguage('en', 'zh-TW'), 'en')
  assert.equal(outputLanguage('zh-TW', 'en'), 'zh-TW')
  assert.equal(outputLanguage('', 'zh-TW'), 'zh-TW')
  assert.equal(outputLanguage('', 'en'), 'en')
})
test('reordering slides does not mutate the previous version', () => {
  const previous = ['question', 'method', 'results']
  assert.deepEqual(move(previous, 1, -1), ['method', 'question', 'results'])
  assert.deepEqual(previous, ['question', 'method', 'results'])
  assert.deepEqual(move(previous, 0, -1), previous)
  assert.deepEqual(move(previous, 2, 1), previous)
})
test('important platform errors are localized, with the source error code retained for unknown errors', () => {
  assert.match(errorText('confirm_knowledge_first', 'en'), /lesson’s knowledge/)
  assert.match(errorText('confirm_knowledge_first', 'zh-TW'), /課次的知識/)
  assert.match(errorText('new_error_code', 'zh-TW'), /操作未完成.*new_error_code/)
})
test('model markdown markers are removed for previews', async () => {
  const { plainText } = await import('../frontend/src/lib/markup.ts')
  assert.equal(plainText('**研究動機**\n網路 **監控**\n- 一\n## 方法'), '研究動機\n網路 監控\n• 一\n方法')
  assert.equal(plainText('a * b * c'), 'a * b * c')
  const id = '1c379de376f7d26460d3a7966799d12e'
  assert.equal(plainText(`大部分。〔${id}:p1〕〔${id}:p2〕`), '大部分。')
  assert.equal(plainText('時間（TRCA）。【p1】【p2】 and (1) [CLS]'), '時間（TRCA）。 and (1) [CLS]')
})
test('reader editing round-trips the stored markup subset', async () => {
  const { JSDOM } = await import('jsdom').catch(() => ({}))
  if (!JSDOM) return // DOMParser is only needed in the browser; covered by the UI flow
  const dom = new JSDOM('')
  globalThis.DOMParser = dom.window.DOMParser
  const { markupToHtml, htmlToMarkup } = await import('../frontend/src/lib/markup.ts')
  const text = '**研究動機**\n網路 **監控** 很重要。\n- 第一點\n- 第二點'
  assert.equal(htmlToMarkup(markupToHtml(text)), text)
})
test('figures are assigned once, explicit lists win', async () => {
  const { sectionFigures } = await import('../frontend/src/lib/markup.ts')
  const images = [{ path: 'f1', citation: 'p1' }, { path: 'f2', citation: 'p2' }]
  assert.deepEqual(sectionFigures([{ citations: ['p1', 'p2'] }, { citations: ['p2'] }, { figures: [], citations: ['p1'] }], images), [['f1', 'f2'], [], []])
})
test('AI edit selections expand to blocks and accepted text updates the draft', async () => {
  const { blockIDs, selectionBlocks, applyBlock, blockText, boxCapacity } = await import('../frontend/src/lib/aiEdit.ts')
  const reader = [{ title: '方法', body: '**重點**\n- 一', citations: ['u1'] }, { title: '結果', body: 'b', citations: [] }]
  assert.deepEqual(blockIDs('A', reader, ['sec:1', 'sec:0', 'sec:0']), ['sec:0:title', 'sec:0:body', 'sec:1:title', 'sec:1:body'])
  const [title, body] = selectionBlocks('A', reader, ['sec:0'])
  assert.equal(title.kind, 'title'); assert.equal(body.kind, 'body'); assert.deepEqual(body.citations, ['u1'])
  applyBlock('A', reader, 'sec:0:title', '新\n標題')
  assert.equal(reader[0].title, '新 標題')

  const slides = [{ title: 'T', body: 'x', notes: '講稿', citations: [], layout: [
    { id: 't', kind: 'text', role: 'title', x: 0.06, y: 0.04, w: 0.88, h: 0.12, text: 'T', size: 28 },
    { id: 'b', kind: 'text', role: 'body', x: 0.06, y: 0.2, w: 0.5, h: 0.5, text: 'x', size: 20 },
    { id: 'i', kind: 'image', role: 'figure', x: 0.6, y: 0.2, w: 0.3, h: 0.5, path: 'p.png' },
  ] }]
  assert.deepEqual(blockIDs('B', slides, ['slide:0']), ['el:0:t', 'el:0:b', 'notes:0'])
  assert.deepEqual(blockIDs('B', slides, ['el:0:b']), ['el:0:b'])
  const blocks = selectionBlocks('B', slides, ['slide:0'], { points: 960, ratio: 16 / 9, language: 'zh-TW' })
  assert.deepEqual(blocks.map(b => b.kind), ['slide_title', 'slide_text', 'notes'])
  assert.equal(blocks[1].max_chars, boxCapacity(slides[0].layout[1], 960, 16 / 9, 'zh-TW'))
  assert.ok(boxCapacity(slides[0].layout[1], 960, 16 / 9, 'en') > blocks[1].max_chars)
  applyBlock('B', slides, 'el:0:b', '- 新內容')
  assert.equal(slides[0].body, '- 新內容', 'slide fields follow the edited box')
  applyBlock('B', slides, 'notes:0', '新講稿')
  assert.equal(blockText('B', slides, 'notes:0'), '新講稿')
  assert.equal(blockText('B', slides, 'el:0:gone'), null)
})
test('template AI changes apply to the working spec only when the value is unchanged', async () => {
  const { applySpecValue, sameValue, specValue, styleChanges, specKeyLabel } = await import('../frontend/src/lib/templateSpec.ts')
  const spec = { kind: 'docx', styles: { h1: { font: 'Arial', size: 15, color: '', bold: true } }, margins: { top: 25, bottom: 25, left: 25, right: 25 }, footer: { text: '', editable: true }, colors: {} }
  assert.ok(sameValue({ size: 15, font: 'Arial' }, { font: 'Arial', size: 15 }), 'key order does not matter')
  assert.ok(!sameValue(specValue(spec, 'styles.h1'), { font: 'Arial', size: 16, color: '', bold: true }))
  applySpecValue(spec, 'styles.h1', { font: 'Arial', size: 20, color: '1F4E79', bold: true })
  applySpecValue(spec, 'footer', 'Lab')
  assert.equal(spec.styles.h1.size, 20); assert.equal(spec.footer.text, 'Lab'); assert.equal(spec.footer.editable, true)
  const t = k => k
  assert.deepEqual(styleChanges({ size: 15, color: '' }, { size: 20, color: '1F4E79' }, t).map(c => c.field), ['size', 'color'])
  assert.equal(specKeyLabel('colors.accent3', k => k === 'tplAccent' ? 'Accent {n}' : k), 'Accent 3')
})

test('reader figures sit under the paragraph that mentions them, in the editor as in exports', async () => {
  const { figureBlocks, joinBlocks } = await import('../frontend/src/lib/markup.ts')
  const body = '**Overview**\nAIDA has three stages.\n\nThe pipeline is shown in Figure 9.\n- step one\n- step two\n\nResults come last.'
  const captions = { 'a.png': 'Figure 9: System architecture.', 'b.png': 'Table 2: Results.' }
  const blocks = figureBlocks(body, ['b.png', 'a.png'], captions)
  const at = name => blocks.findIndex(b => b.figures.includes(name))
  assert.ok(blocks[at('a.png')].text.endsWith('- step two'), 'a mentioned figure follows its paragraph (after its list)')
  assert.ok(at('b.png') >= 0 && !blocks[at('b.png')].text.endsWith('**Overview**'), 'others are spread between paragraphs')
  assert.equal(joinBlocks(blocks), body, 'joining the blocks gives the body back')
  assert.deepEqual(figureBlocks('one line', [], {}), [{ text: 'one line', figures: [] }])
  assert.equal(figureBlocks('', ['a.png'], captions).length, 1, 'an empty body still shows its figures')
})

test('formulas in reader text become chips the editor renders, and prices stay text', async () => {
  const { splitMath, displayFormula, markupToHtml } = await import('../frontend/src/lib/markup.ts')
  assert.deepEqual(splitMath('權重 $w_{uv}$ 與 \\(\\alpha\\)，費用 $5 與 $10'), [{ text: '權重 ' }, { tex: 'w_{uv}' }, { text: ' 與 ' }, { tex: '\\alpha' }, { text: '，費用 $5 與 $10' }])
  assert.equal(displayFormula('$$ e(f) = \\prod_i c_i $$'), 'e(f) = \\prod_i c_i')
  assert.equal(displayFormula('text $$x$$ more'), null)
  const html = markupToHtml('式中 $a<b$：\n$$x^2$$\n- **重點** $y$')
  assert.match(html, /<span class="math-chip" contenteditable="false" data-tex="a&lt;b">\$a&lt;b\$<\/span>/)
  assert.match(html, /<div class="math-block" contenteditable="false" data-tex="x\^2" data-display="1">/)
  assert.match(html, /<li><b>重點<\/b> <span class="math-chip"[^>]*data-tex="y"/)
})

test('teaching order lists unfiled lessons first, then chapters, each by position', async () => {
  const { chapterList, groupLessons, orderedLessons, orderOf, moveLessonTo, moveLessonBy, moveChapterBy } = await import('../frontend/src/lib/library.ts')
  const chapter = (id, position) => ({ id, data: { name: id, position, settings: {} } })
  const lesson = (id, position, chapterID = '') => ({ id, data: { title: id, position, chapter: chapterID, settings: {} } })
  const chapters = chapterList({ chapter: [chapter('two', 1), chapter('one', 0)] })
  assert.deepEqual(chapters.map(c => c.id), ['one', 'two'])
  const lessons = [lesson('b', 1, 'one'), lesson('a', 0, 'one'), lesson('x', 0, 'two'), lesson('free', 0), lesson('gone', 0, 'deleted-chapter')]
  assert.deepEqual(orderedLessons(lessons, chapters).map(l => l.id), ['free', 'gone', 'a', 'b', 'x'])
  const groups = groupLessons(lessons, chapters)
  assert.equal(groups[0].key, '')
  const order = orderOf(groups)
  assert.deepEqual(order.chapters, ['one', 'two'])
  // Dropping before a lesson, onto the end of a chapter, and keyboard moves.
  assert.deepEqual(moveLessonTo(order, 'x', 'one', 'a').groups.find(g => g.chapter === 'one').lessons, ['x', 'a', 'b'])
  assert.deepEqual(moveLessonTo(order, 'free', 'two').groups.find(g => g.chapter === 'two').lessons, ['x', 'free'])
  assert.deepEqual(moveLessonTo(order, 'a', '').groups.find(g => g.chapter === '').lessons, ['free', 'gone', 'a'])
  assert.deepEqual(moveLessonBy(order, 'a', 1).groups.find(g => g.chapter === 'one').lessons, ['b', 'a'])
  assert.deepEqual(moveLessonBy(order, 'b', 1).groups.find(g => g.chapter === 'two').lessons, ['b', 'x'], 'the last lesson of a chapter joins the next chapter')
  assert.deepEqual(moveLessonBy(order, 'x', -1).groups.find(g => g.chapter === 'one').lessons, ['a', 'b', 'x'], 'the first lesson of a chapter joins the previous one')
  assert.equal(moveLessonBy(order, 'free', -1), order, 'the first lesson cannot move earlier')
  assert.deepEqual(moveChapterBy(order, 'one', 1).chapters, ['two', 'one'])
  assert.equal(moveChapterBy(order, 'one', -1), order)
  assert.deepEqual(order.groups.find(g => g.chapter === 'one').lessons, ['a', 'b'], 'moves do not change the original order')
})
test('settings inherit course, then chapter, then lesson; empty fields never blank a value', async () => {
  const { effectiveSettings } = await import('../frontend/src/lib/settings.ts')
  const course = { audience: 'undergraduates', minutes: 90, level: 'advanced', depth: 'deep', instructions: { A: 'plain' } }
  assert.deepEqual(effectiveSettings(undefined), { depth: 'standard', level: 'intermediate', minutes: 50, languages: {}, instructions: {} })
  const merged = effectiveSettings(course, { minutes: 30, audience: '' }, { audience: 'graduate students', instructions: { A: '' , B: 'brief' } })
  assert.equal(merged.audience, 'graduate students')
  assert.equal(merged.minutes, 30)
  assert.equal(merged.level, 'advanced')
  assert.equal(merged.depth, 'deep')
  assert.deepEqual(merged.instructions, { A: 'plain', B: 'brief' })
  assert.equal(effectiveSettings(course, {}, {}).minutes, 90)
})
