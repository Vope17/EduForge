<script setup lang="ts">
import type { Block, TemplateSpec } from '../../api/documents'

defineProps<{ title?: string; blocks: Block[]; spec?: TemplateSpec }>()
</script>

<template>
  <section
    class="document-preview"
    :data-palette="spec?.palette"
    :data-font="spec?.font"
    :data-layout="spec?.layout"
    :data-density="spec?.density"
  >
    <h2 v-if="title">{{ title }}</h2>
    <div v-for="(block, index) in blocks" :key="block.id ?? index" class="preview-block">
      <h3 v-if="block.kind === 'heading'">{{ block.text }}</h3>
      <p v-else-if="block.kind === 'paragraph'">{{ block.text }}</p>
      <template v-else-if="block.kind === 'list'">
        <p v-if="block.text">{{ block.text }}</p>
        <ul>
          <li v-for="(item, itemIndex) in block.items" :key="itemIndex">{{ item }}</li>
        </ul>
      </template>
      <template v-else-if="block.kind === 'table'">
        <p v-if="block.text">{{ block.text }}</p>
        <div class="document-table">
          <table>
            <tbody>
              <tr v-for="(row, rowIndex) in block.rows" :key="rowIndex">
                <td v-for="(cell, cellIndex) in row" :key="cellIndex">{{ cell }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
      <figure v-else-if="block.kind === 'image'">
        <img :src="block.image_url" :alt="block.text" referrerpolicy="no-referrer" loading="lazy">
        <figcaption v-if="block.text">{{ block.text }}</figcaption>
      </figure>
    </div>
  </section>
</template>
