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
/**
 * TaskProgressRing — circular progress indicator for seedance video tasks.
 * Ring color follows task status (success=green, failure=red, running=blue,
 * queued=amber, unknown=grey). Terminal states show the localized status text
 * in the ring center (成功/失败); running states show the percent.
 */
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

const STATUS_RING_COLORS: Record<string, string> = {
  SUCCESS: 'stroke-emerald-500',
  FAILURE: 'stroke-rose-500',
  IN_PROGRESS: 'stroke-blue-500',
  SUBMITTED: 'stroke-amber-500',
  QUEUED: 'stroke-amber-500',
  NOT_START: 'stroke-muted-foreground/40',
  UNKNOWN: 'stroke-muted-foreground/40',
}

const STATUS_TEXT_COLORS: Record<string, string> = {
  SUCCESS: 'text-emerald-600 dark:text-emerald-400',
  FAILURE: 'text-rose-600 dark:text-rose-400',
  IN_PROGRESS: 'text-blue-600 dark:text-blue-400',
  SUBMITTED: 'text-amber-600 dark:text-amber-400',
  QUEUED: 'text-amber-600 dark:text-amber-400',
}

interface TaskProgressRingProps {
  status: string
  /** e.g. "50%" — SUCCESS always renders as 100% */
  progress?: string
  /** ring diameter in px */
  size?: number
  className?: string
}

export function TaskProgressRing(props: TaskProgressRingProps) {
  const { t } = useTranslation()
  const { status, progress, size = 34, className } = props
  let pct = parseInt(progress || '0', 10) || 0
  if (status === 'SUCCESS') pct = 100
  pct = Math.max(0, Math.min(100, pct))

  // 终态环心显示中文状态（成功/失败），进行中显示百分比。
  const centerLabel =
    status === 'SUCCESS'
      ? t('Success')
      : status === 'FAILURE'
        ? t('Failed')
        : `${pct}%`

  const strokeWidth = 3
  const radius = (size - strokeWidth) / 2
  const circumference = 2 * Math.PI * radius
  const dashOffset = circumference * (1 - pct / 100)

  return (
    <div
      className={cn('relative inline-flex shrink-0', className)}
      style={{ width: size, height: size }}
      role='progressbar'
      aria-valuenow={pct}
      aria-valuemin={0}
      aria-valuemax={100}
    >
      <svg width={size} height={size} className='-rotate-90'>
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill='none'
          strokeWidth={strokeWidth}
          className='stroke-muted/60'
        />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill='none'
          strokeWidth={strokeWidth}
          strokeLinecap='round'
          strokeDasharray={circumference}
          strokeDashoffset={dashOffset}
          className={cn(
            'transition-[stroke-dashoffset]',
            STATUS_RING_COLORS[status] || STATUS_RING_COLORS.UNKNOWN
          )}
        />
      </svg>
      <span
        className={cn(
          'absolute inset-0 flex items-center justify-center text-[9px] font-semibold tabular-nums',
          STATUS_TEXT_COLORS[status] || 'text-muted-foreground'
        )}
      >
        {centerLabel}
      </span>
    </div>
  )
}
