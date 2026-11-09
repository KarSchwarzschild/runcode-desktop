import { effectiveKnowledgeIds, type KnowledgeState } from '@/session/knowledge-state'
import { useEffect, useRef, useState, type CSSProperties } from 'react'
import { Icon } from '@/ui/icons'
import { Popover } from '@/ui/popover'
import { useKnowledgePickerScroll } from './use-knowledge-picker-scroll'

type PickerProps = { open: boolean; state: KnowledgeState; locked: boolean; onClose: () => void; onSearch: (query: string) => void; onLoadMore: () => void; onToggle: (id: string) => void; onRetry: () => void; onRefresh?: () => void }
type StatusProps = { state: KnowledgeState; locked: boolean; onRetry: () => void }

/** 可视行数上限：限制列表视口，不截断已加载条目，也不改变最多选 5 库。 */
export const MAX_VISIBLE_KNOWLEDGE_ROWS = 7
/** 标准行高：标题 18 + 次行 16 + 上下各 6，与列表 max-height 共用同一常量。 */
export const KNOWLEDGE_ROW_HEIGHT_PX = 46
export const MAX_KNOWLEDGE_LIST_HEIGHT_PX = MAX_VISIBLE_KNOWLEDGE_ROWS * KNOWLEDGE_ROW_HEIGHT_PX

const testNotice = '测试数据，仅验证选择与保存，暂不参与回答'
const actionClass = 'rounded-field border border-line2 px-2 py-1 text-[12px] text-muted hover:text-ink hover:bg-surface2 disabled:opacity-40 disabled:cursor-default'

/** 保存提示：300ms 后出现，2s 升级慢提示；跨连续保存链只计一次。 */
function useDelayedSavingLabel(saving: boolean, open: boolean): string {
  const [label, setLabel] = useState('')
  useEffect(() => {
    if (!open || !saving) { setLabel(''); return }
    const show = window.setTimeout(() => setLabel('正在保存…'), 300)
    const slow = window.setTimeout(() => setLabel('保存较慢，仍可继续选择…'), 2000)
    return () => { window.clearTimeout(show); window.clearTimeout(slow) }
  }, [saving, open])
  return label
}

/** 满额且仍在保存时提示上限；保存完成后显示「已选择 N/max」，未满额仍用延迟保存文案或「已选择 N / max」。 */
export function knowledgePickerFooterLabel(selectedCount: number, maxSelection: number, savingLabel: string, saving = false): string {
  if (selectedCount >= maxSelection) {
    return saving ? `保存中...最多${maxSelection}个` : `已选择${selectedCount}/${maxSelection}`
  }
  return savingLabel || `已选择 ${selectedCount} / ${maxSelection}`
}

