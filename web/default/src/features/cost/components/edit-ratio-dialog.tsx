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
import { useQueryClient } from '@tanstack/react-query'
import { Pencil } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getChannel, updateChannel } from '@/features/channels/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Dialog } from '@/components/dialog'

interface EditRatioDialogProps {
  channelId: number
  channelName: string
  currentRatio: number
}

export function EditRatioDialog({
  channelId,
  channelName,
  currentRatio,
}: EditRatioDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [value, setValue] = useState(currentRatio ? String(currentRatio) : '')
  const [submitting, setSubmitting] = useState(false)

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) setValue(currentRatio ? String(currentRatio) : '')
    setOpen(nextOpen)
  }

  const parsedRatio = Number(value)
  const isValidRatio =
    value.trim() !== '' && Number.isFinite(parsedRatio) && parsedRatio >= 0

  const handleSubmit = async () => {
    if (!isValidRatio) return
    setSubmitting(true)
    try {
      const channelRes = await getChannel(channelId)
      if (!channelRes.success || !channelRes.data) {
        throw new Error(channelRes.message || t('Failed to load channel'))
      }

      let settingObj: Record<string, unknown> = {}
      if (channelRes.data.setting) {
        try {
          const parsed = JSON.parse(channelRes.data.setting)
          if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
            settingObj = parsed as Record<string, unknown>
          }
        } catch {
          settingObj = {}
        }
      }
      settingObj.cost_ratio = parsedRatio

      const updateRes = await updateChannel(channelId, {
        setting: JSON.stringify(settingObj),
      })
      if (!updateRes.success) {
        throw new Error(updateRes.message || t('Failed to update cost ratio'))
      }

      toast.success(t('Cost ratio updated'))
      await queryClient.invalidateQueries({ queryKey: ['cost'] })
      setOpen(false)
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('Failed to update cost ratio')
      )
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
      trigger={
        <Button
          type='button'
          variant='ghost'
          size='icon'
          className='size-6'
          aria-label={t('Edit Cost Ratio')}
        >
          <Pencil className='size-3.5' />
        </Button>
      }
      title={t('Edit Cost Ratio')}
      description={channelName}
      contentClassName='sm:max-w-sm'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => setOpen(false)}
            disabled={submitting}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            onClick={handleSubmit}
            disabled={!isValidRatio || submitting}
          >
            {t('Save')}
          </Button>
        </>
      }
    >
      <div className='flex flex-col gap-2'>
        <Label htmlFor='cost-ratio-input'>
          {t('Cost Ratio (CNY per USD)')}
        </Label>
        <Input
          id='cost-ratio-input'
          type='number'
          min={0}
          step={0.01}
          value={value}
          onChange={(event) => setValue(event.target.value)}
        />
        <p className='text-muted-foreground text-xs'>
          {isValidRatio
            ? t(
                'Ratio {{r}} → every $1 of list-price usage costs ¥{{cny}}',
                { r: parsedRatio, cny: parsedRatio.toFixed(2) }
              )
            : t('Used by cost accounting. Leave empty if unknown.')}
        </p>
      </div>
    </Dialog>
  )
}
