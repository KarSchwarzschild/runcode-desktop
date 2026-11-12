import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { BlockView } from './block-view'

it('renders knowledge answer links and images as text without opening model-provided files or remote images', () => {
  const html = renderToStaticMarkup(<BlockView textOnly block={{ kind: 'assistant', id: 'answer', text: '[未验证来源](https://example.com/fake) ![图片说明](https://example.com/image.png) `report.pdf`', streaming: false, ts: '' }} resolveFile={() => 'report.pdf'} onOpenFile={() => {}} />)
  expect(html).toContain('未验证来源')
  expect(html).toContain('图片说明')
  expect(html).not.toContain('<img')
  expect(html).not.toContain('href=')
  expect(html).not.toContain('title="预览')
})

it('keeps generated-image-unrelated assistant svg fences on the inline preview path without touching image tool cards', () => {
  const svg = '```svg\n<svg viewBox="0 0 10 10"><rect width="5" height="5"/></svg>\n```'
  const html = renderToStaticMarkup(<BlockView block={{ kind: 'assistant', id: 'a1', text: svg, streaming: false, ts: '' }} />)
  expect(html).toContain('min-h-12')
  expect(html).not.toContain('language-svg')
})

describe('retry notice', () => {
  it('uses a quiet note, keeps the attempt count and moves diagnostics to the tooltip', () => {
    const reason = '模型仅返回思考或空内容，未返回回答或工具调用'
    const html = renderToStaticMarkup(<BlockView block={{ kind: 'retry', id: 'retry', reason, attempt: 1, maxAttempts: 2 }} />)
    const visible = html.replace(/<[^>]*>/g, '')
    expect(visible).toContain('刚才没接上，我再试一下')
    expect(visible).toContain('1/2')
    expect(visible).not.toContain(reason)
    expect(html).toContain(`title="自动重试：${reason}`)
    expect(html).toContain('text-faint')
    expect(html).not.toContain('text-amberink')
    expect(html).not.toContain('bg-amber')
    expect(html).not.toContain('text-red')
  })

  it('does not soften the error when automatic recovery actually fails', () => {
    const html = renderToStaticMarkup(<BlockView block={{ kind: 'error', id: 'error', text: '请求失败，请重试' }} />)
    expect(html).toContain('text-red')
    expect(html).toContain('请求失败，请重试')
  })
})

it('only identified questions expose retry actions, disabled during work', () => {
  const action = () => {}
  const identified = renderToStaticMarkup(<BlockView block={{ kind: 'user', id: 'render-id', questionId: 'durable-id', text: '提问', ts: '' }} onRetryQuestion={action} onEditQuestion={action} questionActionsDisabled />)
  expect(identified).toContain('重新生成')
  expect(identified).toContain('修改后重试')
  expect(identified.match(/disabled=""/g)).toHaveLength(2)
  const optimistic = renderToStaticMarkup(<BlockView block={{ kind: 'user', id: 'pending', text: '尚未接收', ts: '' }} onRetryQuestion={action} onEditQuestion={action} />)
  expect(optimistic).not.toContain('重新生成')
})

it('renders question actions as icons with tooltips and screen-reader labels', () => {
  const action = () => {}
  const html = renderToStaticMarkup(<BlockView block={{ kind: 'user', id: 'row', questionId: 'question', text: '提问', ts: '' }} onRetryQuestion={action} onEditQuestion={action} />)
  const buttons = html.match(/<button[^>]*>[\s\S]*?<\/button>/g) ?? []
  expect(buttons).toHaveLength(2)
  for (const [index, label] of ['重新生成', '修改后重试'].entries()) {
    const button = buttons[index]
    expect(button).toContain('<svg')
    expect(button).toContain('aria-hidden="true"')
    expect(button).toContain(`title="${label}`)
    expect(button).toContain(`<span class="sr-only">${label}</span>`)
    const visible = button.replace(/<span class="sr-only">[^<]*<\/span>/g, '').replace(/<[^>]*>/g, '')
    expect(visible).toBe('')
  }
})
