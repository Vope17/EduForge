<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { slotLabel, specKeyLabel, specTheme, wordRoleLabel, BOX_KEYS, FONT_KEYS, THEME_SLOTS, WORD_ROLES, FONT_SUGGESTIONS } from '../../lib/templateSpec.ts'
import SubNav from '../SubNav.vue'
import Icon from '../Icon.vue'
import TemplateCanvas from './TemplateCanvas.vue'
import WordSample from './WordSample.vue'
import TemplateAIPanel from './TemplateAIPanel.vue'
import { useUndo } from '../../lib/undo.ts'
import type { CanvasBox } from './TemplateCanvas.vue'
import type { Template, TemplateSpec, TemplateVersion } from '../../api/types.ts'

// Edits an imported template's design (colours, fonts, title/content areas;
// Word styles, margins, header/footer). Saving rewrites the template file as
// a new version; earlier versions can be restored.
const props = defineProps<{ id: string }>()
const emit = defineEmits<{ close: [] }>()
const HEADER_PARTS = ['header', 'footer'] as const
const MARGIN_SIDES = ['top', 'bottom', 'left', 'right'] as const
const { state, t, api, act, fileURL, explain } = useWorkspace()
const record = computed(() => (state.snapshot.template || []).find(x => x.id === props.id))
const word = computed(() => record.value?.data.kind === 'docx')
const spec = ref<TemplateSpec | null>(null), loaded = ref(''), loadError = ref(''), saving = ref(false)
const tab = ref('edit')
// AI editing: the panel and the selected spec keys.
const aiOpen = ref(false), aiSel = ref<string[]>([])
const picked = (key: string) => aiSel.value.includes(key)
function togglePick(key: string) { aiSel.value = picked(key) ? aiSel.value.filter(k => k !== key) : [...aiSel.value, key] }
const fontsPicked = computed(() => FONT_KEYS.every(picked))
function toggleFonts() { aiSel.value = fontsPicked.value ? aiSel.value.filter(k => !FONT_KEYS.includes(k)) : [...new Set([...aiSel.value, ...FONT_KEYS])] }
const samplePick = (role: string) => togglePick(['header', 'footer'].includes(role) ? role : 'styles.' + role)
const sampleSelected = computed(() => aiSel.value.map(k => k.replace('styles.', '')))
const dirty = computed(() => !!spec.value && JSON.stringify(spec.value) !== loaded.value)
const history = computed(() => record.value?.data.history || [])

async function load() {
  loadError.value = ''
  try {
    const s = await api<TemplateSpec>(`/templates/${props.id}/spec`)
    spec.value = s; loaded.value = JSON.stringify(s)
    undoer.reset()
  } catch (err) { loadError.value = explain((err as Error).message) }
}
onMounted(load)
const undoer = useUndo(() => spec.value, value => { spec.value = value }, { enabled: () => tab.value === 'edit' })
// A save or restore changes the record version: read the new file's spec.
watch(() => record.value?.version, (now, before) => { if (before && now !== before) load() })

async function save() {
  if (!record.value) return
  const version = record.value.version
  saving.value = true
  const result = await act(() => api(`/templates/${props.id}/spec`, { method: 'PUT', body: { version, spec: spec.value } }))
  saving.value = false
  if (result) tab.value = 'edit'
}
async function restore(entry: TemplateVersion) {
  if (!record.value || (dirty.value && !confirm(t('discardChanges')))) return
  const version = record.value.version
  await act(() => api(`/templates/${props.id}/restore`, { method: 'POST', body: { version, from: entry.version } }))
}
function close() {
  if (dirty.value && !confirm(t('discardChanges'))) return
  emit('close')
}
function onKey(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  if (aiOpen.value) { aiOpen.value = false; return }
  close()
}
onMounted(() => { document.addEventListener('keydown', onKey); document.body.classList.add('no-scroll') })
onUnmounted(() => { document.removeEventListener('keydown', onKey); document.body.classList.remove('no-scroll') })

