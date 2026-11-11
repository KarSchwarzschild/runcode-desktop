import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { AskCard } from './ask-card'
import { PlanBoard, PlanStageBar } from './plan-board'
import { PlanStates, type ToolEvent } from '@/core/bridge'

const reason = '测试知识库暂不参与回答，请先移除知识库'
const noop = () => {}
const tool: ToolEvent = { type: 'completed', toolUseID: 'ask', toolName: 'AskUser', input: { question: '是否继续？', options: ['继续', '稍后'] } }
const planProps = {
  run: { state: PlanStates.AwaitingApproval }, draft: { title: '待审批方案', steps: [{ id: 's1', title: '执行一步' }] }, approving: false, busy: false,
  actions: { move: noop, remove: noop, insertAfter: noop, patch: noop, flush: noop }, onApprove: noop, onCancel: noop,
}
describe('knowledge send block on chat actions', () => {
  it('keeps an unanswered AskUser card visible and disables options while knowledge blocks sending', () => {
    const markup = renderToStaticMarkup(<AskCard tool={tool} busy={false} sendBlock={reason} onAnswer={noop} />)
    expect(markup).toContain(reason)
    expect(markup).toMatch(/<button[^>]*disabled=""[^>]*>继续<\/button>/)
    expect(markup).toMatch(/<button[^>]*disabled=""[^>]*>稍后<\/button>/)
    expect(markup).not.toContain('已回复')
    const restored = renderToStaticMarkup(<AskCard tool={tool} busy={false} sendBlock="" onAnswer={noop} />)
    expect(restored).not.toMatch(/<button[^>]*disabled=""[^>]*>继续<\/button>/)
  })
  it('disables both plan approvals while retaining the editable plan and cancellation', () => {
    const markup = renderToStaticMarkup(<PlanBoard {...planProps} sendBlock={reason} />)
    expect(markup).toContain(reason)
    expect(markup).toMatch(/<button[^>]*disabled=""[^>]*>确认并执行（交互模式）<\/button>/)
    expect(markup).toMatch(/<button[^>]*disabled=""[^>]*>确认并执行（智能模式）<\/button>/)
    expect(markup).toContain('执行一步')
    expect(markup).not.toMatch(/<button[^>]*disabled=""[^>]*>取消<\/button>/)
    const restored = renderToStaticMarkup(<PlanBoard {...planProps} sendBlock="" />)
    expect(restored).not.toMatch(/<button[^>]*disabled=""[^>]*>确认并执行（交互模式）<\/button>/)
  })
  it('disables plan continuation without pretending the conversation is busy', () => {
    const markup = renderToStaticMarkup(<PlanStageBar run={{ state: PlanStates.Planning }} busy={false} sendBlock={reason} onResume={noop} onCancel={noop} />)
    expect(markup).toMatch(/<button[^>]*disabled=""[^>]*>继续规划<\/button>/)
    expect(markup).toContain(reason)
    expect(markup).not.toMatch(/<button[^>]*disabled=""[^>]*>取消规划<\/button>/)
  })
})
