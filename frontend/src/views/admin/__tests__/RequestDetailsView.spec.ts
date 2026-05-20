import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import RequestDetailsView from '../RequestDetailsView.vue'

const { requestDetailsAPIMock, listMock, listBackupsMock, getBackupScheduleMock, getMock, getArtifactDownloadURLMock } = vi.hoisted(() => {
  const requestDetailsAPIMock = {
    list: vi.fn(),
    listBackups: vi.fn(),
    getBackupSchedule: vi.fn(),
    get: vi.fn(),
    getArtifactDownloadURL: vi.fn()
  }

  return {
    requestDetailsAPIMock,
    listMock: requestDetailsAPIMock.list,
    listBackupsMock: requestDetailsAPIMock.listBackups,
    getBackupScheduleMock: requestDetailsAPIMock.getBackupSchedule,
    getMock: requestDetailsAPIMock.get,
    getArtifactDownloadURLMock: requestDetailsAPIMock.getArtifactDownloadURL
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

beforeEach(() => {
  vi.clearAllMocks()
})

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
          completed_at: '2026-05-12T13:00:02Z',
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
      response_body: '{"ok":true}',
      image_artifacts: [
        {
          id: 9,
          request_id: 'req-1',
          direction: 'response',
          source: '$.data.0.b64_json',
          status: 'stored',
          s3_key: 'backups/request-detail-images/2026/05/12/req-1/artifact-1.png',
          content_type: 'image/png',
          size_bytes: 123,
          created_at: '2026-05-12T13:00:01Z',
          updated_at: '2026-05-12T13:00:01Z'
        }
      ]
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

    expect(listMock).toHaveBeenCalledWith(
      expect.objectContaining({
        sort_by: 'completed_at',
        sort_order: 'desc'
      })
    )
    await wrapper.get('button.btn-sm').trigger('click')
    await flushPromises()

    expect(getMock).toHaveBeenCalledWith(1)
    expect(wrapper.find('[data-testid="request-detail-dialog"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('请求详情明细')
    expect(wrapper.text()).toContain('req-1')
    expect(wrapper.text()).toContain('响应内容')
    expect(wrapper.text()).toContain('最终回复内容')
    expect(wrapper.text()).toContain('图片附件')
    expect(wrapper.text()).toContain('$.data.0.b64_json')
    expect(wrapper.text()).toContain('123 B')
    expect(wrapper.text().indexOf('响应内容')).toBeLessThan(wrapper.text().indexOf('响应体'))
  })

  it('点击图片附件预览时按需获取预签名链接', async () => {
    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
    listMock.mockResolvedValueOnce({
      items: [{ id: 1, request_id: 'req-1', created_at: '2026-05-12T13:00:00Z', status_code: 200, success: true, platform: 'openai', endpoint: '/v1/images/generations', upstream_endpoint: '/v1/images/generations', model: 'gpt-image-2', upstream_model: 'gpt-image-2', stream: false }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    listBackupsMock.mockResolvedValueOnce({ items: [] })
    getBackupScheduleMock.mockResolvedValueOnce({ enabled: false, cron_expr: '0 2 * * *', retain_days: 0, retain_count: 0 })
    getMock.mockResolvedValueOnce({
      id: 1,
      request_id: 'req-1',
      created_at: '2026-05-12T13:00:00Z',
      status_code: 200,
      success: true,
      platform: 'openai',
      endpoint: '/v1/images/generations',
      upstream_endpoint: '/v1/images/generations',
      model: 'gpt-image-2',
      upstream_model: 'gpt-image-2',
      stream: false,
      response_truncated: false,
      image_artifacts: [{ id: 9, request_id: 'req-1', direction: 'response', source: '$.data.0.b64_json', status: 'stored', s3_key: 'key', content_type: 'image/png', size_bytes: 123, created_at: '2026-05-12T13:00:01Z', updated_at: '2026-05-12T13:00:01Z' }]
    })
    getArtifactDownloadURLMock.mockResolvedValueOnce({ url: 'https://example.com/presigned' })

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
    await wrapper.findAll('button').find((button) => button.text() === '预览')?.trigger('click')
    await flushPromises()

    expect(getArtifactDownloadURLMock).toHaveBeenCalledWith(1, 9)
    expect(openSpy).toHaveBeenCalledWith('https://example.com/presigned', '_blank', 'noopener,noreferrer')
    openSpy.mockRestore()
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
    expect(wrapper.find('input[placeholder="API Key"]').exists()).toBe(true)
    expect(wrapper.find('input[placeholder="用户"]').exists()).toBe(true)
    expect(wrapper.find('input[placeholder="模型"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('全部平台类型')
    expect(wrapper.find('input[placeholder="Request ID"]').exists()).toBe(false)
    expect(wrapper.find('input[placeholder="账号 ID"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('导出 Excel')
    expect(wrapper.text()).toContain('S3 备份')
  })

  it('展开高级筛选并使用 API Key 和用户文本搜索', async () => {
    listMock
      .mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
      .mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
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

    expect(wrapper.find('input[placeholder="Request ID"]').exists()).toBe(false)
    await wrapper.findAll('button').find((button) => button.text() === '更多筛选')?.trigger('click')
    await flushPromises()
    expect(wrapper.find('input[placeholder="Request ID"]').exists()).toBe(true)
    expect(wrapper.find('input[placeholder="账号 ID"]').exists()).toBe(true)
    expect(wrapper.find('input[placeholder="Endpoint"]').exists()).toBe(true)

    await wrapper.get('input[placeholder="API Key"]').setValue('prod-key')
    await wrapper.get('input[placeholder="用户"]').setValue('admin@example.com')
    await wrapper.get('input[placeholder="模型"]').setValue('gpt-5.5')
    await wrapper.get('select').setValue('openai')
    await wrapper.findAll('button').find((button) => button.text() === '查询')?.trigger('click')
    await flushPromises()

    const params = listMock.mock.calls.at(-1)?.[0]
    expect(params).toEqual(expect.objectContaining({
      api_key: 'prod-key',
      user: 'admin@example.com',
      model: 'gpt-5.5',
      platform: 'openai'
    }))
    expect(params).not.toHaveProperty('api_key_id')
    expect(params).not.toHaveProperty('user_id')
  })

  it('列表和备份大小按 B、KB、M 分档显示', async () => {
    listMock.mockResolvedValueOnce({
      items: [{
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
        request_body_bytes: 1048576,
        response_body_bytes: 2097152
      }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    listBackupsMock.mockResolvedValueOnce({
      items: [{
        id: 'backup-1',
        status: 'completed',
        backup_type: 'request_details',
        file_name: 'request_details.ndjson.gz',
        s3_key: 'request_details.ndjson.gz',
        size_bytes: 3145728,
        triggered_by: 'manual',
        started_at: '2026-05-12T13:00:00Z'
      }]
    })
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

    expect(wrapper.text()).toContain('1024.00 KB / 2.00 M')
    expect(wrapper.text()).toContain('3.00 M')
  })
})
