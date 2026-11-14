import { computed, onMounted, onUnmounted, ref, watch } from 'vue'

// Undo/redo for an editor's working copy (same behaviour as Paper2Lab). Changes are recorded as JSON
// snapshots once editing pauses (so typing a sentence or dragging a box is one
// step). Ctrl/⌘+Z undoes, Ctrl/⌘+Shift+Z or Ctrl+Y redoes, except inside
// elements marked data-native-undo (free-text fields with their own undo).
//   read()       → the state to record (null while not loaded)
//   write(value) → put a recorded state back
export function useUndo<T>(
  read: () => T | null | undefined,
  write: (value: T) => void,
  { delay = 400, limit = 100, enabled = () => true }: { delay?: number; limit?: number; enabled?: () => boolean } = {},
) {
  const stack = ref<string[]>([]), index = ref(-1), pending = ref(false)
  let timer: ReturnType<typeof setTimeout> | undefined

  function commit() {
    clearTimeout(timer); timer = undefined; pending.value = false
    const value = read()
    if (value == null) return
    const json = JSON.stringify(value)
    if (json === stack.value[index.value]) return
    const next = [...stack.value.slice(0, index.value + 1), json]
    stack.value = next.slice(-limit)
    index.value = stack.value.length - 1
  }
  // Start over from the current state (after loading or reloading).
  function reset() {
    clearTimeout(timer); timer = undefined; pending.value = false
    const value = read()
    stack.value = value == null ? [] : [JSON.stringify(value)]
    index.value = stack.value.length - 1
  }
  watch(read, () => {
    clearTimeout(timer); pending.value = true
    timer = setTimeout(commit, delay)
  }, { deep: true })

  function go(step: number): boolean {
    if (pending.value) commit()
    const next = index.value + step
    const json = stack.value[next]
    if (json === undefined) return false
    index.value = next
    write(JSON.parse(json) as T)
    return true
  }
  const undo = () => go(-1), redo = () => go(1)
  const canUndo = computed(() => index.value > 0 || (pending.value && index.value >= 0))
  const canRedo = computed(() => !pending.value && index.value < stack.value.length - 1)

  function onKey(event: KeyboardEvent) {
    if (!(event.ctrlKey || event.metaKey) || event.altKey) return
    const key = event.key.toLowerCase()
    const isUndo = key === 'z' && !event.shiftKey
    const isRedo = (key === 'z' && event.shiftKey) || (key === 'y' && !event.shiftKey && !event.metaKey)
    if (!isUndo && !isRedo) return
    if ((event.target instanceof Element && event.target.closest('[data-native-undo]')) || !enabled()) return
    event.preventDefault()
    if (isUndo) undo()
    else redo()
  }
  onMounted(() => document.addEventListener('keydown', onKey))
  onUnmounted(() => { document.removeEventListener('keydown', onKey); clearTimeout(timer) })

  return { undo, redo, canUndo, canRedo, reset, commit }
}
