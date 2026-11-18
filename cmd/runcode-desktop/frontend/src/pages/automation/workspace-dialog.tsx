import { useEffect, useRef, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { Icon } from '@/ui/icons'
import { isComposingKey } from '@/ui/keys'
import { useAutomation } from '@/session/use-automation'
import { AutomationManager } from './index'

export function AutomationWorkspaceDialog({ open, onClose }: {
  open: boolean
  onClose: () => void
}) {
  const panelRef = useRef<HTMLDivElement>(null)
  const auto = useAutomation(open)

  useEffect(() => {
    if (!open) return
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const frame = requestAnimationFrame(() => panelRef.current?.focus())
    return () => {
      cancelAnimationFrame(frame)
      opener?.focus()
    }
  }, [open])

  if (!open) return null

  function closeTop() {
    if (auto.createOpen) {
      auto.closeCreate()
      return
    }
    onClose()
  }

  function onKeyDown(e: ReactKeyboardEvent) {
    if (isComposingKey(e)) return
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      closeTop()
      return
    }
    if (e.key === 'Tab' && panelRef.current) trapTab(e, panelRef.current)
  }

  return (
    <div className="fixed inset-0 z-30 bg-ink/30 flex items-center justify-center">
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label="自动化任务"
        tabIndex={-1}
        className="automation-window bg-surface rounded-card shadow-modal flex flex-col outline-none overflow-hidden"
        onKeyDown={onKeyDown}
      >
        <header className="flex items-center gap-3 px-5 h-14 border-b border-line flex-none">
          <h2 className="m-0 text-[18px] font-bold text-ink flex-1">自动化任务</h2>
          <button
            type="button"
            aria-label="关闭"
            onClick={closeTop}
            className="w-8 h-8 inline-flex items-center justify-center rounded-btn text-muted hover:bg-surface2 hover:text-ink"
          >
            <Icon name="close" size={16} />
          </button>
        </header>
        <AutomationManager auto={auto} />
      </div>
    </div>
  )
}

function trapTab(e: ReactKeyboardEvent, root: HTMLElement) {
  const items = [...root.querySelectorAll<HTMLElement>('button, [href], input, select, textarea')]
    .filter((el) => !el.hasAttribute('disabled') && el.tabIndex !== -1 && !el.closest('[hidden]'))
  if (items.length === 0) {
    e.preventDefault()
    return
  }
  const first = items[0]
  const last = items[items.length - 1]
  const active = document.activeElement
  if (e.shiftKey && (active === first || !root.contains(active))) {
    e.preventDefault()
    last.focus()
  } else if (!e.shiftKey && (active === last || !root.contains(active))) {
    e.preventDefault()
    first.focus()
  }
}
