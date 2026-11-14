<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SavedDocument } from '../api/documents'
import AppIcon from '../components/AppIcon.vue'

const props = defineProps<{ document: SavedDocument; kind: 'document' | 'slides'; disabled?: boolean; dirty?: boolean }>()
const { t } = useI18n()
const busy = ref(false)
const error = ref('')
const controller = new AbortController()
const urls = new Set<string>()

async function download(format: string) {
  if (busy.value || props.disabled || props.dirty) return
  busy.value = true
  error.value = ''
  const snapshot = props.document
  try {
    const base = props.kind === 'slides' ? 'slides' : 'documents'
    const response = await fetch(`/api/v1/${base}/${encodeURIComponent(snapshot.id)}/download?revision=${snapshot.revision}&format=${format}`, { signal: controller.signal })
    if (!response.ok) {
      const result = await response.json().catch(() => undefined) as { error?: { code?: string } } | undefined
      throw new Error(result?.error?.code === 'export_assets_failed' ? 'downloads.assetsFailed' : result?.error?.code === 'export_layout_failed' ? 'downloads.layoutFailed' : 'downloads.failed')
    }
    const blob = await response.blob()
    if (controller.signal.aborted) return
    const url = URL.createObjectURL(blob)
    urls.add(url)
    const link = document.createElement('a')
    link.href = url
    link.download = Array.from(snapshot.content.title).map((c) => c.charCodeAt(0) < 32 ? '_' : c).join('').replace(/[\\/:*?"<>|]/g, '_').slice(0, 100) + `-r${snapshot.revision}.${format}`
    document.body.append(link)
    link.click()
    link.remove()
    setTimeout(() => { URL.revokeObjectURL(url); urls.delete(url) }, 30_000)
  } catch (cause) {
    if (!controller.signal.aborted) error.value = cause instanceof Error && ['downloads.assetsFailed', 'downloads.layoutFailed'].includes(cause.message) ? cause.message : 'downloads.failed'
  } finally {
    busy.value = false
  }
}

onBeforeUnmount(() => { controller.abort(); for (const url of urls) URL.revokeObjectURL(url); urls.clear() })
</script>

<template>
  <div class="downloads">
    <div class="button-row">
      <button
        type="button"
        class="small"
        :disabled="disabled || dirty || busy"
        :title="dirty ? t('downloads.saveFirst') : undefined"
        @click="download(kind === 'slides' ? 'pptx' : 'docx')"
      >
        <AppIcon name="download" />{{ t(kind === 'slides' ? 'downloads.powerpoint' : 'downloads.word') }}
      </button>
      <button
        type="button"
        class="small"
        :disabled="disabled || dirty || busy"
        :title="dirty ? t('downloads.saveFirst') : undefined"
        @click="download('pdf')"
      >
        <AppIcon name="download" />{{ t('downloads.pdf') }}
      </button>
      <span
        v-if="busy"
        class="caption"
        role="status"
      >{{ t('downloads.preparing') }}</span>
    </div>
    <p
      v-if="error"
      role="alert"
      class="document-error"
    >
      {{ t(error) }}
    </p>
  </div>
</template>
