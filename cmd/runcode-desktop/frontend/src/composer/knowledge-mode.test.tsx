import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { ComposerToolbar } from './toolbar'
import type { SessionInfo } from '@/core/bridge'

const noop = () => {}
const props = { busy: true, canSend: true, onOpenSkillPicker: noop, onOpenAgentPicker: noop, onOpenFilePicker: noop, onOpenKnowledgePicker: noop, knowledgeDisabled: false, onPickAttachment: noop, onToggleMode: noop, onTogglePlan: noop, onChooseReasoning: noop, onChooseThinking: noop, onPickModel: noop, onSend: noop, onStop: noop }
describe('knowledge mode toolbar', () => {
  it('selectedKnowledgeDoesNotBlockNormalSettings', () => {
    const markup = renderToStaticMarkup(<ComposerToolbar {...props} info={{ kind: 'knowledge', model: 'platform-model', permissionMode: 'default' } as SessionInfo} />)
    // 选库／旧 knowledge kind 不再灰掉模式／计划／思考，也不再显示「知识库问答」灰钮。
    expect(markup).not.toContain('知识库问答')
    expect(markup).toContain('交互模式')
    expect(markup).toContain('title="停止"')
    expect(markup).not.toMatch(/title="计划模式[^"]*"[^>]*disabled=""/)
  })
})
