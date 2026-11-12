<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Block, TemplateSpec } from '../../api/documents'

const props = defineProps<{
  page: Block
  spec?: TemplateSpec
  selected?: Set<string>
  interactive?: boolean
}>()
const emit = defineEmits<{ select: [index: number, additive: boolean] }>()
const { t } = useI18n()
const title = computed(() => props.page.children?.findIndex((block) => block.kind === 'heading') ?? -1)
const heading = computed(() => props.page.children?.[title.value])
const content = computed(() => (props.page.children ?? []).map((block, index) => ({ block, index })).filter(({ index }) => index !== title.value))

function select(index: number, event: MouseEvent | KeyboardEvent) {
  if (props.interactive) emit('select', index, event.shiftKey || event.ctrlKey || event.metaKey)
}

function textSize(block: Block, isTitle = false) {
  const length = block.text.length + (block.items?.join('').length ?? 0)
  const size = isTitle ? Math.max(2.4, Math.min(4.2, 34 / Math.sqrt(Math.max(length, 1)))) :
    Math.max(1.45, Math.min(2.6, 35 / Math.sqrt(Math.max(length, 1))))
  return { fontSize: size + 'cqw' }
}

function imageURL(block: Block) {
  if (!block.image_url) return undefined
  try {
    const url = new URL(block.image_url)
    return url.protocol === 'https:' && !url.username && !url.password ? url.href : undefined
  } catch {
    return undefined
  }
}
</script>

<template>
  <div
    class="slide-canvas"
    :class="{ 'is-interactive': interactive }"
    :data-layout="page.layout ?? 'title_body'"
    :data-palette="spec?.palette ?? 'forest'"
    :data-font="spec?.font ?? 'sans'"
    :data-density="spec?.density ?? 'balanced'"
    :aria-label="t('slides.canvas')"
  >
    <section
      v-if="heading"
      class="slide-title slide-element"
      :class="{ 'is-selected': heading.id && selected?.has(heading.id) }"
      :data-slide-block-id="heading.id"
      :role="interactive ? 'button' : undefined"
      :tabindex="interactive ? 0 : undefined"
      @pointerdown.stop
      @click.stop="select(title, $event)"
      @keydown.enter.stop="select(title, $event)"
      @keydown.space.prevent.stop="select(title, $event)"
    >
      <h2 :style="textSize(heading, true)">
        {{ heading.text }}
      </h2>
    </section>
    <div class="slide-body">
      <section
        v-for="{ block, index } in content"
        :key="block.id ?? index"
        class="slide-element slide-content"
        :class="{ 'is-selected': block.id && selected?.has(block.id), 'is-image': block.kind === 'image' }"
        :data-slide-block-id="block.id"
        :role="interactive ? 'button' : undefined"
        :tabindex="interactive ? 0 : undefined"
        :style="textSize(block)"
        @pointerdown.stop
        @click.stop="select(index, $event)"
        @keydown.enter.stop="select(index, $event)"
        @keydown.space.prevent.stop="select(index, $event)"
      >
        <template v-if="block.kind === 'image'">
          <img
            v-if="imageURL(block)"
            :src="imageURL(block)"
            :alt="block.text"
            referrerpolicy="no-referrer"
          >
          <div
            v-else
            class="slide-image-placeholder"
            role="img"
            :aria-label="block.text || t('slides.imagePlaceholder')"
          >
            <span aria-hidden="true">▧</span>
            <p>{{ block.text || t('slides.imagePlaceholder') }}</p>
          </div>
        </template>
        <template v-else>
          <p v-if="block.text">
            {{ block.text }}
          </p>
          <ul v-if="block.kind === 'list'">
            <li
              v-for="(item, itemIndex) in block.items"
              :key="itemIndex"
            >
              {{ item }}
            </li>
          </ul>
        </template>
      </section>
    </div>
    <slot />
  </div>
</template>
