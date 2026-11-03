import { useCallback, useEffect, useRef, useState } from 'react'
import { errText, Events, grantDirectoryAccess, listDirectoryAccess, onEnvelope, pickWorkspaceFolder, revokeDirectoryAccess, type DirectoryAccessGrant, type DirectoryAccessView } from '@/core/bridge'
import { createActionScope } from './action-scope'

export function useDirectoryAccess(sessionID: string) {
  const scope = useRef(createActionScope())
  scope.current.focus(sessionID)
  const [view, setView] = useState<DirectoryAccessView | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [directory, setDirectory] = useState('')
  const [access, setAccess] = useState('read')
  const [duration, setDuration] = useState('session')
  const serial = useRef(0)
  const mounted = useRef(false)
  useEffect(() => {
    mounted.current = true
    const active = scope.current
    active.activate()
    return () => { mounted.current = false; active.deactivate() }
  }, [])

  const refresh = useCallback(async () => {
    const current = scope.current.capture()
    const ticket = ++serial.current
    if (!sessionID) return
    try {
      const next = await listDirectoryAccess(sessionID)
      if (current() && ticket === serial.current) { setView(next); setError('') }
    } catch (e) { if (current() && ticket === serial.current) setError(errText(e)) }
  }, [sessionID])
  useEffect(() => {
    setView(null); setError(''); setBusy(false); setDirectory(''); setAccess('read'); setDuration('session')
    void refresh()
    return onEnvelope(Events.DirectoryAccessChanged, (env) => {
      if (env.sessionId === sessionID) void refresh()
    })
  }, [sessionID, refresh])

  async function pick() {
    const current = scope.current.capture()
    try { const path = await pickWorkspaceFolder(); if (current() && path) setDirectory(path) }
    catch (e) { if (current()) setError(errText(e)) }
  }
  async function change(grant?: DirectoryAccessGrant, confirmRoot = false) {
    const current = scope.current.capture()
    if (!sessionID || busy) return
    setBusy(true); setError('')
    const ticket = ++serial.current
    try {
      const next = grant
        ? await revokeDirectoryAccess({ sessionId: sessionID, directory: grant.directory, access: grant.access, scope: grant.scope })
        : await grantDirectoryAccess({ sessionId: sessionID, directory, access, scope: duration, confirmRoot })
      if (current() && ticket === serial.current) { setView(next); if (!grant) setDirectory('') }
      if (current()) void refresh()
    } catch (e) { if (current()) setError(errText(e)) }
    finally { if (current() && mounted.current) setBusy(false) }
  }
  return { view, error, busy, directory, setDirectory, access, setAccess, duration, setDuration, refresh, pick, change }
}
export type DirectoryAccessState = ReturnType<typeof useDirectoryAccess>
