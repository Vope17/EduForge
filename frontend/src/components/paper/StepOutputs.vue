<script setup lang="ts">
import { computed, ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import SubNav from '../SubNav.vue'
import GenerationForm from '../generation/GenerationForm.vue'
import ArtifactList from '../generation/ArtifactList.vue'
import TaskRow from '../tasks/TaskRow.vue'
import { ACTIVE } from '../../lib/status.ts'

const { state, t } = useWorkspace()
// Reproduction plans (C) live on the experiment step.
const artifacts = computed(() => (state.snapshot.artifact || []).filter(a => a.data.type !== 'C' && (a.paper_id === state.paperID || a.data.grouped_paper_ids?.includes(state.paperID))).slice().reverse())
const tab = ref(artifacts.value.length ? 'artifacts' : 'setup')
const running = computed(() => (state.snapshot.task || []).filter(x => x.paper_id === state.paperID && !['parse', 'C'].includes(x.data.type) && !x.data.type.startsWith('preview:') && ACTIVE.includes(x.data.status)))
function started() { tab.value = 'artifacts' }
</script>

<template>
  <SubNav v-model="tab" :tabs="[
    { key: 'setup', label: t('tabSetup'), icon: 'sliders' },
    { key: 'artifacts', label: t('artifacts'), icon: 'layers', count: artifacts.length },
  ]" />
  <section v-if="running.length" class="panel running-panel">
    <h3>{{ t('stepGenerating') }}</h3>
    <TaskRow v-for="task in running" :key="task.id" :task="task" compact />
  </section>
  <GenerationForm v-show="tab === 'setup'" :paper-ids="[state.paperID]" @started="started" />
  <ArtifactList v-if="tab === 'artifacts'" :artifacts="artifacts" />
</template>
