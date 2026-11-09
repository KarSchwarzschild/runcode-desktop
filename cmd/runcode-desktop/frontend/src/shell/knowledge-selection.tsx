import { useEffect, useRef, useState } from 'react'
import { errText, openKnowledgeDirectory } from '@/core/bridge'
import { effectiveKnowledgeIds, type KnowledgeState } from '@/session/knowledge-state'
import type { SessionKnowledge } from '@/session/use-knowledge'
import { Icon } from '@/ui/icons'
import { Popover } from '@/ui/popover'
import { NO_DRAG } from '@/ui/tokens'

type Props = {
  knowledge: SessionKnowledge
  onFocusComposer: () => void
  onNotify?: (message: string) => void
}

/** 顶部「知识库 N ▾」：只展示即时已选集合，取消走控制器，不持有第二份权威状态。 */
export function KnowledgeSelection({ knowledge, onFocusComposer, onNotify }: Props) {
  const { state } = knowledge
  const ids = effectiveKnowledgeIds(state)
  const count = ids.length
  const [open, setOpen] = useState(false)
  const [openingId, setOpeningId] = useState<string | null>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (count === 0) setOpen(false)
  }, [count])
  if (count === 0 && !state.saving && !state.error && !state.loading) return null
  const libraries = knowledge.visibleLibraries?.length ? knowledge.visibleLibraries : visibleFromState(state, ids)
  const canOpenDirectory = state.mode === 'real' && !!state.sessionId
  function closeAndFocus() {
    setOpen(false)
    onFocusComposer()
  }
  async function openDirectory(libraryId: string, title: string) {
    if (!canOpenDirectory || !state.sessionId || openingId) return
    setOpeningId(libraryId)
    try {
      // 后端现验权限后再用系统浏览器打开目录 URL（如 …/knowledge-base/<id>/data-collection）。
      await openKnowledgeDirectory(state.sessionId, libraryId)
    } catch (error) {
      onNotify?.(errText(error) || `无法打开知识库「${title || libraryId}」`)
    } finally {
      setOpeningId(null)
    }
  }
  return (
    <div className="relative flex items-center gap-1 text-[12px]" style={NO_DRAG} data-knowledge-selection>
      {(state.saving || state.loading || state.error) && (
        <span role={state.error ? 'alert' : 'status'} className={`max-w-[160px] truncate ${state.error ? 'text-red' : 'text-muted'}`} title={state.error || undefined}>
          {state.error ? state.error : state.saving ? '保存中…' : '读取中…'}
          {state.error && (
            <button type="button" className="ml-1 underline text-red" onClick={() => void knowledge.reload()}>重试</button>
          )}
        </span>
      )}
      {count > 0 && (
        <>
          <button
            ref={triggerRef}
            type="button"
            style={NO_DRAG}
            aria-expanded={open}
            aria-haspopup="listbox"
            aria-label={`已选 ${count} 个知识库`}
            className="inline-flex items-center gap-1 px-2 py-1 rounded-field text-ink hover:bg-surface2"
            onClick={() => setOpen((value) => !value)}
          >
            <Icon name="knowledge" size={14} className="shrink-0 block text-primary" />
            <span className="whitespace-nowrap leading-none">知识库 {count}</span>
            <Icon name="chevron-down" size={12} className="shrink-0 block text-muted" />
          </button>
          <Popover open={open} onClose={closeAndFocus} placement="down-right" className="w-[280px] max-h-[320px] overflow-y-auto p-1">
            <ul role="listbox" aria-label="已选知识库">
              {libraries.map((library) => {
                const title = library.title || '名称待恢复'
                const opening = openingId === library.id
                return (
                  <li key={library.id} className="flex items-center gap-1 px-2 py-1.5 rounded-field hover:bg-surface2">
                    <Icon name="knowledge" size={14} className="shrink-0 block text-primary" />
                    <button
                      type="button"
                      style={NO_DRAG}
                      disabled={!canOpenDirectory || !!openingId}
                      title={canOpenDirectory ? `在浏览器中打开「${title}」` : title}
                      aria-label={canOpenDirectory ? `打开知识库 ${title}` : title}
                      className={`min-w-0 flex-1 text-left truncate text-[13px] leading-none disabled:opacity-60 ${canOpenDirectory ? 'text-primaryink hover:underline' : 'text-ink'}`}
                      onClick={() => { void openDirectory(library.id, title) }}
                    >
                      {opening ? '正在打开…' : title}
                    </button>
                    <button
                      type="button"
                      style={NO_DRAG}
                      aria-label={`删除 ${title}`}
                      className="flex-none px-1 text-[12px] leading-none text-red hover:opacity-80"
                      onClick={(event) => {
                        event.stopPropagation()
                        void (knowledge.remove ? knowledge.remove(library.id) : knowledge.save(ids.filter((id) => id !== library.id)))
                      }}
                    >
                      删除
                    </button>
                  </li>
                )
              })}
            </ul>
          </Popover>
        </>
      )}
    </div>
  )
}

function visibleFromState(state: KnowledgeState, ids: string[]) {
  const byId = new Map(state.libraries.map((library) => [library.id, library]))
  return ids.map((id) => byId.get(id) ?? { id, title: '名称待恢复', description: '', path: '', source: '', documentCount: 0, documentCountKnown: false })
}
