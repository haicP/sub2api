import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import RequestDetailsView from '../RequestDetailsView.vue'

const { requestDetailsAPIMock, listMock, listBackupsMock, getBackupScheduleMock, getMock } = vi.hoisted(() => {
  const requestDetailsAPIMock = {
    list: vi.fn(),
    listBackups: vi.fn(),
    getBackupSchedule: vi.fn(),
    get: vi.fn()
  }

  return {
    requestDetailsAPIMock,
    listMock: requestDetailsAPIMock.list,
    listBackupsMock: requestDetailsAPIMock.listBackups,
    getBackupScheduleMock: requestDetailsAPIMock.getBackupSchedule,
    getMock: requestDetailsAPIMock.get
  }
})

vi.mock('@/api/admin/requestDetails', () => ({
  default: requestDetailsAPIMock,
  requestDetailsAPI: requestDetailsAPIMock
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showSuccess: vi.fn(),
    showError: vi.fn()
  })
}))

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: { type: Boolean, default: false },
    title: { type: String, default: '' }
  },
  template: `
    <div v-if="show" data-testid="request-detail-dialog">
      <div>{{ title }}</div>
      <slot />
      <slot name="footer" />
    </div>
  `
})

describe('RequestDetailsView', () => {
  it('点击查看后使用独立弹窗展示请求详情', async () => {
    listMock.mockResolvedValueOnce({
      items: [
        {
          id: 1,
          request_id: 'req-1',
          created_at: '2026-05-12T13:00:00Z',
          status_code: 200,
          success: true,
          platform: 'openai',
          endpoint: '/v1/responses',
          upstream_endpoint: '/v1/responses',
          model: 'gpt-5.5',
          upstream_model: 'gpt-5.5',
          stream: true,
          request_body_bytes: 12,
          response_body_bytes: 24
        }
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    listBackupsMock.mockResolvedValueOnce({ items: [] })
    getBackupScheduleMock.mockResolvedValueOnce({
      enabled: false,
      cron_expr: '0 2 * * *',
      retain_days: 0,
      retain_count: 0
    })
    getMock.mockResolvedValueOnce({
      id: 1,
      request_id: 'req-1',
      created_at: '2026-05-12T13:00:00Z',
      status_code: 200,
      success: true,
      platform: 'openai',
      endpoint: '/v1/responses',
      upstream_endpoint: '/v1/responses',
      model: 'gpt-5.5',
      upstream_model: 'gpt-5.5',
      stream: true,
      request_body_bytes: 12,
      response_body_bytes: 24,
      response_truncated: false,
      request_body: '{"foo":"bar"}',
      response_content: '最终回复内容',
      response_body: '{"ok":true}'
    })

    const wrapper = mount(RequestDetailsView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Pagination: true,
          BaseDialog: BaseDialogStub
        }
      }
    })

    await flushPromises()
    await wrapper.get('button.btn-sm').trigger('click')
    await flushPromises()

    expect(getMock).toHaveBeenCalledWith(1)
    expect(wrapper.find('[data-testid="request-detail-dialog"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('请求详情明细')
    expect(wrapper.text()).toContain('req-1')
    expect(wrapper.text()).toContain('响应内容')
    expect(wrapper.text()).toContain('最终回复内容')
    expect(wrapper.text().indexOf('响应内容')).toBeLessThan(wrapper.text().indexOf('响应体'))
  })

  it('renders request details filters and backup section', async () => {
    listMock.mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    listBackupsMock.mockResolvedValueOnce({ items: [] })
    getBackupScheduleMock.mockResolvedValueOnce({ enabled: false, cron_expr: '0 2 * * *', retain_days: 0, retain_count: 0 })

    const wrapper = mount(RequestDetailsView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Pagination: true,
          BaseDialog: BaseDialogStub
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('请求详情')
    expect(wrapper.text()).toContain('导出 Excel')
    expect(wrapper.text()).toContain('S3 备份')
  })
})
