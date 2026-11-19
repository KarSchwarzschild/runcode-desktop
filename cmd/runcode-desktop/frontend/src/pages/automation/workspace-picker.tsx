import { useState } from 'react'
import { errText, pickWorkspaceFolder } from '@/core/bridge'
import { Banner } from '@/ui/feedback'
import { FIELD_CLS } from '@/ui/fields'
import { Icon } from '@/ui/icons'

// 历史目录来自后端的 RecentWorkspaces。选中或取消都只改表单里的路径，不开会话。
export function WorkspacePicker({ value, recent, onChange, disabled = false }: {
  value: string
  recent: string[]
  onChange: (path: string) => void
  disabled?: boolean
}) {
  const [query, setQuery] = useState('')
  const [error, setError] = useState('')
  const needle = query.trim().toLowerCase()
  const matches = recent.filter((path) => path && (!needle || path.toLowerCase().includes(needle)))

  async function browse() {
    if (disabled) return
    setError('')
    try {
      const picked = await pickWorkspaceFolder()
      if (!picked) return
      onChange(picked)
    } catch (e) {
      setError(errText(e))
    }
  }

  return (
    <div className="rounded-card border border-line2 bg-surface overflow-hidden" title={value || undefined}>
      <div className="p-2.5 border-b border-line">
          <input
            aria-label="搜索历史工作区"
            value={query}
            disabled={disabled}
            placeholder="搜索历史工作区..."
          onChange={(e) => setQuery(e.target.value)}
          className={`${FIELD_CLS} w-full`}
        />
      </div>
      <div className="max-h-[220px] overflow-y-auto py-1">
        {matches.map((path) => (
            <button
            key={path}
            type="button"
            title={path}
            disabled={disabled}
            onClick={() => onChange(path)}
            className={`w-full text-left px-3 py-2 flex items-center gap-2 hover:bg-surface2 ${path === value ? 'text-primaryink' : 'text-ink'}`}
          >
            <Icon name="folder" size={14} />
            <span className="font-mono text-[13px] truncate">{path}</span>
          </button>
        ))}
        {matches.length === 0 && <div className="px-3 py-3 text-center text-[12px] text-muted">没有匹配的工作区</div>}
        <button type="button" disabled={disabled} onClick={() => void browse()} className="w-full text-left px-3 py-2 text-[13px] text-primaryink border-t border-line">
          浏览其他目录...
        </button>
      </div>
      {error && <Banner tone="danger" className="m-2">{error}</Banner>}
    </div>
  )
}
