<script setup lang="ts">
import { computed } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { paperSteps } from '../../lib/status.ts'
import type { Paper, Rec } from '../../api/types.ts'
const props = defineProps<{ paper: Rec<Paper>; labelled?: boolean }>()
const { state, t } = useWorkspace()
const steps = computed(() => paperSteps(props.paper, state.snapshot))
</script>

<template>
  <ol class="step-mini" :class="{ labelled }">
    <li v-for="step in steps" :key="step.key" :class="step.state" :title="t('step_' + step.key) + ' · ' + t('state_' + step.state)">
      <i></i><span v-if="labelled">{{ t('step_' + step.key) }}</span>
    </li>
  </ol>
</template>
