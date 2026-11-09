import { createContext } from 'react'

// Supplied by the session layer; rendering components never infer an owner.
export type ArtifactActions = {
  external: (path: string) => Promise<void>
  reveal: (path: string) => Promise<void>
  copyPath: (path: string) => Promise<void>
}
const unavailable = async () => { throw new Error('没有文件来源会话') }
export const ArtifactActionsContext = createContext<ArtifactActions>({ external: unavailable, reveal: unavailable, copyPath: unavailable })
