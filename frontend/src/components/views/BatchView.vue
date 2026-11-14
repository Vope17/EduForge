<script setup lang="ts">
import { computed, ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import SubNav from '../SubNav.vue'
import StepDots from '../lesson/StepDots.vue'
import GenerationForm from '../generation/GenerationForm.vue'
import ArtifactList from '../generation/ArtifactList.vue'

const { state, t, go, openLesson } = useWorkspace()
const lessons = computed(() => (state.snapshot.lesson || []).filter(p => state.selected.includes(p.id)))
const ids = computed(() => lessons.value.map(p => p.id))
// Grouped outputs that cover the selected lessons.
const artifacts = computed(() => (state.snapshot.artifact || []).filter(a => a.data.grouped_lesson_ids?.some(id => ids.value.includes(id)) || ids.value.includes(a.lesson_id ?? '')).slice().reverse())
const tab = ref('setup')
function started() { tab.value = 'artifacts' }
function remove(id: string) { state.selected.splice(state.selected.indexOf(id), 1) }
</script>

<template>
  <section class="page-content">
    <div class="page-heading">
      <div><h1>{{ t('batchStudio') }}</h1><p class="lede">{{ t('batchIntro') }}</p></div>
    </div>
    <div v-if="!lessons.length" class="empty-state">
      <h2>{{ t('batchEmpty') }}</h2>
      <p>{{ t('batchEmptyHint') }}</p>
      <button class="secondary" @click="go({ name: 'home', course: state.courseID })">{{ t('backToCourse') }}</button>
    </div>
    <template v-else>
      <div class="batch-lessons">
        <div v-for="lesson in lessons" :key="lesson.id" class="batch-lesson">
          <button class="text-button" @click="openLesson(lesson.id)">{{ lesson.data.title }}</button>
          <StepDots :lesson="lesson" />
          <button class="icon-button" :aria-label="t('remove')" @click="remove(lesson.id)">×</button>
        </div>
      </div>
      <SubNav v-model="tab" :tabs="[
        { key: 'setup', label: t('tabSetup'), icon: 'sliders' },
        { key: 'artifacts', label: t('artifacts'), icon: 'layers', count: artifacts.length },
      ]" />
      <GenerationForm v-show="tab === 'setup'" :key="ids.join(',')" :lesson-ids="ids" @started="started" />
      <ArtifactList v-if="tab === 'artifacts'" :artifacts="artifacts" />
    </template>
  </section>
</template>
