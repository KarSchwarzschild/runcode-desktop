// 会后纪要：把一场录音的转写变成发给模型的请求。
//
// **两段式**：录完先出一份「速览」（短、结构固定、不落盘、不套模板），用户看完再
// 决定要不要出正式的纪要文档。这不是拿成本换体验，两头都赚——输出 token 比输入贵
// 得多，而一篇正式纪要两三千 token 的输出，在「只想知道刚才聊了啥」的场次里是纯
// 浪费；不要文档的场次直接省掉整个长输出。想跳过这一步的人可以在录音设置里打开
// 「录完直接出纪要文档」，那条老路径原样保留。
//
// 纯函数、无副作用——这两段提示词的措辞直接决定产出的质量，而它是整个功能里最容易
// 被随手改坏的东西，所以单独拎出来测。
import type { RecordingInfo } from '@/core/bridge'


// RECORDING_MARKER 是纪要请求末尾那行机器可读的标记。
//
// 它存在的理由是「历史恢复」：对话历史由引擎回放，回放回来的只有消息原文，
// 客户端那张录音卡片本身不在里面。把这场录音的要点编进消息里，恢复时就能原地把
// 卡片重建出来——而不是让它变成一个飘在界面上、换条对话还赖着不走的浮层。
//
// 放末尾不放开头：会话标题在模型生成之前会回落到消息首行，标记占了首行的话，
// 侧栏里就是一串 JSON。
//
// 用 HTML 注释：模型会当它不存在，Markdown 渲染也不显示，而它又是消息的一部分，
// 引擎存什么就回放什么，不需要额外的存储。
const MARKER_PREFIX = '<!-- runcode-recording '
const MARKER_SUFFIX = ' -->'

/** RecordingMark 是标记里带的那点信息，够画出卡片就行。 */
export interface RecordingMark {
  id: string
  title: string
  audioMs: number
  dir?: string
  transcript?: string
  needsBackfill?: boolean
  startedAt?: string
}

/**
 * MinutesStage 区分同一场录音的两条请求。
 *
 * 之所以要编进标记：**历史恢复得按它分流**。速览那条要在上面补出录音卡片，文档那条
 * 不能再补一张——卡片属于这场录音，不属于每一次请求；而两条又都得把几千字的提示词
 * 原文换回一句短的，所以文档那条也必须带标记，只是不带卡片。
 */
export type MinutesStage = 'digest' | 'doc'

/** ParsedMarker 是从一条消息里认出来的标记。 */
export interface ParsedMarker {
  mark: RecordingMark
  // stage 为空是两段式之前的老消息：那时候一条消息既是卡片的来源，也是完整纪要
  // 请求，恢复时两件事都要做。存量历史里全是这种，不能当成漏填而丢掉。
  stage?: MinutesStage
}

export function recordingMark(info: RecordingInfo): RecordingMark {
  return {
    id: info.id, title: info.title, audioMs: info.audioMs,
    dir: info.dir, transcript: info.transcript, needsBackfill: info.needsBackfill,
    startedAt: info.startedAt,
  }
}

export function recordingMarker(mark: RecordingMark, stage?: MinutesStage): string {
  return MARKER_PREFIX + JSON.stringify(stage ? { ...mark, stage } : mark) + MARKER_SUFFIX
}

/**
 * parseRecordingMarker 从一条消息里认出录音标记。
 * 认不出返回 null——历史里绝大多数消息都不是纪要请求，这条路要便宜。
 */
