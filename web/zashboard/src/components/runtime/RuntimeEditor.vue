<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { parse, stringify } from 'yaml'
import { fetchConfigs } from '@/assembly/config'
import { fetchProxies } from '@/assembly/proxies'
import { fetchRules } from '@/assembly/rules'
import SearchableEditor from '@/components/common/SearchableEditor.vue'
import SearchablePolicySelect from '@/components/common/SearchablePolicySelect.vue'
import { runtimeSearch, type RuntimeKind } from '@/store/runtime-edits'
import { ArrowLeftIcon, ArrowPathIcon, PlusIcon } from '@heroicons/vue/24/outline'

type Row = {
  id: string
  kind: RuntimeKind
  status: 'added' | 'modified' | 'unchanged'
  enabled: boolean
  effective_enabled: boolean
  missing: boolean
  moved: boolean
  blocked_by?: string[]
  value: any
  original: any
  yaml: string
  original_yaml: string
}
type Snapshot = {
  profile_id: string
  profile_name: string
  revision: string
  groups: Row[]
  rules: Row[]
  providers: Row[]
  proxy_options: string[]
  proxy_provider_options: string[]
}
const props = defineProps<{ initialKind: RuntimeKind }>()
const emit = defineEmits<{ close: [] }>()
const kind = ref(props.initialKind)
const data = ref<Snapshot>()
const busy = ref(false)
const message = ref('')
const error = ref('')
const query = ref(runtimeSearch.value)
const status = ref('all')
const checked = ref<string[]>([])
const page = ref(1)
const labels = { groups: '代理组', rules: '规则', providers: '规则集' }
const statusLabels = { added: '新增', modified: '已修改', unchanged: '原配置' }
const rows = computed(() => data.value?.[kind.value] || [])
const filtered = computed(() =>
  rows.value.filter((r) => {
    const hit = !query.value || r.yaml.toLowerCase().includes(query.value.toLowerCase())
    return (
      hit &&
      (status.value === 'all' ||
        r.status === status.value ||
        (status.value === 'disabled' && !r.effective_enabled) ||
        (status.value === 'missing' && r.missing))
    )
  }),
)
const pages = computed(() => Math.max(1, Math.ceil(filtered.value.length / 50)))
const visible = computed(() => filtered.value.slice((page.value - 1) * 50, page.value * 50))
const selected = computed(() => rows.value.filter((r) => checked.value.includes(r.id)))
const addedSelected = computed(() => selected.value.filter((r) => r.status === 'added'))
const modifiedSelected = computed(() => selected.value.filter((r) => r.status === 'modified'))
const allChecked = computed(
  () => visible.value.length > 0 && visible.value.every((r) => checked.value.includes(r.id)),
)
const modal = ref<'' | 'edit' | 'confirm' | 'compare'>('')
const target = ref<Row>()
const form = ref<Record<string, any>>({})
const advanced = ref(false)
const editEnabled = ref(true)
const source = ref('')
const modalError = ref('')
const memberSearch = ref('')
const ruleType = ref('DOMAIN-SUFFIX')
const payload = ref('')
const policy = ref('DIRECT')
const flags = ref<string[]>([])
const before = ref('')
const confirmation = ref({ action: '', ids: [] as string[], label: '' })
const ruleTypes = [
  'DOMAIN',
  'DOMAIN-SUFFIX',
  'DOMAIN-KEYWORD',
  'IP-CIDR',
  'IP-CIDR6',
  'GEOSITE',
  'GEOIP',
  'RULE-SET',
  'PROCESS-NAME',
  'PROCESS-PATH',
  'DST-PORT',
  'SRC-IP-CIDR',
  'NETWORK',
  'MATCH',
  'AND',
  'OR',
  'NOT',
  'SUB-RULE',
]
const members = computed(() =>
  (data.value?.proxy_options || []).filter(
    (name) =>
      name !== target.value?.value?.name &&
      name !== form.value.name &&
      name.toLowerCase().includes(memberSearch.value.toLowerCase()),
  ),
)
const policyChoices = computed(() => [
  ...new Set([
    'DIRECT',
    'REJECT',
    'REJECT-DROP',
    'PASS',
    ...(data.value?.groups || []).map((group) => String(group.value.name)),
    ...(data.value?.proxy_options || []),
  ]),
])
const currentRule = computed(() =>
  [
    ruleType.value,
    ...(ruleType.value === 'MATCH' ? [] : [payload.value]),
    policy.value,
    ...flags.value,
  ].join(','),
)
watch([kind, query, status], () => {
  page.value = 1
  checked.value = []
})
watch(pages, (n) => {
  if (page.value > n) page.value = n
})

