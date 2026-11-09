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
import { Eye, FileCode } from 'lucide-react'
import { useState } from 'react'
import type { ControllerRenderProps } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormDescription,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'

import { RELAY_CONFIG_EXAMPLE } from '../../../constants'
import type { ChannelFormValues } from '../../../lib/channel-form'
import { RelayConfigPreviewDialog } from '../../dialogs/relay-config-preview-dialog'

type ChannelRelayConfigSectionProps = {
  field: ControllerRenderProps<ChannelFormValues, 'task_plugin_config'>
  disabled?: boolean
  channelType: number
  taskPluginKey?: string
  baseUrl?: string
  model: string
}

export function ChannelRelayConfigSection(
  props: ChannelRelayConfigSectionProps
) {
  const { t } = useTranslation()
  const [previewOpen, setPreviewOpen] = useState(false)
  const value = props.field.value || ''

  return (
    <FormItem className='space-y-3 border-t pt-4'>
      <div className='flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between'>
        <div className='space-y-1'>
          <FormLabel>{t('Relay configuration')}</FormLabel>
          <FormDescription>
            {t(
              'Describe how this channel talks to a third-party relay. Leave empty to connect to Alibaba Cloud Bailian directly.'
            )}
          </FormDescription>
        </div>
        <div className='flex flex-wrap gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.disabled}
            onClick={() =>
              props.field.onChange(
                JSON.stringify(RELAY_CONFIG_EXAMPLE, null, 2)
              )
            }
          >
            <FileCode className='mr-2 h-4 w-4' />
            {t('Insert example')}
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={!value.trim()}
            onClick={() => setPreviewOpen(true)}
          >
            <Eye className='mr-2 h-4 w-4' />
            {t('Preview')}
          </Button>
        </div>
      </div>
      <FormControl>
        <JsonCodeEditor
          value={value}
          onChange={props.field.onChange}
          name={props.field.name}
          onBlur={props.field.onBlur}
          textareaRef={props.field.ref}
          disabled={props.disabled}
          ariaLabel={t('Relay configuration')}
          heightClassName='h-72 min-h-72 max-h-72'
        />
      </FormControl>
      <FormMessage />
      <RelayConfigPreviewDialog
        open={previewOpen}
        onOpenChange={setPreviewOpen}
        config={value}
        channelType={props.channelType}
        taskPluginKey={props.taskPluginKey}
        baseUrl={props.baseUrl}
        model={props.model}
      />
    </FormItem>
  )
}
