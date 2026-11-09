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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useForm } from 'react-hook-form'
import { describe, expect, test } from 'vitest'

import { Form } from '@/components/ui/form'

import {
  CHANNEL_FORM_DEFAULT_VALUES,
  type ChannelFormValues,
} from '../../../../lib/channel-form'
import { VideoChannelSettings } from '../video-channel-settings'

function Harness(props: {
  type: number
  disabled?: boolean
  values?: Partial<ChannelFormValues>
}) {
  const form = useForm<ChannelFormValues>({
    defaultValues: {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      ...props.values,
      type: props.type,
    },
  })
  return (
    <Form {...form}>
      <VideoChannelSettings
        form={form}
        type={props.type}
        disabled={props.disabled ?? false}
      />
    </Form>
  )
}

describe('VideoChannelSettings', () => {
  test('upstream channel types render no video channel settings', () => {
    const { container } = render(<Harness type={1} />)
    expect(container).toBeEmptyDOMElement()
  })

  test('VolcEngine shows the asset library and upstream task id blocks', () => {
    render(<Harness type={45} />)
    expect(
      screen.getByRole('group', { name: 'Asset library pre-upload' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('group', { name: 'Upstream Task ID Passthrough' })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('group', { name: 'Seedance(第三方) Asset Library' })
    ).not.toBeInTheDocument()
  })

  test('enabling the asset library reveals the official credentials with their defaults', async () => {
    const user = userEvent.setup()
    render(<Harness type={54} />)
    expect(
      screen.queryByRole('combobox', { name: 'Asset provider' })
    ).not.toBeInTheDocument()

    await user.click(
      screen.getByRole('switch', { name: 'Enable asset pre-upload' })
    )

    expect(
      screen.getByRole('combobox', { name: 'Asset provider' })
    ).toBeInTheDocument()
    expect(screen.getByLabelText('Access Key (AK)')).toHaveAttribute(
      'type',
      'password'
    )
    expect(screen.getByLabelText('Secret Key (SK)')).toHaveAttribute(
      'type',
      'password'
    )
    expect(screen.getByRole('textbox', { name: 'Project Name' })).toHaveValue(
      'default'
    )
    expect(screen.getByRole('textbox', { name: 'Region' })).toHaveValue(
      'cn-beijing'
    )
  })

  test('choosing Cloudwise hides the official credentials', async () => {
    const user = userEvent.setup()
    render(<Harness type={45} values={{ byteplus_asset_enabled: true }} />)

    await user.click(screen.getByRole('combobox', { name: 'Asset provider' }))
    await user.click(
      screen.getByRole('option', { name: 'Third-party (Cloudwise)' })
    )

    expect(
      screen.getByText(
        'Third-party asset library reuses the channel API address and key.'
      )
    ).toBeInTheDocument()
    expect(screen.queryByLabelText('Access Key (AK)')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('textbox', { name: 'Project Name' })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('textbox', { name: 'Asset Group ID' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('switch', { name: 'Skip content pre-filter' })
    ).toBeChecked()
  })

  test('Seedance third-party shows its asset switch and the upstream task id switch', () => {
    render(<Harness type={65} values={{ seedance3rd_asset_enabled: true }} />)
    expect(
      screen.getByRole('switch', {
        name: 'Seedance 3rd-party asset preupload',
      })
    ).toBeChecked()
    expect(
      screen.getByRole('switch', { name: 'Expose upstream task id to clients' })
    ).not.toBeChecked()
    expect(
      screen.queryByRole('group', { name: 'Asset library pre-upload' })
    ).not.toBeInTheDocument()
  })

  test('locked settings disable every asset library control', () => {
    render(
      <Harness type={45} disabled values={{ byteplus_asset_enabled: true }} />
    )
    for (const toggle of screen.getAllByRole('switch')) {
      expect(toggle).toHaveAttribute('aria-disabled', 'true')
    }
    expect(
      screen.getByRole('combobox', { name: 'Asset provider' })
    ).toBeDisabled()
    expect(screen.getByLabelText('Access Key (AK)')).toBeDisabled()
    expect(
      screen.getByRole('textbox', { name: 'Asset Group ID' })
    ).toBeDisabled()
  })
})
