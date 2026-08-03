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
import { useState } from 'react'
import { CalendarRange } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  dateToUnixTimestamp,
  getEndOfDay,
  getRollingDateRange,
  getStartOfDay,
} from '@/lib/time'
import { formatDateStr } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import { DateTimePicker } from '@/components/datetime-picker'
import { Dialog } from '@/components/dialog'

export const DEFAULT_EXCHANGE_RATE = 6.8

export interface CostFilterValue {
  start: number
  end: number
  username?: string
  channel?: number
  model_name?: string
  exchange_rate: number
}

interface CostFilterProps {
  value: CostFilterValue
  onApply: (next: CostFilterValue) => void
}

interface Preset {
  label: string
  range: () => { start: Date; end: Date }
}

function serializeFilterValue(value: CostFilterValue): string {
  return [
    value.start,
    value.end,
    value.username ?? '',
    value.channel ?? '',
    value.model_name ?? '',
    value.exchange_rate,
  ].join('|')
}

/**
 * Default filter range is TODAY (00:00:00 local -> now), matching the
 * "Today" quick-range preset below. Exported so the route/page-level default
 * (used before the user has ever applied a filter) stays in sync with what
 * Reset restores.
 */
export function defaultFilterValue(): CostFilterValue {
  return {
    start: dateToUnixTimestamp(getStartOfDay(new Date())),
    end: dateToUnixTimestamp(new Date()),
    username: undefined,
    channel: undefined,
    model_name: undefined,
    exchange_rate: DEFAULT_EXCHANGE_RATE,
  }
}

