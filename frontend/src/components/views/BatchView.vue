<script setup lang="ts">
import { computed, ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import SubNav from '../SubNav.vue'
import StepDots from '../paper/StepDots.vue'
import GenerationForm from '../generation/GenerationForm.vue'
import ArtifactList from '../generation/ArtifactList.vue'

const { state, t, go, openPaper } = useWorkspace()
const papers = computed(() => (state.snapshot.paper || []).filter(p => state.selected.includes(p.id)))
const ids = computed(() => papers.value.map(p => p.id))
// Grouped outputs that cover the selected papers.
const artifacts = computed(() => (state.snapshot.artifact || []).filter(a => a.data.grouped_paper_ids?.some(id => ids.value.includes(id)) || ids.value.includes(a.paper_id ?? '')).slice().reverse())
const tab = ref('setup')
function started() { tab.value = 'artifacts' }
function remove(id: string) { state.selected.splice(state.selected.indexOf(id), 1) }
</script>

<template>
  <section class="page-content">
    <div class="page-heading">
      <div><h1>{{ t('batchStudio') }}</h1><p class="lede">{{ t('batchIntro') }}</p></div>
    </div>
    <div v-if="!papers.length" class="empty-state">
      <h2>{{ t('batchEmpty') }}</h2>
      <p>{{ t('batchEmptyHint') }}</p>
      <button class="secondary" @click="go({ name: 'home', project: state.projectID })">{{ t('backToProject') }}</button>
    </div>
    <template v-else>
      <div class="batch-papers">
        <div v-for="paper in papers" :key="paper.id" class="batch-paper">
          <button class="text-button" @click="openPaper(paper.id)">{{ paper.data.title }}</button>
          <StepDots :paper="paper" />
          <button class="icon-button" :aria-label="t('remove')" @click="remove(paper.id)">×</button>
        </div>
      </div>
      <SubNav v-model="tab" :tabs="[
        { key: 'setup', label: t('tabSetup'), icon: 'sliders' },
        { key: 'artifacts', label: t('artifacts'), icon: 'layers', count: artifacts.length },
      ]" />
      <GenerationForm v-show="tab === 'setup'" :key="ids.join(',')" :paper-ids="ids" @started="started" />
      <ArtifactList v-if="tab === 'artifacts'" :artifacts="artifacts" />
    </template>
  </section>
</template>
