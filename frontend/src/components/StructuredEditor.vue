<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useWorkspace } from '../lib/workspace.ts'
import { useUndo } from '../lib/undo.ts'
import SubNav from './SubNav.vue'
import Icon from './Icon.vue'
import type { Tab } from './SubNav.vue'
import type { Artifact, Rec } from '../api/types.ts'

// Editing a lesson plan (C), a worksheet (D) or a syllabus (S): forms and
// tables for the structured content, a preview that is the HTML export, and
// the downloads. Saving makes a new version, which the server checks again.
const props = defineProps<{ record: Rec<Artifact> }>()
const emit = defineEmits<{ close: []; saved: [artifact: Rec<Artifact>] }>()
const { state, t, api, act, explain } = useWorkspace()
const type = computed(() => props.record.data.type)
const draft = ref<Artifact | null>(null), saved = ref(''), tab = ref('edit'), version = ref('student')

// What is edited: the structured part and, for a worksheet, the question mix.
const payload = (a: Artifact) => ({ plan: a.plan, worksheet: a.worksheet, syllabus: a.syllabus, mix: a.settings?.questions })
const undoer = useUndo(
  () => draft.value ? payload(draft.value) : null,
  (snap: ReturnType<typeof payload>) => {
    const d = draft.value
    if (!d) return
    d.plan = snap.plan; d.worksheet = snap.worksheet; d.syllabus = snap.syllabus
    if (d.settings) d.settings.questions = snap.mix
  },
  { enabled: () => tab.value === 'edit' },
)
watch(() => props.record.id, () => {
  draft.value = JSON.parse(JSON.stringify(props.record.data))
  saved.value = JSON.stringify(payload(draft.value!))
  undoer.reset()
}, { immediate: true })
const dirty = computed(() => !!draft.value && JSON.stringify(payload(draft.value)) !== saved.value)

onMounted(() => { document.addEventListener('keydown', onKey); document.body.classList.add('no-scroll') })
onUnmounted(() => { document.removeEventListener('keydown', onKey); document.body.classList.remove('no-scroll') })
function close() { if (dirty.value && !confirm(t('discardChanges'))) return; emit('close') }
function onKey(event: KeyboardEvent) { if (event.key === 'Escape') close() }
async function save() {
  ;(document.activeElement as HTMLElement | null)?.blur?.()
  const result = await act(() => api<Rec<Artifact>>(`/artifacts/${props.record.id}`, { method: 'PUT', body: { version: props.record.version, artifact: draft.value } }))
  if (result) emit('saved', result)
}

const tabs = computed((): Tab[] => [{ key: 'edit', label: t('edit'), icon: 'sliders' }, { key: 'preview', label: t('previewTab'), icon: 'eye' }])
const base = computed(() => `/api/artifacts/${props.record.id}/download`)
const downloads = computed(() => {
  if (type.value === 'D') return [
    { label: t('studentCopy') + ' · Word', href: `${base.value}?format=docx` }, { label: t('studentCopy') + ' · PDF', href: `${base.value}?format=pdf` },
    { label: t('teacherCopy') + ' · Word', href: `${base.value}?format=docx&version=teacher` }, { label: t('teacherCopy') + ' · PDF', href: `${base.value}?format=pdf&version=teacher` },
  ]
  return [
    { label: 'Word', href: `${base.value}?format=docx` }, { label: 'PDF', href: `${base.value}?format=pdf` },
    ...(type.value === 'S' ? [{ label: t('tsvDownload'), href: `${base.value}?format=tsv` }] : []),
  ]
})
const previewURL = computed(() => `${base.value}?format=html&preview=true&version=${type.value === 'D' && version.value === 'teacher' ? 'teacher' : 'student'}`)
const issues = computed(() => props.record.data.check?.issues || [])

// ---- lesson plan
const plan = computed(() => draft.value?.plan)
const total = computed(() => (plan.value?.activities || []).reduce((n, a) => n + (Number(a.minutes) || 0), 0))
const codes = computed(() => (plan.value?.objectives || []).map(o => o.code))
function addObjective() {
  const used = new Set(codes.value); let n = 1
  while (used.has('O' + n)) n++
  plan.value!.objectives!.push({ code: 'O' + n, domain: 'cognitive', text: '' })
}
function removeObjective(i: number) { plan.value!.objectives!.splice(i, 1) }
function addActivity() { plan.value!.activities!.push({ phase: 'develop', title: '', description: '', minutes: 5, objectives: [], resources: '', assessment: '' }) }
function toggleCode(a: { objectives: string[] | null }, code: string) {
  const list = a.objectives || []
  a.objectives = list.includes(code) ? list.filter(c => c !== code) : [...list, code]
}
const move = <T,>(list: T[], i: number, d: number) => { const j = i + d; if (j >= 0 && j < list.length) [list[i], list[j]] = [list[j]!, list[i]!] }
const lineList = (get: () => string[] | null | undefined, set: (v: string[]) => void) => computed({ get: () => (get() || []).join('\n'), set: (v: string) => set(v.split('\n').map(x => x.trim()).filter(Boolean)) })
const references = lineList(() => plan.value?.references, v => { plan.value!.references = v })

