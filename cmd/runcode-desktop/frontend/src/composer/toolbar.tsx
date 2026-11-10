// 输入框底部工具条：+ 菜单、权限模式、计划模式、思考强度、模型选择、发送/停止。
// 自行测量自身宽度做响应式(见下方注释)，会话状态一律经 on* 回调交还上层。
import { useEffect, useRef, useState } from 'react'
import { Icon } from '@/ui/icons'
import { GhostBtn } from '@/ui/ghost-btn'
import { Popover } from '@/ui/popover'
import { ModelPickerPopover, toModelOptions, type ModelOption } from '@/ui/model-picker'
import { listCustomModels, sessionModels, type SessionInfo } from '@/core/bridge'
import { DEFAULT_MODE, MODE_LABEL } from '@/core/permission-modes'

// modeLabel 是工具条上那个按钮的字。
//
// 认得**全部**模式，包括暂时隐藏的——会话可能就停在隐藏模式上（旧配置、旧会话），
// 那时按钮得如实说它是什么。没有会话信息时落到真正的默认模式；这里原先写死回落
// 「安全模式」，而后端 defaultRequest() 从来就是 interactive，那句话从没对过。
function modeLabel(mode: string | undefined): string {
  return MODE_LABEL[mode ?? ''] ?? MODE_LABEL[DEFAULT_MODE]
}


// "Thinking model" options for the in-conversation picker.
const REASONING: { value: string; label: string }[] = [
  { value: 'off', label: '不启用' },
  { value: 'auto', label: '自动分类（每轮多一次调用）' },
  { value: 'troubleshooting', label: '排障 · 5 Whys' },
  { value: 'proposal', label: '方案 · 金字塔原理' },
  { value: 'architecture', label: '架构 · 第一性原理' },
  { value: 'project_management', label: '项目 · 闭环 + 80/20' },
  { value: 'incident_response', label: '救火 · OODA' },
  { value: 'general', label: '通用 · 10 步清单' },
]
const REASONING_LABEL: Record<string, string> = Object.fromEntries(REASONING.map((r) => [r.value, r.label]))

// 思考模型（reasoning scenario）按钮暂时隐藏；改回 true 即可恢复整块 UI 与逻辑。
const SHOW_THINKING_MODEL = false

// "Thinking strength" options: provider-native reasoning effort (OpenAI
// reasoning_effort / an Anthropic thinking budget). This is the knob that makes a
// reasoning model actually emit the reasoning content shown above each answer.
const THINKING: { value: string; label: string }[] = [
  { value: 'off', label: '不启用' },
  { value: 'low', label: '低 · 快速' },
  { value: 'medium', label: '中 · 均衡' },
  { value: 'high', label: '高 · 深度' },
]
const THINKING_LABEL: Record<string, string> = { low: '低', medium: '中', high: '高' }

// ImageGenerationChip 是工具条要画「生成图片」所需的全部：开关是不是开着、下一轮能不能
// 真的生成（不能时 reason 说为什么）、用哪个模型。状态本身归 useImageGeneration。
export type ImageGenerationChip = { enabled: boolean; available: boolean; reason: string; model: string; size: string; saving: boolean }

