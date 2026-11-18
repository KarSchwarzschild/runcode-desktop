import { useCallback, useEffect, useState } from 'react'
import { MODE_LABEL } from '@/core/permission-modes'
import { Banner } from '@/ui/feedback'
import type { AutomationController } from '@/session/use-automation'
import { browserTimeZone, defaultAutomationDraft } from './draft'
import { TaskForm } from './task-form'
import { TaskList } from './task-list'

export function AutomationManager({ auto, onDirty, onCancelCreate, onOpenSettings }: {
  auto: AutomationController
  onDirty?: (dirty: boolean) => void
  onCancelCreate?: () => void
  onOpenSettings?: () => void
}) {
  const [initial, setInitial] = useState<ReturnType<typeof defaultAutomationDraft> | null>(null)
  const markDirty = useCallback((dirty: boolean) => onDirty?.(dirty), [onDirty])
  const zone = browserTimeZone()

  useEffect(() => {
    if (!auto.createOpen) {
      setInitial(null)
      return
    }
    setInitial((current) => current ?? defaultAutomationDraft(new Date(), zone.zone, auto.defaults?.recentWorkspaces ?? []))
  }, [auto.createOpen, auto.defaults, zone.zone])

  const modelName = auto.defaults?.model?.name
  const permission = auto.defaults?.permissionMode || auto.defaults?.model?.permissionMode || ''
  const permissionLabel = MODE_LABEL[permission] || permission || '未设置'
  const canSave = !!auto.defaults?.available && !!modelName

  return (
    <div className="relative flex-1 min-h-0">
      <div className="h-full overflow-y-auto overscroll-contain">
        {auto.notice && <div className="px-5 pt-4"><Banner tone="warning">{auto.notice}</Banner></div>}
        <TaskList auto={auto} />
      </div>
      {auto.createOpen && initial && (
        <aside className="automation-drawer absolute inset-y-0 right-0 z-10 bg-surface border-l border-line2 shadow-card overflow-y-auto" aria-label="新建自动化任务">
          <TaskForm
            initial={initial}
            recent={auto.defaults?.recentWorkspaces ?? []}
            modelSummary={modelName ? `${modelName} · ${permissionLabel}` : undefined}
            zoneNote={zone.fallback ? '无法识别当前时区，已使用 Asia/Shanghai' : undefined}
            canSave={canSave}
            onSave={auto.saveCreate}
            onCancel={onCancelCreate ?? auto.closeCreate}
            onDirty={markDirty}
            onOpenSettings={onOpenSettings}
          />
        </aside>
      )}
    </div>
  )
}
