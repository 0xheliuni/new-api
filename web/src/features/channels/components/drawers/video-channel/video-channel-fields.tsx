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

import { PasswordInput } from '@/components/password-input'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import type { ChannelFormValues } from '../../../lib/channel-form'

type VideoChannelSwitchName =
  | 'byteplus_asset_enabled'
  | 'byteplus_moderation_skip'
  | 'seedance3rd_asset_enabled'
  | 'expose_upstream_task_id'

type VideoChannelTextName =
  | 'byteplus_access_key'
  | 'byteplus_secret_key'
  | 'byteplus_asset_group_id'
  | 'byteplus_project_name'
  | 'byteplus_region'
  | 'volc_sign_region'
  | 'volc_openapi_endpoint'

type VideoChannelSwitchFieldProps = {
  form: UseFormReturn<ChannelFormValues>
  name: VideoChannelSwitchName
  label: string
  description: string
  disabled: boolean
}

type VideoChannelTextFieldProps = {
  form: UseFormReturn<ChannelFormValues>
  name: VideoChannelTextName
  label: string
  placeholder: string
  description?: string
  secret?: boolean
  disabled: boolean
}

/** 一行开关：左边标题和说明，右边开关。label、description 传已翻译的文案。 */
export function VideoChannelSwitchField(props: VideoChannelSwitchFieldProps) {
  return (
    <FormField
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem className='flex items-center justify-between gap-3 px-1 py-2'>
          <div className='space-y-0.5'>
            <FormLabel className='text-sm'>{props.label}</FormLabel>
            <FormDescription>{props.description}</FormDescription>
          </div>
          <FormControl>
            <Switch
              checked={field.value === true}
              onCheckedChange={field.onChange}
              disabled={props.disabled}
            />
          </FormControl>
        </FormItem>
      )}
    />
  )
}

/** 文本输入；secret 为真时用共享的 PasswordInput。文案传已翻译的字符串。 */
export function VideoChannelTextField(props: VideoChannelTextFieldProps) {
  return (
    <FormField
      control={props.form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{props.label}</FormLabel>
          <FormControl>
            {props.secret ? (
              <PasswordInput
                {...field}
                value={field.value ?? ''}
                autoComplete='new-password'
                placeholder={props.placeholder}
                disabled={props.disabled}
              />
            ) : (
              <Input
                {...field}
                value={field.value ?? ''}
                autoComplete='off'
                placeholder={props.placeholder}
                disabled={props.disabled}
              />
            )}
          </FormControl>
          {props.description && (
            <FormDescription>{props.description}</FormDescription>
          )}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
