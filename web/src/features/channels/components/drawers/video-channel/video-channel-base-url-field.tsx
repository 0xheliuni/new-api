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
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ComboboxInput } from '@/components/ui/combobox-input'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'

import type { ChannelFormValues } from '../../../lib/channel-form'
import {
  ARK_CN_BASE_URL,
  CHANNEL_TYPE_VOLCENGINE,
  VIDEO_CHANNEL_BASE_URL_PRESETS,
} from '../../../lib/video-channel-extensions'

const BASE_URL_OPTIONS = VIDEO_CHANNEL_BASE_URL_PRESETS.map((url) => ({
  value: url,
  label: url,
}))

type VideoChannelBaseUrlFieldProps = {
  form: UseFormReturn<ChannelFormValues>
  type: number
  placeholder: string
  disabled: boolean
}

/** 火山方舟和字节火山透传的地址：可选官方地址，也可手填 Cloudwise 等第三方地址。 */
export function VideoChannelBaseUrlField(props: VideoChannelBaseUrlFieldProps) {
  const { t } = useTranslation()
  // 45 已有必填校验；66 留空时后端用默认地址，所以不标必填
  const isVolcEngine = props.type === CHANNEL_TYPE_VOLCENGINE

  return (
    <FormField
      control={props.form.control}
      name='base_url'
      render={({ field }) => (
        <FormItem>
          <FormLabel required={isVolcEngine}>{t('API Base URL')}</FormLabel>
          <FormControl>
            <ComboboxInput
              options={BASE_URL_OPTIONS}
              // 上游把豆包 Coding Plan 存成这个占位值，显示为国内地址
              value={
                field.value === 'doubao-coding-plan'
                  ? ARK_CN_BASE_URL
                  : field.value || ''
              }
              onValueChange={field.onChange}
              placeholder={props.placeholder}
              allowCustomValue
              disabled={props.disabled}
            />
          </FormControl>
          <FormDescription>
            {isVolcEngine
              ? t(
                  'Select an official regional endpoint or enter a custom API address'
                )
              : t(
                  'Data plane host for passthrough. Also determines the control plane signing region when left unset below.'
                )}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
