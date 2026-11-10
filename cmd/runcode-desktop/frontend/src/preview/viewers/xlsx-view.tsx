// XlsxView renders a workbook as one React table per sheet (SheetJS), with a sheet
// switcher when there is more than one. Cells go through sheet_to_json as plain
// strings and React's own text escaping — workbook content is untrusted, and
// sheet_to_html output must never be injected raw (its escaping has known gaps for
// rich-text cells, so that path is an XSS vector inside the privileged WebView).
import { useEffect, useRef, useState } from 'react'
import { errCode, errText, readArtifactBytes } from '@/core/bridge'
import type { WorkbookSheet } from './xlsx-workbook'
import { ViewerError } from './viewer-error'
import { PreviewLoading } from '../loading'

// onMissing fires when the file is gone rather than unreadable, so the panel can
// close the tab instead of showing an error for something the user cannot fix.
export function XlsxView({ relPath: _relPath, reference, reloadKey, onMissing }: { relPath: string; reference: string; reloadKey: number; onMissing?: () => void }) {
  const [sheets, setSheets] = useState<WorkbookSheet[] | null>(null)
  const [active, setActive] = useState(0)
  const [err, setErr] = useState('')
  const onMissingRef = useRef(onMissing)
  onMissingRef.current = onMissing
  useEffect(() => {
    let cancelled = false
    setSheets(null)
    setErr('')
    setActive(0)
    void (async () => {
      try {
        const buf = await readArtifactBytes(reference)
        const { readWorkbook } = await import('./xlsx-workbook')
        if (cancelled) return
        const sheets = readWorkbook(buf)
        if (!cancelled) setSheets(sheets)
      } catch (e) {
        if (!cancelled) {
          if (errCode(e) === 'not_found') { onMissingRef.current?.(); return }
          setErr(errText(e))
        }
      }
    })()
    return () => { cancelled = true }
  }, [reference, reloadKey])
  if (err) return <ViewerError reference={reference} message={err} />
  if (!sheets) return <PreviewLoading hint="正在加载表格预览…" />
  return (
    <div className="min-h-full flex flex-col">
      {sheets.length > 1 && (
        <div className="flex-none flex gap-1 flex-wrap px-3 py-2 border-b border-line2 bg-surface">
          {sheets.map((s, i) => (
            <button
              key={s.name}
              type="button"
              onClick={() => setActive(i)}
              className={`px-2.5 py-1 rounded-md text-[12px] ${i === active ? 'bg-primarysoft text-primaryink font-medium' : 'text-muted hover:bg-surface2'}`}
            >
              {s.name}
            </button>
          ))}
        </div>
      )}
      <div className="flex-1 overflow-auto p-3 xlsx-host">
        {sheets[active].rows.length === 0 ? (
          <em>空表</em>
        ) : (
          <table>
            <tbody>
              {sheets[active].rows.map((row, ri) => (
                <tr key={ri}>
                  {row.map((cell, ci) => (
                    <td key={ci}>{cell}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
