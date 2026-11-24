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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DateTimePicker } from '@/components/datetime-picker'
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
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { translateServerText } from '@/lib/server-error-message'
import { dateToUnixTimestamp, formatDateTimeObject } from '@/lib/time'

import {
  createChannelCostVersion,
  deleteChannelCostVersion,
  getChannelCostVersions,
  updateChannelCostVersion,
} from '../api'
import type { ChannelCostVersion } from '../types'

type CostPricingMode = 'ratio' | 'discount'
type VersionTiming = 'initial' | 'dated'
/** Which form is open: the add form (optionally preset to the initial price) or a record being edited. */
type FormTarget =
  | { kind: 'add'; initial: boolean }
  | { kind: 'edit'; id: number }

/** The version api rejects with the backend's translated reason; fall back to nothing. */
function errorMessage(error: unknown): string | undefined {
  return error instanceof Error ? error.message || undefined : undefined
}

/**
 * "0.8 × ¥7.20" for discount mode, "¥2.50" for ratio mode, 'Not set' when the
 * mode's own price is missing.
 *
 * An empty cost_mode means ratio (see model.ChannelCostVersion.CostMode).
 * Testing `!== 'discount'` rather than `=== 'ratio'` keeps a channel priced at
 * ¥2.5/$1 from reading as unpriced.
 */
function describeVersion(v: ChannelCostVersion, t: TFunction): string {
  if (v.cost_mode === 'discount') {
    return v.cost_discount > 0
      ? `${v.cost_discount} × ¥${v.exchange_rate.toFixed(2)}`
      : t('Not set')
  }
  return v.cost_ratio > 0 ? `¥${v.cost_ratio.toFixed(2)}` : t('Not set')
}

type VersionFormProps = {
  channelId: number
  /** The record being edited; absent when adding. */
  version?: ChannelCostVersion
  /** The channel already has an initial price, so a second one can't be added. */
  hasInitial: boolean
  /** Preselect "Initial" when adding (the "Set initial price" shortcut). */
  initialSelected: boolean
  defaultExchangeRate: number
  onDone: () => void
  onCancel: () => void
}

/**
 * Adds a record or edits one. A record's effective time decides which logs it
 * prices; the initial price has none and covers every log before the earliest
 * dated record. The settlement rate is frozen with the record, and only
 * discount pricing uses it.
 */
