import { useEffect, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { errText, type AutomationContext, type AutomationTask, type AutomationTaskDraft } from '@/core/bridge'
import { Banner, InlineError } from '@/ui/feedback'
import { FIELD_CLS, LABEL_CLS, SelectField } from '@/ui/fields'
import { isComposingKey } from '@/ui/keys'
import { BTN, BTN_PRIMARY } from '@/ui/tokens'
import { EXECUTION_MODES, draftFromTask, scheduleForSave, validateAutomationDraft } from './draft'
import { RunList } from './run-list'
import { ScheduleFields } from './schedule-fields'
import { formatNextRun, runIsActive, taskStatusLine } from './task-list'
import { WorkspacePicker } from './workspace-picker'

export function TaskDetail({ task, context, recent, pending, onSave, onSetPaused, onClose, onDirty, onOpenRun }: {
  task: AutomationTask
  context: AutomationContext
  recent: string[]
  pending: boolean
  onSave: (draft: AutomationTaskDraft, expectedRevision: number) => Promise<AutomationTask>
  onSetPaused: (paused: boolean) => Promise<void>
  onClose: () => void
  onDirty?: (dirty: boolean) => void
  onOpenRun: (taskId: string, runId: string) => void
}) {
  const [tab, setTab] = useState<'rules' | 'runs'>('rules')
  const [draft, setDraft] = useState(() => draftFromTask(task))
  const [saved, setSaved] = useState(() => draftFromTask(task))
  const [baseline, setBaseline] = useState(task.revision)
  const [conflict, setConflict] = useState(false)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const dirty = JSON.stringify(draft) !== JSON.stringify(saved)
  const locked = runIsActive(task.lastRun?.status)

  useEffect(() => { onDirty?.(dirty) }, [dirty, onDirty])

  useEffect(() => {
    if (task.revision === baseline) return
    if (dirty) {
      setConflict(true)
      return
    }
    const next = draftFromTask(task)
    setDraft(next)
    setSaved(next)
    setBaseline(task.revision)
    setConflict(false)
  }, [task, baseline, dirty])

  async function save() {
    if (saving || locked || pending) return
    const next: AutomationTaskDraft = {
      ...draft,
      name: draft.name.trim(),
      prompt: draft.prompt.trim(),
      workspace: draft.workspace.trim(),
      schedule: scheduleForSave(draft.schedule),
    }
    const found = validateAutomationDraft(next)
    setErrors(found)
    if (Object.keys(found).length > 0) return
    setSaving(true)
    setError('')
    try {
      const updated = await onSave(next, baseline)
      const fresh = draftFromTask(updated)
      setDraft(fresh)
      setSaved(fresh)
      setBaseline(updated.revision)
      setConflict(false)
    } catch (e) {
      const text = errText(e)
      setError(text)
      if (text.includes('请刷新')) setConflict(true)
    } finally {
      setSaving(false)
    }
  }

  function discard() {
    setDraft(saved)
    setErrors({})
    setError('')
  }

  function refresh() {
    const next = draftFromTask(task)
    setDraft(next)
    setSaved(next)
    setBaseline(task.revision)
    setConflict(false)
    setError('')
  }

  function onKeyDown(e: ReactKeyboardEvent) {
    if (isComposingKey(e)) return
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      void save()
    }
  }

  const continueRun = task.state === 'paused' && runIsActive(task.lastRun?.status)

  return (
    <div className="flex flex-col min-h-full" onKeyDown={onKeyDown}>
      <div className="px-4 py-3 border-b border-line flex items-center gap-2">
        <h3 className="m-0 flex-1 text-[15px] font-semibold text-ink truncate">{task.name}</h3>
        <button type="button" className={BTN} onClick={onClose}>关闭</button>
      </div>
      <div className="px-4 pt-3 flex gap-2" role="tablist">
        <button type="button" role="tab" aria-selected={tab === 'rules'} className={tab === 'rules' ? 'text-primaryink font-semibold text-[13px]' : 'text-muted text-[13px]'} onClick={() => setTab('rules')}>规则</button>
        <button type="button" role="tab" aria-selected={tab === 'runs'} className={tab === 'runs' ? 'text-primaryink font-semibold text-[13px]' : 'text-muted text-[13px]'} onClick={() => setTab('runs')}>任务运行记录</button>
      </div>
      {tab === 'runs' ? (
        <div className="flex-1 overflow-y-auto px-4 py-4">
          <RunList taskId={task.id} context={context} onOpen={onOpenRun} />
        </div>
      ) : (
        <form className="flex flex-col flex-1 min-h-0" onSubmit={(e) => { e.preventDefault(); void save() }}>
          <div className="flex-1 overflow-y-auto px-4 py-4 flex flex-col gap-3">
            <p className="m-0 text-[12px] text-muted">{taskStatusLine(task)} · {formatNextRun(task.nextRunAtUTC, task.schedule?.timeZone || 'Asia/Shanghai')}</p>
            {continueRun && <Banner tone="warning">已暂停 · 本次执行继续完成</Banner>}
            {locked && <Banner tone="warning">任务正在执行，规则暂时不能修改。暂停只停止以后的调度。</Banner>}
            {conflict && (
              <Banner tone="warning" title="任务已发生变化，请刷新后重试">
                <button type="button" className="text-primaryink underline" onClick={refresh}>刷新</button>
              </Banner>
            )}
            <label className={LABEL_CLS}>
              任务名称
              <input aria-label="任务名称" disabled={locked} value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} className={`${FIELD_CLS} w-full`} />
              {errors.name && <InlineError variant="text">{errors.name}</InlineError>}
            </label>
            <label className={LABEL_CLS}>
              任务内容
              <textarea aria-label="任务内容" disabled={locked} rows={5} value={draft.prompt} onChange={(e) => setDraft({ ...draft, prompt: e.target.value })} className={`${FIELD_CLS} w-full resize-y`} />
            </label>
            <label className={LABEL_CLS}>
              执行位置
              <SelectField aria-label="执行位置" disabled={locked} value={draft.executionMode} onChange={(executionMode) => setDraft({ ...draft, executionMode })}>
                {EXECUTION_MODES.map((mode) => <option key={mode.value} value={mode.value}>{mode.label}</option>)}
              </SelectField>
            </label>
            <div className={LABEL_CLS}>
              <span>关联工作区</span>
              <WorkspacePicker value={draft.workspace} recent={recent} disabled={locked} onChange={(workspace) => setDraft({ ...draft, workspace })} />
            </div>
            <ScheduleFields value={draft.schedule} disabled={locked} onChange={(schedule) => setDraft({ ...draft, schedule })} />
            {error && <Banner tone="danger">{error}</Banner>}
          </div>
          <div className="flex-none px-4 py-3 border-t border-line flex justify-end gap-2">
            {task.state === 'paused' ? (
              <button type="button" className={BTN} disabled={pending} onClick={() => void onSetPaused(false).catch((e) => setError(errText(e)))}>
                {pending ? '恢复中' : '恢复'}
              </button>
            ) : task.state === 'enabled' ? (
              <button type="button" className={BTN} disabled={pending} onClick={() => void onSetPaused(true).catch((e) => setError(errText(e)))}>
                {pending ? '暂停中' : '暂停'}
              </button>
            ) : null}
            {dirty && <button type="button" className={BTN} onClick={discard}>放弃</button>}
            {dirty && <button type="submit" className={`${BTN} ${BTN_PRIMARY}`} disabled={saving || locked || pending}>{saving ? '保存中' : '保存修改'}</button>}
          </div>
        </form>
      )}
    </div>
  )
}
