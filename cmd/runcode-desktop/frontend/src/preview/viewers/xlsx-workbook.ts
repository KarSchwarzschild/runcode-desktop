import { read, utils } from 'xlsx'
import { normalizeSheetGrid } from '@/preview/classify'

export type WorkbookSheet = { name: string; rows: string[][] }

// Keep the parser behind the viewer's lazy import. Only plain text reaches React;
// library-generated HTML must never be injected into the privileged WebView.
export function readWorkbook(buffer: ArrayBuffer): WorkbookSheet[] {
  const workbook = read(buffer, { type: 'array' })
  const sheets = workbook.SheetNames.map((name) => ({
    name,
    rows: normalizeSheetGrid(utils.sheet_to_json(workbook.Sheets[name], { header: 1, raw: false, defval: '' }) as unknown[][]),
  }))
  return sheets.length ? sheets : [{ name: 'Sheet1', rows: [] }]
}
