import { BTN } from '@/ui/tokens'
import { TaskList } from './task-list'
import type { AutomationController } from '@/session/use-automation'

// 列表和创建抽屉都活在管理窗里。步骤 8 会换掉这里的表单占位。
export function AutomationManager({ auto }: { auto: AutomationController }) {
  return (
    <div className="relative flex-1 min-h-0">
      <div className="h-full overflow-y-auto overscroll-contain">
        <TaskList auto={auto} />
      </div>
      {auto.createOpen && (
        <aside className="automation-drawer absolute inset-y-0 right-0 z-10 bg-surface border-l border-line2 shadow-card flex flex-col" aria-label="新建自动化任务">
          <div className="px-4 py-3 border-b border-line text-[15px] font-semibold text-ink">新建自动化任务</div>
          <p className="px-4 py-4 text-[13px] text-muted leading-relaxed">创建表单将在下一步接入。关闭此抽屉不会保存任务，也不会发起执行。</p>
          <div className="mt-auto px-4 py-3 border-t border-line">
            <button type="button" className={BTN} onClick={auto.closeCreate}>关闭</button>
          </div>
        </aside>
      )}
    </div>
  )
}