export function KnowledgePicker({ open, state, locked, onClose, onSearch, onLoadMore, onToggle, onRetry, onRefresh }: PickerProps) {
  // saving 不禁用整表：否则每次落盘都会 opacity 闪烁并挡住连选。
  const rowsBlocked = locked || state.loading || !state.loaded
  const selectedIds = effectiveKnowledgeIds(state)
  const selectedCount = selectedIds.length
  const demo = state.mode === 'demo'
  const hasMore = state.page > 0 && state.hasMore
  const visibleItems = state.items.length > 0 ? state.items : state.previewItems
  const showingPreview = state.items.length === 0 && state.previewItems.length > 0
  const savingLabel = useDelayedSavingLabel(state.saving, open)
  const inputRef = useRef<HTMLInputElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const headerRef = useRef<HTMLDivElement>(null)
  const footerRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const [panelMaxHeight, setPanelMaxHeight] = useState(440)
  // 首屏 SSR／静态渲染也要带上 7 行上限；真实可用高度由下方测量再夹紧。
  const [listMaxHeight, setListMaxHeight] = useState(MAX_KNOWLEDGE_LIST_HEIGHT_PX)
  useEffect(() => {
    if (!open) return
    inputRef.current?.focus()
    const footer = containerRef.current?.closest('footer')
    const measure = () => {
      const panelBudget = Math.max(80, Math.min(440, (footer?.getBoundingClientRect().top ?? window.innerHeight * 0.6) - 12))
      setPanelMaxHeight(panelBudget)
      const chrome =
        (headerRef.current?.offsetHeight ?? 0) +
        (footerRef.current?.offsetHeight ?? 0) +
        // 列表外的状态／错误条也占用面板高度，必须从列表预算里扣除。
        Array.from(containerRef.current?.querySelectorAll<HTMLElement>('[data-knowledge-chrome]') ?? [])
          .reduce((sum, node) => sum + node.offsetHeight, 0)
      const remaining = Math.max(KNOWLEDGE_ROW_HEIGHT_PX, panelBudget - chrome)
      setListMaxHeight(Math.min(MAX_KNOWLEDGE_LIST_HEIGHT_PX, remaining))
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !event.isComposing) { event.preventDefault(); onClose() }
    }
    measure()
    const observer = new ResizeObserver(measure)
    if (footer) observer.observe(footer)
    if (headerRef.current) observer.observe(headerRef.current)
    if (footerRef.current) observer.observe(footerRef.current)
    window.addEventListener('resize', measure)
    document.addEventListener('keydown', escape)
    return () => { observer.disconnect(); window.removeEventListener('resize', measure); document.removeEventListener('keydown', escape) }
  }, [open, onClose, state.error, state.unavailableReason, state.listError, state.refreshing, showingPreview, demo])
  useEffect(() => {
    if (listRef.current) listRef.current.scrollTop = 0
  }, [open, state.query])
  // 正常续页只走滚轮／键盘意图；错误重试仍用列表内的「重试」按钮。
  useKnowledgePickerScroll({ open, listRef, state, onLoadMore })
  return <div ref={containerRef} className="contents" style={{ '--knowledge-picker-height': `${panelMaxHeight}px` } as CSSProperties}><Popover open={open} onClose={onClose} placement="up-full" variant="panel" className="max-h-[var(--knowledge-picker-height)]">
    <div ref={headerRef} className="px-3 pt-2 pb-2 border-b border-line2 flex-none">
      <div className="flex items-center justify-between gap-2 text-[13px] text-ink font-medium">
        <span className="inline-flex items-center gap-2 leading-none"><Icon name="knowledge" size={16} className="shrink-0 block" />知识库{demo ? ' · 测试目录' : ''}</span>
        <span className="inline-flex items-center gap-1">
          {onRefresh && <button type="button" aria-label="刷新知识库" title="刷新知识库" disabled={locked || state.loading || state.listing || state.refreshing || !state.loaded} onClick={onRefresh} className="text-muted px-1 hover:text-ink disabled:opacity-40">↻</button>}
          <button type="button" aria-label="关闭知识库选择器" onClick={onClose} className="text-muted px-1">✕</button>
        </span>
      </div>
      {demo && <p className="mt-1 text-[11px] text-amberink" title="目录和文档数量均为测试值">{testNotice}</p>}
      <input ref={inputRef} type="search" aria-label={demo ? '按名称、简介或路径搜索知识库' : '搜索知识库名称'} placeholder={demo ? '按名称、简介或路径搜索' : '搜索知识库名称'} value={state.query} onChange={(event) => onSearch(event.target.value)} className="w-full mt-1.5 px-2.5 py-1.5 rounded-field border border-line2 bg-surface2 text-[12px] text-ink outline-none focus:border-primary" />
    </div>
    {state.refreshing && <p data-knowledge-chrome role="status" className="m-3 text-[12px] text-muted">正在更新知识库…</p>}
    {showingPreview && state.listing && <p data-knowledge-chrome role="status" className="m-3 text-[12px] text-muted">正在查询全部知识库…</p>}
    {!demo && state.unavailableReason && <p data-knowledge-chrome role="status" className="m-3 text-[12px] text-amberink">{state.unavailableReason}</p>}
    {state.error && <div data-knowledge-chrome role="alert" className="px-3 py-2 text-[12px] text-red">{state.error} <button type="button" className={actionClass} disabled={locked || state.loading || state.saving} onClick={onRetry}>重试读取</button></div>}
    <div
      ref={listRef}
      role="region"
      aria-label="知识库列表"
      tabIndex={0}
      className="overflow-y-auto min-h-0 overscroll-contain"
      style={{ maxHeight: listMaxHeight }}
    >
      {visibleItems.map((library) => {
        const selected = selectedIds.includes(library.id)
        const atLimit = !selected && selectedCount >= state.maxSelection
        const rowDisabled = rowsBlocked || atLimit || (!selected && (state.mode === 'unavailable' || !!library.unavailableReason))
        return <button
          type="button"
          aria-pressed={selected}
          disabled={rowDisabled}
          key={library.id}
          onClick={() => onToggle(library.id)}
          title={`${library.title}\n${library.description}\n目录：${library.path}${library.documentCountKnown ? `\n${library.documentCount} 篇文档` : ''}\nID：${library.id}`}
          style={{ height: KNOWLEDGE_ROW_HEIGHT_PX }}
          className={`w-full text-left px-3 box-border flex items-start gap-2 disabled:cursor-default disabled:opacity-50 ${selected ? 'bg-primarysoft hover:bg-primarysoft' : 'hover:bg-surface2'}`}
        >
          <span className="min-w-0 flex-1 py-[6px]">
            <span className="flex items-center gap-2 min-w-0">
              <Icon name="knowledge" size={14} className="shrink-0 block text-primary" />
              <span className={`min-w-0 truncate text-[13px] leading-[18px] ${selected ? 'text-primaryink' : 'text-ink'}`}>{library.title}</span>
            </span>
            <span className="mt-0 flex items-center gap-1 pl-[22px] text-[11px] leading-4 text-muted">
              <span className="min-w-0 truncate flex-1">{library.description}</span><span>·</span>
              <span className="min-w-0 truncate max-w-[45%]">{library.path}</span>
              {library.documentCountKnown && <><span>·</span><span className="flex-none">{library.documentCount} 篇文档</span></>}
            </span>
          </span>
        </button>
      })}
      {(state.listing || state.loading) && !state.refreshing && <p role="status" className="px-3 py-2 text-[12px] text-muted" style={{ minHeight: KNOWLEDGE_ROW_HEIGHT_PX }}>{visibleItems.length ? '正在读取更多知识库…' : '正在读取知识库…'}</p>}
      {state.listError && <div role="alert" className="px-3 py-2 text-[12px] text-red">{state.listError} <button type="button" className={actionClass} onClick={() => state.listError.includes('更新失败') && onRefresh ? onRefresh() : state.page > 0 ? onLoadMore() : onSearch(state.query)}>{state.listError.includes('更新失败') ? '重试更新' : '重试'}</button></div>}
      {!state.listing && !state.loading && !state.refreshing && !state.listError && visibleItems.length === 0 && <p className="px-3 py-3 text-[12px] text-muted" style={{ minHeight: KNOWLEDGE_ROW_HEIGHT_PX }}>{hasMore ? '本页暂无可显示的知识库，向下滚动继续查看' : state.mode === 'unavailable' ? '知识库暂不可用' : demo ? '没有匹配的测试目录' : state.query ? '没有匹配的知识库' : '暂无知识库'}</p>}
    </div>
    <div ref={footerRef} className="flex-none border-t border-line2 px-3 py-1.5 flex items-center justify-between text-[11px] text-muted">
      <span>{knowledgePickerFooterLabel(selectedCount, state.maxSelection, savingLabel, state.saving)}</span>
      {state.total >= 0 && <span>{state.total} {demo ? '个测试目录' : '个知识库'}</span>}
    </div>
  </Popover></div>
}

// References themselves live in the editor. This strip only reports persistence/errors.
// 单库访问权待确认由控制器自动重验，不展示「无法确认访问权限」或手动「重新确认」按钮。
export function KnowledgeStatus({ state, locked, onRetry }: StatusProps) {
  // KB-UI-002：删除“每次提问重新检索…”常驻说明；仅保留保存／错误／会话级不可用反馈。
  // 测试目录仍在列表入口保留“测试数据”标记，不在此常驻横幅重复。
  const hasFeedback = state.loading || state.saving || !!state.error || !!state.unavailableReason
  if (!hasFeedback) return null
  return <div className="border-x border-line2 bg-surface px-4 pb-2 text-[11px] text-muted">
    {state.loading || state.saving ? <span role="status">{state.saving ? '正在保存知识库选择…' : '正在读取会话知识库…'}</span> : null}
    {state.unavailableReason && <p role="status" className="text-amberink">{state.unavailableReason}</p>}
    {state.error && <div role="alert" className="mt-1 text-[12px] text-red">{state.error} <button type="button" disabled={locked || state.loading || state.saving} className={actionClass} onClick={onRetry}>重试读取</button></div>}
  </div>
}
