<template>
  <BaseDialog :show="show" :title="t('usage.detail.title')" width="wide" :close-on-click-outside="true" @close="close">
    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-10">
      <svg class="h-7 w-7 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
      </svg>
    </div>

    <!-- Error state -->
    <div v-else-if="loadError" class="py-8 text-center text-sm text-red-500">
      {{ t('usage.detail.loadFailed') }}
    </div>

    <!-- Detail content -->
    <div v-else-if="detail" class="space-y-4 text-sm">
      <!-- Summary -->
      <div class="grid grid-cols-2 gap-x-6 gap-y-3">
        <div>
          <span class="font-medium text-gray-500 dark:text-dark-400">{{ t('usage.detail.time') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">{{ formatDateTime(detail.created_at) }}</p>
        </div>
        <div>
          <span class="font-medium text-gray-500 dark:text-dark-400">{{ t('usage.detail.model') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">
            <span class="font-mono">{{ detail.model || '-' }}</span>
            <template v-if="detail.upstream_model && detail.upstream_model !== detail.model">
              <span class="mx-1 text-gray-400">→</span>
              <span class="font-mono">{{ detail.upstream_model }}</span>
            </template>
          </p>
        </div>
        <div>
          <span class="font-medium text-gray-500 dark:text-dark-400">{{ t('usage.detail.endpoint') }}</span>
          <p class="mt-0.5 break-all text-gray-900 dark:text-dark-100">
            {{ detail.inbound_endpoint || '-' }}<template v-if="detail.upstream_endpoint"> → <span class="font-mono">{{ detail.upstream_endpoint }}</span></template>
          </p>
        </div>
        <div>
          <span class="font-medium text-gray-500 dark:text-dark-400">{{ t('usage.detail.stream') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">{{ detail.stream ? t('usage.detail.streamOn') : t('usage.detail.streamOff') }}</p>
        </div>
        <div>
          <span class="font-medium text-gray-500 dark:text-dark-400">{{ t('usage.detail.tokens') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">{{ formatTokens(totalTokens) }}</p>
        </div>
        <div>
          <span class="font-medium text-gray-500 dark:text-dark-400">{{ t('usage.detail.cost') }}</span>
          <p class="mt-0.5 text-gray-900 dark:text-dark-100">{{ formatCost(detail.total_cost) }}</p>
        </div>
        <div v-if="detail.request_id">
          <span class="font-medium text-gray-500 dark:text-dark-400">Request ID</span>
          <p class="mt-0.5 break-all font-mono text-xs text-gray-900 dark:text-dark-100">{{ detail.request_id }}</p>
        </div>
        <div v-if="detail.session_id">
          <span class="font-medium text-gray-500 dark:text-dark-400">Session ID</span>
          <p class="mt-0.5 break-all font-mono text-xs text-gray-900 dark:text-dark-100">{{ detail.session_id }}</p>
        </div>
      </div>

      <!-- Request body -->
      <div>
        <div class="flex items-center justify-between">
          <span class="font-medium text-gray-500 dark:text-dark-400">
            {{ t('usage.detail.requestBody') }}
            <span
              v-if="detail.request_body_truncated"
              class="ml-1 rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-500/20 dark:text-amber-400"
            >
              {{ t('usage.detail.truncated') }}
            </span>
          </span>
          <button
            v-if="requestBodyText"
            type="button"
            class="text-xs font-medium text-primary-600 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
            @click="copyBody(requestBodyText, 'request')"
          >
            {{ copiedSection === 'request' ? t('usage.detail.copied') : t('usage.detail.copy') }}
          </button>
        </div>
        <pre
          v-if="requestBodyText"
          class="mt-1 max-h-[40vh] overflow-auto whitespace-pre-wrap break-all rounded-lg border border-gray-200 bg-gray-50 p-3 text-xs text-gray-800 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-200"
        >{{ requestBodyText }}</pre>
        <p v-else class="mt-1 text-xs text-gray-400 dark:text-dark-500">{{ t('usage.detail.notCaptured') }}</p>
      </div>

      <!-- Response body -->
      <div>
        <div class="flex items-center justify-between">
          <span class="font-medium text-gray-500 dark:text-dark-400">
            {{ t('usage.detail.responseBody') }}
            <span
              v-if="detail.response_body_truncated"
              class="ml-1 rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-500/20 dark:text-amber-400"
            >
              {{ t('usage.detail.truncated') }}
            </span>
          </span>
          <button
            v-if="responseBodyText"
            type="button"
            class="text-xs font-medium text-primary-600 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
            @click="copyBody(responseBodyText, 'response')"
          >
            {{ copiedSection === 'response' ? t('usage.detail.copied') : t('usage.detail.copy') }}
          </button>
        </div>
        <pre
          v-if="responseBodyText"
          class="mt-1 max-h-[40vh] overflow-auto whitespace-pre-wrap break-all rounded-lg border border-gray-200 bg-gray-50 p-3 text-xs text-gray-800 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-200"
        >{{ responseBodyText }}</pre>
        <p v-else class="mt-1 text-xs text-gray-400 dark:text-dark-500">{{ t('usage.detail.notCaptured') }}</p>
      </div>

      <p class="text-xs text-gray-400 dark:text-dark-500">{{ t('usage.detail.captureHint') }}</p>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { adminUsageAPI } from '@/api/admin/usage'
import { formatDateTime } from '@/utils/format'
import { useClipboard } from '@/composables/useClipboard'
import type { AdminUsageLogDetail } from '@/types'

const props = defineProps<{
  show: boolean
  usageLogId: number | null
}>()

const emit = defineEmits<{
  (e: 'update:show', v: boolean): void
}>()

const { t } = useI18n()
const { copyToClipboard } = useClipboard()

const loading = ref(false)
const loadError = ref(false)
const detail = ref<AdminUsageLogDetail | null>(null)
const copiedSection = ref<'request' | 'response' | null>(null)

const totalTokens = computed(() => {
  const d = detail.value
  if (!d) return 0
  return (
    (d.input_tokens || 0) +
    (d.output_tokens || 0) +
    (d.cache_creation_tokens || 0) +
    (d.cache_read_tokens || 0)
  )
})

const requestBodyText = computed(() => prettyBody(detail.value?.request_body))
const responseBodyText = computed(() => prettyBody(detail.value?.response_body))

function prettyBody(raw?: string | null): string {
  if (!raw) return ''
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

function formatTokens(value: number): string {
  return value.toLocaleString()
}

function formatCost(value?: number | null): string {
  if (value == null) return '-'
  return `$${value.toFixed(6)}`
}

function copyBody(text: string, section: 'request' | 'response') {
  copyToClipboard(text)
  copiedSection.value = section
  window.setTimeout(() => {
    if (copiedSection.value === section) copiedSection.value = null
  }, 1500)
}

function close() {
  emit('update:show', false)
}

watch(
  () => [props.show, props.usageLogId] as const,
  ([show, id]) => {
    if (show && id != null) {
      fetchDetail(id)
    } else if (!show) {
      detail.value = null
      loadError.value = false
      copiedSection.value = null
    }
  }
)

async function fetchDetail(id: number) {
  loading.value = true
  loadError.value = false
  detail.value = null
  try {
    detail.value = await adminUsageAPI.getById(id)
  } catch (e) {
    console.error('[UsageDetailModal] Failed to load usage detail:', e)
    loadError.value = true
  } finally {
    loading.value = false
  }
}
</script>