// ---- worksheet
const sheet = computed(() => draft.value?.worksheet)
const objectiveCodes = computed(() => {
  const ids = [props.record.data.knowledge_id]
  const k = (state.snapshot.knowledge || []).find(x => ids.includes(x.id))
  const fromKnowledge = (k?.data.claims || []).filter(c => c.section === 0).map((_, i) => 'O' + (i + 1))
  return fromKnowledge.length ? fromKnowledge : ['O1']
})
function addQuestion() { sheet.value!.questions!.push({ type: 'short', prompt: '', choices: [], answer: '', explanation: '', objective: objectiveCodes.value[0] || 'O1', points: 1, citations: props.record.data.worksheet?.questions?.[0]?.citations || [] }) }
function setType(q: { type: string; choices?: string[] | null }, v: string) { q.type = v; q.choices = v === 'choice' ? (q.choices?.length ? q.choices : ['', '', '', '']) : [] }
const pointsTotal = computed(() => (sheet.value?.questions || []).reduce((n, q) => n + (Number(q.points) || 0), 0))

// ---- syllabus
const syllabus = computed(() => draft.value?.syllabus)
const gradeTotal = computed(() => (syllabus.value?.grading || []).reduce((n, g) => n + (Number(g.percent) || 0), 0))
const objectivesText = lineList(() => syllabus.value?.objectives, v => { syllabus.value!.objectives = v })
const booksText = lineList(() => syllabus.value?.books, v => { syllabus.value!.books = v })
</script>

