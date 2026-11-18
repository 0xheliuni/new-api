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
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { toIntlLocale } from '@/i18n/languages'
import { formatTimestampRelative } from '@/lib/format'

import { getChannelCooldowns } from '../api'
import { channelsQueryKeys } from '../lib'

const WEIGHT_SHARES: Record<number, string> = { 0.25: '1/4', 0.5: '1/2' }

/**
 * Marks an enabled channel that is cooling down after temporary failures or
 * ramping its weight back up. Renders nothing for a healthy channel.
 */
export function ChannelCooldownBadge(props: { channelId: number }) {
  const { t, i18n } = useTranslation()
  const query = useQuery({
    queryKey: channelsQueryKeys.cooldowns(),
    queryFn: getChannelCooldowns,
    staleTime: 15_000,
  })
  const item = query.data?.items.find(
    (entry) => entry.channel_id === props.channelId
  )
  if (!item) return null
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const cooling = item.state === 'cooling'
  const share =
    WEIGHT_SHARES[item.weight_factor] ??
    `${Math.round(item.weight_factor * 100)}%`
  return (
    <TooltipProvider delay={100}>
      <Tooltip>
        <TooltipTrigger render={<span />}>
          <StatusBadge
            label={
              cooling ? t('Cooling down') : t('Recovering {{share}}', { share })
            }
            variant={cooling ? 'warning' : 'info'}
            size='sm'
            copyable={false}
          />
        </TooltipTrigger>
        <TooltipContent side='top' className='max-w-xs'>
          <div className='space-y-1 text-xs'>
            {cooling ? (
              <div>
                {t('Cooldown ends {{time}}', {
                  time: formatTimestampRelative(
                    item.cooldown_until,
                    'seconds',
                    locale
                  ),
                })}
              </div>
            ) : (
              <div>{t('Weight grows back to full as requests succeed.')}</div>
            )}
            {item.cooldown_count > 0 ? (
              <div>
                {t('Cooldowns in a row: {{number}}', {
                  number: item.cooldown_count,
                })}
              </div>
            ) : null}
            {item.last_status_code > 0 ? (
              <div>
                {t('Last status code: {{code}}', {
                  code: item.last_status_code,
                })}
              </div>
            ) : null}
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
