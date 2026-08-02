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
import { Label } from '@/components/ui/label'
import { DateTimePicker } from '@/components/datetime-picker'
import { Dialog } from '@/components/dialog'

export interface CostFilterValue {
  start: number
  end: number
}

interface CostFilterProps {
  value: CostFilterValue
  onChange: (next: CostFilterValue) => void
}

interface Preset {
  label: string
  range: () => { start: Date; end: Date }
}

export function CostFilter({ value, onChange }: CostFilterProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [draftStart, setDraftStart] = useState<Date>(
    new Date(value.start * 1000)
  )
  const [draftEnd, setDraftEnd] = useState<Date>(new Date(value.end * 1000))

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      setDraftStart(new Date(value.start * 1000))
      setDraftEnd(new Date(value.end * 1000))
    }
    setOpen(nextOpen)
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

  const applyRange = (start: Date, end: Date) => {
    onChange({
      start: dateToUnixTimestamp(start),
      end: dateToUnixTimestamp(end),
    })
    setOpen(false)
  }

  const triggerLabel = `${formatDateStr(new Date(value.start * 1000))} – ${formatDateStr(new Date(value.end * 1000))}`

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
      trigger={
        <Button variant='outline' size='sm'>
          <CalendarRange className='mr-2 size-4' />
          {triggerLabel}
        </Button>
      }
      title={t('Time Range')}
      contentClassName='sm:max-w-md'
      footer={
        <>
          <Button variant='outline' onClick={() => setOpen(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={() => applyRange(draftStart, draftEnd)}>
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
            value={draftStart}
            onChange={(date) => date && setDraftStart(date)}
          />
        </div>

        <div className='grid gap-2'>
          <Label htmlFor='cost-filter-end'>{t('End Time')}</Label>
          <DateTimePicker
            value={draftEnd}
            onChange={(date) => date && setDraftEnd(date)}
          />
        </div>
      </div>
    </Dialog>
  )
}