<template>
  <Teleport to="body">
    <div class="sheet-backdrop" @click.self="close">
      <section v-if="draft" class="sheet" role="dialog" aria-modal="true" :aria-label="t(type)">
        <header class="sheet-head">
          <div class="grow">
            <span class="eyebrow">{{ t(type) }} · {{ record.data.language }} · v{{ record.version }}</span>
            <h2>{{ type === 'C' ? plan?.info.unit : type === 'D' ? sheet?.title : syllabus?.name }}</h2>
          </div>
          <div class="sheet-downloads" :aria-label="t('download')">
            <a v-for="d in downloads" :key="d.href" class="secondary small" :href="d.href" download><Icon name="download" /> {{ d.label }}</a>
          </div>
          <button class="primary" :disabled="!!state.busy || !dirty" @click="save">{{ t('saveRevision') }}</button>
          <button class="icon-button" :aria-label="t('close')" @click="close">×</button>
        </header>
        <div class="sheet-tabs">
          <SubNav v-model="tab" :tabs="tabs" />
          <span class="caption">{{ t('saveCreatesVersion') }}</span>
          <span v-if="tab === 'edit'" class="undo-tools">
            <button class="icon-button undo-button" :disabled="!undoer.canUndo.value" :title="t('undo')" :aria-label="t('undo')" @click="undoer.undo()"><Icon name="undo" /></button>
            <button class="icon-button redo-button" :disabled="!undoer.canRedo.value" :title="t('redo')" :aria-label="t('redo')" @click="undoer.redo()"><Icon name="redo" /></button>
          </span>
        </div>
        <div class="sheet-body structured">
          <p v-if="record.data.mode === 'source-extract'" class="draft-notice">{{ t('extractive') }}</p>
          <section v-if="issues.length" class="panel check-panel">
            <h3>{{ t('outputChecks') }}</h3>
            <ul><li v-for="(issue, i) in issues" :key="i" class="issue-text">{{ explain(issue.code) }}<template v-if="issue.detail"> · {{ issue.detail }}</template></li></ul>
          </section>

          <template v-if="tab === 'edit'">
            <!-- lesson plan -->
            <div v-if="type === 'C' && plan" class="structured-form">
              <div class="form-grid">
                <label>{{ t('planUnit') }}<input v-model="plan.info.unit"></label>
                <label>{{ t('planDesigner') }}<input v-model="plan.info.designer"></label>
                <label>{{ t('audience') }}<input v-model="plan.info.audience"></label>
                <label>{{ t('planMinutes') }}<input v-model.number="plan.info.minutes" type="number" min="1" max="180"></label>
              </div>
              <label>{{ t('planSource') }}<input v-model="plan.info.source"></label>
              <label>{{ t('planGoal') }}<textarea v-model="plan.goal" rows="2"></textarea></label>
              <label>{{ t('planAnalysis') }}<textarea v-model="plan.analysis" rows="4"></textarea></label>
              <label>{{ t('planExperience') }}<textarea v-model="plan.experience" rows="3"></textarea></label>

              <h3>{{ t('planObjectives') }}</h3>
              <div v-for="(o, i) in plan.objectives" :key="i" class="objective-row">
                <input v-model="o.code" class="code-input" :aria-label="t('objectiveCode')">
                <select v-model="o.domain" :aria-label="t('objectiveDomain')"><option v-for="d in ['cognitive', 'affective', 'skill']" :key="d" :value="d">{{ t('domain_' + d) }}</option></select>
                <input v-model="o.text" class="grow" :aria-label="t('planObjectives')">
                <button type="button" class="icon-button danger" :aria-label="t('delete')" @click="removeObjective(i)">×</button>
              </div>
              <button type="button" class="text-button" @click="addObjective">＋ {{ t('addObjective') }}</button>

              <h3>{{ t('planActivities') }} <span class="pill" :class="total === plan.info.minutes ? 'success' : 'warning'">{{ t('planTotal').replace('{sum}', String(total)).replace('{total}', String(plan.info.minutes)) }}</span></h3>
              <div v-for="(a, i) in plan.activities" :key="i" class="activity-card">
                <div class="activity-head">
                  <select v-model="a.phase" :aria-label="t('planActivities')"><option v-for="p in ['prepare', 'develop', 'wrap']" :key="p" :value="p">{{ t('phase_' + p) }}</option></select>
                  <input v-model="a.title" class="grow" :placeholder="t('activityTitle')" :aria-label="t('activityTitle')">
                  <input v-model.number="a.minutes" type="number" min="0" class="minutes-input" :aria-label="t('activityMinutes')">
                  <button type="button" class="icon-button" :title="t('moveUp')" @click="move(plan.activities!, i, -1)">↑</button>
                  <button type="button" class="icon-button" :title="t('moveDown')" @click="move(plan.activities!, i, 1)">↓</button>
                  <button type="button" class="icon-button danger" :aria-label="t('delete')" @click="plan.activities!.splice(i, 1)">×</button>
                </div>
                <textarea v-model="a.description" rows="3" :placeholder="t('activityDescription')" :aria-label="t('activityDescription')"></textarea>
                <div class="code-chips" :aria-label="t('activityObjectives')">
                  <span class="caption">{{ t('activityObjectives') }}</span>
                  <button v-for="code in codes" :key="code" type="button" class="chip-button" :class="{ on: (a.objectives || []).includes(code) }" :aria-pressed="(a.objectives || []).includes(code)" @click="toggleCode(a, code)">{{ code }}</button>
                </div>
                <div class="form-grid">
                  <label>{{ t('activityResources') }}<input v-model="a.resources"></label>
                  <label>{{ t('activityAssessment') }}<input v-model="a.assessment"></label>
                </div>
              </div>
              <button type="button" class="text-button" @click="addActivity">＋ {{ t('addActivity') }}</button>
              <label>{{ t('planReferences') }}<textarea v-model="references" rows="3"></textarea></label>
            </div>

            <!-- worksheet -->
            <div v-else-if="type === 'D' && sheet" class="structured-form">
              <label>{{ t('sheetTitle') }}<input v-model="sheet.title"></label>
              <p class="caption">{{ sheet.questions?.length }} · {{ pointsTotal }} {{ t('questionPoints') }}</p>
              <div v-for="(q, i) in sheet.questions" :key="i" class="activity-card">
                <div class="activity-head">
                  <strong>{{ i + 1 }}.</strong>
                  <select :value="q.type" :aria-label="t('questionType')" @change="setType(q, ($event.target as HTMLSelectElement).value)"><option v-for="ty in ['choice', 'fill', 'short', 'calc', 'practice']" :key="ty" :value="ty">{{ t('qt_' + ty) }}</option></select>
                  <select v-model="q.objective" :aria-label="t('questionObjective')"><option v-for="c in objectiveCodes" :key="c" :value="c">{{ c }}</option><option v-if="!objectiveCodes.includes(q.objective)" :value="q.objective">{{ q.objective || '—' }}</option></select>
                  <input v-model.number="q.points" type="number" min="1" class="minutes-input" :aria-label="t('questionPoints')">
                  <span class="grow"></span>
                  <button type="button" class="icon-button" :title="t('moveUp')" @click="move(sheet.questions!, i, -1)">↑</button>
                  <button type="button" class="icon-button" :title="t('moveDown')" @click="move(sheet.questions!, i, 1)">↓</button>
                  <button type="button" class="icon-button danger" :aria-label="t('delete')" @click="sheet.questions!.splice(i, 1)">×</button>
                </div>
                <textarea v-model="q.prompt" rows="2" :placeholder="t('questionPrompt')" :aria-label="t('questionPrompt')"></textarea>
                <div v-if="q.type === 'choice'" class="choice-list">
                  <div v-for="(_, j) in q.choices" :key="j" class="choice-row">
                    <button type="button" class="chip-button" :class="{ on: q.answer === 'ABCDEF'[j] }" :aria-pressed="q.answer === 'ABCDEF'[j]" :title="t('questionAnswer')" @click="q.answer = 'ABCDEF'[j]!">{{ 'ABCDEF'[j] }}</button>
                    <input v-model="q.choices![j]" class="grow" :aria-label="t('questionChoices') + ' ' + 'ABCDEF'[j]">
                    <button type="button" class="icon-button danger" :aria-label="t('delete')" @click="q.choices!.splice(j, 1)">×</button>
                  </div>
                  <button v-if="(q.choices?.length || 0) < 6" type="button" class="text-button" @click="q.choices!.push('')">＋ {{ t('questionChoices') }}</button>
                </div>
                <label v-else>{{ t('questionAnswer') }}<textarea v-model="q.answer" rows="2"></textarea></label>
                <label>{{ t('questionExplanation') }}<textarea v-model="q.explanation" rows="2"></textarea></label>
              </div>
              <button type="button" class="text-button" @click="addQuestion">＋ {{ t('addQuestion') }}</button>
            </div>

            <!-- syllabus -->
            <div v-else-if="type === 'S' && syllabus" class="structured-form">
              <div class="form-grid">
                <label>{{ t('courseName') }}<input v-model="syllabus.name"></label>
                <label>{{ t('audience') }}<input v-model="syllabus.audience"></label>
              </div>
              <label>{{ t('syllabusDescription') }}<textarea v-model="syllabus.description" rows="4"></textarea></label>
              <label>{{ t('syllabusObjectives') }}<textarea v-model="objectivesText" rows="4"></textarea></label>
              <h3>{{ t('syllabusSchedule') }}</h3>
              <div class="table-scroll">
                <table class="term-table">
                  <thead><tr><th>{{ t('weekNo') }}</th><th>{{ t('weekTopic') }}</th><th>{{ t('weekChapters') }}</th><th>{{ t('weekMethod') }}</th><th>{{ t('weekAssessment') }}</th></tr></thead>
                  <tbody>
                    <tr v-for="(w, i) in syllabus.schedule" :key="i">
                      <td><input v-model.number="w.week" type="number" min="1" class="minutes-input" :aria-label="t('weekNo')"></td>
                      <td><textarea v-model="w.topic" rows="2" :aria-label="t('weekTopic')"></textarea></td>
                      <td><textarea v-model="w.chapters" rows="2" :aria-label="t('weekChapters')"></textarea></td>
                      <td><input v-model="w.method" :aria-label="t('weekMethod')"></td>
                      <td><textarea v-model="w.assessment" rows="2" :aria-label="t('weekAssessment')"></textarea></td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <h3>{{ t('syllabusGrading') }} <span class="pill" :class="gradeTotal === 100 ? 'success' : 'warning'">{{ gradeTotal }}%</span></h3>
              <div v-for="(g, i) in syllabus.grading" :key="i" class="objective-row">
                <input v-model="g.item" class="grow" :aria-label="t('gradeItem')">
                <input v-model.number="g.percent" type="number" min="0" max="100" class="minutes-input" :aria-label="t('gradePercent')">
                <button type="button" class="icon-button danger" :aria-label="t('delete')" @click="syllabus.grading!.splice(i, 1)">×</button>
              </div>
              <button type="button" class="text-button" @click="syllabus.grading!.push({ item: '', percent: 0 })">＋ {{ t('addGrade') }}</button>
              <label>{{ t('syllabusBooks') }}<textarea v-model="booksText" rows="3"></textarea></label>
            </div>
          </template>

          <template v-else>
            <div v-if="type === 'D'" class="button-row">
              <button class="chip-button" :class="{ on: version === 'student' }" @click="version = 'student'">{{ t('studentCopy') }}</button>
              <button class="chip-button" :class="{ on: version === 'teacher' }" @click="version = 'teacher'">{{ t('teacherCopy') }}</button>
            </div>
            <p v-if="dirty" class="caption">{{ t('saveToPreview') }}</p>
            <iframe :key="previewURL" class="reader-frame" :title="t('previewTab')" sandbox="" :src="previewURL"></iframe>
          </template>
        </div>
      </section>
    </div>
  </Teleport>
</template>