function VersionForm(props: VersionFormProps) {
  const { t } = useTranslation()
  const editing = props.version !== undefined
  const [timing, setTiming] = useState<VersionTiming>(() => {
    if (props.version) {
      return props.version.effective_from === 0 ? 'initial' : 'dated'
    }
    return props.initialSelected && !props.hasInitial ? 'initial' : 'dated'
  })
  const [effectiveFrom, setEffectiveFrom] = useState<Date | undefined>(() =>
    props.version && props.version.effective_from > 0
      ? new Date(props.version.effective_from * 1000)
      : undefined
  )
  const [mode, setMode] = useState<CostPricingMode>(
    props.version?.cost_mode === 'discount' ? 'discount' : 'ratio'
  )
  const [value, setValue] = useState(() => {
    if (!props.version) return ''
    return String(
      props.version.cost_mode === 'discount'
        ? props.version.cost_discount
        : props.version.cost_ratio
    )
  })
  const [rate, setRate] = useState(() => {
    if (props.version?.cost_mode === 'discount') {
      return String(props.version.exchange_rate)
    }
    return Number.isFinite(props.defaultExchangeRate)
      ? String(props.defaultExchangeRate)
      : ''
  })
  const [note, setNote] = useState(props.version?.note ?? '')

  const parsedValue = Number(value)
  const parsedRate = Number(rate)
  // Must be > 0, not >= 0: the API refuses a zero ratio/discount because no log
  // under such a record could ever be priced.
  const isValid =
    (timing === 'initial' || effectiveFrom !== undefined) &&
    value.trim() !== '' &&
    Number.isFinite(parsedValue) &&
    parsedValue > 0 &&
    (mode !== 'discount' || (Number.isFinite(parsedRate) && parsedRate > 0))

  const save = useMutation({
    mutationFn: () => {
      const body = {
        effective_from:
          timing === 'initial' ? 0 : dateToUnixTimestamp(effectiveFrom as Date),
        cost_mode: mode,
        cost_ratio: mode === 'ratio' ? parsedValue : 0,
        cost_discount: mode === 'discount' ? parsedValue : 0,
        exchange_rate: mode === 'discount' ? parsedRate : 0,
        note: note.trim(),
      }
      return props.version
        ? updateChannelCostVersion(props.version.id, body)
        : createChannelCostVersion(props.channelId, body)
    },
    onSuccess: () => {
      toast.success(
        editing ? t('Price version updated') : t('Price version added')
      )
      props.onDone()
    },
    onError: (err) => {
      toast.error(
        errorMessage(err) ||
          (editing
            ? t('Failed to update price version')
            : t('Failed to add price version'))
      )
    },
  })

  return (
    <div
      role='group'
      aria-label={editing ? t('Edit price record') : t('Add price record')}
      className='bg-muted/40 flex flex-col gap-2 rounded-md border p-2'
    >
      <div className='flex flex-col gap-1'>
        <Label className='text-xs' id='cost-version-timing-label'>
          {t('Price effective from')}
        </Label>
        {editing ? (
          timing === 'initial' && (
            <p className='text-sm font-medium'>{t('Initial')}</p>
          )
        ) : (
          <ToggleGroup
            value={[timing]}
            onValueChange={(values) => {
              if (values[0] === 'initial' || values[0] === 'dated') {
                setTiming(values[0])
              }
            }}
            variant='outline'
            size='sm'
            aria-labelledby='cost-version-timing-label'
          >
            <ToggleGroupItem value='initial' disabled={props.hasInitial}>
              {t('Initial')}
            </ToggleGroupItem>
            <ToggleGroupItem value='dated'>
              {t('Specific time')}
            </ToggleGroupItem>
          </ToggleGroup>
        )}
        {timing === 'dated' && (
          <DateTimePicker
            id='cost-version-from'
            value={effectiveFrom}
            onChange={setEffectiveFrom}
            className='w-full'
          />
        )}
        {timing === 'initial' && (
          <p className='text-muted-foreground text-xs'>
            {t('Applies to every log before the earliest dated record.')}
          </p>
        )}
        {!editing && props.hasInitial && (
          <p className='text-muted-foreground text-xs'>
            {t(
              'This channel already has an initial price. Edit it in the list instead.'
            )}
          </p>
        )}
      </div>

      <div className='grid grid-cols-2 gap-2'>
        <div className='flex flex-col gap-1'>
          <Label className='text-xs' htmlFor='cost-version-mode'>
            {t('Pricing Mode')}
          </Label>
          <Select
            value={mode}
            onValueChange={(v) =>
              setMode(v === 'discount' ? 'discount' : 'ratio')
            }
            items={[
              { value: 'ratio', label: t('Cost Ratio (CNY per USD)') },
              { value: 'discount', label: t('Cost Discount') },
            ]}
          >
            <SelectTrigger id='cost-version-mode' className='w-full'>
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
        <div className='flex flex-col gap-1'>
          <Label className='text-xs' htmlFor='cost-version-value'>
            {mode === 'discount'
              ? t('Cost Discount')
              : t('Cost Ratio (CNY per USD)')}
          </Label>
          <Input
            id='cost-version-value'
            type='number'
            min={0.01}
            step={0.01}
            value={value}
            onChange={(e) => setValue(e.target.value)}
          />
        </div>
      </div>

      {mode === 'discount' && (
        <div className='flex flex-col gap-1'>
          <Label className='text-xs' htmlFor='cost-version-rate'>
            {t('Settlement exchange rate')}
          </Label>
          <Input
            id='cost-version-rate'
            type='number'
            min={0.01}
            step={0.1}
            value={rate}
            onChange={(e) => setRate(e.target.value)}
          />
          <p className='text-muted-foreground text-xs'>
            {t('Frozen with this version so historical cost never drifts.')}
          </p>
        </div>
      )}

      <div className='flex flex-col gap-1'>
        <Label className='text-xs' htmlFor='cost-version-note'>
          {t('Note')}
        </Label>
        <Input
          id='cost-version-note'
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder={t('Optional')}
        />
      </div>

      <div className='flex justify-end gap-2'>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={props.onCancel}
          disabled={save.isPending}
        >
          {t('Cancel')}
        </Button>
        <Button
          type='button'
          size='sm'
          onClick={() => save.mutate()}
          disabled={!isValid || save.isPending}
        >
          {editing ? t('Save') : t('Add')}
        </Button>
      </div>
    </div>
  )
}

/**
 * The channel's price records. Saving the price above the panel records the
 * price from now on; this panel adds, corrects and deletes records, including
 * the initial price.
 */
