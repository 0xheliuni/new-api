/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { toast } from 'sonner'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { createAppQueryClient } from '@/lib/query-client'

import { channelsQueryKeys } from '../../lib'
import { channelSchema, type Channel } from '../../types'
import { useChannelsColumns } from '../channels-columns'
import { ChannelsProvider } from '../channels-provider'

function ExampleAutoDisabledStatusCell(props: { channel: Channel }) {
  const table = useReactTable({
    data: [props.channel],
    columns: useChannelsColumns({ enableSelection: false }),
    getCoreRowModel: getCoreRowModel(),
  })
  const cell = table
    .getRowModel()
    .rows[0]?.getAllCells()
    .find((item) => item.column.id === 'status')

  return cell ? flexRender(cell.column.columnDef.cell, cell.getContext()) : null
}

test('keeps the long string inside the status tooltip is wrapped when show', async () => {
  const reason = '114514'.repeat(40)
  const channelItem = channelSchema.parse({
    id: 1,
    type: 1,
    key: 'test-key',
    name: 'Test-Channel-Item',
    status: 3,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    other_info: JSON.stringify({ status_reason: reason }),
  })
  const queryClient = new QueryClient()
  const user = userEvent.setup()

  render(
    <QueryClientProvider client={queryClient}>
      <ChannelsProvider>
        <ExampleAutoDisabledStatusCell channel={channelItem} />
      </ChannelsProvider>
    </QueryClientProvider>
  )

  await user.hover(screen.getByText('Auto Disabled'))

  expect(await screen.findByText(reason, { exact: false })).toHaveClass(
    'wrap-anywhere'
  )
})

afterEach(() => {
  vi.restoreAllMocks()
})

function enabledChannel() {
  return channelSchema.parse({
    id: 1,
    type: 1,
    key: 'test-key',
    name: 'Enabled-Channel',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
}

function mockCooldowns(items: unknown[]) {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: { enabled: true, memory_cache_enabled: true, items },
    },
  })
}

// Interpolates like the app does (escapeValue off), so "1/2" stays readable.
async function renderStatusCell(
  channel: Channel,
  queryClient: QueryClient = new QueryClient()
) {
  const i18n = createInstance()
  await i18n.init({
    lng: 'en',
    fallbackLng: 'en',
    resources: { en: { translation: {} } },
    interpolation: { escapeValue: false },
  })
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <ChannelsProvider>
          <ExampleAutoDisabledStatusCell channel={channel} />
        </ChannelsProvider>
      </QueryClientProvider>
    </I18nextProvider>
  )
}

test('an enabled channel in a cooldown shows the cooldown details on hover and keyboard focus', async () => {
  mockCooldowns([
    {
      channel_id: 1,
      state: 'cooling',
      cooldown_until: Math.floor(Date.now() / 1000) + 120,
      cooldown_count: 2,
      weight_factor: 0.25,
      last_status_code: 429,
      last_error_code: 'rate_limit_exceeded',
    },
  ])
  await renderStatusCell(enabledChannel())
  const user = userEvent.setup()
  const trigger = await screen.findByRole('button', { name: 'Cooling down' })
  await user.hover(trigger)
  expect(await screen.findByText('Cooldowns in a row: 2')).toBeVisible()
  expect(screen.getByText('Last status code: 429')).toBeVisible()

  await user.unhover(trigger)
  await waitFor(() =>
    expect(screen.queryByText('Cooldowns in a row: 2')).not.toBeInTheDocument()
  )
  await user.tab()
  expect(trigger).toHaveFocus()
  expect(await screen.findByText('Cooldowns in a row: 2')).toBeVisible()
  expect(screen.getByText('Last status code: 429')).toBeVisible()
})

test('a recovering channel shows its weight share', async () => {
  mockCooldowns([
    {
      channel_id: 1,
      state: 'recovering',
      cooldown_until: 0,
      cooldown_count: 0,
      weight_factor: 0.5,
      last_status_code: 429,
      last_error_code: 'rate_limit_exceeded',
    },
  ])
  await renderStatusCell(enabledChannel())
  expect(await screen.findByText('Recovering 1/2')).toBeVisible()
})

test('a healthy channel shows no cooldown badge', async () => {
  mockCooldowns([])
  await renderStatusCell(enabledChannel())
  await waitFor(() =>
    expect(api.get).toHaveBeenCalledWith(
      '/api/channel/cooldowns',
      expect.anything()
    )
  )
  expect(screen.queryByText('Cooling down')).not.toBeInTheDocument()
  expect(screen.getByText('Enabled')).toBeVisible()
})

test('a channel that is not enabled shows no cooldown badge even with a matching payload item', async () => {
  mockCooldowns([
    {
      channel_id: 1,
      state: 'cooling',
      cooldown_until: Math.floor(Date.now() / 1000) + 120,
      cooldown_count: 2,
      weight_factor: 0.25,
      last_status_code: 429,
      last_error_code: 'rate_limit_exceeded',
    },
  ])
  const autoDisabled = channelSchema.parse({
    id: 1,
    type: 1,
    key: 'test-key',
    name: 'Auto-Disabled-Channel',
    status: 3,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
  await renderStatusCell(autoDisabled)
  expect(await screen.findByText('Auto Disabled')).toBeVisible()
  expect(screen.queryByText('Cooling down')).not.toBeInTheDocument()
  expect(screen.queryByText('Cooldowns in a row: 2')).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Cooling down' })).toBeNull()
})

test('a failed cooldowns request shows no badge, keeps the status, and does not toast', async () => {
  vi.spyOn(api, 'get').mockRejectedValue(new Error('cooldowns unavailable'))
  const toastError = vi.spyOn(toast, 'error')
  const queryClient = createAppQueryClient()
  await renderStatusCell(enabledChannel(), queryClient)
  expect(await screen.findByText('Enabled')).toBeVisible()
  // Waiting for the error state proves QueryCache.onError already ran, so the
  // no-toast assertion below is not vacuous.
  await waitFor(() =>
    expect(
      queryClient.getQueryState(channelsQueryKeys.cooldowns())?.status
    ).toBe('error')
  )
  expect(toastError).not.toHaveBeenCalled()
  expect(screen.queryByText('Cooling down')).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Cooling down' })).toBeNull()
})
