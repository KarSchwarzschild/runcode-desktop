// @vitest-environment jsdom
import { expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import type { ToolEvent } from '@/core/bridge'
import { ExecutionCard } from './execution-card'

it('does not render an execution row for image generation', () => {
  const tool = {
    type: 'completed',
    toolName: 'image_generate',
    toolUseID: 'image-1',
    input: { prompt: '一只真实的狗狗照片', size: '1024x1024', model: 'wanx-v1' },
  } satisfies ToolEvent

  expect(renderToStaticMarkup(<ExecutionCard tools={[tool]} />)).toBe('')
})
