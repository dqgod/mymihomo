<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { fetchConfigs } from '@/assembly/config'
import { fetchProxies } from '@/assembly/proxies'
import { fetchRules } from '@/assembly/rules'
import QRCode from 'qrcode'
import SearchableEditor from '@/components/common/SearchableEditor.vue'
import {
  ArrowPathIcon,
  PlusIcon,
  DocumentTextIcon,
  CodeBracketIcon,
  EllipsisHorizontalIcon,
  CheckCircleIcon,
} from '@heroicons/vue/24/outline'

type Profile = {
  id: string
  name: string
  url: string
  homepage: string
  content?: string
  merge: string
  script: string
  cron: string
  use_proxy: boolean
  proxy_url: string
  user_agent: string
  base64: boolean
  timeout: number
  retries: number
  updated_at: string
  next_update: string
  last_error: string
  usage?: { upload: number; download: number; total: number; expire: number }
}
type State = {
  profiles: Profile[]
  active_id: string
  global_merge: string
  global_script: string
  timezone: string
}
const state = ref<State>({
  profiles: [],
  active_id: '',
  global_merge: '',
  global_script: '',
  timezone: 'Asia/Hong_Kong',
})
const busy = ref(false)
const loaded = ref(false)
const notice = ref('')
const failed = ref(false)
const url = ref('')
const importProxy = ref(true)
const menu = ref('')
const modal = ref<'' | 'metadata' | 'editor' | 'share' | 'delete'>('')
const selected = ref<Profile>()
const form = ref<Partial<Profile>>({})
const title = ref('')
const editor = ref('')
const editorKind = ref('')
const section = ref('')
const modalError = ref('')
const qr = ref('')
const baseline = 'proxies: []\nproxy-groups: []\nrules:\n  - MATCH,DIRECT\n'
const sampleMerge =
  'prepend-rules:\n  - DOMAIN-SUFFIX,example.com,DIRECT\n# 普通数组会替换原值；prepend-/append- 可追加规则或节点。\n'
const sampleScript =
  'function main(config, profileName) {\n  // 修改 config 后返回；不支持网络、文件或 Node.js API。\n  return config;\n}\n'
const readOnly = computed(
  () => editorKind.value === 'runtime' || (editorKind.value === 'content' && !!selected.value?.url),
)
const isGlobal = computed(() => editorKind.value.startsWith('global'))
const isScript = computed(() => editorKind.value.endsWith('script'))

