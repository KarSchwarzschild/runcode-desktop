// 文生图界面预览页：由 main.tsx 按 ?preview=imagegen 挂载，正常流程不会用到。
// 不连宿主：设置小节与成功卡片的缩略图会走到「加载失败/无法预览」那一支，用来看排版；
// 其余状态（进行中、未知、保存失败、未生成、工具条入口与状态标记）全部是静态数据。
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

function Toolbar({ chip }: { chip?: Parameters<typeof ComposerToolbar>[0]['imageGeneration'] }) {
  return <ComposerToolbar info={info} busy={false} canSend onOpenSkillPicker={noop} onOpenAgentPicker={noop} onOpenFilePicker={noop}
    onOpenKnowledgePicker={noop} knowledgeDisabled={false} onPickAttachment={noop} onToggleMode={noop} onTogglePlan={noop}
    onChooseReasoning={noop} onChooseThinking={noop} onPickModel={noop} onSend={noop} onStop={noop}
    imageGeneration={chip} onToggleImageGeneration={noop} />
}

export function ImageGenerationPreview() {
  return (
    <div className="min-h-screen bg-surface2 p-6 flex flex-col gap-6 text-ink">
      <h1 className="text-[16px] font-semibold">文生图 · 界面预览</h1>
      <section className="flex flex-col gap-3">
        <h2 className="text-[13px] text-muted">生成结果卡片</h2>
        <div className="grid grid-cols-[repeat(auto-fill,minmax(320px,1fr))] gap-4">
          <GeneratedImageCard tool={tool(undefined, 'progress', '正在生成图片')} sessionId="preview" />
          <GeneratedImageCard tool={tool(base)} sessionId="preview" />
          <GeneratedImageCard tool={tool({ ...base, status: 'unknown', code: 'unknown_outcome', images: [], message: '生成结果无法确认，可能已生成或计费，请勿重复提交；如需核查请联系管理员' }, 'failed')} sessionId="preview" />
          <GeneratedImageCard tool={tool({ ...base, status: 'save_failed', code: 'save_failed', images: [], message: '已收到生成结果，但下载或保存失败，计费待核验；不会自动重新生成' }, 'failed')} sessionId="preview" />
          <GeneratedImageCard tool={tool({ ...base, status: 'failed', code: 'forbidden', images: [], requestId: '', message: '当前账号或租户无权使用该生图模型，本次未生成图片' }, 'failed')} sessionId="preview" />
          <GeneratedImageCard tool={tool({ ...base, status: 'cancelled', images: [], requestId: '', message: '已停止等待；请求已发出，上游可能已生成或计费' }, 'failed')} sessionId="preview" />
        </div>
      </section>
      <section className="flex flex-col gap-3 max-w-[900px]">
        <h2 className="text-[13px] text-muted">输入框工具条：未开启 / 已开启可用 / 已开启未就绪（点「+」看菜单，点标记看说明）</h2>
        <Toolbar chip={{ enabled: false, available: true, reason: '', model: 'image-model', size: '1024x1024', saving: false }} />
        <Toolbar chip={{ enabled: true, available: true, reason: '', model: base.model, size: '2048x2048', saving: false }} />
        <Toolbar chip={{ enabled: true, available: false, reason: '尚未选择生图模型，请在「设置 → 图片生成」中选择', model: '', size: '', saving: false }} />
      </section>
      <section className="flex flex-col gap-3 max-w-[640px]">
        <h2 className="text-[13px] text-muted">设置小节（未连宿主，显示加载失败态）</h2>
        <ImageGenerationSection tenantId="tenant-preview" />
      </section>
    </div>
  )
}
