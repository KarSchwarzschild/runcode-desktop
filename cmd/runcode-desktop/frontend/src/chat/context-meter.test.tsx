import { type ComponentProps } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { ContextMeter } from './context-meter'

const render = (props: Partial<ComponentProps<typeof ContextMeter>> = {}) => renderToStaticMarkup(
  <ContextMeter used={250000} budget={1000000} onCompact={() => {}} compacting={false} busy={false} {...props} />,
)
const text = (markup: string) => markup.replace(/<[^>]+>/g, '')

describe('ContextMeter', () => {
  it.each([
    [65536, '66k'], [131072, '131k'], [262144, '262k'], [1000000, '1M'], [1048576, '1.05M'],
  ])('以本会话实际预算 %i 显示分母', (budget, label) => {
    expect(text(render({ used: 0, budget: budget as number }))).toContain(`0 / ${label}`)
  })

  it('百万窗口不再显示为 1000k，悬浮说明保留完整数值和当前模型', () => {
    const markup = render({ model: 'qwen3.8-flash' })
    expect(text(markup)).toContain('250k / 1M')
    expect(text(markup)).toContain('25%')
    expect(markup).not.toContain('1000k')
    expect(markup).toContain('当前会话模型：qwen3.8-flash')
    expect(markup).toContain('250,000 / 1,000,000 tokens')
    expect(markup).toContain('800,000 tokens')
    expect(markup).toContain('不是厂商硬容量')
  })

  it('0 明确关闭自动压缩，不假称模型无限，手动压缩仍可用', () => {
    const markup = render({ used: 44000, budget: 0 })
    expect(text(markup)).toContain('44k · 自动压缩关闭')
    expect(markup).toContain('不代表模型上下文无限')
    expect(markup).not.toContain('未限')
    expect(markup).not.toContain('role="progressbar"')
    expect(markup).not.toContain('disabled=""')
  })

  it('未建立会话与预算 0 不混淆，并禁用无目标的压缩', () => {
    const markup = render({ budget: null })
    expect(text(markup)).toContain('待建立会话')
    expect(text(markup)).not.toContain('自动压缩关闭')
    expect(markup).toContain('disabled=""')
    expect(markup).not.toContain('role="progressbar"')
  })

  it('79.x% 不提前四舍五入显示已经触发 80%', () => {
    const below = render({ used: 79999, budget: 100000 })
    expect(text(below)).toContain('79%')
    expect(below).toContain('bg-primary')
    expect(below).not.toContain('text-amberink')
    const reached = render({ used: 80000, budget: 100000 })
    expect(text(reached)).toContain('80%')
    expect(reached).toContain('bg-amber')
    expect(reached).toContain('text-amberink')
  })

  it('超限显示真实比例，条宽与无障碍进度值夹到 100%', () => {
    const markup = render({ used: 1250000 })
    expect(text(markup)).toContain('125%')
    expect(markup).toContain('width:100%')
    expect(markup).toContain('aria-valuenow="100"')
    expect(markup).toContain('aria-valuetext="125%')
    expect(markup).toContain('text-red')
  })

  it('估算标记和说明不伪称供应商精确 token 计数', () => {
    const markup = render({ estimated: true })
    expect(text(markup)).toContain('≈250k / 1M')
    expect(markup).toContain('占用为校准估算值')
    expect(markup).not.toContain('完成一次往返后自动校准')
  })

  it.each([{ busy: true }, { compacting: true }])('运行或压缩期间按钮禁用：%o', (state) => {
    expect(render(state)).toContain('disabled=""')
  })
})
