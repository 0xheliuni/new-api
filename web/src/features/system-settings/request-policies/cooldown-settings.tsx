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
import { useQuery } from '@tanstack/react-query'
import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

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
import { parseHttpStatusCodeRules } from '@/lib/http-status-code-rules'

import {
  SettingsControlChildren,
  SettingsControlGroup,
  SettingsFormGrid,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { safeNumberFieldProps } from '../utils/numeric-field'
import { getPolicyConfig } from './api'
import type { ChannelHealthFormInput } from './channel-health-section'

// The temporary failure cooldown group of the channel health form.
export function CooldownSettings() {
  const { t } = useTranslation()
  const form = useFormContext<ChannelHealthFormInput>()
  const autoDisable = useWatch({
    control: form.control,
    name: 'AutomaticDisableChannelEnabled',
  })
  const enabled = useWatch({
    control: form.control,
    name: 'channel_scheduling_setting.cooldown_enabled',
  })
  const disableAfter = useWatch({
    control: form.control,
    name: 'channel_scheduling_setting.disable_after_cooldowns',
  })
  const statusCodes = useWatch({
    control: form.control,
    name: 'channel_scheduling_setting.cooldown_status_codes',
  })
  const policy = useQuery({
    queryKey: ['request-policy'],
    queryFn: getPolicyConfig,
    staleTime: Infinity,
  })
  const parsedStatusCodes = parseHttpStatusCodeRules(statusCodes)

  return (
    <div className='flex min-w-0 flex-col gap-4'>
      <h4 className='text-sm font-medium'>{t('Temporary failure cooldown')}</h4>
      {policy.data?.memory_cache_enabled === false ? (
        <p className='text-muted-foreground text-sm'>
          {t(
            'Cooldowns and tiered attempts need the in-memory cache. Enable Redis or set MEMORY_CACHE_ENABLED=true; until then channels are selected as before.'
          )}
        </p>
      ) : null}
      {enabled && Number(disableAfter) > 0 && !autoDisable ? (
        <p className='text-muted-foreground text-sm'>
          {t(
            'Channels keep cooling down instead of being disabled until Disable on failure is turned on.'
          )}
        </p>
      ) : null}
      <SettingsFormGrid>
        <SettingsControlGroup>
          <FormField
            control={form.control}
            name='channel_scheduling_setting.cooldown_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Cool down on temporary failures')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Rate limits, upstream server errors and connection failures pause the channel for a while instead of disabling it right away.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
          <SettingsControlChildren
            role='group'
            aria-label={t('Cooldown options')}
            className='grid gap-x-5 gap-y-4 lg:grid-cols-2'
          >
            <FormField
              control={form.control}
              name='channel_scheduling_setting.cooldown_status_codes'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Cooldown status codes')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('e.g. 401, 403, 429, 500-599')}
                      value={field.value}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Accepts comma-separated status codes and inclusive ranges.'
                    )}{' '}
                    {parsedStatusCodes.ok &&
                      parsedStatusCodes.normalized &&
                      parsedStatusCodes.normalized !== field.value.trim() && (
                        <span className='text-muted-foreground'>
                          {t('Normalized:')} {parsedStatusCodes.normalized}
                        </span>
                      )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='channel_scheduling_setting.cooldown_base_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Initial cooldown (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={3600}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Each further failure in a row doubles the cooldown.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='channel_scheduling_setting.cooldown_max_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Maximum cooldown (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={3600}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Upper limit of a single cooldown, at most 3600 seconds.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='channel_scheduling_setting.disable_after_cooldowns'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Disable after consecutive cooldowns')}
                  </FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={100}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      '0 keeps cooling down without disabling. Disabling also needs Disable on failure and the channel option.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </SettingsControlChildren>
        </SettingsControlGroup>
      </SettingsFormGrid>
    </div>
  )
}
