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
import { useQueryClient } from '@tanstack/react-query'
import { Pencil } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'

import { setChannelCostPrice } from '../api'
import { VersionHistoryPanel } from './cost-version-history'

type CostPricingMode = 'ratio' | 'discount'

interface EditRatioDialogProps {
  channelId: number
  channelName: string
  currentRatio: number
  currentMode?: string
  currentDiscount?: number
  /** Settlement rate frozen with the current discount-mode price. */
  currentExchangeRate?: number
  /** The report's query rate; seeds the settlement rate when none is stored. */
  exchangeRate: number
}

export function EditRatioDialog({
  channelId,
  channelName,
  currentRatio,
  currentMode,
  currentDiscount,
  currentExchangeRate,
  exchangeRate,
}: EditRatioDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const initialRate = String(currentExchangeRate || exchangeRate)
  const [open, setOpen] = useState(false)
  const [mode, setMode] = useState<CostPricingMode>(
    currentMode === 'discount' ? 'discount' : 'ratio'
  )
  const [ratioValue, setRatioValue] = useState(
    currentRatio ? String(currentRatio) : ''
  )
  const [discountValue, setDiscountValue] = useState(
    currentDiscount ? String(currentDiscount) : ''
  )
  const [rateValue, setRateValue] = useState(initialRate)
  const [submitting, setSubmitting] = useState(false)

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      setMode(currentMode === 'discount' ? 'discount' : 'ratio')
      setRatioValue(currentRatio ? String(currentRatio) : '')
      setDiscountValue(currentDiscount ? String(currentDiscount) : '')
      setRateValue(initialRate)
    }
    setOpen(nextOpen)
  }

  // Must be > 0: the API refuses a zero price because no log could be priced.
  const parsedRatio = Number(ratioValue)
  const isValidRatio =
    ratioValue.trim() !== '' && Number.isFinite(parsedRatio) && parsedRatio > 0
  const parsedRate = Number(rateValue)
  const isValidRate =
    rateValue.trim() !== '' && Number.isFinite(parsedRate) && parsedRate > 0
  const parsedDiscount = Number(discountValue)
  const isValidDiscount =
    discountValue.trim() !== '' &&
    Number.isFinite(parsedDiscount) &&
    parsedDiscount > 0
  const isValid =
    mode === 'discount' ? isValidDiscount && isValidRate : isValidRatio

  const handleSubmit = async () => {
    if (!isValid) return
    setSubmitting(true)
    try {
      // The server compares this with the price in effect now and records a new
      // price from now on only when it differs; the frozen rate rides along.
      await setChannelCostPrice(channelId, {
        cost_mode: mode,
        cost_ratio: mode === 'ratio' ? parsedRatio : 0,
        cost_discount: mode === 'discount' ? parsedDiscount : 0,
        exchange_rate: mode === 'discount' ? parsedRate : 0,
      })

      toast.success(t('Cost ratio updated'))
      // Saving may record a price version server-side, so the history list below
      // is stale too. ['cost'] does not prefix-match ['cost-versions', id], and
      // the global staleTime would serve the cached list if the admin reopens the
      // dialog to confirm.
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['cost'] }),
        queryClient.invalidateQueries({
          queryKey: ['cost-versions', channelId],
        }),
      ])
      setOpen(false)
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t('Failed to update cost ratio')
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
      contentClassName='sm:max-w-md'
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
            disabled={!isValid || submitting}
          >
            {t('Save')}
          </Button>
        </>
      }
    >
      <div className='flex flex-col gap-4'>
        <div className='flex flex-col gap-2'>
          <Label>{t('Pricing Mode')}</Label>
          <Select
            value={mode}
            onValueChange={(value) => setMode(value as CostPricingMode)}
            items={[
              { value: 'ratio', label: t('Cost Ratio (CNY per USD)') },
              { value: 'discount', label: t('Cost Discount') },
            ]}
          >
            <SelectTrigger className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value='ratio'>
                  {t('Cost Ratio (CNY per USD)')}
                </SelectItem>
                <SelectItem value='discount'>{t('Cost Discount')}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>

        {mode === 'discount' ? (
          <div className='flex flex-col gap-2'>
            <div className='grid gap-3 sm:grid-cols-2'>
              <div className='flex flex-col gap-2'>
                <Label htmlFor='cost-discount-input'>
                  {t('Cost Discount')}
                </Label>
                <Input
                  id='cost-discount-input'
                  type='number'
                  min={0}
                  step={0.01}
                  value={discountValue}
                  onChange={(event) => setDiscountValue(event.target.value)}
                />
              </div>
              <div className='flex flex-col gap-2'>
                <Label htmlFor='cost-exchange-rate-input'>
                  {t('Settlement exchange rate')}
                </Label>
                <Input
                  id='cost-exchange-rate-input'
                  type='number'
                  min={0}
                  step={0.01}
                  value={rateValue}
                  onChange={(event) => setRateValue(event.target.value)}
                />
              </div>
            </div>
            <p className='text-muted-foreground text-xs'>
              {isValidDiscount && isValidRate
                ? t(
                    'Discount {{d}} × rate {{r}} → $1 list price costs ¥{{cny}}',
                    {
                      d: parsedDiscount,
                      r: parsedRate,
                      cny: (parsedDiscount * parsedRate).toFixed(2),
                    }
                  )
                : t('Used by cost accounting. Leave empty if unknown.')}
            </p>
          </div>
        ) : (
          <div className='flex flex-col gap-2'>
            <Label htmlFor='cost-ratio-input'>
              {t('Cost Ratio (CNY per USD)')}
            </Label>
            <Input
              id='cost-ratio-input'
              type='number'
              min={0}
              step={0.01}
              value={ratioValue}
              onChange={(event) => setRatioValue(event.target.value)}
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
        )}

        <p className='text-muted-foreground text-xs'>
          {t(
            'Only logs after saving use this price. To correct an earlier price, edit the records below.'
          )}
        </p>

        <Separator />

        <VersionHistoryPanel
          channelId={channelId}
          open={open}
          exchangeRate={exchangeRate}
        />
      </div>
    </Dialog>
  )
}
