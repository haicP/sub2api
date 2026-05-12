import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export interface RequestDetailListParams {
  page?: number
  page_size?: number
  start_date?: string
  end_date?: string
  timezone?: string
  request_id?: string
  user_id?: number
  api_key_id?: number
  account_id?: number
  group_id?: number
  platform?: string
  model?: string
  endpoint?: string
  status_code?: number
  success?: boolean
  stream?: boolean
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

export interface RequestDetailSummary {
  id: number
  request_id: string
  created_at: string
  completed_at?: string
  duration_ms?: number
  status_code: number
  success: boolean
  platform: string
  endpoint: string
  upstream_endpoint: string
  model: string
  upstream_model: string
  stream: boolean
  user_id?: number
  api_key_id?: number
  account_id?: number
  group_id?: number
  subscription_id?: number
  ip_address: string
  user_agent: string
  request_body_bytes?: number
  response_body_bytes?: number
  error_message?: string
}

export interface RequestDetail extends RequestDetailSummary {
  request_headers?: Record<string, string[]>
  request_body?: string
  upstream_request_body?: string
  response_headers?: Record<string, string[]>
  response_content?: string
  response_body?: string
  response_truncated: boolean
}

export interface RequestDetailBackupRecord {
  id: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  backup_type: string
  file_name: string
  s3_key: string
  size_bytes: number
  triggered_by: string
  error_message?: string
  started_at: string
  finished_at?: string
}

export interface RequestDetailBackupSchedule {
  enabled: boolean
  cron_expr: string
  retain_days: number
  retain_count: number
}

export async function list(
  params: RequestDetailListParams,
  options?: { signal?: AbortSignal }
): Promise<PaginatedResponse<RequestDetailSummary>> {
  const { data } = await apiClient.get<PaginatedResponse<RequestDetailSummary>>('/admin/request-details', {
    params,
    signal: options?.signal
  })
  return data
}

export async function get(id: number): Promise<RequestDetail> {
  const { data } = await apiClient.get<RequestDetail>(`/admin/request-details/${id}`)
  return data
}

export async function exportExcel(params: RequestDetailListParams): Promise<Blob> {
  const { data } = await apiClient.get('/admin/request-details/export', {
    params,
    responseType: 'blob'
  })
  return data
}

export async function createBackup(): Promise<RequestDetailBackupRecord> {
  const { data } = await apiClient.post<RequestDetailBackupRecord>('/admin/request-details/backups', {})
  return data
}

export async function listBackups(): Promise<{ items: RequestDetailBackupRecord[] }> {
  const { data } = await apiClient.get<{ items: RequestDetailBackupRecord[] }>('/admin/request-details/backups')
  return data
}

export async function getBackup(id: string): Promise<RequestDetailBackupRecord> {
  const { data } = await apiClient.get<RequestDetailBackupRecord>(`/admin/request-details/backups/${id}`)
  return data
}

export async function getDownloadURL(id: string): Promise<{ url: string }> {
  const { data } = await apiClient.get<{ url: string }>(`/admin/request-details/backups/${id}/download-url`)
  return data
}

export async function getBackupSchedule(): Promise<RequestDetailBackupSchedule> {
  const { data } = await apiClient.get<RequestDetailBackupSchedule>('/admin/request-details/backup-schedule')
  return data
}

export async function updateBackupSchedule(config: RequestDetailBackupSchedule): Promise<RequestDetailBackupSchedule> {
  const { data } = await apiClient.put<RequestDetailBackupSchedule>('/admin/request-details/backup-schedule', config)
  return data
}

export const requestDetailsAPI = {
  list,
  get,
  exportExcel,
  createBackup,
  listBackups,
  getBackup,
  getDownloadURL,
  getBackupSchedule,
  updateBackupSchedule
}

export default requestDetailsAPI
