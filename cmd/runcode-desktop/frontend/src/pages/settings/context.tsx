import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { errText, getModelContextSettings, saveModelContextSettings, type CustomModel, type ModelContextView, type PassportModel } from '@/core/bridge'
import { CONTEXT_FIELDS, contextDraft, contextModelCandidates, modelContextKey, parseContextDraft, type ContextDraft } from '@/core/model-context'
import { Banner, InlineError } from '@/ui/feedback'
import { FIELD_CLS, LABEL_CLS, SelectField } from '@/ui/fields'
import { BTN, BTN_PRIMARY } from '@/ui/tokens'
import { Section } from './section'

export function ContextSection({ tenantId, platform, custom, disabled = false }: {
  tenantId: string
  platform: PassportModel[]
  custom: CustomModel[]
  disabled?: boolean
}) {
  const [selected, setSelected] = useState('')
  const [view, setView] = useState<ModelContextView | null>(null)
  const [draft, setDraft] = useState<ContextDraft>(() => contextDraft({}))
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState('')
  const generation = useRef(0)
  const invalidate = useCallback(() => { generation.current++ }, [])
  const candidates = useMemo(() => contextModelCandidates(platform, custom, tenantId), [platform, custom, tenantId])
  const choice = candidates.find((c) => modelContextKey(c.ref) === selected) ?? candidates[0]
  const key = choice ? modelContextKey(choice.ref) : ''
  // The parent re-creates the platform/custom arrays on every refresh, so
  // `choice` gets a new identity while still naming the same model. Loading
  // follows `key` only; otherwise a background refresh would wipe the draft the
  // user is typing and invalidate a pending save.
  const choiceRef = useRef(choice)
  useEffect(() => { choiceRef.current = choice })

  useEffect(() => {
    const gen = ++generation.current
    setView(null)
    setSaved(false)
    setBusy(false)
    setError('')
    setDraft(contextDraft({}))
    const current = choiceRef.current
    if (key && current && !disabled) {
      getModelContextSettings(current.ref).then((next) => {
        if (gen !== generation.current) return
        setView(next)
        setDraft(contextDraft(next.overrides))
      }).catch((e: unknown) => { if (gen === generation.current) setError(errText(e)) })
    }
    return invalidate
  }, [key, disabled, invalidate])

  async function save(reset = false) {
    if (!view) return
    const gen = generation.current
    setBusy(true)
    setSaved(false)
    setError('')
    try {
      const overrides = reset ? {} : parseContextDraft(draft)
      const next = await saveModelContextSettings({ model: view.model, overrides })
      if (gen !== generation.current) return
      setView(next)
      setDraft(contextDraft(next.overrides))
      setSaved(true)
    } catch (e) { if (gen === generation.current) setError(errText(e)) }
    finally { if (gen === generation.current) setBusy(false) }
  }

  return <Section title="模型上下文配置" hint="独立保存 · 下次新建、重开或重新选择该模型时生效">
    <p className="text-[12px] text-muted">平台按模型提供默认值，本机可逐项覆盖；原统一设置已弃用，不迁移旧值。保存不会中断正在执行的回合，也不会修改其他会话。</p>
    <label className={LABEL_CLS}>配置模型
      <SelectField value={key} disabled={disabled || candidates.length === 0} onChange={(next) => {
        invalidate()
        setView(null)
        setSelected(next)
      }}>
        {candidates.length === 0 && <option value="">暂无模型，请先登录或添加自定义模型</option>}
        {candidates.map((c) => <option key={modelContextKey(c.ref)} value={modelContextKey(c.ref)}>{c.label}</option>)}
      </SelectField>
    </label>
    {key && !view && !error && <p className="text-[12px] text-muted">正在加载模型默认值…</p>}
    {view && <>
      {view.warning && <Banner tone="warning">{view.warning}</Banner>}
      {CONTEXT_FIELDS.map(({ key: field, label, hint }) => <label key={field} className={LABEL_CLS}>
        <span>{label}<span className="ml-2 text-[11px]">{draft[field].trim() ? '本机覆盖' : view.defaults ? '跟随服务端' : view.model.kind === 'platform' ? '服务端未声明' : '引擎 / 供应商默认'}</span></span>
        <input className={FIELD_CLS} inputMode="numeric" value={draft[field]} disabled={busy || disabled}
          onChange={(e) => { setSaved(false); setDraft((value) => ({ ...value, [field]: e.target.value })) }} placeholder="留空继承默认值" />
        <span className="text-[12px]">默认：{view.defaults?.[field] ?? '未声明'} · 已保存值：{!view.defaults && view.model.kind === 'platform' && view.overrides[field] == null ? '未配置' : view.effective[field] || (field === 'maxOutputTokens' ? '引擎 / 供应商默认' : '0（关闭）')}</span>
        <span className="text-[12px]">{hint}</span>
      </label>)}
      <div className="flex flex-wrap items-center gap-2">
        <button type="button" className={`${BTN} ${BTN_PRIMARY}`} disabled={busy || disabled} onClick={() => void save()}>{busy ? '正在保存…' : '保存此模型配置'}</button>
        <button type="button" className={BTN} disabled={busy || disabled} onClick={() => void save(true)}>{view.model.kind === 'platform' ? '恢复服务端默认' : '清除本机覆盖'}</button>
        {saved && <span role="status" className="text-[12px] text-muted">已保存；重新选择该模型或重开会话后生效。</span>}
      </div>
    </>}
    {error && <InlineError>{error}</InlineError>}
  </Section>
}
