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
import { useTranslation } from 'react-i18next'
import { formatNumber } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/dialog'

interface TokensDetailDialogProps {
  promptTokens: number
  completionTokens: number
  cacheReadTokens: number
  cacheCreationTokens: number
  totalTokens: number
}

function TokenRow({ label, value }: { label: string; value: number }) {
  return (
    <div className='flex items-center justify-between gap-6 py-1.5 text-sm'>
      <span className='text-muted-foreground'>{label}</span>
      <span className='tabular-nums'>{formatNumber(value)}</span>
    </div>
  )
}

/** Breaks a row's `total_tokens` down into its four raw components on click. */
export function TokensDetailDialog({
  promptTokens,
  completionTokens,
  cacheReadTokens,
  cacheCreationTokens,
  totalTokens,
}: TokensDetailDialogProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      trigger={
        <Button
          type='button'
          variant='link'
          className='h-auto p-0 tabular-nums'
        >
          {formatNumber(totalTokens)}
        </Button>
      }
      title={t('Total Tokens')}
      contentClassName='sm:max-w-xs'
    >
      <div className='flex flex-col divide-y'>
        <TokenRow label={t('Input Tokens')} value={promptTokens} />
        <TokenRow label={t('Output Tokens')} value={completionTokens} />
        <TokenRow label={t('Cache Read Tokens')} value={cacheReadTokens} />
        <TokenRow
          label={t('Cache Creation Tokens')}
          value={cacheCreationTokens}
        />
      </div>
    </Dialog>
  )
}
