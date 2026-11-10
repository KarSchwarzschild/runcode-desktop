// DiffPanel renders the red/green review of one edit (baseline vs the turn's latest
// content), fetched via ReviewEdit. Reuses the diff-line CSS classes (.cl.diff_*).
import { useEffect, useState } from 'react'
import { Icon } from '@/ui/icons'
import { basename } from '@/core/paths'
import { errText, reviewEdit, type EditDiff } from '@/core/bridge'
import { IconBtn } from './icon-btn'
import { InlineError } from '@/ui/feedback'

export function DiffPanel({ sessionID, snapshotId, relPath, onClose }: { sessionID?: string; snapshotId: string; relPath: string; onClose: () => void }) {
  const [diff, setDiff] = useState<EditDiff | null>(null)
  const [err, setErr] = useState('')
  const name = basename(relPath)
  useEffect(() => {
    let ignore = false
    setDiff(null)
    setErr('')
    // 必须使用标签保存的来源会话；缺失时明确报错，不回退到焦点。
    void (sessionID ? reviewEdit(sessionID, snapshotId) : Promise.reject(new Error('缺少来源会话')))
      .then((d) => { if (!ignore) setDiff(d) })
      .catch((e) => { if (!ignore) setErr(errText(e)) })
    return () => { ignore = true }
  }, [sessionID, snapshotId])
  return (
    <div className="flex flex-col h-full min-h-0 bg-surface">
      <div className="flex-none flex items-center gap-1.5 h-[44px] px-2.5 border-b border-line2">
        <Icon name="diff" size={15} className="flex-none text-muted" />
        <span className="flex-none text-[11px] text-faint bg-inset rounded px-1.5 py-0.5 mr-auto">审核 · {name}</span>
        <IconBtn name="win-close" title="关闭" onClick={onClose} />
      </div>
      <div className="flex-1 min-h-0 overflow-auto py-2 font-mono text-[13px] leading-[1.6]">
        {err && <InlineError variant="text" className="p-6">{err}</InlineError>}
        {diff && (diff.lines ?? []).length === 0 && <div className="p-6 text-[13px] text-muted">无差异。</div>}
        {diff && (diff.lines ?? []).map((l, i) => (
          <div key={i} className={(l.stream || '').startsWith('diff') ? `cl ${l.stream}` : 'px-2.5 whitespace-pre text-muted'}>{l.text}</div>
        ))}
      </div>
    </div>
  )
}