export function VersionHistoryPanel(props: {
  channelId: number
  open: boolean
  exchangeRate: number
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [formTarget, setFormTarget] = useState<FormTarget | null>(null)
  const [pendingDelete, setPendingDelete] = useState<ChannelCostVersion | null>(
    null
  )

  const {
    data: versions = [],
    isLoading,
    error,
  } = useQuery({
    queryKey: ['cost-versions', props.channelId],
    queryFn: () => getChannelCostVersions(props.channelId),
    enabled: props.open,
    // A business refusal is a plain Error the global predicate would retry four
    // times; retrying cannot change it, and reopening the dialog is a cheap
    // manual retry for a genuine network blip.
    retry: false,
  })
  const hasInitial = versions.some((v) => v.effective_from === 0)

  // Cost figures are derived from the versions, so the report is stale too.
  const invalidate = () => {
    void queryClient.invalidateQueries({
      queryKey: ['cost-versions', props.channelId],
    })
    void queryClient.invalidateQueries({ queryKey: ['cost'] })
  }
  const formDone = () => {
    setFormTarget(null)
    invalidate()
  }

  // The api layer silences the global handlers, so onError here is the only
  // toast — and it carries the backend's reason instead of a generic failure.
  const remove = useMutation({
    mutationFn: (versionId: number) => deleteChannelCostVersion(versionId),
    onSuccess: () => {
      toast.success(t('Price version deleted'))
      setPendingDelete(null)
      invalidate()
    },
    onError: (err) => {
      toast.error(errorMessage(err) || t('Failed to delete price version'))
    },
  })

  const adding = formTarget?.kind === 'add'

  return (
    <div className='flex flex-col gap-2'>
      <div className='flex items-center justify-between'>
        <Label>{t('Version history')}</Label>
        {!adding && (
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='h-7 gap-1 text-xs'
            onClick={() => setFormTarget({ kind: 'add', initial: false })}
          >
            <Plus className='size-3.5' />
            {t('Add historical price')}
          </Button>
        )}
      </div>

      {formTarget?.kind === 'add' && (
        <VersionForm
          channelId={props.channelId}
          hasInitial={hasInitial}
          initialSelected={formTarget.initial}
          defaultExchangeRate={props.exchangeRate}
          onDone={formDone}
          onCancel={() => setFormTarget(null)}
        />
      )}

      {isLoading && (
        <p className='text-muted-foreground text-xs'>{t('Loading...')}</p>
      )}
      {!isLoading && Boolean(error) && (
        <p className='text-destructive text-xs'>
          {errorMessage(error) || t('Failed to load price versions')}
        </p>
      )}
      {!isLoading && !error && (
        <ul className='flex flex-col gap-1 text-xs'>
          {versions.map((v) =>
            formTarget?.kind === 'edit' && formTarget.id === v.id ? (
              <li key={v.id}>
                <VersionForm
                  channelId={props.channelId}
                  version={v}
                  hasInitial={hasInitial}
                  initialSelected={false}
                  defaultExchangeRate={props.exchangeRate}
                  onDone={formDone}
                  onCancel={() => setFormTarget(null)}
                />
              </li>
            ) : (
              <li
                key={v.id}
                className='hover:bg-muted/50 flex items-center justify-between gap-2 rounded-md px-2 py-1.5'
              >
                <div className='flex min-w-0 flex-col leading-tight'>
                  {/* Date + time, not date alone: two records effective the same
                      day would otherwise read as identical rows. */}
                  <span className='font-medium tabular-nums'>
                    {v.effective_from === 0
                      ? t('Initial')
                      : formatDateTimeObject(new Date(v.effective_from * 1000))}
                  </span>
                  {v.note && (
                    <span className='text-muted-foreground truncate'>
                      {translateServerText(t, v.note)}
                    </span>
                  )}
                </div>
                <div className='flex items-center gap-1'>
                  <span className='text-muted-foreground me-1 tabular-nums'>
                    {describeVersion(v, t)}
                  </span>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    className='size-6'
                    aria-label={t('Edit')}
                    onClick={() => setFormTarget({ kind: 'edit', id: v.id })}
                  >
                    <Pencil className='size-3.5' />
                  </Button>
                  {/* The initial record is deletable too: logs before the
                      earliest remaining record use that record's price. The
                      API only refuses a channel's last record. */}
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    className='size-6'
                    aria-label={t('Delete')}
                    onClick={() => setPendingDelete(v)}
                  >
                    <Trash2 className='size-3.5' />
                  </Button>
                </div>
              </li>
            )
          )}
          {!hasInitial && versions.length > 0 && !adding && (
            <li className='text-muted-foreground flex items-center justify-between gap-2 rounded-md border border-dashed px-2 py-1.5'>
              <span>
                {t(
                  'No initial price. Logs before the earliest record use its price.'
                )}
              </span>
              <Button
                type='button'
                variant='link'
                size='xs'
                className='h-auto shrink-0 p-0 text-xs'
                onClick={() => setFormTarget({ kind: 'add', initial: true })}
              >
                {t('Set initial price')}
              </Button>
            </li>
          )}
        </ul>
      )}

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null)
        }}
        title={t('Delete this price record?')}
        desc={t(
          'Logs in the period it covers will use the price of the neighboring record.'
        )}
        confirmText={t('Delete')}
        destructive
        isLoading={remove.isPending}
        handleConfirm={() => {
          if (pendingDelete) remove.mutate(pendingDelete.id)
        }}
      />
    </div>
  )
}