async function request(body?: object) {
  const response = await fetch('/api/runtime-edits', {
    method: body ? 'POST' : 'GET',
    headers: { 'Content-Type': 'application/json' },
    ...(body ? { body: JSON.stringify(body) } : {}),
  })
  if (response.status === 401) {
    location.assign('/login')
    throw new Error('请重新登录')
  }
  const value = await response.json()
  if (!response.ok) throw new Error(value.error || '操作失败')
  return value
}
async function reload() {
  data.value = await request()
  checked.value = []
}
async function run(fn: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  message.value = ''
  modalError.value = ''
  try {
    await fn()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
    if (modal.value) modalError.value = error.value
  } finally {
    busy.value = false
  }
}
async function change(action: object) {
  if (!data.value) return
  await request({ profile_id: data.value.profile_id, revision: data.value.revision, ...action })
  await reload()
  await Promise.allSettled([fetchConfigs(), fetchProxies(), fetchRules()])
  message.value = '已保存并应用；订阅更新和重启后会继续保留。'
}
function selectPage() {
  checked.value = allChecked.value
    ? checked.value.filter((id) => !visible.value.some((r) => r.id === id))
    : [...new Set([...checked.value, ...visible.value.map((r) => r.id)])]
}
function splitRule(text: string) {
  const parts: string[] = []
  let start = 0,
    depth = 0
  for (let i = 0; i < text.length; i++) {
    if (text[i] === '(') depth++
    if (text[i] === ')') depth--
    if (text[i] === ',' && depth === 0) {
      parts.push(text.slice(start, i).trim())
      start = i + 1
    }
  }
  parts.push(text.slice(start).trim())
  return parts
}
function readRule(text: string) {
  const parts = splitRule(text)
  ruleType.value = parts[0]
  payload.value = parts[0] === 'MATCH' ? '' : parts[1] || ''
  const at = parts[0] === 'MATCH' ? 1 : 2
  policy.value = parts[at] || 'DIRECT'
  flags.value = parts.slice(at + 1)
}
function edit(row?: Row) {
  target.value = row
  editEnabled.value = row?.enabled ?? true
  modalError.value = ''
  memberSearch.value = ''
  advanced.value = false
  before.value = ''
  if (kind.value === 'rules') {
    readRule(row ? String(row.value) : 'DOMAIN-SUFFIX,example.com,DIRECT')
  } else if (row) {
    form.value = JSON.parse(JSON.stringify(row.value))
  } else if (kind.value === 'groups') {
    form.value = { name: '', type: 'select', proxies: ['DIRECT'], use: [] }
  } else {
    form.value = { name: '', type: 'inline', behavior: 'domain', payload: ['+.example.com'] }
  }
  modal.value = 'edit'
}
function basicYAML() {
  return kind.value === 'rules' ? currentRule.value : stringify(form.value)
}
function switchEditor() {
  modalError.value = ''
  if (!advanced.value) {
    source.value = basicYAML()
    advanced.value = true
  } else {
    try {
      if (kind.value === 'rules') readRule(source.value)
      else {
        const parsed = parse(source.value)
        if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object')
          throw new Error('请输入 YAML 对象')
        for (const field of ['proxies', 'use', 'payload']) {
          if (parsed[field] !== undefined && !Array.isArray(parsed[field]))
            throw new Error(field + ' 必须是列表')
        }
        form.value = parsed
      }
      advanced.value = false
    } catch (e) {
      modalError.value = e instanceof Error ? e.message : 'YAML 无效'
    }
  }
}
function groupType() {
  if (form.value.type === 'url-test') {
    form.value.url ||= 'https://www.gstatic.com/generate_204'
    form.value.interval ||= 300
    form.value.tolerance ??= 50
  }
}
function providerType() {
  if (form.value.type === 'inline') {
    form.value.payload ||= ['+.example.com']
    delete form.value.url
    delete form.value.path
  } else {
    form.value.path ||= './rule-providers/' + (form.value.name || 'custom') + '.yaml'
    form.value.format ||= 'yaml'
    delete form.value.payload
    if (form.value.type === 'http') form.value.interval ||= 86400
    else delete form.value.url
  }
}
function toggleMember(name: string, field = 'proxies') {
  const values = (form.value[field] || []) as string[]
  form.value[field] = values.includes(name) ? values.filter((v) => v !== name) : [...values, name]
}
async function save() {
  await run(async () => {
    await change({
      action: target.value ? 'edit' : 'create',
      kind: kind.value,
      id: target.value?.id,
      yaml: advanced.value ? source.value : basicYAML(),
      before: before.value,
      disabled: kind.value === 'groups' ? undefined : !editEnabled.value,
    })
    modal.value = ''
  })
}
function confirm(action: string, items: Row[]) {
  confirmation.value = {
    action,
    ids: items.map((r) => r.id),
    label:
      action === 'delete'
        ? `删除 ${items.length} 个新增${labels[kind.value]}？若仍被引用，将拒绝删除。`
        : `恢复 ${items.length} 个条目的当前默认配置？原配置已移除的条目将从运行配置中移除。`,
  }
  modalError.value = ''
  modal.value = 'confirm'
}
async function doConfirm() {
  await run(async () => {
    await change({ action: confirmation.value.action, ids: confirmation.value.ids })
    modal.value = ''
  })
}
async function move(row: Row, delta: number) {
  const index = rows.value.findIndex((r) => r.id === row.id)
  const next = delta < 0 ? rows.value[index - 1]?.id : rows.value[index + 2]?.id || ''
  await run(() => change({ action: 'move', id: row.id, before: next }))
}
function compare(row: Row) {
  target.value = row
  modalError.value = ''
  modal.value = 'compare'
}
onMounted(() => run(reload))
</script>

