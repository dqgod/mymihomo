<template>
  <div ref="containerRef">
    <div :class="gridClass">
      <div
        v-for="(port, index) in ports"
        :key="port.key"
        class="rounded-box bg-base-200/50 grid gap-1.5 p-2.5"
        :class="getPortTileLayoutClass(index)"
      >
        <label
          :for="`port-${port.key}`"
          class="text-base-content/55 truncate text-xs font-semibold"
        >
          {{ $t(port.label) }}
        </label>
        <input
          :id="`port-${port.key}`"
          :value="configs?.[port.key] ?? ''"
          class="input input-sm border-transparent text-center shadow-none"
          type="number"
          inputmode="numeric"
          min="0"
          max="65535"
          readonly
          aria-readonly="true"
          title="由 Docker 部署管理"
        />
      </div>
    </div>
    <p class="mt-3 text-xs leading-5 opacity-60">
      端口由 Docker 部署管理，此处仅显示容器内监听端口。修改请同步调整 .env 与 compose.local.yaml
      的端口映射，再重建容器。
    </p>
  </div>
</template>

<script setup lang="ts">
import { configs } from '@/assembly/config'
import { useElementSize } from '@vueuse/core'
import { computed, ref } from 'vue'

type PortKey = 'mixed-port' | 'port' | 'socks-port' | 'redir-port' | 'tproxy-port'

type PortItem = {
  label: string
  key: PortKey
}

const ports: PortItem[] = [
  {
    label: 'mixedPort',
    key: 'mixed-port',
  },
  {
    label: 'httpPort',
    key: 'port',
  },
  {
    label: 'socksPort',
    key: 'socks-port',
  },
  {
    label: 'redirPort',
    key: 'redir-port',
  },
  {
    label: 'tproxyPort',
    key: 'tproxy-port',
  },
]

const containerRef = ref<HTMLElement | null>(null)
const { width } = useElementSize(containerRef)

const layoutMode = computed(() => {
  if (width.value >= 560) return 'five'
  if (width.value >= 320) return 'three-two'
  return 'two-two-one'
})

const gridClass = computed(() => {
  if (layoutMode.value === 'five') {
    return 'grid grid-cols-5 gap-2'
  }

  if (layoutMode.value === 'three-two') {
    return 'grid grid-cols-6 gap-2'
  }

  return 'grid grid-cols-2 gap-2'
})

const getPortTileLayoutClass = (index: number) => {
  if (layoutMode.value === 'five') {
    return 'col-span-1'
  }

  if (layoutMode.value === 'three-two') {
    return [
      'col-span-2',
      {
        'col-start-2': index === 3,
        'col-start-4': index === 4,
      },
    ]
  }

  return [
    'col-span-1',
    {
      'col-span-2 w-full max-w-[calc(50%-0.25rem)] justify-self-center': index === 4,
    },
  ]
}
</script>
