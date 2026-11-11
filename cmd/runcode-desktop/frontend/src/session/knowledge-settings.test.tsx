import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { ComposerToolbar } from '@/composer/toolbar'
import { SettingsPage } from '@/pages/settings'
import type { SessionInfo } from '@/core/bridge'

const noop = () => {}
const asyncNoop = async () => {}

describe('knowledge settings unlock', () => {
  it('selectedKnowledgeDoesNotBlockNormalSettings', () => {
    const info = {
      sessionId: 'A',
      kind: 'agent',
      model: 'platform-model',
      permissionMode: 'interactive',
      planMode: false,
      thinkingEffort: 'medium',
      reasoningScenario: 'auto',
    } as SessionInfo
    const toolbar = renderToStaticMarkup(
      <ComposerToolbar
        busy={false}
        canSend
        info={info}
        onOpenSkillPicker={noop}
        onOpenAgentPicker={noop}
        onOpenFilePicker={noop}
        onOpenKnowledgePicker={noop}
        knowledgeDisabled={false}
        onPickAttachment={noop}
        onToggleMode={noop}
        onTogglePlan={noop}
        onChooseReasoning={noop}
        onChooseThinking={noop}
        onPickModel={asyncNoop}
        onSend={noop}
        onStop={noop}
      />,
    )
    expect(toolbar).not.toContain('知识库问答')
    expect(toolbar).toContain('交互模式')
    expect(toolbar).not.toMatch(/title="计划模式[^"]*"[^>]*disabled=""/)

    const settings = renderToStaticMarkup(
      <SettingsPage
        initial={{ permissionMode: 'interactive' }}
        info={info}
        busy={false}
        update={{ info: null, stage: 'idle', hasUpdate: false, busy: false, error: '', check: noop, download: noop, cancel: noop, install: noop, reveal: noop }}
        onSaved={noop}
        onSwitchModel={noop}
      />,
    )
    expect(settings).not.toContain('请新建普通会话')
    expect(settings).not.toContain('知识库会话')
  })

  it('settingsFailureKeepsSelectionAndCurrentValues', async () => {
    const pickModel = vi.fn(async () => { throw new Error('模型切换失败') })
    const info = {
      sessionId: 'A',
      kind: 'agent',
      model: 'keep-me',
      permissionMode: 'interactive',
      readOnlyReason: '',
    } as SessionInfo
    // 工具栏仅在只读历史禁用；选库本身不禁用，失败由上层 toast／状态处理，不清选库。
    const markup = renderToStaticMarkup(
      <ComposerToolbar
        busy={false}
        canSend
        info={info}
        onOpenSkillPicker={noop}
        onOpenAgentPicker={noop}
        onOpenFilePicker={noop}
        onOpenKnowledgePicker={noop}
        knowledgeDisabled={false}
        onPickAttachment={noop}
        onToggleMode={noop}
        onTogglePlan={noop}
        onChooseReasoning={noop}
        onChooseThinking={noop}
        onPickModel={pickModel}
        onSend={noop}
        onStop={noop}
      />,
    )
    expect(markup).toContain('keep-me')
    expect(markup).not.toMatch(/disabled=""[^>]*keep-me|keep-me[^>]*disabled=""/)
  })
})
