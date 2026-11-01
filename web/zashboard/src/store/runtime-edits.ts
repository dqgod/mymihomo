import { ref } from 'vue'
export type RuntimeKind = 'groups' | 'rules' | 'providers'
export const runtimeEditor = ref<'groups' | 'rules' | null>(null)
export const runtimeInitialKind = ref<RuntimeKind>('groups')
export const runtimeSearch = ref('')
export function openRuntimeEditor(kind: RuntimeKind, search = '') {
  runtimeInitialKind.value = kind
  runtimeSearch.value = search
  runtimeEditor.value = kind === 'groups' ? 'groups' : 'rules'
}
