<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">请求详情</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">查看大模型调用的完整输入输出参数</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button class="btn btn-secondary" :disabled="loading" @click="handleExport">导出 Excel</button>
          <button class="btn btn-primary" :disabled="backupRunning" @click="handleCreateBackup">备份请求详情</button>
        </div>
      </div>

      <section class="card p-4">
        <div class="grid gap-3 md:grid-cols-4">
          <input v-model="filters.request_id" class="input" placeholder="Request ID" />
          <input v-model="filters.user_id" class="input" placeholder="用户 ID" />
          <input v-model="filters.api_key_id" class="input" placeholder="API Key ID" />
          <input v-model="filters.account_id" class="input" placeholder="账号 ID" />
          <input v-model="filters.group_id" class="input" placeholder="分组 ID" />
          <input v-model="filters.model" class="input" placeholder="模型" />
          <input v-model="filters.endpoint" class="input" placeholder="Endpoint" />
          <input v-model="filters.status_code" class="input" placeholder="状态码" />
          <select v-model="filters.platform" class="input">
            <option value="">全部平台</option>
            <option value="anthropic">Anthropic</option>
            <option value="openai">OpenAI</option>
            <option value="gemini">Gemini</option>
            <option value="antigravity">Antigravity</option>
          </select>
          <select v-model="filters.success" class="input">
            <option value="">全部状态</option>
            <option value="true">成功</option>
            <option value="false">失败</option>
          </select>
          <select v-model="filters.stream" class="input">
            <option value="">全部模式</option>
            <option value="true">流式</option>
            <option value="false">非流式</option>
          </select>
        </div>
        <div class="mt-4 flex gap-2">
          <button class="btn btn-primary" @click="loadData">查询</button>
          <button class="btn btn-secondary" @click="resetFilters">重置</button>
        </div>
      </section>

      <section class="card p-4">
        <div class="overflow-x-auto">
          <table class="w-full min-w-[1100px] text-sm">
            <thead>
              <tr class="border-b border-gray-200 text-left text-xs uppercase tracking-wide text-gray-500 dark:border-dark-700 dark:text-gray-400">
                <th class="py-2 pr-4">请求时间</th>
                <th class="py-2 pr-4">Request ID</th>
                <th class="py-2 pr-4">平台</th>
                <th class="py-2 pr-4">模型</th>
                <th class="py-2 pr-4">状态码</th>
                <th class="py-2 pr-4">耗时</th>
                <th class="py-2 pr-4">流式</th>
                <th class="py-2 pr-4">用户</th>
                <th class="py-2 pr-4">API Key</th>
                <th class="py-2 pr-4">正文大小</th>
                <th class="py-2">详情</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in rows" :key="row.id" class="border-b border-gray-100 align-top dark:border-dark-800">
                <td class="py-3 pr-4 text-xs">{{ formatDate(row.created_at) }}</td>
                <td class="py-3 pr-4 font-mono text-xs">{{ row.request_id }}</td>
                <td class="py-3 pr-4 text-xs">{{ row.platform }}</td>
                <td class="py-3 pr-4 text-xs">{{ row.model }}</td>
                <td class="py-3 pr-4 text-xs">{{ row.status_code }}</td>
                <td class="py-3 pr-4 text-xs">{{ row.duration_ms ?? '-' }}</td>
                <td class="py-3 pr-4 text-xs">{{ row.stream ? '是' : '否' }}</td>
                <td class="py-3 pr-4 text-xs">{{ row.user_id ?? '-' }}</td>
                <td class="py-3 pr-4 text-xs">{{ row.api_key_id ?? '-' }}</td>
                <td class="py-3 pr-4 text-xs">{{ formatBytes(row.request_body_bytes) }} / {{ formatBytes(row.response_body_bytes) }}</td>
                <td class="py-3">
                  <button class="btn btn-secondary btn-sm" @click="openDetail(row.id)">查看</button>
                </td>
              </tr>
              <tr v-if="!rows.length">
                <td colspan="11" class="py-6 text-center text-sm text-gray-500 dark:text-gray-400">暂无请求详情</td>
              </tr>
            </tbody>
          </table>
        </div>
        <Pagination
          v-if="total > 0"
          :page="page"
          :total="total"
          :page-size="pageSize"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </section>

      <section class="card p-4">
        <div class="mb-4">
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">S3 备份</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">独立配置请求详情定时备份，复用系统备份的 S3 连接。</p>
        </div>
        <div class="grid gap-3 md:grid-cols-4">
          <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input v-model="schedule.enabled" type="checkbox" />
            <span>启用定时备份</span>
          </label>
          <input v-model="schedule.cron_expr" class="input" placeholder="0 2 * * *" />
          <input v-model.number="schedule.retain_days" type="number" min="0" class="input" placeholder="保留天数" />
          <input v-model.number="schedule.retain_count" type="number" min="0" class="input" placeholder="保留份数" />
        </div>
        <div class="mt-4 flex gap-2">
          <button class="btn btn-primary" @click="saveSchedule">保存定时配置</button>
          <button class="btn btn-secondary" @click="loadBackups">刷新备份记录</button>
        </div>
        <div class="mt-4 overflow-x-auto">
          <table class="w-full min-w-[900px] text-sm">
            <thead>
              <tr class="border-b border-gray-200 text-left text-xs uppercase tracking-wide text-gray-500 dark:border-dark-700 dark:text-gray-400">
                <th class="py-2 pr-4">ID</th>
                <th class="py-2 pr-4">状态</th>
                <th class="py-2 pr-4">文件</th>
                <th class="py-2 pr-4">大小</th>
                <th class="py-2 pr-4">触发方式</th>
                <th class="py-2 pr-4">开始时间</th>
                <th class="py-2">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="backup in backups" :key="backup.id" class="border-b border-gray-100 dark:border-dark-800">
                <td class="py-3 pr-4 font-mono text-xs">{{ backup.id }}</td>
                <td class="py-3 pr-4 text-xs">{{ backup.status }}</td>
                <td class="py-3 pr-4 text-xs">{{ backup.file_name }}</td>
                <td class="py-3 pr-4 text-xs">{{ formatBytes(backup.size_bytes) }}</td>
                <td class="py-3 pr-4 text-xs">{{ backup.triggered_by }}</td>
                <td class="py-3 pr-4 text-xs">{{ formatDate(backup.started_at) }}</td>
                <td class="py-3">
                  <button class="btn btn-secondary btn-sm" @click="downloadBackup(backup.id)">下载</button>
                </td>
              </tr>
              <tr v-if="!backups.length">
                <td colspan="7" class="py-4 text-center text-sm text-gray-500 dark:text-gray-400">暂无备份记录</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </AppLayout>

  <BaseDialog
    :show="detailDialogOpen"
    title="请求详情明细"
    width="extra-wide"
    :close-on-click-outside="true"
    @close="closeDetailDialog"
  >
    <div v-if="detailLoading" class="flex min-h-[240px] items-center justify-center text-sm text-gray-500 dark:text-gray-400">
      <div class="flex items-center gap-3">
        <span class="spinner"></span>
        <span>正在加载请求详情...</span>
      </div>
    </div>

    <div v-else-if="selectedDetail" class="space-y-4">
      <div>
        <p class="font-mono text-xs text-gray-500 dark:text-gray-400">{{ selectedDetail.request_id }}</p>
      </div>
      <div class="grid gap-4 lg:grid-cols-2">
        <div class="space-y-2 text-sm text-gray-700 dark:text-gray-300">
          <div><span class="font-medium">平台：</span>{{ selectedDetail.platform }}</div>
          <div><span class="font-medium">模型：</span>{{ selectedDetail.model }}</div>
          <div><span class="font-medium">状态码：</span>{{ selectedDetail.status_code }}</div>
          <div><span class="font-medium">耗时：</span>{{ selectedDetail.duration_ms ?? '-' }}</div>
        </div>
        <div class="space-y-2 text-sm text-gray-700 dark:text-gray-300">
          <div><span class="font-medium">用户：</span>{{ selectedDetail.user_id ?? '-' }}</div>
          <div><span class="font-medium">API Key：</span>{{ selectedDetail.api_key_id ?? '-' }}</div>
          <div><span class="font-medium">账号：</span>{{ selectedDetail.account_id ?? '-' }}</div>
          <div><span class="font-medium">Endpoint：</span>{{ selectedDetail.endpoint }}</div>
        </div>
      </div>
      <div class="grid gap-4 lg:grid-cols-2">
        <div>
          <div class="mb-2 flex items-center justify-between">
            <h3 class="text-sm font-medium text-gray-900 dark:text-white">请求头</h3>
            <button class="btn btn-secondary btn-sm" @click="copyText(formatJSON(selectedDetail.request_headers))">复制</button>
          </div>
          <pre class="max-h-24 overflow-auto rounded bg-gray-50 p-3 text-xs dark:bg-dark-800">{{ formatJSON(selectedDetail.request_headers) }}</pre>
        </div>
        <div>
          <div class="mb-2 flex items-center justify-between">
            <h3 class="text-sm font-medium text-gray-900 dark:text-white">响应头</h3>
            <button class="btn btn-secondary btn-sm" @click="copyText(formatJSON(selectedDetail.response_headers))">复制</button>
          </div>
          <pre class="max-h-24 overflow-auto rounded bg-gray-50 p-3 text-xs dark:bg-dark-800">{{ formatJSON(selectedDetail.response_headers) }}</pre>
        </div>
        <div>
          <div class="mb-2 flex items-center justify-between">
            <h3 class="text-sm font-medium text-gray-900 dark:text-white">入站请求体</h3>
            <button class="btn btn-secondary btn-sm" @click="copyText(selectedDetail.request_body || '')">复制</button>
          </div>
          <pre class="max-h-96 overflow-auto rounded bg-gray-50 p-3 text-xs dark:bg-dark-800">{{ selectedDetail.request_body }}</pre>
        </div>
        <div>
          <div class="mb-2 flex items-center justify-between">
            <h3 class="text-sm font-medium text-gray-900 dark:text-white">上游请求体</h3>
            <button class="btn btn-secondary btn-sm" @click="copyText(selectedDetail.upstream_request_body || '')">复制</button>
          </div>
          <pre class="max-h-96 overflow-auto rounded bg-gray-50 p-3 text-xs dark:bg-dark-800">{{ selectedDetail.upstream_request_body }}</pre>
        </div>
        <div class="lg:col-span-2">
          <div class="mb-2 flex items-center justify-between">
            <h3 class="text-sm font-medium text-gray-900 dark:text-white">响应内容</h3>
            <button class="btn btn-secondary btn-sm" @click="copyText(selectedDetail.response_content || '')">复制</button>
          </div>
          <pre class="max-h-96 overflow-auto rounded bg-gray-50 p-3 text-xs dark:bg-dark-800">{{ selectedDetail.response_content || '-' }}</pre>
        </div>
        <div class="lg:col-span-2">
          <div class="mb-2 flex items-center justify-between">
            <h3 class="text-sm font-medium text-gray-900 dark:text-white">响应体</h3>
            <button class="btn btn-secondary btn-sm" @click="copyText(selectedDetail.response_body || '')">复制</button>
          </div>
          <pre class="max-h-96 overflow-auto rounded bg-gray-50 p-3 text-xs dark:bg-dark-800">{{ selectedDetail.response_body }}</pre>
        </div>
      </div>
    </div>

    <template #footer>
      <button class="btn btn-secondary" @click="closeDetailDialog">关闭</button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { saveAs } from 'file-saver'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import { requestDetailsAPI, type RequestDetail, type RequestDetailBackupRecord, type RequestDetailBackupSchedule, type RequestDetailListParams, type RequestDetailSummary } from '@/api/admin/requestDetails'