export function parseRecordingMarker(text: string): ParsedMarker | null {
  const at = text.indexOf(MARKER_PREFIX)
  if (at < 0) return null
  const end = text.indexOf(MARKER_SUFFIX, at)
  if (end < 0) return null
  try {
    const raw = JSON.parse(text.slice(at + MARKER_PREFIX.length, end)) as RecordingMark & { stage?: string }
    if (!raw || typeof raw.id !== 'string' || !raw.id) return null
    const { stage, ...mark } = raw
    // 只有认识的两个值才当 stage，其余一律按老消息处理：将来若加了新的段，旧客户端
    // 至少还能把卡片画出来，而不是整条历史渲染不出。
    return { mark, stage: stage === 'digest' || stage === 'doc' ? stage : undefined }
  } catch {
    // 标记坏了就当没有：宁可少画一张卡片，也不能让一条历史消息渲染不出来。
    return null
  }
}
// MINUTES_SKILL 是内置的国开会议纪要技能，随应用一起发布（见
// internal/desktop/builtinskills）。**只有正式纪要文档那一段**用它：速览要的恰恰是
// 模板之外的短东西，把技能拉进来等于把整套公文格式一起拉进来。
//
// 必须按确切的名字认，不能靠下面那组关键词：它叫 guokai-huiyijiyao-format，是拼音，
// 「纪要」「minutes」一个都不沾。产品上这条链路答应的就是"按国开模板出纪要"，那就
// 得指名道姓，而不是指望模糊匹配碰巧命中。
const MINUTES_SKILL = 'guokai-huiyijiyao-format'

// SKILL_HINTS 是用来认「会议纪要」类技能的关键词。内置那个不在时的兜底——用户自己
// 装了或写了一个纪要技能，也该被用上，拿到的是机构自己的模板，而不是模型即兴发挥
// 的格式。
const SKILL_HINTS = ['会议纪要', '纪要', 'minutes']

/**
 * pickMinutesSkill 从**可用的**技能名里挑一个整理纪要用的：先认内置那个确切的名字，
 * 再退回关键词匹配。挑不到返回空串，此时模型按通用要求整理。
 *
 * 传进来的必须是已启用的技能。停用的技能引擎根本不会加载，点名它只会换来一句
 * "找不到这个技能"——比不点名更糟。过滤在调用方（App）做，因为启用与否是两个作用域
 * 的标志位算出来的，这里只认名字。
 */
export function pickMinutesSkill(names: string[]): string {
  if (names.includes(MINUTES_SKILL)) return MINUTES_SKILL
  for (const hint of SKILL_HINTS) {
    const hit = names.find((n) => n.includes(hint))
    if (hit) return hit
  }
  return ''
}