export function ComposerToolbar({
  info,
  busy,
  canSend, sendLabel,
  onOpenSkillPicker,
  onOpenAgentPicker,
  onOpenFilePicker,
  onOpenKnowledgePicker,
  knowledgeDisabled,
  onPickAttachment,
  onToggleMode,
  onTogglePlan,
  onChooseReasoning,
  onChooseThinking,
  onPickModel,
  onSend,
  onStop,
  imageGeneration,
  onToggleImageGeneration,
  onOpenImageGenerationSettings,
}: {
  info: SessionInfo | null
  busy: boolean
  sendLabel?: string
  canSend: boolean
  onOpenSkillPicker: () => void
  onOpenAgentPicker: () => void
  onOpenFilePicker: () => void
  onOpenKnowledgePicker: () => void
  knowledgeDisabled: boolean
  // 不传 imageGeneration 就不画「生成图片」入口（例如还没接入的宿主）。
  imageGeneration?: ImageGenerationChip
  onToggleImageGeneration?: (enabled: boolean) => void
  // 前往「设置 → 图片生成」；宿主没接时状态标记里改为文字提示。
  onOpenImageGenerationSettings?: () => void
  onPickAttachment: () => void
  onToggleMode: () => void
  onTogglePlan: () => void
  onChooseReasoning: (scenario: string) => void
  onChooseThinking: (effort: string) => void
  onPickModel: (choice: ModelOption) => Promise<void> | void
  onSend: () => void
  onStop: () => void
}) {
  // 模式由后端 kind 决定，不能因为移除了最后一个库就重新开放工具或外部模型。
  // 选库不锁模式／模型／思考；仅真实只读历史禁用。旧独立 knowledge kind 也不再灰掉整条工具栏。
  const restricted = !!info?.readOnlyReason
  const [addMenu, setAddMenu] = useState(false)
  const [imageMenu, setImageMenu] = useState(false)
  // 回合进行中不能切换：本轮已冻结目标，改了也只对下一轮生效，所以干脆不让改。
  const imageToggleLocked = restricted || busy || !!imageGeneration?.saving
  const [reasonMenu, setReasonMenu] = useState(false)
  const [thinkMenu, setThinkMenu] = useState(false)
  // 对话内模型选择器：点底部模型名弹出，模糊检索，最多显示 10 个。平台(通行证)模型
  // 与自定义直连模型合并展示，都能被搜索和切换；切换本身（换连接/重建会话）由
  // 上层的 onPickModel 完成。
  const [modelPickerOpen, setModelPickerOpen] = useState(false)
  const [modelOptions, setModelOptions] = useState<ModelOption[]>([])
  const openModelPicker = async () => {
    setModelPickerOpen(true)
    try {
      const [platform, custom] = await Promise.all([
        sessionModels().catch(() => null),
        listCustomModels().catch(() => null),
      ])
      // 平台模型 + 自定义模型合并成同一份候选(与设置页共用 toModelOptions)。
      setModelOptions(toModelOptions(platform ?? [], custom ?? []))
    } catch { setModelOptions([]) }
  }
  // 菜单先收起再回调上层——与拆分前 chooseReasoning/chooseThinking 的时序一致。
  const chooseReasoning = (scenario: string) => {
    setReasonMenu(false)
    onChooseReasoning(scenario)
  }
  const chooseThinking = (effort: string) => {
    setThinkMenu(false)
    onChooseThinking(effort)
  }

  // Composer toolbar responsiveness. The bar's width depends on the sidebar and
  // preview pane, not the viewport, so a CSS media query can't see it — and a CSS
  // container query is unusable here because container-type applies layout
  // containment, which would trap the menus' `fixed inset-0` click-away overlays
  // inside the bar. So measure the bar itself and drop labels to icons as it
  // narrows (tooltips and the active-state colors still convey each button).
  const toolbarRef = useRef<HTMLDivElement | null>(null)
  const [toolbarW, setToolbarW] = useState(0)
  useEffect(() => {
    const el = toolbarRef.current
    if (!el) {
      setToolbarW(0)
      return
    }
    const ro = new ResizeObserver(([e]) => setToolbarW(e.contentRect.width))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  // Thresholds are the bar's natural content width: ~660px with every label, ~440px
  // once the secondary labels are icons. 0 = not measured yet → stay expanded.
  const compactBar = toolbarW > 0 && toolbarW < 660
  const tinyBar = toolbarW > 0 && toolbarW < 450

  return (
    <div
      ref={toolbarRef}
      className="flex items-center justify-between gap-2 bg-surface border border-line2 border-t-0 rounded-b-[14px] px-3 py-[9px] shadow-card"
    >
      {/* Left group never shrinks — labels collapse to icons instead (see compactBar),
          so the buttons keep their shape rather than being squeezed. */}
      <div className="flex items-center gap-1.5 flex-none">
        <div className="relative flex-none">
          <button
            onClick={() => setAddMenu((v) => !v)}
            title={imageGeneration ? '添加：知识库 / 生成图片 / 技能 / 智能体 / 文件 / 图片' : '添加：技能 / 智能体 / 知识库 / 文件 / 图片'}
            className="border-none bg-transparent text-muted text-[13px] px-2.5 py-1.5 rounded-lg cursor-pointer inline-flex items-center gap-1.5 hover:bg-surface2 hover:text-ink"
          >
            <Icon name="plus" size={16} />
          </button>
          <Popover open={addMenu} onClose={() => setAddMenu(false)} placement="up-left" className="w-[180px]">
            <button type="button" disabled={knowledgeDisabled} onClick={() => { setAddMenu(false); onOpenKnowledgePicker() }} className="w-full text-left px-3 py-[7px] text-[13px] text-ink hover:bg-surface2 flex items-center gap-2 disabled:opacity-40 disabled:cursor-default"><Icon name="knowledge" size={15} />知识库</button>
            {imageGeneration && (
              <button
                type="button"
                role="menuitemcheckbox"
                aria-checked={imageGeneration.enabled}
                disabled={imageToggleLocked}
                title={busy ? '回合进行中不能切换，结束或停止后再试' : imageGeneration.enabled ? '关闭当前会话的图片生成' : '为当前会话开启图片生成：你明确要求时才会生成，每次都要批准'}
                onClick={() => { setAddMenu(false); onToggleImageGeneration?.(!imageGeneration.enabled) }}
                className="w-full text-left px-3 py-[7px] text-[13px] text-ink hover:bg-surface2 flex items-center gap-2 disabled:opacity-40 disabled:cursor-default"
              >
                <Icon name="image-gen" size={15} />生成图片
                {imageGeneration.enabled && <span className="ml-auto text-primary text-[12px]">✓</span>}
              </button>
            )}
            {/* 四项结构相同、只差图标/文案/动作，走数据驱动——加第五项不必再复制一遍类名。
                关菜单是所有项的共同动作，提到这里，避免每项各写一次。 */}
            {([
              { icon: 'book', label: '技能', run: onOpenSkillPicker },
              { icon: 'bot', label: '智能体', run: onOpenAgentPicker },
              { icon: 'hash', label: '文件', run: onOpenFilePicker },
              { icon: 'paperclip', label: '图片附件', run: onPickAttachment },
            ] as const).map((it) => (
              <button type="button" disabled={restricted}
                key={it.label}
                onClick={() => { setAddMenu(false); it.run() }}
                className="w-full px-3 py-[7px] text-[13px] cursor-pointer text-ink hover:bg-surface2 flex items-center gap-2 disabled:opacity-40 disabled:cursor-default"
              >
                <Icon name={it.icon} size={15} /> {it.label}
              </button>
            ))}
          </Popover>
        </div>
        {imageGeneration?.enabled && (
          <div className="relative flex-none">
            <button
              type="button"
              onClick={() => setImageMenu((v) => !v)}
              title={imageGeneration.available ? `生成图片已开启：${imageGeneration.model} · ${imageGeneration.size}` : `生成图片已开启但暂不可用：${imageGeneration.reason}`}
              className={`border text-[13px] px-2.5 py-1.5 rounded-lg cursor-pointer inline-flex items-center gap-1.5 flex-none whitespace-nowrap transition ${imageGeneration.available ? 'border-primary text-primaryink bg-primarysoft font-medium' : 'border-amber/40 text-amberink bg-amber/12'}`}
            >
              <Icon name="image-gen" size={16} />
              {!compactBar && <span className="truncate max-w-[160px]">{imageGeneration.available ? `生图 · ${imageGeneration.model}` : '生图 · 未就绪'}</span>}
            </button>
            <Popover open={imageMenu} onClose={() => setImageMenu(false)} placement="up-left" className="w-[260px]">
              <div className="px-3 py-2 text-[12px] text-muted break-words">
                {imageGeneration.available
                  ? <>模型 <span className="font-mono text-ink2">{imageGeneration.model}</span> · {imageGeneration.size}。下一轮生效；每次生成会计费，并照常需要你批准。</>
                  : imageGeneration.reason}
              </div>
              <button type="button" disabled={imageToggleLocked} onClick={() => { setImageMenu(false); onToggleImageGeneration?.(false) }} className="w-full text-left px-3 py-[7px] text-[13px] text-ink hover:bg-surface2 disabled:opacity-40 disabled:cursor-default">关闭生成图片</button>
              {onOpenImageGenerationSettings
                ? <button type="button" onClick={() => { setImageMenu(false); onOpenImageGenerationSettings() }} className="w-full text-left px-3 py-[7px] text-[13px] text-primary hover:bg-surface2">{imageGeneration.available ? '更换生图模型' : '选择生图模型'}</button>
                : <div className="px-3 py-[7px] text-[12px] text-faint">在「设置 → 图片生成」中{imageGeneration.available ? '更换' : '选择'}生图模型</div>}
            </Popover>
          </div>
        )}
        {/* The mode label is the last to go: unlike the toggles below, the shield
            icon alone carries no hint of which mode is active. */}
        <GhostBtn disabled={restricted} className="flex-none whitespace-nowrap" onClick={onToggleMode} title={info?.readOnlyReason || `点击切换权限模式\n当前：${modeLabel(info?.permissionMode)}`}>
          <Icon name="shield" size={16} />
          {info?.readOnlyReason ? '只读历史' : !tinyBar && modeLabel(info?.permissionMode)}
        </GhostBtn>
        <button
          disabled={restricted}
          onClick={onTogglePlan}
          title="计划模式：先调研、产出方案；可新建和修改文件，不删除、不执行命令"
          className={`border text-[13px] px-2.5 py-1.5 rounded-lg cursor-pointer inline-flex items-center gap-1.5 flex-none whitespace-nowrap transition ${info?.planMode ? 'border-primary text-primaryink bg-primarysoft font-medium' : 'border-transparent bg-transparent text-muted hover:bg-surface2 hover:text-ink'}`}
        >
          <Icon name="compass" size={16} />
          {!compactBar && '计划模式'}
        </button>
        {SHOW_THINKING_MODEL && (
        <div className="relative flex-none">
          <button
            disabled={restricted}
            onClick={() => setReasonMenu((v) => !v)}
            title="思考模型：为本轮注入一套思维方法论"
            className={`border text-[13px] px-2.5 py-1.5 rounded-lg cursor-pointer inline-flex items-center gap-1.5 flex-none whitespace-nowrap transition ${(info?.reasoningScenario ?? 'off') !== 'off' ? 'border-primary text-primaryink bg-primarysoft font-medium' : 'border-transparent bg-transparent text-muted hover:bg-surface2 hover:text-ink'}`}
          >
            <Icon name="sparkles" size={16} />
            {!compactBar && ((info?.reasoningScenario ?? 'off') === 'off' ? '思考模型' : (REASONING_LABEL[info!.reasoningScenario!] ?? info!.reasoningScenario))}
            <Icon name="chevron-down" size={12} />
          </button>
          <Popover open={reasonMenu} onClose={() => setReasonMenu(false)} placement="up-left" className="w-[224px]">
            {REASONING.map((r) => (
              <div
                key={r.value}
                onClick={() => chooseReasoning(r.value)}
                className={`px-3 py-[7px] text-[13px] cursor-pointer ${(info?.reasoningScenario ?? 'off') === r.value ? 'bg-primarysoft text-primaryink font-medium' : 'text-ink hover:bg-surface2'}`}
              >
                {r.label}
              </div>
            ))}
          </Popover>
        </div>
        )}
        <div className="relative flex-none">
          <button
            disabled={restricted}
            onClick={() => setThinkMenu((v) => !v)}
            title={`思考强度：让推理模型输出思考过程（reasoning_effort）\n当前：${(info?.thinkingEffort ?? 'off') === 'off' ? '不启用' : (THINKING_LABEL[info!.thinkingEffort!] ?? info!.thinkingEffort)}`}
            className={`border text-[13px] px-2.5 py-1.5 rounded-lg cursor-pointer inline-flex items-center gap-1.5 flex-none whitespace-nowrap transition ${(info?.thinkingEffort ?? 'off') !== 'off' ? 'border-primary text-primaryink bg-primarysoft font-medium' : 'border-transparent bg-transparent text-muted hover:bg-surface2 hover:text-ink'}`}
          >
            <Icon name="sparkles" size={16} />
            {!compactBar && ((info?.thinkingEffort ?? 'off') === 'off' ? '思考强度' : `思考 · ${THINKING_LABEL[info!.thinkingEffort!] ?? info!.thinkingEffort}`)}
            <Icon name="chevron-down" size={12} />
          </button>
          <Popover open={thinkMenu} onClose={() => setThinkMenu(false)} placement="up-left" className="w-[200px]">
            {THINKING.map((t) => (
              <div
                key={t.value}
                onClick={() => chooseThinking(t.value)}
                className={`px-3 py-[7px] text-[13px] cursor-pointer ${(info?.thinkingEffort ?? 'off') === t.value ? 'bg-primarysoft text-primaryink font-medium' : 'text-ink hover:bg-surface2'}`}
              >
                {t.label}
              </div>
            ))}
          </Popover>
        </div>
      </div>
      {/* Right group takes the remaining pressure: the model name truncates rather
          than pushing the send button out — model ids get long (deepseek-ai/...). */}
      <div className="flex items-center gap-3 min-w-0">
        <div className="relative min-w-0">
          <button
            type="button"
            disabled={busy || restricted}
            onClick={() => (modelPickerOpen ? setModelPickerOpen(false) : void openModelPicker())}
            className={`font-mono text-[12px] text-muted bg-surface2 border border-line px-[11px] py-[5px] rounded-lg inline-flex items-center gap-1.5 min-w-0 ${tinyBar ? 'max-w-[110px]' : compactBar ? 'max-w-[150px]' : 'max-w-[240px]'} hover:border-primary hover:text-ink transition disabled:opacity-50 disabled:cursor-default disabled:hover:border-line disabled:hover:text-muted`}
            title={busy ? '对话进行中，无法切换模型' : `点击切换模型\n当前：${info?.model ?? ''}`}
          >
            {!compactBar && <span className="flex-none">模型 ·</span>}
            <span className="truncate min-w-0">{info?.model}</span>
            <Icon name="chevron-down" size={12} className="flex-none" />
          </button>
          <ModelPickerPopover
            open={modelPickerOpen}
            onClose={() => setModelPickerOpen(false)}
            placement="up-right"
            className="w-[320px] max-h-[380px]"
            options={modelOptions}
            current={info?.model}
            limit={50}
            allowCustom
            onPick={(id, o) => {
              // 清单里的候选照旧。手输的 id 也放行:后端对平台连接就是一次
              // SetModel(名字原样给引擎，不查白名单)，所以平台清单没收录的模型
              // 在这里照样切得过去——清单是给人挑的，不是准入门槛。
              if (o) { void onPickModel(o); return }
              const typed = id.trim()
              if (typed) void onPickModel({ kind: 'platform', id: typed, label: typed })
            }}
          />
        </div>
        {busy ? (
          <div className="flex items-center gap-2 flex-none">
            {/* 补充按钮:回合进行中把已输入的内容插进正在跑的回合(中途插入),模型会在
                当前步骤结束后看到。描边样式区别于实心红色的停止,避免误当作"立即回复"。 */}
            {canSend && !restricted && (
              <button
                className="w-10 h-10 rounded-btn flex-none border border-primary text-primary bg-transparent inline-flex items-center justify-center cursor-pointer hover:bg-primarysoft transition"
                onClick={onSend}
                title="补充：插入正在进行的回合，模型会在当前步骤结束后看到"
              >
                <Icon name="send" size={16} />
              </button>
            )}
            <button className="w-10 h-10 border-none rounded-btn flex-none bg-red text-white inline-flex items-center justify-center cursor-pointer shadow-lift-danger hover:brightness-105" onClick={onStop} title="停止"><Icon name="stop" size={16} /></button>
          </div>
        ) : (
          <button className={`${sendLabel ? 'min-w-10' : 'w-10'} h-10 border-none rounded-btn flex-none bg-primary text-white inline-flex items-center justify-center cursor-pointer shadow-lift hover:brightness-105 disabled:opacity-40 disabled:shadow-none disabled:cursor-default`} onClick={onSend} disabled={!canSend} title={sendLabel ?? '发送'} aria-label={sendLabel ?? '发送'}>{sendLabel ? <span className="px-2 text-[12px] whitespace-nowrap">{sendLabel}</span> : <Icon name="send" size={17} />}</button>
        )}
      </div>
    </div>
  )
}
