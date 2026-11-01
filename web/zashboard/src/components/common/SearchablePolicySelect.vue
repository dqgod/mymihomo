<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { CheckIcon, ChevronDownIcon } from '@heroicons/vue/24/outline'
const props = defineProps<{
  modelValue: string
  options: string[]
  label: string
  disabled?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const trigger = ref<HTMLButtonElement>()
const panel = ref<HTMLDivElement>()
const search = ref<HTMLInputElement>()
const open = ref(false)
const query = ref('')
const active = ref(0)
const style = ref<Record<string, string>>({})
const id = 'policy-' + useId().replace(/[^\w-]/g, '')
const choices = computed(() => [...new Set(props.options)])
const filtered = computed(() =>
  choices.value.filter((value) =>
    value.toLocaleLowerCase().includes(query.value.trim().toLocaleLowerCase()),
  ),
)
function position() {
  if (!open.value || !trigger.value) return
  const r = trigger.value.getBoundingClientRect()
  const v = window.visualViewport
  const leftEdge = (v?.offsetLeft || 0) + 8
  const topEdge = (v?.offsetTop || 0) + 8
  const rightEdge = leftEdge + (v?.width || innerWidth) - 16
  const bottomEdge = topEdge + (v?.height || innerHeight) - 16
  const belowEdge = Math.max(topEdge, r.bottom + 4)
  const aboveEdge = Math.min(bottomEdge, r.top - 4)
  const below = bottomEdge - belowEdge
  const above = aboveEdge - topEdge
  const up = below < 200 && above > below
  const headerHeight = (panel.value?.firstElementChild as HTMLElement | null)?.offsetHeight || 70
  const contentHeight = panel.value?.querySelector('[role=listbox]')?.scrollHeight || 200
  const height = Math.min(
    340,
    headerHeight + contentHeight + 2,
    bottomEdge - topEdge,
    Math.max(80, up ? above : below),
  )
  const width = Math.min(r.width, rightEdge - leftEdge)
  style.value = {
    left: Math.max(leftEdge, Math.min(r.left, rightEdge - width)) + 'px',
    width: width + 'px',
    maxHeight: height + 'px',
    top:
      Math.max(topEdge, Math.min(up ? aboveEdge - height : belowEdge, bottomEdge - height)) + 'px',
  }
}
function close(focus = false) {
  open.value = false
  if (focus) trigger.value?.focus()
}
async function show(focusSearch = true) {
  if (props.disabled) return
  query.value = '' // Opening never filters by the previously selected value.
  active.value = 0
  open.value = true
  await nextTick()
  position()
  if (focusSearch) search.value?.focus()
}
function toggle() {
  if (open.value) close()
  else show(!window.matchMedia('(pointer: coarse)').matches)
}
function select(value: string) {
  emit('update:modelValue', value)
  close(true)
}
async function move(index: number) {
  const n = filtered.value.length
  if (!n) return
  active.value = (index + n) % n
  await nextTick()
  panel.value?.querySelector(`[id="${id}-${active.value}"]`)?.scrollIntoView({ block: 'nearest' })
}
function keydown(e: KeyboardEvent) {
  if (e.isComposing) return
  if (e.key === 'Escape' && open.value) {
    e.preventDefault()
    e.stopPropagation()
    close(true)
  } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    e.stopPropagation()
    if (!open.value) show()
    else move(active.value + (e.key === 'ArrowDown' ? 1 : -1))
  } else if (e.key === 'Enter' || (e.key === ' ' && e.target === trigger.value)) {
    e.preventDefault()
    e.stopPropagation()
    if (!open.value) show()
    else if (filtered.value[active.value]) select(filtered.value[active.value])
  } else if ((e.key === 'Home' || e.key === 'End') && e.target !== search.value) {
    e.preventDefault()
    move(e.key === 'Home' ? 0 : filtered.value.length - 1)
  } else if (e.key === 'Tab' && open.value) close(true)
}
function outside(e: PointerEvent) {
  if (!trigger.value?.contains(e.target as Node) && !panel.value?.contains(e.target as Node))
    close()
}
watch(query, () => {
  active.value = 0
  nextTick(() => {
    const list = panel.value?.querySelector('[role=listbox]')
    if (list) list.scrollTop = 0
    position()
  })
})
watch(
  () => props.disabled,
  (value) => {
    if (value) close()
  },
)
onMounted(() => {
  window.addEventListener('pointerdown', outside, true)
  window.addEventListener('resize', position)
  window.addEventListener('scroll', position, true)
  window.visualViewport?.addEventListener('resize', position)
  window.visualViewport?.addEventListener('scroll', position)
})
onBeforeUnmount(() => {
  window.removeEventListener('pointerdown', outside, true)
  window.removeEventListener('resize', position)
  window.removeEventListener('scroll', position, true)
  window.visualViewport?.removeEventListener('resize', position)
  window.visualViewport?.removeEventListener('scroll', position)
})
</script>
<template>
  <button
    ref="trigger"
    type="button"
    role="combobox"
    class="input mt-1 flex w-full items-center justify-between gap-2 text-left"
    :aria-label="label"
    aria-haspopup="listbox"
    :aria-expanded="open"
    :aria-controls="id"
    :disabled="disabled"
    @click="toggle"
    @keydown="keydown"
  >
    <span
      class="truncate"
      :class="!modelValue && 'opacity-50'"
      >{{ modelValue || '选择策略或代理组' }}</span
    ><ChevronDownIcon class="size-4 shrink-0" />
  </button>
  <Teleport to="body">
    <div
      v-if="open"
      ref="panel"
      class="bg-base-100 text-base-content border-base-content/20 fixed z-[1100] flex flex-col overflow-hidden rounded-xl border shadow-xl"
      :style="style"
      @keydown="keydown"
      @click.stop
    >
      <div class="border-base-content/10 shrink-0 border-b p-2">
        <input
          ref="search"
          v-model="query"
          type="search"
          class="input input-sm w-full"
          placeholder="搜索策略或节点"
          aria-label="搜索策略"
          :aria-controls="id"
          :aria-activedescendant="filtered.length ? `${id}-${active}` : undefined"
          autocomplete="off"
        />
        <p class="mt-1 px-1 text-xs opacity-50">{{ filtered.length }} / {{ choices.length }} 项</p>
      </div>
      <div
        :id="id"
        role="listbox"
        :aria-label="label + '列表'"
        class="min-h-0 overflow-y-auto overscroll-contain p-1"
      >
        <button
          v-for="(value, index) in filtered"
          :id="`${id}-${index}`"
          :key="value"
          type="button"
          role="option"
          :aria-selected="modelValue === value"
          tabindex="-1"
          class="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-sm"
          :class="[
            active === index && 'bg-base-200',
            modelValue === value && 'text-primary font-semibold',
          ]"
          @pointermove="active = index"
          @pointerdown.prevent
          @click="select(value)"
        >
          <span class="min-w-0 flex-1 break-words">{{ value }}</span
          ><CheckIcon
            v-if="modelValue === value"
            class="size-4 shrink-0"
          />
        </button>
        <p
          v-if="!filtered.length"
          class="px-3 py-5 text-center text-sm opacity-60"
        >
          没有匹配的策略
        </p>
      </div>
    </div>
  </Teleport>
</template>