/** minutesFileName 是纪要落盘的文件名。日期放前面，一个工作区里多场会议才排得整齐。 */
export function minutesFileName(mark: RecordingMark): string {
  const info = mark
  const d = info.startedAt ? new Date(info.startedAt) : new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  const stamp = `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}`
  // 文件名里不能出现路径分隔符与 Windows 保留字符，标题是用户填的，得洗一遍。
  const title = (info.title || '录音纪要').replace(/[\\/:*?"<>|]/g, '_').slice(0, 40)
  return `会议纪要-${stamp}-${title}.md`
}

function human(ms: number): string {
  const total = Math.round(Math.max(0, ms) / 1000)
  const m = Math.floor(total / 60)
  const s = total % 60
  return m > 0 ? `${m} 分 ${s} 秒` : `${s} 秒`
}

function clock(iso: string | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/**
 * materialBriefing 是两段共用的材料说明：讲「这份转写是什么」，技能不可能知道，
 * 速览和文档也没有理由各说一套。
 *
 *  - 说明文本来自自动识别，否则模型会把明显的同音错误当成事实照抄进去；
 *  - 说明 S1/S2 是声纹聚类编号不是人名，否则会冒出「S1 表示……」这种句子，更糟的是
 *    把某段发言安到转写里被喊到名字的那个人头上；
 *  - 转写中断过的话说明有缺口，否则模型会把断开处脑补成连贯的讨论。
 */
function materialBriefing(mark: RecordingMark, transcript: string): string[] {
  const out: string[] = []
  out.push(`- 标题：${mark.title || '录音纪要'}`)
  const started = clock(mark.startedAt)
  if (started) out.push(`- 时间：${started}`)
  out.push(`- 时长：${human(mark.audioMs)}`)
  for (const line of speakerBriefing(transcript)) out.push(line)
  for (const line of ASR_CAVEATS) out.push(line)
  if (mark.needsBackfill) {
    out.push('- 这场录音与转写服务断开过，文本有缺口，中间可能整段缺失。发现话题接不上就直接说明可能有缺失，不要脑补衔接。')
  }
  return out
}

// ASR_CAVEATS 讲清这份文本「错在哪儿」，两段共用。
//
// 只说一句「有错别字和断句问题」不够，2026-11 拿一场 97 分钟的真实会议实测，问题是
// 具体的两类，各自会把纪要带歪：
//  - **同一个专有名词写成好几种**：「终身／中身／中山／中心教育平台」「基座／机座／
//    记座」「需规／续规／虚规」是同一个东西。不点破，模型会把它们当成几个不同的系统
//    分别归纳，或者原样照抄进纪要；
//  - **分段切在词语中间**：「…现在应该没有视觉分」「析，嗯…」分属前后两个编号，切口
//    附近几个字的说话人归属基本是错的。不点破，模型会把半句话安到另一个人头上。
const ASR_CAVEATS = [
  '- 下面的转写来自自动语音识别：有错别字和同音字，同一个专有名词、人名常被写成好几种样子。',
  '- 分段是机器切的，常常切在词语中间：一段开头或结尾的几个字可能属于前一位或后一位说话人，切口附近的说话人归属不可靠。',
]

/**
 * transcriptBlock 把转写全文包成代码块，两段共用。
 *
 * 发给模型的是 compactTranscript 压过的版本，不是落盘的原文——理由见那边。
 */
function transcriptBlock(transcript: string): string[] {
  return ['转写全文（每行是「[开始时间] 说话人：内容」，同一说话人紧挨着的几段已合并）：', '', '```', compactTranscript(transcript), '```']
}

// TRANSCRIPT_ROW_RE 认落盘转写里的一行：`**[00:03] S1**：文本`（Go 侧
// Transcript.Markdown），与 SPEAKER_RE 是同一个格式，两边必须一起改。
const TRANSCRIPT_ROW_RE = /^\*\*\[([^\]]+)\]\s*([^*]+?)\*\*：(.*)$/

// MERGE_LIMIT 是合并同一说话人连续段落时单行的字数上限。
//
// 不封顶的话，一个人连说十分钟会被合成一行，中间的时间戳全没了——而速览的大纲要靠
// 时间戳标出每个议题的起止。200 字大约是半分钟到一分钟的话，既吃掉了碎段，又留得住
// 时间轴的颗粒度。
const MERGE_LIMIT = 200

/**
 * compactTranscript 把落盘的转写压成发给模型的样子。
 *
 * 落盘那份是给人看的 Markdown：每段 `**[mm:ss] Sx**：` 加一个空行，同一个人连着说的
 * 话还会被机器切成好几段（实测一场 97 分钟的会议切出 634 段，压完是 596 行，提示词少约 9%）。
 * 这些格式字符对模型没有信息量，却要在速览和正式文档两条请求里各付一遍。这里做两件事：
 *  - 去掉加粗与空行、去掉标题行（标题已在材料说明里）；
 *  - 同一说话人紧挨着的段落合并成一行，保留第一段的时间戳，单行不超过 MERGE_LIMIT。
 *
 * **说话人标签原样保留**，不重新编号：用户在预览栏里看的是落盘原文，纪要里写的编号
 * 得能在原文里找到。认不出格式（Go 侧改了格式却没改这里）就原样返回——宁可多花
 * token，也不能把转写弄丢。
 */
export function compactTranscript(transcript: string): string {
  const rows: { at: string; who: string; text: string }[] = []
  let recognized = false
  for (const raw of transcript.split('\n')) {
    const line = raw.trim()
    if (!line) continue
    const m = TRANSCRIPT_ROW_RE.exec(line)
    if (!m) {
      // 标题行与「没有转写文本」的占位行之外的未知行照样带上，不丢内容。
      if (line.startsWith('# ') || line.startsWith('_这场录音没有转写文本')) continue
      rows.push({ at: '', who: '', text: line })
      continue
    }
    recognized = true
    const [, at, who, text] = m
    const last = rows[rows.length - 1]
    if (last && last.who === who.trim() && last.text.length < MERGE_LIMIT) {
      last.text = joinText(last.text, text.trim())
    } else {
      rows.push({ at, who: who.trim(), text: text.trim() })
    }
  }
  if (!recognized) return transcript.trim()
  return rows.map((r) => (r.who ? `[${r.at}] ${r.who}：${r.text}` : r.text)).join('\n')
}

// joinText 拼接同一说话人的两段。中文直接相连（分段常切在词中间，加空格反而把词
// 拆开）；两边都是拉丁字母或数字时补一个空格，免得两个英文词粘成一个。
function joinText(a: string, b: string): string {
  if (/[A-Za-z0-9]$/.test(a) && /^[A-Za-z0-9]/.test(b)) return `${a} ${b}`
  return a + b
}

// CONTENT_RULES 是所有支路共用的内容红线：只管写什么，不管长什么样，所以技能在场时
// 照给不误。第二条是其中最要紧的——纪要会被当作会议记录用，编出来的待办和责任人是
// 会真害到人的那类错误。
//
// 第三条（已定与待定分开）来自真实会议的实测：需求核对、方案讨论这类会，大部分是
// 「要不…」「看领导怎么协商」，真正拍板的只有几条。不写这条，模型会把讨论中的倾向
// 当成结论写进纪要——与编造待办是同一类会害人的错，只是更隐蔽。
const CONTENT_RULES = [
  '先通读全文再落笔，不要逐句复述。',
  '**转写里没有的信息一律不要补**——写出来的东西会被当作会议记录用，编出来的待办和责任人是会真害到人的。',
  '**分清已经定了的和还没定的**：明确表态同意、拍了板的才写成结论；还在讨论的、倾向性的意见、要等别人（领导、其他部门、合同相关方）确认的，写明「待确认」和等谁确认，不要把倾向写成定论。',
  '按上下文改正明显的同音错误，同一个专有名词全文统一成一种写法；拿不准的保留原文，改动大的在括号里附上原文。',
]

// digestLimit 是速览的字数上限，随会议时长分三档。
//
// 它不是可有可无的修饰：不限长的话模型照样写一大篇，两段式就白做了——用户仍旧要等
// 一篇长文，仍旧没有「先看一眼再决定」的机会。600 字是「结构装得下、一屏读得完」的
// 折中，但那是按半小时以内的会定的：一场一个半小时、七八个议题的会挤进 600 字，大纲
// 每条只剩几个字，待确认事项也放不下。所以长会放宽，最多到 1200 字——仍然是一屏半，
// 仍然远短于正式纪要。
function digestLimit(audioMs: number): number {
  const minutes = audioMs / 60_000
  if (minutes <= 30) return 600
  if (minutes <= 60) return 900
  return 1200
}

// DIGEST_SHAPE 是速览的骨架，给死不给模型发挥。
//
// 腾讯会议、豆包会议那种「一眼能看完」的观感，靠的就是结构固定：主题定位、总结讲
// 清楚干了什么、大纲带时间戳可回溯、待办单独拎出来。模型自由发挥时最爱做的是按
// 发言人分段复述，那读起来和转写本身没区别。
//
// 「待确认」单列一段，是 CONTENT_RULES 第三条在速览里的落点：没有这个位置，没定的
// 事要么被塞进待办（变成了要人去做的事），要么被写成大纲里的结论。
const DIGEST_SHAPE = [
  '**主题**：一句话',
  '**总结**：2–4 句，讲清楚这场会讨论了什么、达成了什么、留下了什么分歧',
  '**大纲**：',
  '- [00:00–04:12] 议题一 —— 一句话结论',
  '- [04:12–11:30] 议题二 —— 一句话结论',
  '**待办**：',
  '- [ ] 事项（责任人 / 时间）',
  '**待确认**：',
  '- 事项（等谁确认）',
]

// digestRules 是速览独有的那几条，排在 CONTENT_RULES 前面。
//
// 第一条的结构样例由 buildDigestPrompt 紧跟着插进去；第三条是防模型顺手去写文件的，
// 见 buildDigestPrompt 的注释。「不按人写」一条：编号本身的归属就不可靠（见
// ASR_CAVEATS），速览又没有篇幅交代这层不确定，按议题写才不会把话安错人。
function digestRules(audioMs: number): string[] {
  return [
    '严格按下面的结构输出，不要增删章节，不要写成正式公文：',
    `全文控制在 ${digestLimit(audioMs)} 字以内。`,
    '**直接输出速览本身**：不要写文件、不要调工具，也不要在前后加说明或寒暄。',
    '大纲按议题列，相邻的小议题合并，通常 3–8 条；每条都要带这段议题在录音里的起止时间，取自转写的时间戳。',
    '按议题写，不要写「S1 认为」「S2 提出」这类按说话人的表述。',
    '待办只写转写里真正交办过的事，责任人只写被明确点名的人（照转写里的称呼写），点不出名的写「待定」；待确认只写会上明确留下没定的事。哪一段没有内容就写「本场未提及」。',
  ]
}

/**
 * buildDigestPrompt 组装「速览」那条请求：录完自动发的就是它。
 *
 * 与正式纪要的三处关键区别，少一条这个功能就退化回原样：
 *  - **不挂技能**：技能一加载，整套公文模板就跟着进来了，产出又变成一篇长文；
 *  - **不落盘**：速览是拿来看的，不是拿来存的。不写死这一条，模型会顺手去调写文件
 *    的工具，白白多一个回合，工作区里还多出一个没人要的文件；
 *  - **限长 + 给死结构**：理由见 digestLimit 与 DIGEST_SHAPE。
 */
export function buildDigestPrompt(opts: { mark: RecordingMark; transcript: string }): string {
  const { mark, transcript } = opts
  const lines: string[] = []

  lines.push('先给这场录音做一份**速览**，我看完再决定要不要出正式的会议纪要文档。')
  lines.push('')

  for (const line of materialBriefing(mark, transcript)) lines.push(line)
  lines.push('')

  lines.push('要求：')
  const rules = [...digestRules(mark.audioMs), ...CONTENT_RULES]
  rules.forEach((rule, i) => {
    lines.push(`${i + 1}. ${rule}`)
    // 结构样例紧跟在第 1 条后面：它是那条规则的一部分，隔开了模型就不一定照着走。
    if (i === 0) {
      lines.push('')
      for (const shape of DIGEST_SHAPE) lines.push(`   ${shape}`)
      lines.push('')
    }
  })
  lines.push('')

  for (const line of transcriptBlock(transcript)) lines.push(line)

  // 标记放**末尾**。放首行的话，会话标题在模型生成之前会回落到消息首行，
  // 侧栏里就是一串 `<!-- runcode-recording {"id":…` 的 JSON —— 实测踩过。
  lines.push('')
  lines.push(recordingMarker(mark, 'digest'))

  return lines.join('\n')
}

/**
 * buildMinutesPrompt 组装正式纪要文档那条请求：用户在卡片上点了按钮，或者设置里
 * 打开了「录完直接出纪要文档」，走的都是它。
 *
 * 要求分两层，分界线是「格式」还是「内容」：
 *  - **格式层**（结构、排版、章节编号、产出形式、文件名）挑到技能时**整层让给技能**，
 *    这里一个字都不说。原先的写法是开头一句「请使用 X 技能」，底下照旧跟着「按议题
 *    归纳」「结论/待办/责任人/时间点分别列出」「写成 Markdown 存到 会议纪要-….md」——
 *    两份要求摆在一起，模型照的是更具体、位置更靠后的那份，于是技能里的模板被整个
 *    盖掉，装了国开模板也出不来国开格式的纪要。没有技能时才由这里给出结构与落盘要求。
 *  - **内容层**（CONTENT_RULES）两条支路都给：它只管纪要写什么，不管长什么样，与任何
 *    技能的模板都不冲突。「转写里没有的不要补」这条国开技能自己也写着，这里是加固。
 *
 * **转写全文照旧重新附上**，哪怕速览那条消息里已经有一份。历史可能被压缩、可能换了
 * 会话、卡片也可能是从标记重建出来的——多花的是输入 token，换来的是「这个按钮任何
 * 时候点都好使」。可靠性优先于省那几百个输入 token。
 */
export function buildMinutesPrompt(opts: {
  mark: RecordingMark
  transcript: string
  skill?: string
  outPath: string
}): string {
  const { mark, transcript, skill, outPath } = opts
  const lines: string[] = []

  if (skill) {
    lines.push(`请使用「${skill}」技能整理下面这场会议的纪要：先加载技能，然后按它写的流程和模板来。`)
  } else {
    lines.push('请把下面这场会议的录音转写整理成一份会议纪要。')
  }
  lines.push('')

  for (const line of materialBriefing(mark, transcript)) lines.push(line)
  lines.push('')

  lines.push('要求：')
  for (const line of minutesRules(skill, outPath, !!mark.startedAt)) lines.push(line)
  lines.push('')

  for (const line of transcriptBlock(transcript)) lines.push(line)

  // 同 buildDigestPrompt：标记放末尾，理由见那边。这里带的是 'doc'——恢复历史时
  // 它只负责把提示词原文换回一句短的，不再补卡片（卡片由速览那条负责）。
  lines.push('')
  lines.push(recordingMarker(mark, 'doc'))

  return lines.join('\n')
}

/**
 * minutesRules 拼出「要求」那几条，序号自动排。
 *
 * 有技能时第一条只做一件事：把格式层整个指给技能，并挡住后面几条——不然「先通读
 * 全文再落笔」这种话也会被当成对产出形式的指示去理解。没有技能时，第一条和最后一条
 * 换成这里自己的结构与落盘要求。
 *
 * 会议信息那一条两条支路都给，它管的是「纪要头上那几栏从哪儿来」：
 *  - 时间是录音自己知道的，明说可以直接用。国开技能的流程是先把缺的栏目问齐再生成，
 *    不点破的话，连录音开始时间这种现成的东西也要回头问一遍用户；
 *  - 参会人员、主持人这几栏转写给不出来，而模型最顺手的做法是把转写里被喊到的名字
 *    填进去——喊到名字不代表在场，更不代表是主持人。这和 speakerBriefing 切断
 *    「人名 ↔ 编号」是同一道防线，在文档这一段落到了具体的栏目上。
 */
function minutesRules(skill: string | undefined, outPath: string, knowsTime: boolean): string[] {
  const head = skill
    ? `纪要的格式、结构、章节编号、产出形式与文件名**一律以「${skill}」技能为准**；下面几条只管内容，不要拿它们去改技能规定的样子。`
    : '按议题归纳，结论、待办、责任人、时间点分别列出。'
  const known = knowsTime ? '会议时间就用上面写的录音时间，不必再问；' : ''
  const meta = `${known}地点、主持人、参会人员、记录人这些转写里确认不了的，${skill ? '按技能的流程问我或留占位' : '写「待补充」'}——**不要**拿转写里被提到的名字去填参会人员或主持人。`
  const tail = skill
    ? []
    : [`写成 Markdown，保存到工作区的 \`${outPath}\`，然后简要说明纪要包含哪几部分。`]
  return [head, meta, ...CONTENT_RULES, ...tail].map((rule, i) => `${i + 1}. ${rule}`)
}

// SPEAKER_RE 从转写里抠出说话人标签。落盘格式是 `**[00:03] S1**：文本`
// （见 Go 侧 Transcript.Markdown），两边必须对上。
const SPEAKER_RE = /^\*\*\[[^\]]+\]\s*([^*]+?)\*\*：/gm

/** speakerLabels 列出这份转写里实际出现过的说话人标签，按首次出现排序。 */
export function speakerLabels(transcript: string): string[] {
  const seen: string[] = []
  for (const m of transcript.matchAll(SPEAKER_RE)) {
    const label = m[1].trim()
    if (label && !seen.includes(label)) seen.push(label)
  }
  return seen
}

/**
 * speakerBriefing 把「这些标签到底是什么」讲清楚，不让模型去猜。
 *
 * 光说一句「S1/S2 是声纹编号」不够，实测模型仍会去猜编号和转写里提到的人名之间
 * 的对应关系（「S1 应该就是阿波哥」）。所以这里做三件事：
 *  - 列出本场实际出现的标签，而不是举例说「S1／S2 这类」；
 *  - 说明编号的**作用域**：同一场内同号即同人，跨场不通用，号大小没有含义；
 *  - 明确切断「内容里提到的人名」与「编号」之间的联系——那是这类纪要最容易出的
 *    硬错误，一旦把发言安到具体某人头上，纪要就成了会害人的东西。
 */
export function speakerBriefing(transcript: string): string[] {
  const labels = speakerLabels(transcript)
  const clustered = labels.filter((l) => /^S\d+$/.test(l))
  const named = labels.filter((l) => !/^S\d+$/.test(l))

  const out: string[] = []
  out.push(`- 本场出现的说话人标签：${labels.length ? labels.join('、') : '（无）'}`)
  if (clustered.length) {
    out.push(
      `- ${clustered.join('、')} 是声纹聚类给出的编号，**不是姓名**。` +
      '同一个编号在本场内指同一个人；编号的数字本身没有含义，也不在不同场次之间通用。',
    )
    // 反过来不成立：聚类常把同一个人拆成几个编号（实测一场会出了 15 个、最大到
    // S92）。不说这句，模型会写出「共有 15 人参会」，或者把同一个人的前后发言
    // 当成两个人的分歧。只有一个编号时没有拆分可言，不说。
    if (clustered.length > 1) {
      out.push('- 聚类可能把同一个人拆成几个编号，编号的个数不等于参会人数。')
    }
  }
  if (named.length) {
    out.push(`- ${named.join('、')} 是配置里填的显示名，对应本机麦克风录到的人。`)
  }
  out.push(
    '- 转写内容里提到的人名（例如有人喊了一声某某），**不能**据此认定某个编号就是那个人——' +
    '喊名字的和被喊的从来不是同一个人。除非某个编号自报姓名，否则一律保留编号，' +
    '并在需要时注明「无法确认对应哪位」。',
  )
  return out
}

// 对话里代替整篇提示词显示的那一句：实时发送与历史恢复共用，两边必须一致，否则
// 同一条消息在「刚发出去」和「重开这条对话」时长得不一样。
//
// 三句分别对应三种消息：速览、正式文档、以及两段式之前的老消息。老的那句原样留着，
// 存量历史里全是它——改掉它不会让旧消息变好看，只会让它对不上当初发出去的样子。

/** digestDisplayText 是速览请求那条。 */
export function digestDisplayText(title: string): string {
  return `录音速览 · 已附上《${title || '录音'}》的转写全文`
}

/** docDisplayText 是正式纪要文档请求那条。 */
export function docDisplayText(title: string): string {
  return `生成会议纪要文档 · 已附上《${title || '录音'}》的转写全文`
}

/** minutesDisplayText 是两段式之前那条一步到位的纪要请求。只有历史恢复还会用到。 */
export function minutesDisplayText(title: string): string {
  return `录音纪要 · 已附上《${title || '录音'}》的转写全文`
}