async function api(path = '', method = 'GET', body?: unknown) {
  const response = await fetch('/api/profiles' + path, {
    method,
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  })
  if (response.status === 401) {
    location.assign('/login')
    throw new Error('登录已过期')
  }
  const data = await response.json()
  if (!response.ok) throw new Error(data.error || '操作失败')
  return data
}
async function reload() {
  state.value = await api()
  loaded.value = true
  await Promise.allSettled([fetchConfigs(), fetchProxies(), fetchRules()])
}
async function run(action: () => Promise<void>, success = '') {
  if (busy.value) return
  busy.value = true
  notice.value = ''
  modalError.value = ''
  failed.value = false
  try {
    await action()
    if (success) notice.value = success
  } catch (e) {
    failed.value = true
    notice.value = e instanceof Error ? e.message : '操作失败'
    if (modal.value) modalError.value = notice.value
  } finally {
    busy.value = false
  }
}
function open(kind: typeof modal.value, p?: Profile) {
  menu.value = ''
  selected.value = p
  modalError.value = ''
  modal.value = kind
}
function metadata(p?: Profile) {
  form.value = p
    ? { ...p }
    : {
        name: '新订阅',
        url: '',
        content: baseline,
        cron: '',
        timeout: 45,
        retries: 1,
        use_proxy: true,
        proxy_url: '',
        user_agent: 'Clash Verge/2.5.2',
        base64: false,
      }
  open('metadata', p)
}
async function saveMetadata() {
  await run(
    async () => {
      await api(
        selected.value ? '/' + selected.value.id : '',
        selected.value ? 'PUT' : 'POST',
        form.value,
      )
      modal.value = ''
      await reload()
    },
    selected.value
      ? '订阅信息已保存。链接变更后，点击更新下载新配置。'
      : '订阅已导入，点击“使用”切换到此配置。',
  )
}
async function importURL() {
  await run(async () => {
    if (!url.value.trim()) throw new Error('请填写订阅链接')
    await api('', 'POST', { url: url.value.trim(), use_proxy: importProxy.value })
    url.value = ''
    await reload()
  }, '订阅已导入并通过校验，点击“使用”启用。')
}
async function update(p: Profile, proxy?: boolean) {
  menu.value = ''
  await run(async () => {
    try {
      await api(`/${p.id}/update`, 'POST', proxy === undefined ? {} : { use_proxy: proxy })
    } finally {
      await reload()
    }
  }, '订阅已更新；正在使用的配置已自动应用。')
}
async function updateAll() {
  await run(async () => {
    const errors: string[] = []
    for (const p of state.value.profiles.filter((p) => p.url)) {
      try {
        await api(`/${p.id}/update`, 'POST', {})
      } catch (e) {
        errors.push(`${p.name}：${e instanceof Error ? e.message : '失败'}`)
      }
    }
    await reload()
    if (errors.length) throw new Error(errors.join('；'))
  }, '所有远程订阅已更新。')
}
async function activate(p: Profile) {
  await run(async () => {
    await api(`/${p.id}/activate`, 'POST', {})
    await reload()
  }, `已切换到 ${p.name}`)
}
async function edit(p: Profile | undefined, kind: string, part = '') {
  menu.value = ''
  await run(async () => {
    selected.value = p
    editorKind.value = kind
    section.value = part
    if (kind === 'runtime') {
      editor.value = (await api('/runtime')).content
      title.value = '查看运行时配置'
    } else if (kind === 'content') {
      editor.value = (await api(`/${p!.id}/content?section=${encodeURIComponent(part)}`)).content
      title.value =
        (
          { rules: '编辑规则', proxies: '编辑节点', 'proxy-groups': '编辑代理组' } as Record<
            string,
            string
          >
        )[part] || '编辑订阅文件'
      if (p?.url) title.value = title.value.replace('编辑', '查看原始')
    } else {
      editor.value =
        kind === 'global-merge'
          ? state.value.global_merge
          : kind === 'global-script'
            ? state.value.global_script
            : kind === 'merge'
              ? p!.merge
              : p!.script
      title.value =
        (kind.startsWith('global') ? '全局' : '订阅') +
        (kind.endsWith('script') ? '扩展脚本' : '覆写配置')
    }
    open('editor', p)
  })
}
async function saveEditor() {
  if (readOnly.value) return
  await run(async () => {
    if (isGlobal.value) {
      await api('/global', 'PUT', {
        merge: isScript.value ? state.value.global_merge : editor.value,
        script: isScript.value ? editor.value : state.value.global_script,
        timezone: state.value.timezone,
      })
    } else if (editorKind.value === 'content') {
      await api(`/${selected.value!.id}/content`, 'PUT', {
        section: section.value,
        content: editor.value,
      })
    } else {
      await api(`/${selected.value!.id}/extensions`, 'PUT', {
        merge: isScript.value ? selected.value!.merge : editor.value,
        script: isScript.value ? editor.value : selected.value!.script,
      })
    }
    modal.value = ''
    await reload()
  }, '校验通过并已保存，当前订阅的改动已应用。')
}
async function timezone(event: Event) {
  const value = (event.target as HTMLSelectElement).value
  await run(async () => {
    await api('/global', 'PUT', {
      merge: state.value.global_merge,
      script: state.value.global_script,
      timezone: value,
    })
    await reload()
  }, '定时更新时区已保存。')
}
async function share(p: Profile) {
  await run(async () => {
    qr.value = await QRCode.toDataURL(p.url, { width: 300, margin: 2 })
    open('share', p)
  })
}
async function remove() {
  await run(async () => {
    await api(`/${selected.value!.id}`, 'DELETE')
    modal.value = ''
    await reload()
  }, '订阅已删除。')
}
async function move(p: Profile, delta: number) {
  menu.value = ''
  await run(async () => {
    const ids = state.value.profiles.map((item) => item.id)
    const index = ids.indexOf(p.id)
    const destination = index + delta
    if (destination < 0 || destination >= ids.length) return
    ;[ids[index], ids[destination]] = [ids[destination], ids[index]]
    await api('/order', 'PUT', { ids })
    await reload()
  })
}
async function loadFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file) return
  if (file.size > 8 * 1024 * 1024) {
    modalError.value = '文件不能超过 8 MiB'
    return
  }
  form.value.content = await file.text()
  form.value.name = file.name.replace(/\.ya?ml$/i, '')
}
function domain(value: string) {
  try {
    return new URL(value).hostname
  } catch {
    return '本地配置'
  }
}
function safeLink(value: string) {
  try {
    const u = new URL(value)
    return ['http:', 'https:'].includes(u.protocol) ? u.href : undefined
  } catch {
    return undefined
  }
}
function date(value?: string | number) {
  if (!value || (typeof value === 'string' && value.startsWith('0001'))) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: state.value.timezone,
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(new Date(typeof value === 'number' ? value * 1000 : value))
}
function bytes(n: number) {
  if (n < 1024) return n + ' B'
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), 4)
  return (n / 1024 ** i).toFixed(2) + ' ' + ['B', 'KiB', 'MiB', 'GiB', 'TiB'][i]
}
onMounted(() => run(reload))
</script>