export function CostFilter({ value, onApply }: CostFilterProps) {
  const { t } = useTranslation()
  const [rangeOpen, setRangeOpen] = useState(false)
  const [draft, setDraft] = useState<CostFilterValue>(value)
  const [prevValueKey, setPrevValueKey] = useState(() =>
    serializeFilterValue(value)
  )
  const [rangeDraftStart, setRangeDraftStart] = useState<Date>(
    new Date(value.start * 1000)
  )
  const [rangeDraftEnd, setRangeDraftEnd] = useState<Date>(
    new Date(value.end * 1000)
  )

  const valueKey = serializeFilterValue(value)
  if (valueKey !== prevValueKey) {
    setPrevValueKey(valueKey)
    setDraft(value)
  }

  const presets: Preset[] = [
    {
      label: t('Today'),
      range: () => ({ start: getStartOfDay(new Date()), end: new Date() }),
    },
    {
      label: t('Yesterday'),
      range: () => {
        const yesterday = new Date()
        yesterday.setDate(yesterday.getDate() - 1)
        return { start: getStartOfDay(yesterday), end: getEndOfDay(yesterday) }
      },
    },
    {
      label: t('Last 7 days'),
      range: () => getRollingDateRange(7),
    },
    {
      label: t('Last 30 days'),
      range: () => getRollingDateRange(30),
    },
    {
      label: t('This month'),
      range: () => {
        const now = new Date()
        return {
          start: getStartOfDay(new Date(now.getFullYear(), now.getMonth(), 1)),
          end: now,
        }
      },
    },
  ]

  const handleRangeOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      setRangeDraftStart(new Date(draft.start * 1000))
      setRangeDraftEnd(new Date(draft.end * 1000))
    }
    setRangeOpen(nextOpen)
  }

  const applyRange = (start: Date, end: Date) => {
    setDraft((prev) => ({
      ...prev,
      start: dateToUnixTimestamp(start),
      end: dateToUnixTimestamp(end),
    }))
    setRangeOpen(false)
  }

  const handleSearch = () => {
    const rate =
      Number.isFinite(draft.exchange_rate) && draft.exchange_rate > 0
        ? draft.exchange_rate
        : DEFAULT_EXCHANGE_RATE
    onApply({ ...draft, exchange_rate: rate })
  }

  const handleReset = () => {
    const next = defaultFilterValue()
    setDraft(next)
    onApply(next)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') handleSearch()
  }

  const triggerLabel = `${formatDateStr(new Date(draft.start * 1000))} – ${formatDateStr(new Date(draft.end * 1000))}`

  return (
    <div className='flex flex-wrap items-end gap-2'>
      <div className='flex flex-col gap-1.5'>
        <Label className='text-muted-foreground text-xs'>
          {t('Time Range')}
        </Label>
        <Dialog
          open={rangeOpen}
          onOpenChange={handleRangeOpenChange}
          trigger={
            // Default size (h-8) to match the h-8 inputs/InputGroup below —
            // this is the row's visual anchor, so every other field mirrors it.
            <Button variant='outline' className='font-normal'>
              <CalendarRange className='mr-2 size-4' />
              {triggerLabel}
            </Button>
          }
          title={t('Time Range')}
          contentClassName='sm:max-w-md'
          footer={
            <>
              <Button variant='outline' onClick={() => setRangeOpen(false)}>
                {t('Cancel')}
              </Button>
              <Button onClick={() => applyRange(rangeDraftStart, rangeDraftEnd)}>
                {t('Apply')}
              </Button>
            </>
          }
        >
          <div className='flex flex-col gap-4'>
            <div className='grid gap-2'>
              <Label className='text-muted-foreground text-xs'>
                {t('Quick Range')}
              </Label>
              <div className='grid grid-cols-2 gap-2 sm:flex sm:flex-wrap'>
                {presets.map((preset) => (
                  <Button
                    key={preset.label}
                    type='button'
                    size='sm'
                    variant='outline'
                    onClick={() => {
                      const { start, end } = preset.range()
                      applyRange(start, end)
                    }}
                  >
                    {preset.label}
                  </Button>
                ))}
              </div>
            </div>

            <div className='grid gap-2'>
              <Label htmlFor='cost-filter-start'>{t('Start Time')}</Label>
              <DateTimePicker
                value={rangeDraftStart}
                onChange={(date) => date && setRangeDraftStart(date)}
              />
            </div>

            <div className='grid gap-2'>
              <Label htmlFor='cost-filter-end'>{t('End Time')}</Label>
              <DateTimePicker
                value={rangeDraftEnd}
                onChange={(date) => date && setRangeDraftEnd(date)}
              />
            </div>
          </div>
        </Dialog>
      </div>

      <div className='flex flex-col gap-1.5'>
        <Label htmlFor='cost-filter-username' className='text-muted-foreground text-xs'>
          {t('Username')}
        </Label>
        <Input
          id='cost-filter-username'
          className='h-8 w-36'
          placeholder={t('Username')}
          value={draft.username ?? ''}
          onChange={(e) =>
            setDraft((prev) => ({
              ...prev,
              username: e.target.value || undefined,
            }))
          }
          onKeyDown={handleKeyDown}
        />
      </div>

      <div className='flex flex-col gap-1.5'>
        <Label htmlFor='cost-filter-channel' className='text-muted-foreground text-xs'>
          {t('Channel ID')}
        </Label>
        <Input
          id='cost-filter-channel'
          type='number'
          className='h-8 w-28'
          placeholder={t('Channel ID')}
          value={draft.channel ?? ''}
          onChange={(e) =>
            setDraft((prev) => ({
              ...prev,
              channel: e.target.value === '' ? undefined : Number(e.target.value),
            }))
          }
          onKeyDown={handleKeyDown}
        />
      </div>

      <div className='flex flex-col gap-1.5'>
        <Label htmlFor='cost-filter-model' className='text-muted-foreground text-xs'>
          {t('Model Name')}
        </Label>
        <Input
          id='cost-filter-model'
          className='h-8 w-40'
          placeholder={t('Model Name')}
          value={draft.model_name ?? ''}
          onChange={(e) =>
            setDraft((prev) => ({
              ...prev,
              model_name: e.target.value || undefined,
            }))
          }
          onKeyDown={handleKeyDown}
        />
      </div>

      <div className='flex flex-col gap-1.5'>
        <Label htmlFor='cost-filter-rate' className='text-muted-foreground text-xs'>
          {t('Exchange Rate')}
        </Label>
        <InputGroup className='w-36'>
          <InputGroupAddon>$1 =</InputGroupAddon>
          <InputGroupInput
            id='cost-filter-rate'
            type='number'
            step={0.1}
            value={Number.isFinite(draft.exchange_rate) ? draft.exchange_rate : ''}
            onChange={(e) =>
              setDraft((prev) => ({
                ...prev,
                exchange_rate:
                  e.target.value === '' ? NaN : Number(e.target.value),
              }))
            }
            onKeyDown={handleKeyDown}
          />
          <InputGroupAddon align='inline-end'>CNY</InputGroupAddon>
        </InputGroup>
      </div>

      <div className='flex gap-2'>
        <Button type='button' onClick={handleSearch}>
          {t('Search')}
        </Button>
        <Button type='button' variant='outline' onClick={handleReset}>
          {t('Reset')}
        </Button>
      </div>
    </div>
  )
}
