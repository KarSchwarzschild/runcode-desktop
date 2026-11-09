// Tool-input labels mirror the backend approval scope; raw proxy credentials
// must not become a convenient second display of a rejected argument.
export function networkRouteLabel(proxy: unknown): string {
  const route = typeof proxy === 'string' ? proxy.trim() : ''
  if (!route || route === 'auto') return '自动：原配置失败后检查本机代理（不绕过固定配置）'
  if (route === 'configured') return '仅使用原配置，不自动切换代理'
  try {
    const u = new URL(route)
    if (route.length > 256 || u.username || u.password || u.search || u.hash || !['http:', 'https:', 'socks5:', 'socks5h:'].includes(u.protocol)) return '不支持的代理配置'
    return `${u.protocol}//${u.host}（仅本次请求）`
  } catch { return '不支持的代理配置' }
}