// Slides preview.
const look = computed(() => spec.value && record.value ? specTheme(spec.value, record.value.data, JSON.parse(loaded.value)) : null)
const ratio = computed(() => record.value?.data.width ? `${record.value.data.width} / ${record.value.data.height}` : '16 / 9')
const points = computed(() => record.value?.data.width ? record.value.data.width / 12700 : 960)
const coverBoxes = computed((): CanvasBox[] => spec.value ? ([
  { key: 'cover_title_box', label: t('tplCoverTitle'), box: spec.value.cover_title_box, role: 'cover', sample: t('tplSampleTitle') },
  { key: 'cover_subtitle_box', label: t('tplCoverSubtitle'), box: spec.value.cover_subtitle_box, role: 'subtitle', sample: t('tplSampleSubtitle') },
].filter(b => b.box) as CanvasBox[]) : [])
const contentBoxes = computed((): CanvasBox[] => spec.value ? ([
  { key: 'title_box', label: t('tplTitleArea'), box: spec.value.title_box, role: 'title', sample: t('tplSampleHeading') },
  { key: 'body_box', label: t('tplBodyArea'), box: spec.value.body_box, role: 'body', sample: [t('tplSamplePoint1'), t('tplSamplePoint2'), t('tplSamplePoint3')] },
].filter(b => b.box) as CanvasBox[]) : [])
// key is one of BOX_KEYS.
function setBox(key: string, box: number[]) { if (spec.value) (spec.value as unknown as Record<string, number[]>)[key] = box }
const colorInput = (c: string | undefined) => '#' + (c || '000000')
function setColor(target: object, key: string, value: string) { (target as Record<string, string>)[key] = value.replace('#', '').toUpperCase() }
const when = (iso: string | undefined) => iso ? new Date(iso).toLocaleString(state.language) : ''
function entryImage(entry: TemplateVersion) {
  const d = entry.template as Partial<Template>
  const path = word.value ? d.preview : (d.cover_image || d.content_image)
  return path && record.value ? fileURL(record.value, path) : ''
}
</script>

