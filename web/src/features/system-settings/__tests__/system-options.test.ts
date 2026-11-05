import { afterEach, expect, it, vi } from 'vitest'

import { getOptionValue } from '../hooks/use-system-options'

afterEach(() => vi.restoreAllMocks())

it('loads object settings alongside existing scalar and array settings', () => {
  const settings = getOptionValue(
    [
      { key: 'proxy_setting.ttft_routing', value: '{"threshold_ms":4500}' },
      { key: 'enabled', value: 'true' },
      { key: 'limit', value: '8' },
      { key: 'groups', value: '["vip"]' },
      { key: 'raw', value: '{"keep":"string"}' },
    ],
    {
      'proxy_setting.ttft_routing': { threshold_ms: 3000 },
      enabled: false,
      limit: 5,
      groups: [] as string[],
      raw: '',
    }
  )

  expect(settings).toEqual({
    'proxy_setting.ttft_routing': { threshold_ms: 4500 },
    enabled: true,
    limit: 8,
    groups: ['vip'],
    raw: '{"keep":"string"}',
  })
})

it.each(['null', '[]', '{invalid'])(
  'keeps the object default when the saved value is %s',
  (value) => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const defaults = {
      'proxy_setting.ttft_routing': { threshold_ms: 3000 },
    }
    expect(
      getOptionValue([{ key: 'proxy_setting.ttft_routing', value }], defaults)
    ).toEqual(defaults)
  }
)
