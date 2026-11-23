// 更多 → 邮箱与通信录：草稿 / 通信录 / 分组 / 发件箱。
// 账号配置只跳转设置，本页不收集安全密码；编辑保存不发信。
import { useEffect, useMemo, useState } from 'react'
import { errText, type MailDraftView, type PrepareMailSendResult } from '@/core/bridge'
import { useMail, type MailTab } from '@/session/use-mail'
import { BTN, BTN_PRIMARY } from '@/ui/tokens'
import { InlineError } from '@/ui/feedback'
import { FIELD_CLS, LABEL_CLS } from '@/ui/fields'
import { PageShell, Placeholder } from '@/ui/layout'
import { ConfirmDialog } from '@/ui/confirm-dialog'
import { isComposingKey } from '@/ui/keys'
import { MailSafePreview } from './safe-preview'

function joinAddresses(addrs: { address: string }[] | null | undefined): string {
  return (addrs ?? []).map((a) => a.address).filter(Boolean).join(', ')
}

function parseAddresses(raw: string): { address: string }[] {
  return raw.split(/[,;，；\s]+/).map((s) => s.trim()).filter(Boolean).map((address) => ({ address }))
}

export function MailPage({
  onOpenSettings,
  initialDraftId,
}: {
  onOpenSettings: () => void
  initialDraftId?: string
}) {
  const mail = useMail()
  const [tab, setTab] = useState<MailTab>('drafts')
  const [activeDraftId, setActiveDraftId] = useState<string | null>(initialDraftId ?? null)
  const [draft, setDraft] = useState<MailDraftView | null>(null)
  const [editSubject, setEditSubject] = useState('')
  const [editBody, setEditBody] = useState('')
  const [editTo, setEditTo] = useState('')
  const [previewMode, setPreviewMode] = useState(false)
  const [saving, setSaving] = useState(false)
  const [leaveOpen, setLeaveOpen] = useState(false)
  const [pendingTab, setPendingTab] = useState<MailTab | null>(null)
  const [pendingDraftId, setPendingDraftId] = useState<string | null>(null)
  const [contactName, setContactName] = useState('')
  const [contactEmail, setContactEmail] = useState('')
  const [groupName, setGroupName] = useState('')
  const [importText, setImportText] = useState('')
  const [importSkip, setImportSkip] = useState<string[]>([])
  const [sendPrep, setSendPrep] = useState<PrepareMailSendResult | null>(null)
  const [confirming, setConfirming] = useState(false)

  const dirty = !!draft && (
    editSubject !== draft.subject
    || editBody !== draft.bodyMarkdown
    || editTo !== joinAddresses(draft.to)
  )

  useEffect(() => {
    if (initialDraftId) setActiveDraftId(initialDraftId)
  }, [initialDraftId])

  useEffect(() => {
    if (!activeDraftId) {
      setDraft(null)
      return
    }
    let cancelled = false
    void mail.loadDraft(activeDraftId).then((d) => {
      if (cancelled) return
      setDraft(d)
      setEditSubject(d.subject)
      setEditBody(d.bodyMarkdown)
      setEditTo(joinAddresses(d.to))
      setPreviewMode(false)
    }).catch((e) => {
      if (!cancelled) mail.setError(errText(e))
    })
    return () => { cancelled = true }
    // 仅随草稿 id 换载；mail.loadDraft 稳定于 store。
    // eslint-disable-next-line react-hooks/exhaustive-deps -- intentional id-only reload
  }, [activeDraftId])

  function discardLocalEdits() {
    if (!draft) return
    setEditSubject(draft.subject)
    setEditBody(draft.bodyMarkdown)
    setEditTo(joinAddresses(draft.to))
  }

  function applyLeave() {
    discardLocalEdits()
    if (pendingTab) setTab(pendingTab)
    if (pendingDraftId) setActiveDraftId(pendingDraftId)
    setPendingTab(null)
    setPendingDraftId(null)
    setLeaveOpen(false)
  }

  function requestTab(next: MailTab) {
    if (dirty && tab === 'drafts') {
      setPendingTab(next)
      setPendingDraftId(null)
      setLeaveOpen(true)
      return
    }
    setTab(next)
  }

  function requestDraft(id: string) {
    if (dirty && activeDraftId && activeDraftId !== id) {
      setPendingDraftId(id)
      setPendingTab(null)
      setLeaveOpen(true)
      return
    }
    setActiveDraftId(id)
  }

  async function saveDraft(current = draft): Promise<MailDraftView | null> {
    if (!current || saving) return null
    setSaving(true)
    mail.setError('')
    try {
      const saved = await mail.persistDraft({
        id: current.id,
        expectedRevision: current.revision,
        accountId: current.accountId || mail.settings?.account?.id || '',
        subject: editSubject,
        bodyMarkdown: editBody,
        to: parseAddresses(editTo),
        cc: current.cc,
        bcc: current.bcc,
        sendMode: current.sendMode || 'together',
        dispatchMode: current.dispatchMode || 'immediate',
        scheduledLocal: current.scheduledLocal,
        timeZone: current.timeZone,
      })
      setDraft(saved)
      setEditSubject(saved.subject)
      setEditBody(saved.bodyMarkdown)
      setEditTo(joinAddresses(saved.to))
      return saved
    } catch (e) {
      // CAS/校验失败保留本地编辑，不覆盖。
      mail.setError(errText(e) || '保存失败，编辑已保留')
      return null
    } finally {
      setSaving(false)
    }
  }

  async function checkAndSend() {
    if (!draft) return
    if (dirty) {
      const saved = await saveDraft()
      if (!saved) return
      await runPrepare(saved)
      return
    }
    await runPrepare(draft)
  }

  async function runPrepare(d: MailDraftView) {
    mail.setError('')
    try {
      const prep = await mail.prepareSend(d.id, d.revision)
      setSendPrep(prep)
    } catch (e) {
      mail.setError(errText(e) || '检查失败')
    }
  }

  async function confirmPrepared() {
    if (!sendPrep || confirming) return
    setConfirming(true)
    mail.setError('')
    try {
      await mail.confirmSend(sendPrep.confirmationId)
      setSendPrep(null)
      if (draft) {
        const refreshed = await mail.loadDraft(draft.id)
        setDraft(refreshed)
      }
    } catch (e) {
      mail.setError(errText(e) || '确认失败，未发送')
    } finally {
      setConfirming(false)
    }
  }

  async function newDraft() {
    mail.setError('')
    try {
      const d = await mail.createDraft()
      setActiveDraftId(d.id)
      setTab('drafts')
    } catch (e) {
      mail.setError(errText(e))
    }
  }

  async function addContact() {
    try {
      await mail.addContact(contactName, contactEmail)
      setContactName('')
      setContactEmail('')
    } catch (e) {
      mail.setError(errText(e))
    }
  }

  async function addGroup() {
    try {
      await mail.addGroup(groupName)
      setGroupName('')
    } catch (e) {
      mail.setError(errText(e))
    }
  }

  async function runImport() {
    const rows = importText.split(/\r?\n/).map((line) => {
      const parts = line.split(/[,，\t]/).map((s) => s.trim()).filter(Boolean)
      const [name, ...emails] = parts
      return { name: name || '', emails }
    }).filter((r) => r.name || r.emails.length)
    try {
      const skip = await mail.importContacts(rows)
      setImportSkip(skip)
      if (skip.length < rows.length) setImportText('')
    } catch (e) {
      mail.setError(errText(e))
    }
  }

  const tabs = useMemo(() => ([
    { id: 'drafts' as const, label: `草稿（${mail.drafts.length}）` },
    { id: 'contacts' as const, label: '通信录' },
    { id: 'groups' as const, label: '分组' },
    { id: 'outbox' as const, label: '发件箱（0）' },
  ]), [mail.drafts.length])

  return (
    <PageShell
      title="邮箱与通信录"
      hint="在此编辑草稿与通信录；账号与安全密码请到设置。"
      width={960}
      action={(
        <button type="button" className={BTN} onClick={onOpenSettings}>
          前往邮箱设置
        </button>
      )}
    >
      {mail.error && <InlineError>{mail.error}</InlineError>}
      {!mail.hasAccount && (
        <div className="rounded-field border border-amber/40 bg-amber/10 px-3 py-2 text-[13px] text-amberink" data-testid="mail-no-account">
          尚未接入邮箱账号。仍可起草与维护通信录；发送请先
          <button type="button" className="ml-1 underline" onClick={onOpenSettings}>去设置接入邮箱</button>
          。本页不收集安全密码。
        </div>
      )}

      <div className="flex gap-1 border-b border-line2 pb-1" role="tablist" aria-label="邮箱分区">
        {tabs.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={tab === t.id}
            className={`px-3 py-1.5 text-[13px] rounded-field ${tab === t.id ? 'bg-surface2 text-ink font-medium' : 'text-muted hover:text-ink'}`}
            onClick={() => requestTab(t.id)}
          >
            {t.label}
          </button>
        ))}
      </div>

      {mail.loading ? <Placeholder>加载中…</Placeholder> : null}

      {!mail.loading && tab === 'drafts' && (
        <div className="grid grid-cols-1 md:grid-cols-[220px_1fr] gap-4 min-h-0">
          <div className="flex flex-col gap-2">
            <button type="button" className={`${BTN} ${BTN_PRIMARY}`} onClick={() => void newDraft()}>新建草稿</button>
            <ul className="m-0 p-0 list-none flex flex-col gap-1" data-testid="mail-draft-list">
              {mail.drafts.map((d) => (
                <li key={d.id}>
                  <button
                    type="button"
                    className={`w-full text-left px-2 py-2 rounded-field text-[13px] ${activeDraftId === d.id ? 'bg-surface2 text-ink' : 'text-muted hover:bg-surface2'}`}
                    onClick={() => requestDraft(d.id)}
                  >
                    {d.subject || '（无主题）'}
                  </button>
                </li>
              ))}
            </ul>
          </div>
          <div className="flex flex-col gap-3 min-w-0">
            {!draft ? (
              <Placeholder>选择或新建草稿</Placeholder>
            ) : (
              <>
                <div className="text-[12px] text-muted" data-testid="mail-draft-status">
                  修订 {draft.revision} · {dirty ? '未保存修改' : '已保存'}
                  {draft.proposalMd ? ' · 含模型提案原文（只读）' : ''}
                </div>
                <label className={LABEL_CLS}>主题
                  <input
                    className={FIELD_CLS}
                    value={editSubject}
                    onChange={(e) => setEditSubject(e.target.value)}
                    data-testid="mail-draft-subject"
                  />
                </label>
                <label className={LABEL_CLS}>收件人（逗号分隔）
                  <input
                    className={FIELD_CLS}
                    value={editTo}
                    onChange={(e) => setEditTo(e.target.value)}
                    data-testid="mail-draft-to"
                  />
                </label>
                <div className="flex gap-2">
                  <button type="button" className={BTN} aria-pressed={!previewMode} onClick={() => setPreviewMode(false)}>
                    Markdown编辑
                  </button>
                  <button type="button" className={BTN} aria-pressed={previewMode} onClick={() => setPreviewMode(true)} data-testid="mail-preview-toggle">
                    预览
                  </button>
                </div>
                {previewMode ? (
                  <div className="border border-line2 rounded-field p-3 bg-surface2 min-h-[200px]" data-testid="mail-preview">
                    <MailSafePreview markdown={editBody} />
                  </div>
                ) : (
                  <textarea
                    className={`${FIELD_CLS} min-h-[220px] resize-y font-mono text-[13px]`}
                    value={editBody}
                    onChange={(e) => setEditBody(e.target.value)}
                    data-testid="mail-draft-body"
                    onKeyDown={(e) => {
                      // 组字中 Enter 不触发任何发送；本页也无发送快捷键。
                      if (isComposingKey(e)) return
                    }}
                  />
                )}
                <div className="flex flex-wrap gap-2">
                  <button
                    type="button"
                    className={`${BTN} ${BTN_PRIMARY}`}
                    disabled={!dirty || saving}
                    onClick={() => void saveDraft()}
                    data-testid="mail-save-draft"
                  >
                    {saving ? '保存中…' : '保存草稿'}
                  </button>
                  <button
                    type="button"
                    className={BTN}
                    disabled={!dirty}
                    onClick={discardLocalEdits}
                  >
                    放弃未保存修改
                  </button>
                  <button
                    type="button"
                    className={`${BTN} ${BTN_PRIMARY}`}
                    disabled={saving || confirming || !mail.hasAccount}
                    onClick={() => void checkAndSend()}
                    data-testid="mail-check-send"
                    title={!mail.hasAccount ? '请先接入邮箱账号' : undefined}
                  >
                    检查并发送
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      )}

      {!mail.loading && tab === 'contacts' && (
        <div className="flex flex-col gap-4" data-testid="mail-contacts">
          <div className="flex flex-wrap gap-2 items-end">
            <label className={LABEL_CLS}>姓名
              <input className={FIELD_CLS} value={contactName} onChange={(e) => setContactName(e.target.value)} />
            </label>
            <label className={LABEL_CLS}>邮箱
              <input className={FIELD_CLS} value={contactEmail} onChange={(e) => setContactEmail(e.target.value)} />
            </label>
            <button type="button" className={`${BTN} ${BTN_PRIMARY}`} onClick={() => void addContact()}>添加</button>
          </div>
          <label className={LABEL_CLS}>CSV 导入（每行：姓名,邮箱）
            <textarea className={`${FIELD_CLS} min-h-[80px]`} value={importText} onChange={(e) => setImportText(e.target.value)} />
          </label>
          <button type="button" className={BTN} onClick={() => void runImport()}>预览并导入</button>
          {importSkip.length > 0 && (
            <ul className="text-[12px] text-amberink m-0 pl-4" data-testid="mail-import-skip">
              {importSkip.map((s) => <li key={s}>{s}</li>)}
            </ul>
          )}
          <ul className="m-0 p-0 list-none flex flex-col gap-2">
            {mail.contacts.map((c) => (
              <li key={c.id} className="flex items-center justify-between border border-line2 rounded-field px-3 py-2 text-[13px]">
                <span>{c.name} · {(c.emails ?? []).join(', ')}</span>
                <button
                  type="button"
                  className={BTN}
                  onClick={() => void mail.removeContact(c.id, c.revision).catch((e) => mail.setError(errText(e)))}
                >
                  删除
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}

      {!mail.loading && tab === 'groups' && (
        <div className="flex flex-col gap-4" data-testid="mail-groups">
          <div className="flex flex-wrap gap-2 items-end">
            <label className={LABEL_CLS}>分组名
              <input className={FIELD_CLS} value={groupName} onChange={(e) => setGroupName(e.target.value)} />
            </label>
            <button type="button" className={`${BTN} ${BTN_PRIMARY}`} onClick={() => void addGroup()}>新建分组</button>
          </div>
          <ul className="m-0 p-0 list-none flex flex-col gap-2">
            {mail.groups.map((g) => (
              <li key={g.id} className="border border-line2 rounded-field px-3 py-2 text-[13px]">
                {g.name} · {(g.memberIds ?? []).length} 人
              </li>
            ))}
          </ul>
          {mail.contacts.length > 1 && (
            <p className="m-0 text-[12px] text-muted">
              同名联系人须在后续加人分组时明确选择，不自动猜测。
            </p>
          )}
        </div>
      )}

      {!mail.loading && tab === 'outbox' && (
        <div data-testid="mail-outbox">
          <Placeholder pad="lg">
            发件箱将在发送链路接入后显示已提交记录。本步不发信。
          </Placeholder>
        </div>
      )}

      {leaveOpen && (
        <ConfirmDialog
          title="有未保存的修改"
          message="离开将丢失未保存的草稿编辑。"
          confirmLabel="放弃修改并离开"
          onCancel={() => {
            setLeaveOpen(false)
            setPendingTab(null)
            setPendingDraftId(null)
          }}
          onConfirm={applyLeave}
        />
      )}

      {sendPrep && (
        <div className="fixed inset-0 bg-[rgba(30,33,50,0.32)] backdrop-blur-[2px] flex items-center justify-center z-30" data-testid="mail-send-confirm">
          <div className="w-[480px] max-w-[92vw] max-h-[85vh] overflow-y-auto bg-surface rounded-2xl p-[22px] shadow-modal flex flex-col gap-3">
            <h3 className="m-0 text-[16px] font-bold">确认发送</h3>
            <p className="m-0 text-[13px] text-muted">以下为服务端冻结的最终修订；返回编辑会使本确认失效。</p>
            <div className="text-[13px] border border-line2 rounded-field p-3 bg-surface2 flex flex-col gap-1.5">
              <div><span className="text-faint">主题</span> {sendPrep.preview.subject}</div>
              <div><span className="text-faint">收件人</span> {joinAddresses(sendPrep.preview.to)}</div>
              {(sendPrep.preview.cc?.length ?? 0) > 0 && (
                <div><span className="text-faint">抄送</span> {joinAddresses(sendPrep.preview.cc)}</div>
              )}
              <div><span className="text-faint">修订</span> {sendPrep.preview.revision} · {sendPrep.preview.dispatchMode}</div>
              <div className="border-t border-line2 pt-2 mt-1">
                <MailSafePreview markdown={sendPrep.preview.bodyMarkdown} />
              </div>
            </div>
            <div className="flex justify-end gap-2.5">
              <button type="button" className={BTN} onClick={() => setSendPrep(null)}>返回编辑</button>
              <button
                type="button"
                className={`${BTN} ${BTN_PRIMARY}`}
                disabled={confirming}
                onClick={() => void confirmPrepared()}
                data-testid="mail-confirm-send"
              >
                {confirming ? '提交中…' : '确认发送'}
              </button>
            </div>
          </div>
        </div>
      )}
    </PageShell>
  )
}
