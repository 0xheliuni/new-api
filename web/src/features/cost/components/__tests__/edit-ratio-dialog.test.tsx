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

function mockVersions(versions: ChannelCostVersion[]) {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: versions },
  })
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

async function openDialog() {
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Edit Cost Ratio' }))
  return user
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('edit cost ratio dialog', () => {
  test('saving the current price sends it to the cost price api with its settlement rate', async () => {
    mockVersions([initialVersion])
    const put = vi.spyOn(api, 'put').mockResolvedValue({
      data: { success: true },
    })
    renderDialog({ currentMode: 'discount', currentDiscount: 0.31 })

    const user = await openDialog()
    const rateInput = screen.getByLabelText('Settlement exchange rate')
    expect(rateInput).toHaveValue(6.8)
    await user.clear(rateInput)
    await user.type(rateInput, '7')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(put).toHaveBeenCalledTimes(1))
    expect(put).toHaveBeenCalledWith(
      '/api/cost/channels/7/price',
      {
        cost_mode: 'discount',
        cost_ratio: 0,
        cost_discount: 0.31,
        exchange_rate: 7,
      },
      expect.anything()
    )
  })

  test('editing the initial price sends the corrected values to the version api', async () => {
    mockVersions([initialVersion])
    const put = vi.spyOn(api, 'put').mockResolvedValue({
      data: { success: true, data: initialVersion },
    })
    renderDialog({ currentRatio: 3.1 })

    const user = await openDialog()
    const row = (await screen.findByText('Initial')).closest(
      'li'
    ) as HTMLElement
    await user.click(within(row).getByRole('button', { name: 'Edit' }))
    const form = screen.getByRole('group', { name: 'Edit price record' })
    const value = within(form).getByRole('spinbutton', {
      name: 'Cost Ratio (CNY per USD)',
    })
    await user.clear(value)
    await user.type(value, '2.9')
    await user.click(within(form).getByRole('button', { name: 'Save' }))

    await waitFor(() =>
      expect(put).toHaveBeenCalledWith(
        '/api/cost/versions/11',
        {
          effective_from: 0,
          cost_mode: 'ratio',
          cost_ratio: 2.9,
          cost_discount: 0,
          exchange_rate: 0,
          note: 'migrated from channel settings',
        },
        expect.anything()
      )
    )
  })

  test('the initial option is unavailable when adding to a channel that has an initial price', async () => {
    mockVersions([laterVersion, initialVersion])
    renderDialog({ currentMode: 'discount', currentDiscount: 0.31 })

    const user = await openDialog()
    await user.click(
      await screen.findByRole('button', { name: 'Add historical price' })
    )

    expect(screen.getByRole('button', { name: 'Initial' })).toBeDisabled()
    expect(
      screen.getByText(
        'This channel already has an initial price. Edit it in the list instead.'
      )
    ).toBeVisible()
  })

  test('setting the initial price on a channel without one creates a record with no effective time', async () => {
    mockVersions([laterVersion])
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: true, data: initialVersion },
    })
    renderDialog({ currentMode: 'discount', currentDiscount: 0.31 })

    const user = await openDialog()
    await user.click(
      await screen.findByRole('button', { name: 'Set initial price' })
    )
    await user.type(
      screen.getByRole('spinbutton', { name: 'Cost Ratio (CNY per USD)' }),
      '3.1'
    )
    await user.click(screen.getByRole('button', { name: 'Add' }))

    await waitFor(() =>
      expect(post).toHaveBeenCalledWith(
        '/api/cost/channels/7/versions',
        {
          effective_from: 0,
          cost_mode: 'ratio',
          cost_ratio: 3.1,
          cost_discount: 0,
          exchange_rate: 0,
          note: '',
        },
        expect.anything()
      )
    )
  })

  test('deleting a price record asks for confirmation before calling the api', async () => {
    mockVersions([laterVersion, initialVersion])
    const remove = vi.spyOn(api, 'delete').mockResolvedValue({
      data: { success: true },
    })
    renderDialog({ currentMode: 'discount', currentDiscount: 0.31 })

    const user = await openDialog()
    const row = (await screen.findByText('Initial')).closest(
      'li'
    ) as HTMLElement
    await user.click(within(row).getByRole('button', { name: 'Delete' }))
    expect(remove).not.toHaveBeenCalled()

    const confirm = await screen.findByRole('alertdialog')
    await user.click(within(confirm).getByRole('button', { name: 'Delete' }))

    await waitFor(() =>
      expect(remove).toHaveBeenCalledWith(
        '/api/cost/versions/11',
        expect.anything()
      )
    )
  })
})
