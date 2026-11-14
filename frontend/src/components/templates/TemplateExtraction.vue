<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import Icon from '../Icon.vue'
import { slotLabel } from '../../lib/templateSpec.ts'
import type { DeckDecoration, DeckSuggestion } from '../../api/types.ts'

// Reviewing a template extracted from an uploaded presentation: its name and
// description, which layouts are the cover and content pages, each layout's
// role, and which design shapes to keep. Confirming builds the template.
const props = defineProps<{ id: string }>()
const emit = defineEmits<{ close: []; done: [id: string] }>()
const { state, t, api, act, fileURL, explain } = useWorkspace()
const record = computed(() => (state.snapshot.template || []).find(x => x.id === props.id))
// The extraction with Go's null lists read as empty ones.
const ex = computed(() => {
  const e = record.value?.data.extraction
  if (!e) return undefined
  const a = e.analysis
  return {
    ...e,
    thumbs: e.thumbs || [],
    analysis: {
      ...a,
      slides: a.slides || [],
      layouts: (a.layouts || []).map(l => ({ ...l, slides: l.slides || [] })),
      decorations: (a.decorations || []).map(d => ({ ...d, slides: d.slides || [], box: d.box || [] })),
    },
  }
})
// The user's choices, starting from the suggestion; lists are never null here.
const choice = ref<DeckSuggestion & { layout_types: Record<string, string>; decorations: string[] } | null>(null)
const ROLES = ['cover', 'section', 'content', 'two_column', 'figure', 'closing', 'other']
watch(ex, value => {
  if (value && !choice.value) choice.value = JSON.parse(JSON.stringify({ ...value.suggestion, layout_types: value.suggestion.layout_types || {}, decorations: value.suggestion.decorations || [] }))
}, { immediate: true })
const thumb = (n: number) => { const path = ex.value?.thumbs?.[n - 1]; return path && record.value ? fileURL(record.value, path) : '' }
const ratio = computed(() => ex.value ? `${ex.value.analysis.width} / ${ex.value.analysis.height}` : '16 / 9')
const kept = (id: string) => !!choice.value?.decorations.includes(id)
function toggle(id: string) {
  const c = choice.value
  if (c) c.decorations = kept(id) ? c.decorations.filter(x => x !== id) : [...c.decorations, id]
}
const layoutName = (part: string) => ex.value?.analysis.layouts.find(l => l.part === part)?.name || part.split('/').pop()
const pct = (v: number) => `${(v * 100).toFixed(2)}%`
function where(d: DeckDecoration) {
  const slides = d.slides || []
  const cover = slides.includes(1), other = slides.some(n => n !== 1)
  return t(cover && other ? 'decoBoth' : cover ? 'decoCover' : 'decoContent')
}
// The design measured from the slides, drawn over a cover and a content slide.
const style = computed(() => ex.value?.analysis.style || null)
const styleViews = computed(() => {
  const st = style.value
  if (!st) return []
  const second = ex.value?.analysis.slides.find(s => s.n > 1)?.n
  return [
    { key: 'cover', slide: 1, label: t('extractCoverSlide'), boxes: [
      { cls: 'title', label: t('tplCoverTitle'), box: st.cover_title_box },
      { cls: 'body', label: t('tplCoverSubtitle'), box: st.cover_subtitle_box }] },
    ...(second ? [{ key: 'content', slide: second, label: t('extractContentSlide'), boxes: [
      { cls: 'title', label: t('tplTitleArea'), box: st.title_box },
      { cls: 'body', label: t('tplBodyArea'), box: st.body_box }] }] : []),
  ].map(v => ({ ...v, boxes: v.boxes.filter(b => b.box) }))
})
const boxStyle = (b: number[] | undefined) => !b ? {} : ({ left: pct(b[0]), top: pct(b[1]), width: pct(b[2]), height: pct(b[3]) })
const building = ref(false)
async function confirmTemplate() {
  if (!record.value) return
  const version = record.value.version
  building.value = true
  const result = await act(() => api(`/templates/${props.id}/extract/confirm`, { method: 'POST', body: { version, suggestion: choice.value } }))
  building.value = false
  if (result) emit('done', props.id)
}
async function discard() {
  if (!confirm(t('extractDiscardConfirm'))) return
  const result = await act(() => api('/records/' + props.id, { method: 'DELETE' }))
  if (result) emit('close')
}
function onKey(event: KeyboardEvent) { if (event.key === 'Escape') emit('close') }
onMounted(() => { document.addEventListener('keydown', onKey); document.body.classList.add('no-scroll') })
onUnmounted(() => { document.removeEventListener('keydown', onKey); document.body.classList.remove('no-scroll') })
</script>