import { useAppStore } from '@/stores'

const appStore = useAppStore()
const loading = ref(false)
const backupRunning = ref(false)
const rows = ref<RequestDetailSummary[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const selectedDetail = ref<RequestDetail | null>(null)
const detailDialogOpen = ref(false)
const detailLoading = ref(false)
const backups = ref<RequestDetailBackupRecord[]>([])
const schedule = reactive<RequestDetailBackupSchedule>({
  enabled: false,
  cron_expr: '0 2 * * *',
  retain_days: 0,
  retain_count: 0
})
const filters = reactive({
  request_id: '',
  user_id: '',
  api_key_id: '',
  account_id: '',
  group_id: '',
  platform: '',
  model: '',
  endpoint: '',
  status_code: '',
  success: '',
  stream: ''
})

const buildQueryParams = (): RequestDetailListParams => ({
  page: page.value,
  page_size: pageSize.value,
  request_id: filters.request_id || undefined,
  user_id: filters.user_id ? Number(filters.user_id) : undefined,
  api_key_id: filters.api_key_id ? Number(filters.api_key_id) : undefined,
  account_id: filters.account_id ? Number(filters.account_id) : undefined,
  group_id: filters.group_id ? Number(filters.group_id) : undefined,
  platform: filters.platform || undefined,
  model: filters.model || undefined,
  endpoint: filters.endpoint || undefined,
  status_code: filters.status_code ? Number(filters.status_code) : undefined,
  success: filters.success === '' ? undefined : filters.success === 'true',
  stream: filters.stream === '' ? undefined : filters.stream === 'true',
  sort_by: 'created_at',
  sort_order: 'desc'
})

const loadData = async () => {
  loading.value = true
  try {
    const result = await requestDetailsAPI.list(buildQueryParams())
    rows.value = result.items
    total.value = result.total
  } catch (error) {
    appStore.showError((error as Error).message || '加载请求详情失败')
  } finally {
    loading.value = false
  }
}

const loadBackups = async () => {
  try {
    const [backupResult, scheduleResult] = await Promise.all([
      requestDetailsAPI.listBackups(),
      requestDetailsAPI.getBackupSchedule()
    ])
    backups.value = backupResult.items
    schedule.enabled = scheduleResult.enabled
    schedule.cron_expr = scheduleResult.cron_expr
    schedule.retain_days = scheduleResult.retain_days
    schedule.retain_count = scheduleResult.retain_count
  } catch (error) {
    appStore.showError((error as Error).message || '加载备份配置失败')
  }
}

const resetFilters = () => {
  filters.request_id = ''
  filters.user_id = ''
  filters.api_key_id = ''
  filters.account_id = ''
  filters.group_id = ''
  filters.platform = ''
  filters.model = ''
  filters.endpoint = ''
  filters.status_code = ''
  filters.success = ''
  filters.stream = ''
  page.value = 1
  void loadData()
}

const openDetail = async (id: number) => {
  detailDialogOpen.value = true
  detailLoading.value = true
  selectedDetail.value = null
  try {
    selectedDetail.value = await requestDetailsAPI.get(id)
  } catch (error) {
    detailDialogOpen.value = false
    appStore.showError((error as Error).message || '加载请求详情失败')
  } finally {
    detailLoading.value = false
  }
}

const closeDetailDialog = () => {
  detailDialogOpen.value = false
  detailLoading.value = false
  selectedDetail.value = null
}

const handleExport = async () => {
  try {
    const blob = await requestDetailsAPI.exportExcel(buildQueryParams())
    saveAs(blob, `request_details_${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '')}.xlsx`)
    appStore.showSuccess('导出成功')
  } catch (error) {
    appStore.showError((error as Error).message || '导出失败')
  }
}

const handleCreateBackup = async () => {
  backupRunning.value = true
  try {
    await requestDetailsAPI.createBackup()
    appStore.showSuccess('备份任务已创建')
    await loadBackups()
  } catch (error) {
    appStore.showError((error as Error).message || '备份失败')
  } finally {
    backupRunning.value = false
  }
}

const saveSchedule = async () => {
  try {
    await requestDetailsAPI.updateBackupSchedule({ ...schedule })
    appStore.showSuccess('定时备份配置已保存')
  } catch (error) {
    appStore.showError((error as Error).message || '保存定时配置失败')
  }
}

const downloadBackup = async (id: string) => {
  try {
    const result = await requestDetailsAPI.getDownloadURL(id)
    window.open(result.url, '_blank', 'noopener,noreferrer')
  } catch (error) {
    appStore.showError((error as Error).message || '获取下载链接失败')
  }
}

const copyText = async (value: string) => {
  try {
    await navigator.clipboard.writeText(value)
    appStore.showSuccess('已复制')
  } catch {
    appStore.showError('复制失败')
  }
}

const handlePageChange = (value: number) => {
  page.value = value
  void loadData()
}

const handlePageSizeChange = (value: number) => {
  pageSize.value = value
  page.value = 1
  void loadData()
}

const formatDate = (value?: string) => value ? new Date(value).toLocaleString() : '-'
const formatBytes = (value?: number) => typeof value === 'number' ? `${value} B` : '-'
const formatJSON = (value: unknown) => value ? JSON.stringify(value, null, 2) : ''

onMounted(async () => {
  await Promise.all([loadData(), loadBackups()])
})
</script>
