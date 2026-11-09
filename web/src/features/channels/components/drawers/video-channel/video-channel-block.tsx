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
import { SlidersHorizontal } from 'lucide-react'
import { useId, type ReactNode } from 'react'

import { IconBadge } from '@/components/ui/icon-badge'

type VideoChannelBlockProps = {
  title: string
  children: ReactNode
}

// 样式与渠道抽屉的 SubHeading 一致。SubHeading 是 channel-mutate-drawer.tsx 的私有组件，
// 从这里引用会形成循环依赖，搬出来又要改上游文件，所以在 fork 目录里组合一份。
export function VideoChannelBlock(props: VideoChannelBlockProps) {
  const headingId = useId()

  return (
    <div
      role='group'
      aria-labelledby={headingId}
      className='border-border/60 flex flex-col gap-3 border-y py-4'
    >
      <div className='flex items-center gap-2'>
        <IconBadge size='xs'>
          <SlidersHorizontal aria-hidden='true' />
        </IconBadge>
        <h4
          id={headingId}
          className='text-muted-foreground text-xs font-medium tracking-wide uppercase'
        >
          {props.title}
        </h4>
      </div>
      {props.children}
    </div>
  )
}
