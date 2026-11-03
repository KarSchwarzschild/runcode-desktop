// Synthetic approval UI only: no native bindings, accounts, network or tools.
import { useState } from 'react'
import { type PermissionRequest } from '@/core/bridge'
import { PermissionModal } from '@/shell/permission-modal'

const fixtures: PermissionRequest[] = [
  {
    id: 'synthetic-network',
    summary: { toolName: 'WebFetch', operation: 'network', risk: 'medium', networkHost: 'example.invalid', networkOrigin: 'https://example.invalid:8443', networkRoute: 'auto' },
    inputPreview: '{"url":"https://example.invalid:8443/docs?token=[redacted]","prompt":"读取这份合成文档"}',
    grantScope: '记住此网络 origin 与访问路线：https://example.invalid:8443；跨 origin 跳转须另行授权',
    allowedDecisions: ['allow-once', 'allow-session', 'allow-project', 'deny'],
  },
  {
    id: 'synthetic-command',
    summary: { toolName: 'Bash', operation: 'execute', risk: 'high', commandCategory: 'unknown' },
    command: 'python report.py --source synthetic',
    harmReason: '风险等级：低风险 · 但与当前用户任务未明确相关，已转人工确认。',
    grantScope: '记住此条完整命令及执行壳；不同参数、脚本或组合命令须重新批准',
    allowedDecisions: ['allow-once', 'allow-session', 'allow-project', 'deny'],
  },
  {
    id: 'synthetic-mcp',
    summary: { toolName: 'mcp__fixture__review', operation: 'external', risk: 'high', mcpServer: 'fixture', mcpTool: 'review' },
    inputPreview: JSON.stringify({ operation: 'review', items: Array.from({ length: 24 }, (_, i) => `合成参数 ${i + 1}`), authorization: '[redacted]' }, null, 2),
    allowedDecisions: ['allow-once', 'deny'],
  },
]

export function AuthorizationPreview() {
  const [index, setIndex] = useState(0)
  const [answers, setAnswers] = useState<string[]>([])
  return (
    <main className="min-h-screen bg-bg p-6 text-ink">
      <h2 className="text-[20px] m-0">权限审批合成交互测试</h2>
      <output data-testid="approval-answers">{answers.join(',')}</output>
      {index < fixtures.length && <PermissionModal req={fixtures[index]} onDecide={(decision) => {
        setAnswers((previous) => [...previous, decision])
        setIndex((previous) => previous + 1)
      }} />}
    </main>
  )
}
