<script setup lang="ts">
import { computed, nextTick, onMounted, onBeforeUnmount, ref, watch } from 'vue'
const props = withDefaults(
  defineProps<{ modelValue: string; readonly?: boolean; label: string; height?: string }>(),
  { height: '45dvh' },
)
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const input = ref<HTMLTextAreaElement>()
const search = ref<HTMLInputElement>()
const showing = ref(false)
const query = ref('')
const sensitive = ref(false)
const current = ref(0)
const matches = computed(() => {
  if (!query.value) return []
  const text = sensitive.value ? props.modelValue : props.modelValue.toLowerCase()
  const needle = sensitive.value ? query.value : query.value.toLowerCase()
  const found: number[] = []
  let start = 0
  while (start <= text.length) {
    const index = text.indexOf(needle, start)
    if (index < 0) break
    found.push(index)
    start = index + Math.max(needle.length, 1)
  }
  return found
})
const line = computed(() =>
  matches.value.length
    ? props.modelValue.slice(0, matches.value[current.value]).split('\n').length
    : 0,
)
async function reveal(focus = false) {
  await nextTick()
  const el = input.value
  if (!el || !matches.value.length) return
  if (focus) el.focus()
  const position = matches.value[current.value]
  el.setSelectionRange(position, position + query.value.length)
  const height = parseFloat(getComputedStyle(el).lineHeight) || 24
  el.scrollTop = Math.max(0, (line.value - 1) * height - el.clientHeight / 2)
  const lineStart = props.modelValue.lastIndexOf('\n', position - 1) + 1
  const context = document.createElement('canvas').getContext('2d')
  if (context) {
    context.font = getComputedStyle(el).font
    const width = context.measureText(props.modelValue.slice(lineStart, position)).width
    el.scrollLeft = Math.max(0, width - el.clientWidth / 2)
  }
}
function step(delta: number) {
  if (!matches.value.length) return
  current.value = (current.value + delta + matches.value.length) % matches.value.length
  reveal(true)
}
async function find() {
  const el = input.value
  const selection = el?.value.slice(el.selectionStart, el.selectionEnd)
  if (selection && !selection.includes('\n')) query.value = selection
  showing.value = true
  await nextTick()
  search.value?.focus()
  search.value?.select()
}
function keydown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'f') {
    e.preventDefault()
    e.stopImmediatePropagation()
    find()
  } else if (
    showing.value &&
    (e.key === 'F3' || (e.key === 'Enter' && e.target === search.value))
  ) {
    e.preventDefault()
    e.stopImmediatePropagation()
    step(e.shiftKey ? -1 : 1)
  } else if (showing.value && e.key === 'Escape') {
    e.preventDefault()
    e.stopImmediatePropagation()
    showing.value = false
    input.value?.focus()
  }
}
watch([query, sensitive, () => props.modelValue], () => {
  current.value = 0
  reveal()
})
onMounted(() => window.addEventListener('keydown', keydown, true))
onBeforeUnmount(() => window.removeEventListener('keydown', keydown, true))
</script>
<template>
  <div class="border-base-content/15 overflow-hidden rounded-xl border">
    <div class="bg-base-200/50 flex flex-wrap items-center gap-2 px-3 py-2 text-xs">
      <span class="mr-auto opacity-60"
        >{{ readonly ? '只读' : '可编辑' }} · {{ modelValue.split('\n').length }} 行</span
      >
      <button
        type="button"
        class="btn btn-ghost btn-xs"
        title="在配置中查找（Ctrl+F / ⌘F）"
        @click="find"
      >
        查找 Ctrl+F
      </button>
    </div>
    <div
      v-if="showing"
      class="border-base-content/10 flex flex-wrap items-center gap-2 border-b p-2"
      role="search"
      aria-label="配置内搜索"
    >
      <input
        ref="search"
        v-model="query"
        type="search"
        class="input input-sm min-w-24 flex-1"
        placeholder="搜索当前配置"
        aria-label="搜索当前配置"
      />
      <button
        type="button"
        class="btn btn-sm btn-ghost"
        :class="sensitive && 'btn-active'"
        :aria-pressed="sensitive"
        title="区分大小写"
        @click="sensitive = !sensitive"
      >
        Aa
      </button>
      <span
        class="text-xs tabular-nums"
        role="status"
        >{{ matches.length ? current + 1 : 0 }} / {{ matches.length
        }}<span v-if="line"> · 第 {{ line }} 行</span></span
      >
      <button
        type="button"
        class="btn btn-ghost btn-sm"
        :disabled="!matches.length"
        aria-label="上一个匹配"
        @click="step(-1)"
      >
        ↑
      </button>
      <button
        type="button"
        class="btn btn-ghost btn-sm"
        :disabled="!matches.length"
        aria-label="下一个匹配"
        @click="step(1)"
      >
        ↓
      </button>
      <button
        type="button"
        class="btn btn-ghost btn-sm"
        aria-label="关闭搜索"
        @click="showing = false"
      >
        ✕
      </button>
    </div>
    <textarea
      ref="input"
      :value="modelValue"
      :readonly="readonly"
      :aria-label="label"
      :style="{ height }"
      class="textarea block min-h-36 w-full resize-y rounded-none border-0 font-mono text-sm leading-6 focus:outline-none"
      wrap="off"
      spellcheck="false"
      autocapitalize="off"
      @input="emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)"
    />
  </div>
</template>
