import { useEffect, useState } from 'react'
import { InlineError, Banner } from '@/ui/feedback'
import { FIELD_CLS, LABEL_CLS, SelectField } from '@/ui/fields'
import { BTN, BTN_PRIMARY } from '@/ui/tokens'
import { ConfirmDialog } from '@/ui/confirm-dialog'
import type { DirectoryAccessState } from '@/session/use-directory-access'

export function DirectoryAccessSection({ sessionID, state: d }: { sessionID: string; state: DirectoryAccessState }) {
  const [confirmRoot, setConfirmRoot] = useState(false)
  useEffect(() => { setConfirmRoot(false) }, [sessionID])
  const add = () => {
    const path = d.directory.trim().split(String.fromCharCode(92)).join('/')
    const root = path === '/' || path.length === 3 && path[1] === ':' && path[2] === '/' || path.startsWith('//') && path.split('/').filter(Boolean).length === 2
    if (root) setConfirmRoot(true)
    else void d.change()
  }
  return (
    <section className="flex flex-col gap-4 rounded-card border border-line2 bg-surface p-5">
      <div className="flex items-center justify-between gap-3">
        <h3 className="m-0 text-[16px] font-semibold text-ink">目录授权</h3>
        <button className={BTN} disabled={!sessionID || d.busy} onClick={() => void d.refresh()}>刷新</button>
      </div>
      <p className="m-0 text-[13px] text-muted leading-relaxed">授权只扩展文件访问范围，不切换工作区、不加载目录中的配置，也不代替命令执行审批。只读不含写入；读写包含读取。撤销后新读取立即检查，已显示的内容无法收回。</p>
      {!sessionID && <p className="m-0 text-[13px] text-muted">打开一条会话后可管理目录授权。</p>}
      {sessionID && <>
        <div className="text-[12px] text-muted break-all">当前会话：{sessionID}<br />主工作区：{d.view?.workspace ?? '读取中…'}</div>
        <div className="flex gap-2">
          <input className={`${FIELD_CLS} flex-1 min-w-0`} value={d.directory} onChange={(e) => d.setDirectory(e.target.value)} placeholder="选择目录或输入完整路径" disabled={d.busy} aria-label="授权目录" />
          <button className={BTN} onClick={() => void d.pick()} disabled={d.busy}>选择目录</button>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <label className={LABEL_CLS}>访问权限<SelectField value={d.access} onChange={d.setAccess} disabled={d.busy}><option value="read">只读</option><option value="write">读写</option></SelectField></label>
          <label className={LABEL_CLS}>授权范围<SelectField value={d.duration} onChange={d.setDuration} disabled={d.busy}><option value="session">本次会话（关闭即失效）</option><option value="project">本项目（持久保存）</option></SelectField></label>
        </div>
        <div><button className={`${BTN} ${BTN_PRIMARY}`} disabled={d.busy || !d.directory.trim() || !d.view} onClick={add}>{d.busy ? '处理中…' : '授权此目录及子目录'}</button></div>
        {d.view && !d.view.grants?.length && <p className="m-0 text-[13px] text-muted">尚无额外目录授权。主工作区与应用自有目录继续按原规则处理。</p>}
        {d.view?.grants?.map((g) => (
          <div key={`${g.scope}:${g.access}:${g.directory}`} className="border-t border-line pt-3 flex gap-3 items-start">
            <div className="flex-1 min-w-0 text-[13px] text-ink break-all">
              <div className="font-mono">{g.directory}</div>
              <div className="text-[12px] text-muted mt-1">{g.access === 'write' ? '读写' : '只读'} · {g.scope === 'project' ? '本项目' : '本次会话'} · 包含所有子目录</div>
              {!!g.coveredBy?.length && <div className="text-[12px] text-muted mt-1">仍受其他授权覆盖：{g.coveredBy.join('；')}。仅撤销本条不会移除其他授权。</div>}
            </div>
            <button className={BTN} onClick={() => void d.change(g)} disabled={d.busy}>撤销</button>
          </div>
        ))}
      </>}
      {d.error && <InlineError>{d.error}</InlineError>}
      {confirmRoot && <ConfirmDialog title="确认根目录授权" confirmLabel="确认授权" onCancel={() => setConfirmRoot(false)} onConfirm={() => { setConfirmRoot(false); void d.change(undefined, true) }} message={<Banner tone="warning">将允许访问 {d.directory} 及其所有子目录，范围可能覆盖整个磁盘。权限：{d.access === 'write' ? '读写' : '只读'}；范围：{d.duration === 'project' ? '本项目持久保存' : '本次会话'}。</Banner>} />}
    </section>
  )
}
