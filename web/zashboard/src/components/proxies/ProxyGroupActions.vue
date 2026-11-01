<script setup lang="ts">
import { computed } from 'vue'
import { proxyMap } from '@/assembly/proxies'
import { PROXY_SORT_TYPE } from '@/constant'
import {
  collapseGroupMap,
  proxyGroupFilterMap,
  proxyGroupSortMap,
  proxyGroupHideUnavailableMap,
  hideUnavailableProxies,
} from '@/store/settings'
import { proxiesFilter } from '@/store/proxies'
import { isProxyNodeSearchMode } from '@/helper/proxy-search'
import { highlightProxyNode, proxyLocateRequest } from '@/helper/proxies-scroll'
import { MapPinIcon, BoltIcon, EyeSlashIcon, ClockIcon } from '@heroicons/vue/24/outline'
const props = defineProps<{ name: string; loading: boolean }>()
const emit = defineEmits<{ 'latency-test': [] }>()
const order = computed({
  get: () => proxyGroupSortMap.value[props.name] ?? '',
  set: (value: string) => {
    if (!value) delete proxyGroupSortMap.value[props.name]
    else proxyGroupSortMap.value[props.name] = value as PROXY_SORT_TYPE
  },
})
const hide = computed({
  get: () => proxyGroupHideUnavailableMap.value[props.name] ?? hideUnavailableProxies.value,
  set: (value: boolean) => {
    proxyGroupHideUnavailableMap.value[props.name] = value
  },
})
function locate() {
  const node = proxyMap.value[props.name]?.now
  if (!node) return
  collapseGroupMap.value[props.name] = true
  proxyGroupFilterMap.value[props.name] = ''
  if (isProxyNodeSearchMode.value) proxiesFilter.value = ''
  hide.value = false
  highlightProxyNode(node)
  proxyLocateRequest.value = {
    group: props.name,
    node,
    sequence: (proxyLocateRequest.value?.sequence ?? 0) + 1,
  }
}
</script>
<template>
  <div
    class="flex shrink-0 items-center gap-0.5"
    @click.stop
    @pointerdown.stop
    @dblclick.stop
  >
    <button
      class="btn btn-ghost btn-xs btn-square"
      title="定位当前节点（清除本组筛选）"
      aria-label="定位当前节点"
      :disabled="!proxyMap[name]?.now"
      @click="locate"
    >
      <MapPinIcon class="size-4" />
    </button>
    <button
      class="btn btn-ghost btn-xs btn-square"
      title="测试本组节点延迟"
      aria-label="测试本组延迟"
      :disabled="loading"
      @click="emit('latency-test')"
    >
      <span
        v-if="loading"
        class="loading loading-spinner loading-xs"
      /><BoltIcon
        v-else
        class="size-4"
      />
    </button>
    <label
      class="relative flex items-center"
      title="本组节点排序"
    >
      <ClockIcon class="pointer-events-none absolute left-1 size-3.5" />
      <select
        v-model="order"
        class="select select-ghost select-xs w-28 max-w-28 pl-5 text-xs"
        aria-label="本组节点排序"
      >
        <option value="">跟随全局</option>
        <option :value="PROXY_SORT_TYPE.DEFAULT">原始顺序</option>
        <option :value="PROXY_SORT_TYPE.LATENCY_ASC">延迟升序</option>
        <option :value="PROXY_SORT_TYPE.LATENCY_DESC">延迟降序</option>
        <option :value="PROXY_SORT_TYPE.NAME_ASC">名称 A–Z</option>
        <option :value="PROXY_SORT_TYPE.NAME_DESC">名称 Z–A</option>
      </select>
    </label>
    <button
      class="btn btn-ghost btn-xs btn-square"
      :class="hide && 'btn-active text-primary'"
      :aria-pressed="hide"
      title="隐藏本组不可用节点"
      aria-label="隐藏本组不可用节点"
      @click="hide = !hide"
    >
      <EyeSlashIcon class="size-4" />
    </button>
  </div>
</template>
