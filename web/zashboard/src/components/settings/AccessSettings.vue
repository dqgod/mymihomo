<script setup lang="ts">
import { ref } from 'vue'
const expanded = ref(false)
const current = ref('')
const password = ref('')
const confirm = ref('')
const busy = ref(false)
const error = ref('')
async function logout() {
  busy.value = true
  error.value = ''
  try {
    const response = await fetch('/auth/logout', { method: 'POST' })
    if (!response.ok && response.status !== 401) throw new Error('退出失败，请重试')
    location.assign('/login')
  } catch (e) {
    error.value = e instanceof Error ? e.message : '退出失败'
    busy.value = false
  }
}
async function save() {
  error.value = ''
  if (password.value !== confirm.value) {
    error.value = '两次新密码不一致'
    return
  }
  busy.value = true
  try {
    const response = await fetch('/auth/password', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        current_password: current.value,
        new_password: password.value,
        confirm_password: confirm.value,
      }),
    })
    if (response.status === 401) {
      location.assign('/login')
      return
    }
    const data = await response.json()
    if (!response.ok) throw new Error(data.error || '密码修改失败')
    current.value = password.value = confirm.value = ''
    location.assign('/login?changed=1')
  } catch (e) {
    error.value = e instanceof Error ? e.message : '密码修改失败'
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <section
    class="bg-base-100 border-base-content/10 mb-5 rounded-2xl border p-4"
    aria-label="访问与安全"
  >
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="font-semibold">访问与安全</h2>
        <p class="mt-1 text-xs opacity-60">管理控制台登录密码与当前会话</p>
      </div>
      <div class="flex gap-2">
        <button
          class="btn btn-sm"
          :disabled="busy"
          :aria-expanded="expanded"
          @click="expanded = !expanded"
        >
          修改访问密码</button
        ><button
          class="btn btn-sm btn-ghost"
          :disabled="busy"
          @click="logout"
        >
          退出登录
        </button>
      </div>
    </div>
    <p
      v-if="error"
      class="bg-error/10 text-error mt-3 rounded-lg p-3 text-sm"
      role="alert"
    >
      {{ error }}
    </p>
    <form
      v-if="expanded"
      class="mt-4 grid max-w-lg gap-3"
      @submit.prevent="save"
    >
      <label class="text-sm"
        >当前密码<input
          v-model="current"
          type="password"
          autocomplete="current-password"
          required
          class="input mt-1 w-full"
          :disabled="busy"
      /></label>
      <label class="text-sm"
        >新密码<input
          v-model="password"
          type="password"
          autocomplete="new-password"
          required
          minlength="8"
          maxlength="72"
          class="input mt-1 w-full"
          :disabled="busy"
      /></label>
      <label class="text-sm"
        >确认新密码<input
          v-model="confirm"
          type="password"
          autocomplete="new-password"
          required
          minlength="8"
          maxlength="72"
          class="input mt-1 w-full"
          :disabled="busy"
      /></label>
      <p class="text-xs leading-5 opacity-60">
        至少 8
        个字符。保存后所有登录会话失效，需使用新密码重新登录。密码会持久保存；代理节点和内核密钥不变。
      </p>
      <button
        class="btn btn-primary justify-self-start"
        :disabled="busy"
        type="submit"
      >
        <span
          v-if="busy"
          class="loading loading-spinner loading-xs"
        />保存新密码
      </button>
    </form>
  </section>
</template>
