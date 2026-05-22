import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import RequestDetailsView from '../RequestDetailsView.vue'

const { requestDetailsAPIMock, listMock, listBackupsMock, getBackupScheduleMock, getMock, getDownloadURLMock, deleteBackupMock, getArtifactDownloadURLMock, copyToClipboardMock } = vi.hoisted(() => {
  const requestDetailsAPIMock = {
    list: vi.fn(),
    listBackups: vi.fn(),
    getBackupSchedule: vi.fn(),
    get: vi.fn(),
    getDownloadURL: vi.fn(),
    deleteBackup: vi.fn(),
    getArtifactDownloadURL: vi.fn()
  }

  return {
    requestDetailsAPIMock,
    listMock: requestDetailsAPIMock.list,
    listBackupsMock: requestDetailsAPIMock.listBackups,
    getBackupScheduleMock: requestDetailsAPIMock.getBackupSchedule,
    getMock: requestDetailsAPIMock.get,
    getDownloadURLMock: requestDetailsAPIMock.getDownloadURL,
    deleteBackupMock: requestDetailsAPIMock.deleteBackup,
    getArtifactDownloadURLMock: requestDetailsAPIMock.getArtifactDownloadURL,
    copyToClipboardMock: vi.fn()
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

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: copyToClipboardMock
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

  it('运行中的备份不显示启动时间拼出的占位文件名', async () => {
    listMock.mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    listBackupsMock.mockResolvedValueOnce({
      items: [{
        id: 'backup-running',
        status: 'running',
        backup_type: 'request_details',
        file_name: '',
        s3_key: '',
        size_bytes: 0,
        triggered_by: 'manual',
        started_at: '2026-05-23T00:42:00Z',
        progress: 'exporting'
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

    expect(wrapper.text()).toContain('running / exporting')
    expect(wrapper.text()).toContain('生成中')
    expect(wrapper.text()).not.toContain('request_details_20260523_004200_0042_0042_part001.ndjson.gz')
  })

  it('多 part 备份下载先显示分片列表再按分片下载', async () => {
    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
    listMock.mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    listBackupsMock.mockResolvedValueOnce({
      items: [{
        id: 'backup-1',
        status: 'completed',
        backup_type: 'request_details',
        file_name: 'request_details_20260522_093000_part001.ndjson.gz',
        s3_key: 'backups/request-details/20260522/request_details_20260522_093000_part001.ndjson.gz',
        size_bytes: 2147483648,
        parts: [
          {
            index: 1,
            file_name: 'request_details_20260522_093000_part001.ndjson.gz',
            s3_key: 'backups/request-details/20260522/request_details_20260522_093000_part001.ndjson.gz',
            size_bytes: 1073741824
          },
          {
            index: 2,
            file_name: 'request_details_20260522_093000_part002.ndjson.gz',
            s3_key: 'backups/request-details/20260522/request_details_20260522_093000_part002.ndjson.gz',
            size_bytes: 1073741824
          }
        ],
        triggered_by: 'manual',
        started_at: '2026-05-22T09:30:00Z'
      }]
    })
    getBackupScheduleMock.mockResolvedValueOnce({ enabled: false, cron_expr: '0 2 * * *', retain_days: 0, retain_count: 0 })
    getDownloadURLMock.mockResolvedValueOnce({
      url: 'https://example.com/part001',
      urls: ['https://example.com/part001', 'https://example.com/part002'],
      parts: [
        { index: 1, file_name: 'part001.ndjson.gz', s3_key: 'hidden-s3-key-1', size_bytes: 1073741824, url: 'https://example.com/part001' },
        { index: 2, file_name: 'part002.ndjson.gz', s3_key: 'hidden-s3-key-2', size_bytes: 1073741824, url: 'https://example.com/part002' }
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
    expect(wrapper.text()).toContain('2 parts')
    expect(wrapper.text()).toContain('2.00 G')

    await wrapper.findAll('button').find((button) => button.text() === '下载')?.trigger('click')
    await flushPromises()

    expect(getDownloadURLMock).toHaveBeenCalledWith('backup-1')
    expect(openSpy).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('下载备份分片')
    expect(wrapper.text()).toContain('part001.ndjson.gz')
    expect(wrapper.text()).toContain('part002.ndjson.gz')
    expect(wrapper.text()).not.toContain('S3 Key')
    expect(wrapper.text()).not.toContain('hidden-s3-key-1')
    expect(wrapper.text()).not.toContain('hidden-s3-key-2')

    const dialogDownloadButtons = wrapper.findAll('button').filter((button) => button.text() === '下载')
    await dialogDownloadButtons.at(-2)?.trigger('click')
    await dialogDownloadButtons.at(-1)?.trigger('click')
    await flushPromises()

    expect(openSpy).toHaveBeenNthCalledWith(1, 'https://example.com/part001', '_blank', 'noopener,noreferrer')
    expect(openSpy).toHaveBeenNthCalledWith(2, 'https://example.com/part002', '_blank', 'noopener,noreferrer')
    openSpy.mockRestore()
  })

  it('单 part 备份下载直接打开链接', async () => {
    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
    listMock.mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    listBackupsMock.mockResolvedValueOnce({
      items: [{
        id: 'backup-1',
        status: 'completed',
        backup_type: 'request_details',
        file_name: 'request_details_20260522_093000_0930_0930_part001.ndjson.gz',
        s3_key: 'backups/request-details/20260522/request_details_20260522_093000_0930_0930_part001.ndjson.gz',
        size_bytes: 1024,
        triggered_by: 'manual',
        started_at: '2026-05-22T09:30:00Z'
      }]
    })
    getBackupScheduleMock.mockResolvedValueOnce({ enabled: false, cron_expr: '0 2 * * *', retain_days: 0, retain_count: 0 })
    getDownloadURLMock.mockResolvedValueOnce({
      url: 'https://example.com/part001',
      urls: ['https://example.com/part001'],
      parts: [
        { index: 1, file_name: 'part001.ndjson.gz', s3_key: 'part001', size_bytes: 1024, url: 'https://example.com/part001' }
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
    await wrapper.findAll('button').find((button) => button.text() === '下载')?.trigger('click')
    await flushPromises()

    expect(getDownloadURLMock).toHaveBeenCalledWith('backup-1')
    expect(openSpy).toHaveBeenCalledWith('https://example.com/part001', '_blank', 'noopener,noreferrer')
    expect(wrapper.text()).not.toContain('下载备份分片')
    openSpy.mockRestore()
  })

  it('删除备份会调用删除接口并刷新列表', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    listMock.mockResolvedValueOnce({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    listBackupsMock
      .mockResolvedValueOnce({
        items: [{
          id: 'backup-1',
          status: 'completed',
          backup_type: 'request_details',
          file_name: 'request_details_20260522_093000_0930_0930_part001.ndjson.gz',
          s3_key: 'backups/request-details/20260522/request_details_20260522_093000_0930_0930_part001.ndjson.gz',
          size_bytes: 1024,
          triggered_by: 'manual',
          started_at: '2026-05-22T09:30:00Z'
        }]
      })
      .mockResolvedValueOnce({ items: [] })
    getBackupScheduleMock
      .mockResolvedValueOnce({ enabled: false, cron_expr: '0 2 * * *', retain_days: 0, retain_count: 0 })
      .mockResolvedValueOnce({ enabled: false, cron_expr: '0 2 * * *', retain_days: 0, retain_count: 0 })
    deleteBackupMock.mockResolvedValueOnce(undefined)

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
    await wrapper.findAll('button').find((button) => button.text() === '删除')?.trigger('click')
    await flushPromises()

    expect(confirmSpy).toHaveBeenCalled()
    expect(deleteBackupMock).toHaveBeenCalledWith('backup-1')
    expect(listBackupsMock).toHaveBeenCalledTimes(2)
    confirmSpy.mockRestore()
  })

  it('大正文只渲染预览但复制完整内容', async () => {
    const largeBody = `{"payload":"${'x'.repeat(70 * 1024)}"}`
    listMock.mockResolvedValueOnce({
      items: [{
        id: 1,
        request_id: 'req-large',
        created_at: '2026-05-12T13:00:00Z',
        status_code: 200,
        success: true,
        platform: 'openai',
        endpoint: '/v1/responses',
        upstream_endpoint: '/v1/responses',
        model: 'gpt-5.5',
        upstream_model: 'gpt-5.5',
        stream: true,
        request_body_bytes: largeBody.length,
        response_body_bytes: 2
      }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    listBackupsMock.mockResolvedValueOnce({ items: [] })
    getBackupScheduleMock.mockResolvedValueOnce({ enabled: false, cron_expr: '0 2 * * *', retain_days: 0, retain_count: 0 })
    getMock.mockResolvedValueOnce({
      id: 1,
      request_id: 'req-large',
      created_at: '2026-05-12T13:00:00Z',
      status_code: 200,
      success: true,
      platform: 'openai',
      endpoint: '/v1/responses',
      upstream_endpoint: '/v1/responses',
      model: 'gpt-5.5',
      upstream_model: 'gpt-5.5',
      stream: true,
      request_body_bytes: largeBody.length,
      response_body_bytes: 2,
      response_truncated: false,
      request_body: largeBody,
      response_body: '{}'
    })
    copyToClipboardMock.mockResolvedValueOnce(true)

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

    expect(wrapper.text()).toContain('仅预览前 64.00 KB')
    expect(wrapper.text()).not.toContain('x'.repeat(70 * 1024))

    const copyButtons = wrapper.findAll('button').filter((button) => button.text() === '复制')
    await copyButtons[2].trigger('click')
    await flushPromises()

    expect(copyToClipboardMock).toHaveBeenCalledWith(largeBody, '已复制')
  })
})
