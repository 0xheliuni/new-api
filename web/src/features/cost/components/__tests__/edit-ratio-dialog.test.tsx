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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ComponentProps } from 'react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { ChannelCostVersion } from '../../types'
import { EditRatioDialog } from '../edit-ratio-dialog'

const initialVersion: ChannelCostVersion = {
  id: 11,
  channel_id: 7,
  effective_from: 0,
  cost_mode: 'ratio',
  cost_ratio: 3.1,
  cost_discount: 0,
  exchange_rate: 1,
  note: 'migrated from channel settings',
  created_at: 1,
  created_by: 1,
}

const laterVersion: ChannelCostVersion = {
  ...initialVersion,
  id: 12,
  effective_from: 1754000000,
  cost_mode: 'discount',
  cost_ratio: 0,
  cost_discount: 0.31,
  exchange_rate: 6.8,
  note: '',
}

function mockApiGet(channelSetting: string, versions: ChannelCostVersion[]) {
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data:
        url === '/api/channel/7'
          ? { id: 7, setting: channelSetting }
          : versions,
    },
  }))
}

function renderDialog(props: Partial<ComponentProps<typeof EditRatioDialog>>) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <EditRatioDialog
        channelId={7}
        channelName='upstream'
        currentRatio={0}
        exchangeRate={6.8}
        {...props}
      />
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('edit cost ratio dialog', () => {
  test('saving a discount price sends the settlement exchange rate entered beside it', async () => {
    mockApiGet('{"cost_ratio":3.1}', [initialVersion])
    const put = vi.spyOn(api, 'put').mockResolvedValue({
      data: { success: true },
    })
    const user = userEvent.setup()
    renderDialog({ currentMode: 'discount', currentDiscount: 0.31 })

    await user.click(screen.getByRole('button', { name: 'Edit Cost Ratio' }))
    const rateInput = screen.getByLabelText('Settlement exchange rate')
    expect(rateInput).toHaveValue(6.8)
    await user.clear(rateInput)
    await user.type(rateInput, '7')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(put).toHaveBeenCalledTimes(1))
    const body = put.mock.calls[0][1] as { id: number; setting: string }
    expect(body.id).toBe(7)
    expect(JSON.parse(body.setting)).toMatchObject({
      cost_mode: 'discount',
      cost_discount: 0.31,
      cost_exchange_rate: 7,
    })
  })

  test('deleting the initial price calls the version delete api', async () => {
    mockApiGet('{"cost_ratio":3.1}', [laterVersion, initialVersion])
    const remove = vi.spyOn(api, 'delete').mockResolvedValue({
      data: { success: true },
    })
    const user = userEvent.setup()
    renderDialog({ currentMode: 'discount', currentDiscount: 0.31 })

    await user.click(screen.getByRole('button', { name: 'Edit Cost Ratio' }))
    const initialRow = (await screen.findByText('Initial')).closest('li')
    expect(initialRow).not.toBeNull()
    await user.click(
      within(initialRow as HTMLElement).getByRole('button', { name: 'Delete' })
    )

    await waitFor(() =>
      expect(remove).toHaveBeenCalledWith(
        '/api/cost/versions/11',
        expect.anything()
      )
    )
  })
})
