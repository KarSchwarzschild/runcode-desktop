// The budget is the current session's effective auto-compaction setting, not a
// catalog draft or a claim about the model's physical window. null means there
// is no session; zero explicitly disables automatic compaction.
import { Icon } from '@/ui/icons'
import { NO_DRAG } from '@/ui/tokens'
import { fmtTokens } from '@/core/format'

export function ContextMeter({
  used,
  budget,
  model,
  estimated,
  onCompact,
  compacting,
  busy,
}: {
  used: number
  budget: number | null
  model?: string
  estimated?: boolean
  onCompact: () => void
  compacting: boolean
  busy: boolean
}) {
  const limited = budget !== null && budget > 0
  const pct = limited ? Math.floor((used / budget) * 100) : 0
  const near = limited && used / budget >= 0.8
  const bar = pct >= 100 ? 'bg-red' : near ? 'bg-amber' : 'bg-primary'
  const tone = pct >= 100 ? 'text-red' : near ? 'text-amberink' : 'text-ink'
  const approx = estimated ? '≈' : ''
  const title = [
    model ? `当前会话模型：${model}` : '',
    budget === null
      ? '尚未建立会话，建立后显示实际生效的上下文预算'
      : limited
        ? `本会话上下文占用 ${used.toLocaleString()} / ${budget.toLocaleString()} tokens
达到预算 80% 自动压缩（约 ${Math.ceil(budget * 0.8).toLocaleString()} tokens），目标约 60%`
        : `本会话上下文占用 ${used.toLocaleString()} tokens · 自动压缩关闭
未启用应用预算，不代表模型上下文无限`,
    budget !== null && estimated ? '占用为校准估算值，每次模型请求实时刷新，并非供应商精确计数' : '',
    limited ? '此处为本会话已生效的应用预算，不是厂商硬容量；修改配置后需新建、重开或重新选择模型生效' : '',
  ].filter(Boolean).join('\n')

  return (
    <div className="inline-flex items-center gap-2 whitespace-nowrap" style={NO_DRAG}>
      <span className="inline-flex items-center gap-1.5" title={title}>
        <span>上下文</span>
        {budget === null ? (
          <span className="text-faint">待建立会话</span>
        ) : limited ? (
          <>
            <span className={`tabular-nums ${tone}`}>
              <b className="font-semibold">{approx}{fmtTokens(used)}</b>
              <span className="text-faint"> / {fmtTokens(budget)}</span>
            </span>
            <span
              role="progressbar"
              aria-label="上下文占用比例"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={Math.min(100, pct)}
              aria-valuetext={`${approx}${pct}% · ${used.toLocaleString()} / ${budget.toLocaleString()} tokens`}
              className="w-[52px] h-[6px] rounded-full bg-surface2 border border-line2 overflow-hidden inline-block align-middle"
            >
              <span className={`block h-full ${bar} transition-[width]`} style={{ width: Math.min(100, pct) + '%' }} />
            </span>
            <span className={`text-[11px] tabular-nums ${tone}`}>{pct}%</span>
          </>
        ) : (
          <span className="tabular-nums">
            <b className="text-ink font-semibold">{approx}{fmtTokens(used)}</b>
            <span className="text-faint"> · 自动压缩关闭</span>
          </span>
        )}
      </span>
      <button
        onClick={onCompact}
        disabled={budget === null || busy || compacting}
        title="压缩上下文：把较早的对话总结成摘要、保留最近几轮原文（磁盘记录保持完整）"
        className="inline-flex items-center gap-1 px-2 py-1 rounded-lg border border-line2 text-[12px] text-muted hover:text-ink hover:bg-surface2 transition cursor-pointer disabled:opacity-40 disabled:cursor-default"
      >
        <Icon name="compress" size={13} /> {compacting ? '压缩中…' : '压缩'}
      </button>
    </div>
  )
}
