<script setup lang="ts">
import { computed, ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { ACTIVE } from '../../lib/status.ts'
import Icon from '../Icon.vue'
import TaskRow from '../tasks/TaskRow.vue'

const { state, t, api, act, fileURL, paper, knowledge, openPaper, explain } = useWorkspace()
const files = ref<File[]>([]), dragging = ref(false)
const sources = computed(() => (state.snapshot.source || []).filter(x => x.paper_id === state.paperID))
const research = computed(() => sources.value.filter(x => x.data.role === 'research'))
const parseTasks = computed(() => (state.snapshot.task || []).filter(x => x.paper_id === state.paperID && x.data.type === 'parse').slice().reverse())
const parsing = computed(() => parseTasks.value.some(x => ACTIVE.includes(x.data.status)))
const lastParse = computed(() => parseTasks.value[0])

function pick(list: FileList | null | undefined) { files.value = Array.from(list || []) }
function drop(event: DragEvent) { dragging.value = false; pick(event.dataTransfer?.files) }
async function upload() {
  await act(async () => {
    const results = await Promise.allSettled(files.value.map(file => {
      const body = new FormData()
      body.append('file', file)
      body.append('role', 'research')
      return api(`/papers/${state.paperID}/sources`, { method: 'POST', body })
    }))
    files.value = []
    const failed = results.filter((x): x is PromiseRejectedResult => x.status === 'rejected')
    if (failed[0]) throw failed[0].reason
  })
}
async function parse() { await act(() => api(`/papers/${state.paperID}/parse`, { method: 'POST' })) }
</script>

<template>
  <section class="panel">
    <div class="section-heading">
      <div>
        <h2>{{ t('tabSourceFiles') }}</h2>
        <p class="muted">{{ t('sourcesHint') }}</p>
      </div>
    </div>

    <form
      class="dropzone" :class="{ dragging }" @submit.prevent="upload"
      @dragover.prevent="dragging = true" @dragleave.prevent="dragging = false" @drop.prevent="drop"
    >
      <Icon name="upload" class="dropzone-icon" />
      <div class="dropzone-body">
        <p class="caption">{{ t('dropResearch') }}</p>
        <div class="button-row">
          <label class="file-picker"><Icon name="plus" /> {{ t('selectFiles') }}<input type="file" multiple accept=".pdf,.pptx" @change="pick(($event.target as HTMLInputElement).files)"></label>
          <span v-if="files.length" class="muted">{{ files.map(x => x.name).join(', ') }}</span>
          <button class="primary small" :disabled="!files.length || !!state.busy">{{ t('importSources') }}</button>
        </div>
      </div>
    </form>

    <div class="source-list">
      <div v-for="source in sources" :key="source.id" class="source-row">
        <span class="file-icon">{{ source.data.name.split('.').pop()!.toUpperCase() }}</span>
        <div class="grow">
          <strong>{{ source.data.name }}</strong>
          <small>{{ Math.round(source.data.size / 1024) }} KB</small>
          <p v-if="source.data.error" class="issue-text">{{ explain(source.data.error) }}</p>
        </div>
        <span class="pill" :class="{ success: source.data.parsed }">{{ source.data.parsed ? `${(source.data.parsed.units || []).length} ${t('units')}` : t('pending') }}</span>
        <a class="text-button" :href="fileURL(source, source.data.path)" target="_blank" rel="noopener" :aria-label="t('openFile')">↗</a>
      </div>
      <p v-if="!sources.length" class="empty-inline">{{ t('stepNeedsSource') }}</p>
    </div>
  </section>

  <section class="panel next-card" :class="{ ready: research.length && !parsing }">
    <div class="grow">
      <h3>{{ t('parseTitle') }}</h3>
      <p class="muted">{{ t(knowledge ? 'parseAgainHint' : 'parseHint') }}</p>
      <TaskRow v-if="lastParse && (parsing || lastParse.data.status === 'failed')" :task="lastParse" compact />
    </div>
    <div class="button-row">
      <button class="primary" :disabled="!research.length || parsing || !!state.busy" @click="parse">
        {{ t(knowledge ? 'parseAgain' : 'parse') }} →
      </button>
      <button v-if="knowledge" class="secondary" @click="openPaper(paper!.id, 'knowledge')">{{ t('goTo') }} {{ t('step_knowledge') }} →</button>
    </div>
  </section>
</template>