<template>
  <section
    class="mx-auto w-full max-w-7xl p-4 pb-28 md:p-6 md:pb-10"
    @click="menu = ''"
  >
    <header class="mb-6 flex flex-wrap items-center justify-between gap-3">
      <div>
        <h1 class="text-2xl font-bold">订阅</h1>
        <p class="mt-1 text-sm opacity-60">管理配置来源、自动更新与扩展</p>
      </div>
      <div class="flex items-center gap-2">
        <button
          class="btn btn-ghost btn-sm"
          :disabled="busy"
          title="查看运行时配置"
          aria-label="查看运行时配置"
          @click="edit(undefined, 'runtime')"
        >
          <DocumentTextIcon class="size-5" />
        </button>
        <span class="badge badge-ghost">{{ state.profiles.length }} 个配置</span>
        <button
          class="btn btn-ghost btn-sm"
          :disabled="busy"
          title="重新读取"
          aria-label="重新读取"
          @click="run(reload)"
        >
          <ArrowPathIcon
            class="size-4"
            :class="{ 'animate-spin': busy }"
          />
        </button>
      </div>
    </header>

    <div
      v-if="notice"
      class="mb-4 rounded-xl border p-3 text-sm break-words"
      :class="failed ? 'border-error/30 bg-error/10 text-error' : 'border-success/30 bg-success/10'"
      role="status"
    >
      {{ notice }}
    </div>
    <form
      class="bg-base-100 border-base-content/10 mb-5 rounded-2xl border p-4"
      @submit.prevent="importURL"
    >
      <label
        for="subscription-url"
        class="mb-2 block text-sm font-medium"
        >订阅链接</label
      >
      <div class="flex flex-wrap gap-2">
        <input
          id="subscription-url"
          v-model="url"
          class="input min-w-0 flex-1 basis-60"
          type="url"
          placeholder="https://…"
          autocomplete="off"
          :disabled="busy"
        />
        <button
          class="btn btn-primary"
          :disabled="busy || !url.trim()"
          type="submit"
        >
          导入
        </button>
        <button
          class="btn btn-soft"
          :disabled="busy"
          type="button"
          @click="metadata()"
        >
          <PlusIcon class="size-4" />新建
        </button>
      </div>
      <div class="mt-3 flex flex-wrap items-center justify-between gap-2 text-xs opacity-70">
        <label class="flex cursor-pointer items-center gap-2"
          ><input
            v-model="importProxy"
            type="checkbox"
            class="checkbox checkbox-xs"
          />通过代理下载</label
        >
        <span>支持 Mihomo YAML 配置；本地文件请点击“新建”</span>
      </div>
    </form>

    <div class="mb-3 flex flex-wrap items-center justify-between gap-2 text-sm">
      <label class="flex items-center gap-2 text-xs opacity-70"
        >定时时区
        <select
          class="select select-xs max-w-44"
          :value="state.timezone"
          :disabled="busy"
          @change="timezone"
        >
          <option
            v-for="zone in [...new Set([state.timezone, 'Asia/Hong_Kong', 'Asia/Shanghai', 'UTC'])]"
            :key="zone"
          >
            {{ zone }}
          </option>
        </select>
      </label>
      <button
        class="btn btn-ghost btn-sm"
        :disabled="busy || !state.profiles.some((p) => p.url)"
        @click="updateAll"
      >
        <ArrowPathIcon class="size-4" />全部更新
      </button>
    </div>
    <div
      v-if="!loaded"
      class="py-16 text-center opacity-60"
    >
      <span class="loading loading-spinner loading-sm" /> 正在读取订阅…
    </div>
    <div
      v-else-if="!state.profiles.length"
      class="border-base-content/15 mb-6 rounded-2xl border border-dashed p-10 text-center"
    >
      <DocumentTextIcon class="mx-auto mb-3 size-9 opacity-35" />
      <h2 class="font-semibold">添加第一份配置</h2>
      <p class="mt-2 text-sm opacity-60">导入订阅链接或本地 YAML 文件，然后点击“使用”。</p>
    </div>
    <div class="mb-7 grid gap-4 lg:grid-cols-2 xl:grid-cols-3">
      <article
        v-for="p in state.profiles"
        :key="p.id"
        class="bg-base-100 relative min-w-0 rounded-2xl border p-4 transition-colors"
        :class="
          p.id === state.active_id
            ? 'border-primary ring-primary/10 ring-2'
            : 'border-base-content/10'
        "
      >
        <div class="flex items-start gap-2">
          <div class="min-w-0 flex-1">
            <h2
              class="truncate font-semibold"
              :title="p.name"
            >
              {{ p.name }}
            </h2>
            <p class="mt-1 truncate text-xs opacity-50">{{ domain(p.url) }}</p>
          </div>
          <span
            v-if="p.id === state.active_id"
            class="badge badge-primary badge-soft badge-sm shrink-0"
            >使用中</span
          >
          <div
            class="relative"
            @click.stop
          >
            <button
              class="btn btn-ghost btn-xs btn-square"
              :disabled="busy"
              :aria-label="p.name + ' 更多操作'"
              @click="menu = menu === p.id ? '' : p.id"
            >
              <EllipsisHorizontalIcon class="size-5" />
            </button>
            <div
              v-if="menu === p.id"
              class="bg-base-100 border-base-content/10 absolute top-8 right-0 z-50 w-44 rounded-xl border p-1 shadow-xl"
            >
              <ul class="menu menu-sm w-full p-0">
                <li v-if="safeLink(p.homepage)">
                  <a
                    :href="safeLink(p.homepage)"
                    target="_blank"
                    rel="noopener noreferrer"
                    >订阅首页 ↗</a
                  >
                </li>
                <li v-if="p.url"><button @click="share(p)">分享二维码</button></li>
                <li><button @click="metadata(p)">编辑信息</button></li>
                <li>
                  <button
                    :disabled="state.profiles[0]?.id === p.id"
                    @click="move(p, -1)"
                  >
                    上移
                  </button>
                </li>
                <li>
                  <button
                    :disabled="state.profiles.at(-1)?.id === p.id"
                    @click="move(p, 1)"
                  >
                    下移
                  </button>
                </li>
                <li>
                  <button @click="edit(p, 'content')">
                    {{ p.url ? '查看原始文件' : '编辑文件' }}
                  </button>
                </li>
                <li>
                  <button @click="edit(p, 'content', 'rules')">
                    {{ p.url ? '查看原始规则' : '编辑规则' }}
                  </button>
                </li>
                <li>
                  <button @click="edit(p, 'content', 'proxies')">
                    {{ p.url ? '查看原始节点' : '编辑节点' }}
                  </button>
                </li>
                <li>
                  <button @click="edit(p, 'content', 'proxy-groups')">
                    {{ p.url ? '查看原始代理组' : '编辑代理组' }}
                  </button>
                </li>
                <li><button @click="edit(p, 'merge')">扩展覆写配置</button></li>
                <li><button @click="edit(p, 'script')">扩展脚本</button></li>
                <li>
                  <a
                    :href="`/api/profiles/${p.id}/export`"
                    download
                    @click="menu = ''"
                    >下载文件</a
                  >
                </li>
                <li v-if="p.url"><button @click="update(p, false)">更新（直连）</button></li>
                <li v-if="p.url"><button @click="update(p, true)">更新（代理）</button></li>
                <li>
                  <button
                    class="text-error"
                    :disabled="p.id === state.active_id"
                    :title="p.id === state.active_id ? '请先切换到其他订阅' : ''"
                    @click="open('delete', p)"
                  >
                    删除
                  </button>
                </li>
              </ul>
            </div>
          </div>
        </div>
        <div
          v-if="p.usage"
          class="mt-4 text-xs"
        >
          <div class="flex justify-between gap-2">
            <span
              >{{ bytes(p.usage.download + p.usage.upload) }} /
              {{ p.usage.total > 0 ? bytes(p.usage.total) : '不限量' }}</span
            ><span class="opacity-50">已用</span>
          </div>
          <progress
            v-if="p.usage.total > 0"
            class="progress progress-primary mt-2 h-1.5 w-full"
            :value="p.usage.download + p.usage.upload"
            :max="p.usage.total"
          />
          <p
            v-if="p.usage.expire"
            class="mt-1 opacity-50"
          >
            到期 {{ date(p.usage.expire) }}
          </p>
        </div>
        <div class="mt-4 space-y-1 text-xs opacity-60">
          <p>
            最近更新 <span class="float-right">{{ date(p.updated_at) }}</span>
          </p>
          <p>
            定时更新
            <span class="float-right">{{ p.cron && p.url ? date(p.next_update) : '未开启' }}</span>
          </p>
        </div>
        <p
          v-if="p.last_error"
          class="bg-error/10 text-error mt-3 rounded-lg p-2 text-xs break-words"
        >
          {{ p.last_error }}
        </p>
        <div class="mt-4 flex items-center justify-between gap-2">
          <div class="flex gap-1">
            <span
              v-if="p.merge"
              class="badge badge-ghost badge-xs"
              >Merge</span
            ><span
              v-if="p.script"
              class="badge badge-ghost badge-xs"
              >Script</span
            ><span
              v-if="!p.url"
              class="badge badge-ghost badge-xs"
              >本地</span
            >
          </div>
          <div class="flex gap-2">
            <button
              v-if="p.url"
              class="btn btn-ghost btn-sm"
              :disabled="busy"
              @click="update(p)"
            >
              <ArrowPathIcon class="size-4" />更新</button
            ><button
              class="btn btn-sm"
              :class="p.id === state.active_id ? 'btn-ghost' : 'btn-primary btn-soft'"
              :disabled="busy || p.id === state.active_id"
              @click="activate(p)"
            >
              <CheckCircleIcon
                v-if="p.id === state.active_id"
                class="size-4"
              />{{ p.id === state.active_id ? '已启用' : '使用' }}
            </button>
          </div>
        </div>
      </article>
    </div>

    <h2 class="mb-3 text-sm font-semibold">全局扩展</h2>
    <div class="grid gap-4 md:grid-cols-2">
      <button
        class="bg-base-100 border-base-content/10 hover:border-primary/40 flex items-center gap-4 rounded-2xl border p-5 text-left disabled:opacity-50"
        :disabled="busy"
        @click="edit(undefined, 'global-merge')"
      >
        <DocumentTextIcon class="text-primary size-8 shrink-0" />
        <div class="flex-1">
          <h3 class="font-semibold">全局覆写配置</h3>
          <p class="mt-1 text-xs opacity-50">
            {{ state.global_merge ? '已配置 · 应用于所有订阅' : '追加规则、合并 YAML 配置' }}
          </p>
        </div>
        <span class="badge badge-outline badge-sm">Merge</span>
      </button>
      <button
        class="bg-base-100 border-base-content/10 hover:border-primary/40 flex items-center gap-4 rounded-2xl border p-5 text-left disabled:opacity-50"
        :disabled="busy"
        @click="edit(undefined, 'global-script')"
      >
        <CodeBracketIcon class="text-primary size-8 shrink-0" />
        <div class="flex-1">
          <h3 class="font-semibold">全局扩展脚本</h3>
          <p class="mt-1 text-xs opacity-50">
            {{
              state.global_script
                ? '已配置 · 应用于所有订阅'
                : '用 JavaScript 处理节点、规则和代理组'
            }}
          </p>
        </div>
        <span class="badge badge-outline badge-sm">Script</span>
      </button>
    </div>
    <p class="mt-4 text-xs leading-6 opacity-45">
      应用顺序：原始订阅 → 全局覆写 → 全局脚本 → 订阅覆写 →
      订阅脚本。校验通过后应用；更新失败时保留上次有效配置。
    </p>

    <Teleport to="body">
      <div
        v-if="modal"
        class="modal modal-open z-[1000]"
        role="dialog"
        aria-modal="true"
        :aria-label="
          modal === 'editor'
            ? title
            : modal === 'metadata'
              ? '订阅信息'
              : modal === 'share'
                ? '分享订阅'
                : '删除订阅'
        "
        @keydown.esc="!busy && (modal = '')"
      >
        <div
          class="modal-box max-h-[90dvh]"
          :class="modal === 'editor' ? 'max-w-4xl' : 'max-w-xl'"
        >
          <div class="mb-4 flex items-center justify-between gap-3">
            <h2 class="text-lg font-bold">
              {{
                modal === 'editor'
                  ? title
                  : modal === 'metadata'
                    ? selected
                      ? '编辑订阅信息'
                      : '新建订阅'
                    : modal === 'share'
                      ? '分享订阅'
                      : '删除订阅'
              }}
            </h2>
            <button
              class="btn btn-ghost btn-sm btn-square"
              :disabled="busy"
              aria-label="关闭"
              @click="modal = ''"
            >
              ✕
            </button>
          </div>
          <div
            v-if="modalError"
            class="bg-error/10 text-error mb-4 rounded-lg p-3 text-sm break-words"
            role="alert"
          >
            {{ modalError }}
          </div>
          <form
            v-if="modal === 'metadata'"
            class="space-y-4"
            @submit.prevent="saveMetadata"
          >
            <label class="block text-sm"
              >名称<input
                v-model="form.name"
                class="input mt-1 w-full"
                required
                maxlength="160"
            /></label>
            <label class="block text-sm"
              >订阅链接<input
                v-model="form.url"
                class="input mt-1 w-full"
                type="url"
                placeholder="留空则为本地配置"
                autocomplete="off"
            /></label>
            <div v-if="!selected && !form.url">
              <label class="block text-sm"
                >导入 YAML 文件<input
                  type="file"
                  accept=".yaml,.yml,.txt"
                  class="file-input mt-1 w-full"
                  @change="loadFile" /></label
              ><SearchableEditor
                :model-value="form.content || ''"
                label="本地 YAML 配置"
                height="12rem"
                @update:model-value="form.content = $event"
              />
            </div>
            <template v-if="form.url">
              <label class="block text-sm"
                >定时更新（Cron）<input
                  v-model="form.cron"
                  class="input mt-1 w-full font-mono"
                  placeholder="留空关闭，例如 30 12 * * *"
                /><span class="mt-1 block text-xs opacity-60"
                  >时区 {{ state.timezone }} · 30 12 * * * = 每天 12:30</span
                ></label
              >
              <div class="grid grid-cols-2 gap-3">
                <label class="text-sm"
                  >超时（秒）<input
                    v-model.number="form.timeout"
                    type="number"
                    min="1"
                    max="120"
                    required
                    class="input mt-1 w-full" /></label
                ><label class="text-sm"
                  >重试次数<input
                    v-model.number="form.retries"
                    type="number"
                    min="0"
                    max="3"
                    required
                    class="input mt-1 w-full"
                /></label>
              </div>
              <label class="flex items-center gap-2 text-sm"
                ><input
                  v-model="form.use_proxy"
                  type="checkbox"
                  class="checkbox checkbox-sm"
                />默认通过代理更新</label
              >
              <label class="block text-sm"
                >下载代理<input
                  v-model="form.proxy_url"
                  class="input mt-1 w-full"
                  placeholder="留空使用部署时的代理"
                  autocomplete="off"
                /><span class="mt-1 block text-xs opacity-60"
                  >从容器发起连接，支持 http://、socks5://；主机代理可使用
                  host.docker.internal。</span
                ></label
              >
              <label class="block text-sm"
                >User-Agent<input
                  v-model="form.user_agent"
                  class="input mt-1 w-full"
              /></label>
              <label class="flex items-center gap-2 text-sm"
                ><input
                  v-model="form.base64"
                  type="checkbox"
                  class="checkbox checkbox-sm"
                />下载内容先进行 Base64 解码</label
              >
            </template>
            <div class="modal-action">
              <button
                class="btn"
                type="button"
                :disabled="busy"
                @click="modal = ''"
              >
                取消</button
              ><button
                class="btn btn-primary"
                :disabled="busy"
                type="submit"
              >
                <span
                  v-if="busy"
                  class="loading loading-spinner loading-xs"
                />{{ selected ? '保存信息' : '导入并校验' }}
              </button>
            </div>
          </form>
          <template v-else-if="modal === 'editor'">
            <p class="mb-3 text-sm opacity-60">
              {{
                editorKind === 'runtime'
                  ? '当前已应用的完整配置，包含覆写、脚本与实时运行参数；内核访问密钥已隐藏。此处仅供查看。'
                  : editorKind === 'content'
                    ? readOnly
                      ? '这是下载后保存的原始订阅，只读。修改请使用覆写配置或扩展脚本。'
                      : '本地配置可以直接编辑，保存前会校验。'
                    : isScript
                      ? '提供 main(config, profileName) 并返回配置对象。限时 2 秒；留空关闭脚本。'
                      : 'YAML 深度合并；普通数组替换，prepend-/append- 前缀可追加。留空关闭覆写。'
              }}
            </p>
            <div
              v-if="!readOnly && editorKind !== 'content'"
              class="mb-2 flex justify-end"
            >
              <button
                class="btn btn-ghost btn-xs"
                :disabled="busy || !!editor.trim()"
                @click="editor = isScript ? sampleScript : sampleMerge"
              >
                插入示例
              </button>
            </div>
            <SearchableEditor
              v-model="editor"
              :readonly="readOnly"
              :label="title"
            />
            <div class="modal-action">
              <button
                class="btn"
                :disabled="busy"
                @click="modal = ''"
              >
                {{ readOnly ? '关闭' : '取消' }}</button
              ><button
                v-if="!readOnly"
                class="btn btn-primary"
                :disabled="busy"
                @click="saveEditor"
              >
                <span
                  v-if="busy"
                  class="loading loading-spinner loading-xs"
                />保存并应用
              </button>
            </div>
          </template>
          <template v-else-if="modal === 'share'"
            ><p class="text-sm opacity-60">此二维码包含完整订阅链接，请仅分享给可信任的人。</p>
            <img
              :src="qr"
              class="mx-auto my-4 max-w-full rounded-lg"
              alt="订阅链接二维码"
            />
            <p class="text-center text-sm font-medium">{{ selected?.name }}</p></template
          >
          <template v-else-if="modal === 'delete'"
            ><p class="text-sm">删除「{{ selected?.name }}」及其覆写配置和脚本？</p>
            <div class="modal-action">
              <button
                class="btn"
                :disabled="busy"
                @click="modal = ''"
              >
                取消</button
              ><button
                class="btn btn-error"
                :disabled="busy"
                @click="remove"
              >
                删除
              </button>
            </div></template
          >
        </div>
        <button
          class="modal-backdrop"
          aria-label="关闭对话框"
          :disabled="busy"
          @click="modal = ''"
        />
      </div>
    </Teleport>
  </section>
</template>
