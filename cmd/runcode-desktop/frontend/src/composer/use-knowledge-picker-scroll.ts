import { useEffect, useRef, type RefObject } from 'react'
import type { KnowledgeState } from '@/session/knowledge-state'

/** 距底部多少像素内视为“接近底部”，触发一次续页机会。 */
export const KNOWLEDGE_LOAD_MORE_BOTTOM_PX = 24
/** 同一滚轮／触控板惯性手势的结束间隔：最后一次向下事件后再等这么久才允许新手势。 */
export const KNOWLEDGE_WHEEL_GESTURE_IDLE_MS = 180

export type KnowledgePickerScrollOptions = {
  open: boolean
  listRef: RefObject<HTMLDivElement | null>
  state: KnowledgeState
  onLoadMore: () => void
}

function nearBottom(list: HTMLElement): boolean {
  // 无溢出时自然滚轮不会产生 scroll，但仍需允许短页／空页下滚续页。
  if (list.scrollHeight <= list.clientHeight + 1) return true
  return list.scrollHeight - list.scrollTop - list.clientHeight <= KNOWLEDGE_LOAD_MORE_BOTTOM_PX
}

function canRequestMore(state: KnowledgeState, open: boolean): boolean {
  return open
    && !!state.sessionId
    && state.page > 0
    && state.hasMore
    && !state.loading
    && !state.listing
    && !state.listError
}

/**
 * 用向下滚轮／键盘意图驱动 KnowledgeController.loadMore。
 * 不做自动哨兵扫描：一次手势最多消耗一次续页机会；请求在途时丢弃输入，不排队。
 */
export function useKnowledgePickerScroll({ open, listRef, state, onLoadMore }: KnowledgePickerScrollOptions): void {
  const stateRef = useRef(state)
  const openRef = useRef(open)
  const onLoadMoreRef = useRef(onLoadMore)
  stateRef.current = state
  openRef.current = open
  onLoadMoreRef.current = onLoadMore

  useEffect(() => {
    const list = listRef.current
    if (!open || !list) return

    let gestureArmed = true
    let idleTimer: ReturnType<typeof setTimeout> | undefined

    const clearIdle = () => {
      if (idleTimer !== undefined) {
        clearTimeout(idleTimer)
        idleTimer = undefined
      }
    }

    const markGestureActivity = () => {
      clearIdle()
      idleTimer = setTimeout(() => {
        gestureArmed = true
        idleTimer = undefined
      }, KNOWLEDGE_WHEEL_GESTURE_IDLE_MS)
    }

    const tryLoadMore = () => {
      if (!gestureArmed) return
      if (!nearBottom(list)) return
      // 在途或出错时只丢弃，不消耗新手势；也不在请求结束后自动补发。
      if (!canRequestMore(stateRef.current, openRef.current)) return
      // 先本地卸武装，避免 listing 回写前同一手势连发多次 onLoadMore。
      gestureArmed = false
      onLoadMoreRef.current()
    }

    const onWheel = (event: WheelEvent) => {
      if (event.deltaY <= 0) return
      // 无溢出或已贴底时吞掉默认滚动，避免把滚轮传给背后的对话区。
      if (list.scrollHeight <= list.clientHeight + 1 || nearBottom(list)) {
        event.preventDefault()
      }
      tryLoadMore()
      markGestureActivity()
    }

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.isComposing) return
      if (event.key !== 'ArrowDown' && event.key !== 'PageDown' && event.key !== 'End') return
      tryLoadMore()
      markGestureActivity()
    }

    list.addEventListener('wheel', onWheel, { passive: false })
    list.addEventListener('keydown', onKeyDown)
    return () => {
      clearIdle()
      list.removeEventListener('wheel', onWheel)
      list.removeEventListener('keydown', onKeyDown)
    }
    // query／session／代次变化时重建监听，清掉旧手势计时器，避免串到新查询。
  }, [open, listRef, state.sessionId, state.contextVersion, state.query])
}