<template>
  <Teleport to="body">
    <div class="sheet-backdrop" @click.self="close">
      <section v-if="record" class="sheet template-editor" role="dialog" aria-modal="true" :aria-label="t('editTemplate')">
        <header class="sheet-head">
          <div class="grow">
            <span class="eyebrow">{{ t(word ? 'readerTemplate' : 'template') }} · v{{ record.version }}</span>
            <h2>{{ record.data.name }}</h2>
          </div>
          <button class="primary" :disabled="!!state.busy || !dirty || saving" @click="save">{{ t(saving ? 'tplSaving' : 'tplSave') }}</button>
          <button class="icon-button" :aria-label="t('close')" @click="close">×</button>
        </header>
        <div class="sheet-tabs">
          <SubNav v-model="tab" :tabs="[{ key: 'edit', label: t('edit'), icon: 'sliders' }, { key: 'versions', label: t('tplVersions'), icon: 'layers', count: history.length + 1 }]" />
          <span class="caption">{{ t('tplSaveHint') }}</span>
          <span v-if="tab === 'edit'" class="undo-tools">
            <button class="icon-button undo-button" :disabled="!undoer.canUndo.value" :title="t('undo') + ' (Ctrl+Z)'" :aria-label="t('undo')" @click="undoer.undo()"><Icon name="undo" /></button>
            <button class="icon-button redo-button" :disabled="!undoer.canRedo.value" :title="t('redo') + ' (Ctrl+Shift+Z)'" :aria-label="t('redo')" @click="undoer.redo()"><Icon name="redo" /></button>
          </span>
          <button v-if="tab === 'edit'" class="secondary small ai-toggle" :class="{ on: aiOpen }" :aria-pressed="aiOpen" @click="aiOpen = !aiOpen"><Icon name="sparkle" /> {{ t('aiEdit') }}</button>
        </div>

        <div class="sheet-body" :class="{ 'with-ai': aiOpen && tab === 'edit' && spec }">
          <p v-if="loadError" class="issue-text">{{ loadError }}</p>

          <div v-if="tab === 'edit' && spec" class="editor-main"><div class="template-edit">
            <form class="template-form" @submit.prevent>
              <template v-if="!word">
                <section class="panel">
                  <h3>{{ t('tplColors') }}</h3>
                  <div class="swatch-grid">
                    <div v-for="slot in THEME_SLOTS" :key="slot" class="swatch-row" :class="{ 'ai-selected': aiOpen && picked('colors.' + slot) }">
                      <label class="swatch">
                        <input type="color" :value="colorInput(spec.colors![slot])" @input="setColor(spec.colors!, slot, ($event.target as HTMLInputElement).value)">
                        <span>{{ slotLabel(slot, t) }}</span>
                        <code>{{ spec.colors![slot] || '—' }}</code>
                      </label>
                      <label v-if="aiOpen" class="ai-select" :title="t('selectForAI')"><input type="checkbox" :checked="picked('colors.' + slot)" :aria-label="t('selectForAI') + ' · ' + slotLabel(slot, t)" @change="togglePick('colors.' + slot)"></label>
                    </div>
                  </div>
                </section>
                <section class="panel" :class="{ 'ai-selected': aiOpen && fontsPicked }">
                  <h3 class="ai-heading">{{ t('tplFonts') }}<label v-if="aiOpen" class="check ai-pick"><input type="checkbox" :checked="fontsPicked" @change="toggleFonts">{{ t('selectForAI') }}</label></h3>
                  <div class="field-grid">
                    <label>{{ t('tplHeadingFont') }}<input v-model="spec.heading_font" list="font-suggestions"></label>
                    <label>{{ t('tplHeadingFontEA') }}<input v-model="spec.heading_font_ea" list="font-suggestions"></label>
                    <label>{{ t('tplBodyFont') }}<input v-model="spec.body_font" list="font-suggestions"></label>
                    <label>{{ t('tplBodyFontEA') }}<input v-model="spec.body_font_ea" list="font-suggestions"></label>
                  </div>
                </section>
                <section v-if="aiOpen" class="panel">
                  <h3>{{ t('tplLayout') }}</h3>
                  <div class="field-grid">
                    <label v-for="key in BOX_KEYS.filter(k => spec![k])" :key="key" class="check"><input type="checkbox" :checked="picked(key)" @change="togglePick(key)">{{ specKeyLabel(key, t) }}</label>
                  </div>
                </section>
                <p class="caption">{{ t('tplLayoutHint') }}</p>
              </template>

              <template v-else>
                <section v-for="role in WORD_ROLES" :key="role" class="panel word-style-card" :class="{ 'ai-selected': aiOpen && picked('styles.' + role) }">
                  <h3 class="ai-heading">{{ wordRoleLabel(role, t) }}<label v-if="aiOpen" class="check ai-pick"><input type="checkbox" :checked="picked('styles.' + role)" @change="togglePick('styles.' + role)">{{ t('selectForAI') }}</label></h3>
                  <div class="field-grid">
                    <label>{{ t('tplFontLatin') }}<input v-model="spec.styles![role].font" list="font-suggestions"></label>
                    <label>{{ t('tplFontEA') }}<input v-model="spec.styles![role].font_ea" list="font-suggestions"></label>
                    <label>{{ t('tplSize') }}<input v-model.number="spec.styles![role].size" type="number" min="4" max="96" step="0.5"></label>
                    <label>{{ t('tplColor') }}
                      <span class="color-field">
                        <input type="color" :value="colorInput(spec.styles![role].color)" :disabled="!spec.styles![role].color" @input="setColor(spec.styles![role], 'color', ($event.target as HTMLInputElement).value)">
                        <span class="check"><input type="checkbox" :checked="!spec.styles![role].color" @change="spec.styles![role].color = ($event.target as HTMLInputElement).checked ? '' : '000000'">{{ t('tplAutoColor') }}</span>
                      </span>
                    </label>
                    <label>{{ t('tplBefore') }}<input v-model.number="spec.styles![role].before" type="number" min="0" max="200" step="1"></label>
                    <label>{{ t('tplAfter') }}<input v-model.number="spec.styles![role].after" type="number" min="0" max="200" step="1"></label>
                    <label class="check"><input v-model="spec.styles![role].bold" type="checkbox">{{ t('bold') }}</label>
                  </div>
                </section>
                <section v-if="spec.margins" class="panel" :class="{ 'ai-selected': aiOpen && picked('margins') }">
                  <h3 class="ai-heading">{{ t('tplMargins') }}<label v-if="aiOpen" class="check ai-pick"><input type="checkbox" :checked="picked('margins')" @change="togglePick('margins')">{{ t('selectForAI') }}</label></h3>
                  <div class="field-grid four">
                    <label v-for="side in MARGIN_SIDES" :key="side">{{ t('tplMargin_' + side) }}<input v-model.number="spec.margins[side]" type="number" min="0" max="100" step="0.5"></label>
                  </div>
                </section>
                <section class="panel">
                  <h3>{{ t('tplHeaderFooter') }}</h3>
                  <template v-for="part in HEADER_PARTS" :key="part">
                    <label v-if="aiOpen && spec[part]?.editable" class="check ai-pick"><input type="checkbox" :checked="picked(part)" @change="togglePick(part)">{{ t('selectForAI') }} · {{ t('tpl_' + part) }}</label>
                    <label v-if="spec[part]">{{ t('tpl_' + part) }}
                      <textarea v-model="spec[part]!.text" rows="2" :disabled="!spec[part]!.editable"></textarea>
                      <small v-if="!spec[part]!.editable" class="caption">{{ t('tplHeaderComplex') }}</small>
                    </label>
                  </template>
                </section>
              </template>
              <datalist id="font-suggestions"><option v-for="f in FONT_SUGGESTIONS" :key="f" :value="f"></option></datalist>
            </form>

            <div class="template-preview">
              <template v-if="!word && look">
                <TemplateCanvas :label="t('tplCoverLayout')" :background="record.data.cover_image ? fileURL(record, record.data.cover_image) : ''" :ratio="ratio" :points="points"
                  :boxes="coverBoxes" :colors="look.colors" :fonts="look.fonts" :selected="aiOpen ? aiSel : []" @update="setBox" @pick="key => aiOpen && togglePick(key)" />
                <TemplateCanvas :label="t('tplContentLayout')" :background="record.data.content_image ? fileURL(record, record.data.content_image) : ''" :ratio="ratio" :points="points"
                  :boxes="contentBoxes" :colors="look.colors" :fonts="look.fonts" :selected="aiOpen ? aiSel : []" @update="setBox" @pick="key => aiOpen && togglePick(key)" />
                <p class="caption">{{ t('tplBackgroundHint') }}</p>
              </template>
              <template v-else-if="word">
                <WordSample :spec="spec" :selected="aiOpen ? sampleSelected : []" @pick="role => aiOpen && samplePick(role)" />
                <details v-if="record.data.preview" class="rendered-preview">
                  <summary>{{ t('tplRendered') }}</summary>
                  <img class="word-preview" :src="fileURL(record, record.data.preview)" :alt="record.data.name">
                </details>
              </template>
            </div>
          </div></div>
          <TemplateAIPanel v-if="aiOpen && tab === 'edit' && spec" :record="record" :spec="spec" :selection="aiSel" @close="aiOpen = false" @clear="aiSel = []" />

          <div v-if="tab === 'versions'" class="template-versions">
            <article class="version-row current">
              <img v-if="word ? record.data.preview : record.data.cover_image" :src="fileURL(record, (word ? record.data.preview : record.data.cover_image)!)" alt="">
              <div class="grow"><strong>v{{ record.version }}</strong> <span class="pill success">{{ t('tplCurrent') }}</span><small>{{ when(record.updated_at) }}</small></div>
            </article>
            <article v-for="entry in history" :key="entry.version" class="version-row">
              <img v-if="entryImage(entry)" :src="entryImage(entry)" alt="">
              <div class="grow"><strong>v{{ entry.version }}</strong><small>{{ t('tplReplacedAt').replace('{time}', when(entry.saved_at)) }}</small></div>
              <button class="secondary small" :disabled="!!state.busy" @click="restore(entry)">{{ t('tplRestore') }}</button>
            </article>
            <p class="caption">{{ t('tplVersionsHint') }}</p>
          </div>
        </div>
      </section>
    </div>
  </Teleport>
</template>