<template>
  <section class="mx-auto w-full max-w-7xl overflow-y-auto p-3 pb-28 md:p-6 md:pb-12">
    <header class="mb-4 flex flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-3">
        <button
          class="btn btn-ghost btn-sm"
          :disabled="busy"
          aria-label="返回运行面板"
          @click="emit('close')"
        >
          <ArrowLeftIcon class="size-4" />返回
        </button>
        <div>
          <h1 class="text-xl font-bold">
            {{ initialKind === 'groups' ? '代理组编辑' : '规则编辑' }}
          </h1>
          <p class="mt-1 text-xs opacity-60">当前订阅：{{ data?.profile_name || '读取中…' }}</p>
        </div>
      </div>
      <div class="flex gap-2">
        <button
          class="btn btn-ghost btn-sm"
          :disabled="busy"
          aria-label="重新读取配置"
          @click="run(reload)"
        >
          <ArrowPathIcon
            class="size-4"
            :class="busy && 'animate-spin'"
          /></button
        ><button
          class="btn btn-primary btn-sm"
          :disabled="busy || !data"
          @click="edit()"
        >
          <PlusIcon class="size-4" />新增{{ labels[kind] }}
        </button>
      </div>
    </header>
    <p
      class="bg-base-100 border-base-content/10 mb-4 rounded-xl border p-3 text-xs leading-6 opacity-75"
    >
      只修改当前订阅的运行配置，不改原始订阅、覆写或脚本。编辑会独立保存，更新和重启后继续叠加。组名/规则集名称变更会同步调整引用；关联调整不单独计为手动编辑。
    </p>
    <div
      v-if="message"
      class="bg-success/10 mb-3 rounded-lg p-3 text-sm"
      role="status"
    >
      {{ message }}
    </div>
    <div
      v-if="error"
      class="bg-error/10 text-error mb-3 rounded-lg p-3 text-sm break-words"
      role="alert"
    >
      {{ error }}
    </div>
    <div class="mb-3 flex flex-wrap items-center gap-2">
      <div
        v-if="initialKind !== 'groups'"
        class="join"
      >
        <button
          class="btn btn-sm join-item"
          :class="kind === 'rules' && 'btn-active'"
          @click="kind = 'rules'"
        >
          规则 {{ data?.rules.length || 0 }}</button
        ><button
          class="btn btn-sm join-item"
          :class="kind === 'providers' && 'btn-active'"
          @click="kind = 'providers'"
        >
          规则集 {{ data?.providers.length || 0 }}
        </button>
      </div>
      <input
        v-model="query"
        class="input input-sm min-w-36 flex-1"
        :placeholder="`搜索${labels[kind]}`"
        :aria-label="`搜索${labels[kind]}`"
      />
      <select
        v-model="status"
        class="select select-sm w-36"
        aria-label="变更筛选"
      >
        <option value="all">全部 {{ rows.length }}</option>
        <option value="added">新增 {{ rows.filter((r) => r.status === 'added').length }}</option>
        <option value="modified">
          已修改 {{ rows.filter((r) => r.status === 'modified').length }}
        </option>
        <option value="unchanged">原有未修改</option>
        <option
          v-if="kind !== 'groups'"
          value="disabled"
        >
          已禁用 / 暂停
        </option>
        <option value="missing">原项已移除</option>
      </select>
    </div>
    <div class="mb-3 flex flex-wrap items-center gap-3 text-xs">
      <label class="flex items-center gap-2"
        ><input
          type="checkbox"
          class="checkbox checkbox-xs"
          :checked="allChecked"
          :disabled="busy || !visible.length"
          @change="selectPage"
        />本页全选</label
      ><span class="opacity-60">已选 {{ selected.length }}</span>
      <button
        class="btn btn-xs btn-ghost text-error"
        :disabled="busy || !addedSelected.length"
        @click="confirm('delete', addedSelected)"
      >
        删除选中新增项（{{ addedSelected.length }}）
      </button>
      <button
        class="btn btn-xs btn-ghost"
        :disabled="busy || !modifiedSelected.length"
        @click="confirm('restore', modifiedSelected)"
      >
        恢复选中修改项（{{ modifiedSelected.length }}）
      </button>
    </div>
    <div class="space-y-2">
      <article
        v-for="row in visible"
        :key="row.id"
        class="bg-base-100 border-base-content/10 rounded-xl border p-3"
        :data-runtime-id="row.id"
        :data-runtime-status="row.status"
      >
        <div class="flex items-start gap-3">
          <input
            v-model="checked"
            :value="row.id"
            type="checkbox"
            class="checkbox checkbox-sm mt-1"
            :disabled="busy"
            :aria-label="`选择 ${kind === 'rules' ? row.value : row.value.name}`"
          />
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <span
                class="badge badge-sm"
                :class="
                  row.status === 'added'
                    ? 'badge-success badge-soft'
                    : row.status === 'modified'
                      ? 'badge-warning badge-soft'
                      : 'badge-ghost'
                "
                >{{ statusLabels[row.status] }}</span
              ><span
                v-if="row.moved"
                class="badge badge-ghost badge-sm"
                >已排序</span
              ><span
                v-if="row.missing"
                class="text-warning text-xs"
                >原项已移除，保留当前编辑</span
              >
            </div>
            <p
              class="mt-2 text-sm break-all"
              :class="kind === 'rules' ? 'font-mono' : 'font-semibold'"
            >
              {{ kind === 'rules' ? row.value : row.value.name }}
            </p>
            <p
              v-if="kind === 'groups'"
              class="mt-1 text-xs opacity-60"
            >
              {{ row.value.type }} · {{ row.value.proxies?.length || 0 }} 个成员 ·
              {{ row.value.use?.length || 0 }} 个代理提供者
            </p>
            <p
              v-if="kind === 'providers'"
              class="mt-1 text-xs opacity-60"
            >
              {{ row.value.type }} · {{ row.value.behavior }} · {{ row.value.format || 'yaml' }}
            </p>
            <p
              v-if="row.blocked_by?.length"
              class="text-warning mt-1 text-xs"
            >
              因规则集禁用而暂停：{{ row.blocked_by.join('、') }}
            </p>
          </div>
          <label
            v-if="kind !== 'groups'"
            class="flex shrink-0 items-center gap-2 text-xs"
            ><span>{{ row.enabled ? '启用' : '禁用' }}</span
            ><input
              type="checkbox"
              class="toggle toggle-sm"
              :checked="row.enabled"
              :disabled="busy"
              :aria-label="`启用 ${kind === 'rules' ? row.value : row.value.name}`"
              @change="run(() => change({ action: 'toggle', id: row.id, disabled: row.enabled }))"
          /></label>
        </div>
        <div class="mt-2 flex flex-wrap items-center justify-end gap-1">
          <template v-if="kind === 'rules'"
            ><button
              class="btn btn-ghost btn-xs"
              :disabled="busy || rows[0]?.id === row.id"
              @click="move(row, -1)"
            >
              上移</button
            ><button
              class="btn btn-ghost btn-xs"
              :disabled="busy || rows.at(-1)?.id === row.id"
              @click="move(row, 1)"
            >
              下移
            </button></template
          >
          <button
            class="btn btn-ghost btn-xs"
            :disabled="busy"
            @click="edit(row)"
          >
            编辑
          </button>
          <button
            v-if="row.status === 'modified'"
            class="btn btn-ghost btn-xs"
            @click="compare(row)"
          >
            对比原配置
          </button>
          <button
            v-if="row.status === 'modified'"
            class="btn btn-ghost btn-xs"
            :disabled="busy"
            @click="confirm('restore', [row])"
          >
            恢复默认
          </button>
          <button
            v-if="row.status === 'added'"
            class="btn btn-ghost btn-xs text-error"
            :disabled="busy"
            @click="confirm('delete', [row])"
          >
            删除
          </button>
        </div>
      </article>
      <p
        v-if="data && !visible.length"
        class="py-12 text-center text-sm opacity-50"
      >
        没有符合条件的{{ labels[kind] }}
      </p>
      <p
        v-if="!data && busy"
        class="py-12 text-center"
      >
        <span class="loading loading-spinner" />
      </p>
    </div>
    <div
      v-if="pages > 1"
      class="mt-4 flex items-center justify-center gap-3 text-sm"
    >
      <button
        class="btn btn-sm"
        :disabled="page <= 1"
        @click="page--"
      >
        上一页</button
      >{{ page }} / {{ pages }} · {{ filtered.length }} 条<button
        class="btn btn-sm"
        :disabled="page >= pages"
        @click="page++"
      >
        下一页
      </button>
    </div>

    <Teleport to="body"
      ><div
        v-if="modal"
        class="modal modal-open z-[1000]"
        role="dialog"
        aria-modal="true"
        :aria-label="
          modal === 'edit' ? `编辑${labels[kind]}` : modal === 'confirm' ? '确认操作' : '对比配置'
        "
        @keydown.esc="!busy && (modal = '')"
      >
        <div
          class="modal-box max-h-[92dvh]"
          :class="modal === 'confirm' ? 'max-w-lg' : 'max-w-4xl'"
        >
          <div class="mb-4 flex items-center justify-between gap-3">
            <h2 class="text-lg font-semibold">
              {{
                modal === 'edit'
                  ? (target ? '编辑' : '新增') + labels[kind]
                  : modal === 'confirm'
                    ? '确认操作'
                    : '对比原配置'
              }}
            </h2>
            <button
              class="btn btn-ghost btn-sm"
              aria-label="关闭"
              :disabled="busy"
              @click="modal = ''"
            >
              ✕
            </button>
          </div>
          <p
            v-if="modalError"
            role="alert"
            class="bg-error/10 text-error mb-3 rounded-lg p-3 text-sm break-words"
          >
            {{ modalError }}
          </p>
          <template v-if="modal === 'confirm'"
            ><p class="text-sm leading-6">{{ confirmation.label }}</p>
            <div class="modal-action">
              <button
                class="btn"
                :disabled="busy"
                @click="modal = ''"
              >
                取消</button
              ><button
                class="btn btn-primary"
                :disabled="busy"
                @click="doConfirm"
              >
                确认
              </button>
            </div></template
          >
          <template v-else-if="modal === 'compare'"
            ><p class="mb-2 text-sm opacity-60">
              默认值来自当前订阅及现有覆写/脚本，不包含本页面的编辑。
            </p>
            <div class="grid gap-4 md:grid-cols-2">
              <div>
                <h3 class="mb-2 text-sm">原配置{{ target?.missing ? '（已移除）' : '' }}</h3>
                <SearchableEditor
                  :model-value="target?.original_yaml || ''"
                  label="原配置"
                  readonly
                />
              </div>
              <div>
                <h3 class="mb-2 text-sm">
                  当前配置{{ target?.enabled === false ? '（已禁用）' : '' }}
                </h3>
                <SearchableEditor
                  :model-value="target?.yaml || ''"
                  label="当前配置"
                  readonly
                />
              </div></div
          ></template>
          <form
            v-else
            class="space-y-4"
            @submit.prevent="save"
          >
            <div class="flex flex-wrap justify-between gap-2 text-xs opacity-70">
              <span>校验通过后应用；失败时保留当前配置。</span
              ><button
                type="button"
                class="btn btn-ghost btn-xs"
                @click="switchEditor"
              >
                {{ advanced ? '切换到表单' : '高级 YAML / 原始规则' }}
              </button>
            </div>
            <SearchableEditor
              v-if="advanced"
              v-model="source"
              :label="`${labels[kind]}配置`"
            />
            <template v-else-if="kind === 'groups'">
              <div class="grid gap-3 sm:grid-cols-2">
                <label class="text-sm"
                  >组名<input
                    v-model="form.name"
                    class="input mt-1 w-full"
                    required /></label
                ><label class="text-sm"
                  >选择方式<select
                    v-model="form.type"
                    aria-label="选择方式"
                    class="select mt-1 w-full"
                    @change="groupType"
                  >
                    <option value="select">手动选择 select</option>
                    <option value="url-test">自动选择 url-test</option>
                    <option value="fallback">故障转移 fallback</option>
                    <option value="load-balance">负载均衡 load-balance</option>
                    <option value="relay">链式 relay</option>
                    <option value="smart">smart</option>
                  </select></label
                >
              </div>
              <div
                v-if="['url-test', 'fallback', 'load-balance'].includes(form.type)"
                class="grid gap-3 sm:grid-cols-3"
              >
                <label class="text-sm sm:col-span-3"
                  >测速地址<input
                    v-model="form.url"
                    class="input mt-1 w-full"
                    type="url"
                    required /></label
                ><label class="text-sm"
                  >间隔（秒）<input
                    v-model.number="form.interval"
                    class="input mt-1 w-full"
                    type="number"
                    min="1"
                    required /></label
                ><label class="text-sm"
                  >容差（毫秒）<input
                    v-model.number="form.tolerance"
                    class="input mt-1 w-full"
                    type="number"
                    min="0" /></label
                ><label class="flex items-center gap-2 text-sm"
                  ><input
                    v-model="form.lazy"
                    type="checkbox"
                    class="checkbox checkbox-sm"
                  />按需测速</label
                >
              </div>
              <div>
                <label class="text-sm"
                  >节点与代理组成员<input
                    v-model="memberSearch"
                    class="input input-sm mt-2 w-full"
                    placeholder="筛选可选成员"
                /></label>
                <div
                  class="bg-base-200/50 mt-2 grid max-h-56 gap-2 overflow-y-auto rounded-lg p-3 sm:grid-cols-2"
                >
                  <label
                    v-for="name in members"
                    :key="name"
                    class="flex min-w-0 items-center gap-2 text-xs"
                    ><input
                      class="checkbox checkbox-xs shrink-0"
                      type="checkbox"
                      :checked="(form.proxies || []).includes(name)"
                      @change="toggleMember(name)"
                    /><span class="break-all">{{ name }}</span></label
                  >
                </div>
                <p class="mt-1 text-xs opacity-60">
                  已选
                  {{ form.proxies?.length || 0 }}
                  个成员；未在列表中的原有选项会保留，可在高级编辑中调整顺序。
                </p>
              </div>
              <div v-if="data?.proxy_provider_options.length">
                <p class="mb-2 text-sm">代理提供者（use）</p>
                <div class="flex flex-wrap gap-3">
                  <label
                    v-for="name in data.proxy_provider_options"
                    :key="name"
                    class="flex items-center gap-2 text-xs"
                    ><input
                      type="checkbox"
                      class="checkbox checkbox-xs"
                      :checked="(form.use || []).includes(name)"
                      @change="toggleMember(name, 'use')"
                    />{{ name }}</label
                  >
                </div>
              </div>
            </template>
            <template v-else-if="kind === 'rules'">
              <label class="block text-sm"
                >规则类型<select
                  v-model="ruleType"
                  aria-label="规则类型"
                  class="select mt-1 w-full"
                >
                  <option
                    v-for="type in [...new Set([...ruleTypes, ruleType])]"
                    :key="type"
                  >
                    {{ type }}
                  </option>
                </select></label
              >
              <label
                v-if="ruleType !== 'MATCH'"
                class="block text-sm"
                >{{ ruleType === 'RULE-SET' ? '规则集名称' : '匹配内容'
                }}<input
                  v-model="payload"
                  class="input mt-1 w-full font-mono"
                  required
                  :list="ruleType === 'RULE-SET' ? 'runtime-providers' : undefined"
              /></label>
              <datalist id="runtime-providers">
                <option
                  v-for="item in data?.providers || []"
                  :key="item.id"
                  :value="item.value.name"
                />
              </datalist>
              <label
                v-if="ruleType === 'SUB-RULE'"
                class="block text-sm"
                >子规则名称<input
                  v-model="policy"
                  class="input mt-1 w-full"
                  required
              /></label>
              <div
                v-else
                class="block text-sm"
              >
                <span>目标策略 / 代理组</span
                ><SearchablePolicySelect
                  v-model="policy"
                  :options="policyChoices"
                  label="目标策略 / 代理组"
                  :disabled="busy"
                />
              </div>

              <label class="flex items-center gap-2 text-sm"
                ><input
                  type="checkbox"
                  class="checkbox checkbox-sm"
                  :checked="flags.includes('no-resolve')"
                  @change="
                    flags = flags.includes('no-resolve')
                      ? flags.filter((f) => f !== 'no-resolve')
                      : [...flags, 'no-resolve']
                  "
                />不触发 DNS 解析（no-resolve）</label
              >
              <p class="bg-base-200 rounded-lg p-3 font-mono text-xs break-all">
                {{ currentRule }}
              </p>
            </template>
            <template v-else>
              <div class="grid gap-3 sm:grid-cols-2">
                <label class="text-sm"
                  >规则集名称<input
                    v-model="form.name"
                    class="input mt-1 w-full"
                    required /></label
                ><label class="text-sm"
                  >来源类型<select
                    v-model="form.type"
                    aria-label="来源类型"
                    class="select mt-1 w-full"
                    @change="providerType"
                  >
                    <option value="inline">内联 inline</option>
                    <option value="http">远程 http</option>
                    <option value="file">文件 file</option>
                  </select></label
                ><label class="text-sm"
                  >规则行为<select
                    v-model="form.behavior"
                    aria-label="规则行为"
                    class="select mt-1 w-full"
                  >
                    <option>domain</option>
                    <option>ipcidr</option>
                    <option>classical</option>
                  </select></label
                ><label
                  v-if="form.type !== 'inline'"
                  class="text-sm"
                  >文件格式<select
                    v-model="form.format"
                    aria-label="文件格式"
                    class="select mt-1 w-full"
                  >
                    <option>yaml</option>
                    <option>text</option>
                    <option>mrs</option>
                  </select></label
                >
              </div>
              <label
                v-if="form.type === 'http'"
                class="block text-sm"
                >下载地址<input
                  v-model="form.url"
                  type="url"
                  class="input mt-1 w-full"
                  required
                  autocomplete="off"
              /></label>
              <label
                v-if="form.type !== 'inline'"
                class="block text-sm"
                >文件路径<input
                  v-model="form.path"
                  class="input mt-1 w-full"
                  required
              /></label>
              <label
                v-if="form.type === 'http'"
                class="block text-sm"
                >更新间隔（秒）<input
                  v-model.number="form.interval"
                  type="number"
                  min="1"
                  class="input mt-1 w-full"
                  required
              /></label>
              <div
                v-if="form.type === 'inline'"
                class="block text-sm"
              >
                <p class="mb-2">规则内容（每行一条）</p>
                <SearchableEditor
                  :model-value="(form.payload || []).join('\n')"
                  label="规则内容"
                  height="12rem"
                  @update:model-value="
                    form.payload = $event.split('\n').filter((line) => line.trim())
                  "
                />
              </div>
            </template>
            <label
              v-if="kind === 'rules' && !target"
              class="block text-sm"
              >插入位置<select
                v-model="before"
                class="select mt-1 w-full"
              >
                <option value="">自动放在 MATCH 之前</option>
                <option
                  v-for="row in data?.rules || []"
                  :key="row.id"
                  :value="row.id"
                >
                  放在 {{ String(row.value).slice(0, 100) }} 之前
                </option>
              </select></label
            >
            <label
              v-if="kind !== 'groups'"
              class="flex items-center gap-2 text-sm"
              ><input
                v-model="editEnabled"
                type="checkbox"
                class="checkbox checkbox-sm"
              />保存后启用</label
            >
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
                />保存并应用
              </button>
            </div>
          </form>
        </div>
        <button
          class="modal-backdrop"
          aria-label="关闭弹窗"
          :disabled="busy"
          @click="modal = ''"
        /></div
    ></Teleport>
  </section>
</template>
