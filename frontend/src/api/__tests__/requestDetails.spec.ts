import { describe, expect, it, vi } from 'vitest'
import { requestDetailsAPI } from '@/api/admin/requestDetails'
import { apiClient } from '@/api/client'

vi.mock('@/api/client', () => ({
  apiClient: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn()
  }
}))

describe('requestDetailsAPI', () => {
  it('passes list filters to admin request details endpoint', async () => {
    vi.mocked(apiClient.get).mockResolvedValueOnce({
      data: { items: [], total: 0, page: 1, page_size: 20, pages: 1 }
    })

    await requestDetailsAPI.list({ page: 1, page_size: 20, platform: 'openai', stream: true })

    expect(apiClient.get).toHaveBeenCalledWith('/admin/request-details', {
      params: { page: 1, page_size: 20, platform: 'openai', stream: true },
      signal: undefined
    })
  })
})