<template>
  <Teleport to="body">
    <div class="sheet-backdrop" @click.self="emit('close')">
      <section v-if="record && ex && choice" class="sheet extraction-sheet" role="dialog" aria-modal="true" :aria-label="t('extractTemplate')">
        <header class="sheet-head">
          <div class="grow">
            <span class="eyebrow">{{ t('extractTemplate') }} · {{ ex.source_name }}</span>
            <h2>{{ choice.name || record.data.name }}</h2>
          </div>
          <button class="text-button danger" @click="discard">{{ t('extractDiscard') }}</button>
          <button class="primary" :disabled="!!state.busy || building || !choice.name.trim()" @click="confirmTemplate">{{ t(building ? 'extractBuilding' : 'extractConfirm') }}</button>
          <button class="icon-button" :aria-label="t('close')" @click="emit('close')">×</button>
        </header>
        <div class="sheet-body">
          <p class="extraction-notice" :class="{ ai: ex.used_ai }">
            <Icon :name="ex.used_ai ? 'sparkle' : 'sliders'" />
            <span>{{ ex.used_ai ? t('extractUsedAI') : t('extractNoAI').replace('{reason}', explain(ex.ai_error || 'model_connection_required')) }}</span>
          </p>

          <section class="panel">
            <div class="field-grid">
              <label>{{ t('templateName') }}<input v-model="choice.name" maxlength="80"></label>
              <label>{{ t('extractDescription') }}<textarea v-model="choice.description" rows="2" maxlength="600"></textarea></label>
            </div>
          </section>

          <section v-if="style" class="panel extract-style-panel">
            <h3>{{ t('extractStyle') }}</h3>
            <p class="caption">{{ t('extractStyleHint') }}</p>
            <label class="check extract-apply"><input v-model="choice.apply_style" type="checkbox">{{ t('extractApplyStyle') }}</label>
            <div class="extract-style" :class="{ off: !choice.apply_style }">
              <figure v-for="v in styleViews" :key="v.key">
                <div class="style-thumb" :style="{ aspectRatio: ratio }">
                  <img v-if="thumb(v.slide)" :src="thumb(v.slide)" alt="">
                  <i v-for="b in v.boxes" :key="b.label" :class="b.cls" :style="boxStyle(b.box)"><span>{{ b.label }}</span></i>
                </div>
                <figcaption>{{ v.label }}</figcaption>
              </figure>
              <div class="style-tokens">
                <span v-for="(hex, slot) in style.colors" :key="slot" class="style-color"><i :style="{ background: '#' + hex }"></i>{{ slotLabel(slot, t) }} <code>{{ hex }}</code></span>
                <span v-if="style.heading_font" class="style-font">{{ t('extractHeadingFont') }}<strong>{{ style.heading_font }}</strong></span>
                <span v-if="style.body_font" class="style-font">{{ t('extractBodyFont') }}<strong>{{ style.body_font }}</strong></span>
                <span v-if="style.body_font_ea" class="style-font">{{ t('extractCJKFont') }}<strong>{{ style.body_font_ea }}</strong></span>
              </div>
            </div>
          </section>

          <section class="panel">
            <h3>{{ t('extractLayouts') }}</h3>
            <p class="caption">{{ t('extractLayoutsHint') }}</p>
            <div class="extract-layouts">
              <article v-for="l in ex.analysis.layouts" :key="l.part" class="extract-layout" :class="{ chosen: choice.cover_layout === l.part || choice.content_layout === l.part }">
                <img v-if="thumb(l.slides[0])" :src="thumb(l.slides[0])" :alt="l.name" :style="{ aspectRatio: ratio }">
                <div v-else class="thumb-missing" :style="{ aspectRatio: ratio }">{{ l.name }}</div>
                <strong>{{ l.name || layoutName(l.part) }}</strong>
                <small>{{ t('extractLayoutSlides').replace('{n}', String(l.slides.length)).replace('{list}', l.slides.join(', ')) }}</small>
                <label>{{ t('extractRole') }}
                  <select v-model="choice.layout_types[l.part]"><option v-for="r in ROLES" :key="r" :value="r">{{ t('role_' + r) }}</option></select>
                </label>
                <div class="extract-pick">
                  <label class="check"><input v-model="choice.cover_layout" type="radio" name="cover" :value="l.part">{{ t('asCover') }}</label>
                  <label class="check"><input v-model="choice.content_layout" type="radio" name="content" :value="l.part">{{ t('asContent') }}</label>
                </div>
              </article>
            </div>
          </section>

          <section class="panel">
            <h3>{{ t('extractDecorations') }} <span class="count">{{ choice.decorations.length }} / {{ ex.analysis.decorations.length }}</span></h3>
            <p class="caption">{{ t('extractDecorationsHint') }}</p>
            <p v-if="!ex.analysis.decorations.length" class="empty-inline">{{ t('extractNoDecorations') }}</p>
            <label v-for="d in ex.analysis.decorations" :key="d.id" class="extract-deco" :class="{ kept: kept(d.id) }">
              <input type="checkbox" :checked="kept(d.id)" @change="toggle(d.id)">
              <span class="deco-map" :style="{ aspectRatio: ratio }"><i :style="{ left: pct(d.box[0]), top: pct(d.box[1]), width: pct(d.box[2]), height: pct(d.box[3]), background: d.color ? '#' + d.color : 'var(--brand)' }"></i></span>
              <span class="grow">
                <strong>{{ t('kind_' + d.kind) }} · {{ d.name || d.id }}</strong>
                <small v-if="d.text">「{{ d.text }}」</small>
                <small>{{ where(d) }} · {{ t('decoSlides').replace('{list}', d.slides.join(', ')) }}</small>
              </span>
            </label>
          </section>

          <section v-if="ex.analysis.skipped?.length" class="panel">
            <h3>{{ t('extractSkipped') }} <span class="count">{{ ex.analysis.skipped.length }}</span></h3>
            <p class="caption">{{ t('extractSkippedHint') }}</p>
            <ul class="extract-skipped"><li v-for="(s, i) in ex.analysis.skipped" :key="i">{{ s }}</li></ul>
          </section>

          <section v-if="ex.thumbs.length" class="panel">
            <h3>{{ t('extractSlides') }}</h3>
            <div class="extract-slides">
              <figure v-for="s in ex.analysis.slides" :key="s.n">
                <img v-if="thumb(s.n)" :src="thumb(s.n)" :alt="String(s.n)" :style="{ aspectRatio: ratio }">
                <figcaption>{{ s.n }} · {{ layoutName(s.layout) }}</figcaption>
              </figure>
            </div>
          </section>
        </div>
      </section>
    </div>
  </Teleport>
</template>
