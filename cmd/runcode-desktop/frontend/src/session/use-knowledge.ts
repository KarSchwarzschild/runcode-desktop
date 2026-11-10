import { useEffect, useMemo, useState, useSyncExternalStore } from 'react'
import { Events, getSessionKnowledge, getSessionKnowledgeSnapshot, listKnowledgeBases, onEvent, setSessionKnowledge } from '@/core/bridge'
import { KnowledgeController, knowledgeSendBlock } from './knowledge-state'

export function useKnowledge(sessionId: string) {
  const [controller] = useState(() => new KnowledgeController({
    get: getSessionKnowledge,
    snapshot: getSessionKnowledgeSnapshot,
    list: listKnowledgeBases,
    set: setSessionKnowledge,
  }))
  const snapshot = useSyncExternalStore(controller.subscribe, controller.snapshot)
  useEffect(() => {
    void controller.activate(sessionId)
    return () => controller.dispose()
  }, [controller, sessionId])
  useEffect(() => onEvent(Events.PassportChanged, () => { void controller.resetAccount() }), [controller])
  // The session prop changes before effects run. Never paint the old session's
  // libraries during that render or enable send against its confirmed state.
  const state = snapshot.sessionId === sessionId
    ? snapshot
    : { ...snapshot, sessionId, libraries: [], pendingIds: null, items: [], previewItems: [], loaded: false, loading: !!sessionId, saving: false, refreshing: false, fromCache: false, error: '', listError: '' }
  const visibleLibraries = snapshot.sessionId === sessionId ? controller.visibleLibraries() : []
  const actions = useMemo(() => ({
    save: (ids: string[]) => controller.save(ids),
    toggle: (id: string) => controller.toggle(id),
    remove: (id: string) => controller.remove(id),
    search: (query: string) => { void controller.search(query) },
    searchDebounced: (query: string) => controller.queueSearch(query),
    openCatalog: () => { void controller.openCatalog() },
    refreshCatalog: () => { void controller.refreshCatalog() },
    closeCatalog: () => controller.closeCatalog(),
    cancelSearch: controller.cancelSearch,
    loadMore: () => { void controller.loadMore() },
    reload: () => { void controller.activate(sessionId) },
  }), [controller, sessionId])
  return { state, visibleLibraries, ...actions, sendBlock: knowledgeSendBlock(state) }
}

export type SessionKnowledge = Omit<ReturnType<typeof useKnowledge>, 'searchDebounced' | 'cancelSearch' | 'openCatalog' | 'refreshCatalog' | 'closeCatalog' | 'toggle' | 'remove'> & {
  searchDebounced?: (query: string) => void
  cancelSearch?: () => void
  openCatalog?: () => void
  refreshCatalog?: () => void
  closeCatalog?: () => void
  toggle?: (id: string) => Promise<void>
  remove?: (id: string) => Promise<void>
}
