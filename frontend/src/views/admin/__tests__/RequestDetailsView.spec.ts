import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import RequestDetailsView from '../RequestDetailsView.vue'

vi.mock('@/api/admin/requestDetails', () => ({
  requestDetailsAPI: {
    list: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 }),
    listBackups: vi.fn().mockResolvedValue({ items: [] }),
    getBackupSchedule: vi.fn().mockResolvedValue({ enabled: false, cron_expr: '0 2 * * *', retain_days: 0, retain_count: 0 })
  }
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showSuccess: vi.fn(),
    showError: vi.fn()
  })
}))

describe('RequestDetailsView', () => {
  it('renders request details filters and backup section', async () => {
    const wrapper = mount(RequestDetailsView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Pagination: true
        }
      }
    })

    expect(wrapper.text()).toContain('请求详情')
    expect(wrapper.text()).toContain('导出 Excel')
    expect(wrapper.text()).toContain('S3 备份')
  })
})
