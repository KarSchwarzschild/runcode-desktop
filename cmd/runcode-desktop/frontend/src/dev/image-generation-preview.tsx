// 文生图界面预览页：由 main.tsx 按 ?preview=imagegen 挂载，正常流程不会用到。
// 不连宿主：成功卡下方预览会落到「暂时无法预览／文件不存在」；用来验收紧凑信息卡排版。
// 其余状态（进行中、未知、保存失败、未生成、工具条入口）全部是静态数据。
import type { ImageGenerationResult, SessionInfo, ToolEvent } from '@/core/bridge'
import { ComposerToolbar } from '@/composer/toolbar'
import { GeneratedImageCard } from '@/chat/generated-image-card'
import { ImageGenerationSection } from '@/pages/settings/image-generation'

const noop = () => {}
const base: ImageGenerationResult = {
  version: 1, generationId: 'gen_preview', status: 'succeeded', model: 'doubao-seedream-4-0-250828-a-very-long-model-name', size: '2048x2048',
  accountingStatus: 'unverified', requestId: 'brg-synthetic-0001',
  images: [{ path: '生成图片/gen_preview/image.png', mimeType: 'image/png', bytes: 1834221, width: 2048, height: 2048 }],
}
const tool = (data: ImageGenerationResult | undefined, type: ToolEvent['type'] = 'completed', message = ''): ToolEvent =>
  ({ type, toolName: 'image_generate', toolUseID: 'preview', data, message } as ToolEvent)
const info = { sessionId: 'preview', kind: 'agent', model: 'platform-chat-model', permissionMode: 'interactive' } as SessionInfo

function Toolbar() {
  return <ComposerToolbar info={info} busy={false} canSend onOpenSkillPicker={noop} onOpenAgentPicker={noop} onOpenFilePicker={noop}
    onPickAttachment={noop} onToggleMode={noop} onTogglePlan={noop}
    onChooseReasoning={noop} onChooseThinking={noop} onPickModel={noop} onSend={noop} onStop={noop} />
}

export function ImageGenerationPreview() {
  return (
    <div className="min-h-screen bg-surface2 p-6 flex flex-col gap-6 text-ink">
      <h1 className="text-[16px] font-semibold">文生图 · 界面预览</h1>
      <section className="flex flex-col gap-3">
        <h2 className="text-[13px] text-muted">生成结果卡片</h2>
        <div className="grid grid-cols-[repeat(auto-fill,minmax(320px,1fr))] gap-4">
          <div className="flex flex-col gap-1">
            <span className="text-[11px] text-faint">生成中 · 下方扫光</span>
            <GeneratedImageCard tool={tool(undefined, 'progress', '一双高跟鞋在地毯上')} sessionId="preview" />
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-[11px] text-faint">历史完成 · 无揭示动画</span>
            <GeneratedImageCard tool={tool({ ...base, displayIntent: '一双高跟鞋在地毯上' })} sessionId="preview" />
          </div>
          <GeneratedImageCard tool={tool({ ...base, status: 'failed', code: 'forbidden', images: [], requestId: '', message: '内部原因不应出现在卡片' }, 'failed')} sessionId="preview" />
          <GeneratedImageCard tool={tool(base)} sessionId="preview-missing" />
          <GeneratedImageCard tool={tool({ ...base, status: 'unknown', images: [], message: '内部' }, 'failed')} sessionId="preview" />
          <GeneratedImageCard tool={tool({ ...base, status: 'save_failed', images: [], message: '内部' }, 'failed')} sessionId="preview" />
        </div>
        <p className="text-[11px] text-faint">
          信息卡无缩略图；扫光／百叶窗在下方预览舞台。画廊无宿主时预览区显示读取失败文案。
        </p>
      </section>
      <section className="flex flex-col gap-3 max-w-[900px]">
        <h2 className="text-[13px] text-muted">输入框工具条（已无生图开关；生图设置入口改在助手回复尾注）</h2>
        <Toolbar />
      </section>
      <section className="flex flex-col gap-3 max-w-[640px]">
        <h2 className="text-[13px] text-muted">设置小节（未连宿主，显示加载失败态）</h2>
        <ImageGenerationSection tenantId="tenant-preview" />
      </section>
    </div>
  )
}
