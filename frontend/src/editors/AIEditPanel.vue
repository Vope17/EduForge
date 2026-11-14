<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '../components/AppIcon.vue'
import type { PatchProposal } from '../api/documents'

// AI editing limited to the selected parts (Paper2Lab's side panel). The
// proposal is a set of before/after changes that is accepted as a new
// revision or discarded.
const props = defineProps<{
  /** One label per selected part. */
  labels: string[]
  dirty: boolean
  pending: boolean
  proposal?: PatchProposal
  /** The revision the editor shows; a proposal for another one is stale. */
  revision: number
  /** Where a change applies, e.g. "Slide 2 · Speaker notes". */
  describe: (change: PatchProposal['changes'][number]) => string
}>()
const emit = defineEmits<{ propose: [instruction: string]; accept: []; reject: []; clear: []; close: [] }>()
const { t } = useI18n()
const instruction = ref('')
const quick = ['concise', 'casual', 'formal', 'example'] as const
const stale = computed(() => !!props.proposal && props.proposal.base_revision !== props.revision)
const canPropose = computed(() => !props.pending && !props.dirty && props.labels.length > 0)

function run(text = instruction.value) {
  if (!canPropose.value || !text.trim()) return
  emit('propose', text.trim())
}
</script>

<template>
  <aside
    class="ai-panel"
    :aria-label="t('ai.title')"
  >
    <header class="ai-panel-head">
      <AppIcon name="sparkle" />
      <strong class="grow">{{ t('ai.title') }}</strong>
      <button
        type="button"
        class="icon-button"
        :aria-label="t('ai.close')"
        @click="emit('close')"
      >
        ×
      </button>
    </header>
    <div class="ai-panel-body">
      <section class="ai-selection">
        <div class="ai-selection-head">
          <span role="status">{{ t('ai.selected', { count: labels.length }) }}</span>
          <button
            v-if="labels.length"
            type="button"
            class="text-button"
            @click="emit('clear')"
          >
            {{ t('ai.clear') }}
          </button>
        </div>
        <p
          v-if="!labels.length"
          class="caption"
        >
          <slot name="hint" />
        </p>
        <div
          v-else
          class="ai-chips"
        >
          <span
            v-for="(label, index) in labels"
            :key="index"
            class="pill"
          >{{ label }}</span>
        </div>
      </section>
      <p
        v-if="dirty"
        class="notice warning"
      >
        {{ t('ai.saveFirst') }}
      </p>
      <div class="ai-quick">
        <button
          v-for="key in quick"
          :key="key"
          type="button"
          class="chip-button"
          :disabled="!canPropose"
          @click="run(t('ai.quick.' + key + 'Text'))"
        >
          {{ t('ai.quick.' + key) }}
        </button>
      </div>
      <form
        class="ai-instruction"
        @submit.prevent="run()"
      >
        <textarea
          v-model="instruction"
          rows="3"
          maxlength="1000"
          data-native-undo
          :aria-label="t('ai.instruction')"
          :placeholder="t('ai.instruction')"
          :disabled="pending"
        />
        <button
          type="submit"
          class="document-primary"
          :disabled="!canPropose || !instruction.trim()"
        >
          <AppIcon name="sparkle" />{{ t(pending ? 'ai.working' : 'ai.propose') }}
        </button>
      </form>

      <section
        v-if="proposal"
        class="ai-results"
      >
        <strong>{{ t('ai.review') }}</strong>
        <article
          v-for="(change, index) in proposal.changes"
          :key="index"
          class="ai-proposal"
        >
          <div class="ai-proposal-head">
            {{ describe(change) }}
          </div>
          <div class="ai-diff">
            <div class="ai-before">
              <small>{{ t('ai.before') }}</small><del>{{ change.before || '—' }}</del>
            </div>
            <div class="ai-after">
              <small>{{ t('ai.after') }}</small><ins>{{ change.after }}</ins>
            </div>
          </div>
        </article>
        <p
          v-if="stale"
          class="notice warning"
        >
          {{ t('ai.stale') }}
        </p>
        <div class="button-row">
          <button
            type="button"
            class="document-primary"
            :disabled="pending || dirty || stale"
            @click="emit('accept')"
          >
            {{ t('ai.accept') }}
          </button>
          <button
            type="button"
            class="text-button"
            :disabled="pending"
            @click="emit('reject')"
          >
            {{ t('ai.reject') }}
          </button>
        </div>
      </section>
    </div>
  </aside>
</template>
