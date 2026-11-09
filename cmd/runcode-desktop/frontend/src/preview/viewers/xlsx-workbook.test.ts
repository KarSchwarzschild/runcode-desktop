import { describe, expect, it } from 'vitest'
import { utils, version, write, type WorkSheet } from 'xlsx'
import { readWorkbook } from './xlsx-workbook'

function encode(sheets: [string, WorkSheet][]): ArrayBuffer {
  const workbook = utils.book_new()
  for (const [name, sheet] of sheets) utils.book_append_sheet(workbook, sheet, name)
  return write(workbook, { type: 'array', bookType: 'xlsx' }) as ArrayBuffer
}

describe('readWorkbook', () => {
  it('loads the security-fixed, pinned SheetJS distribution', () => {
    expect(version).toBe('0.20.3')
  })

  it('reads Chinese names, multiple sheets and rectangular text grids from XLSX bytes', () => {
    const buffer = encode([
      ['统计', utils.aoa_to_sheet([['姓名', '数量', '备注'], ['甲', 2], ['乙', 0, '完成']])],
      ['补充', utils.aoa_to_sheet([['第二页']])],
    ])
    expect(readWorkbook(buffer)).toEqual([
      { name: '统计', rows: [['姓名', '数量', '备注'], ['甲', '2', ''], ['乙', '0', '完成']] },
      { name: '补充', rows: [['第二页']] },
    ])
  })

  it('preserves formatted numbers, dates, booleans and cached formula results', () => {
    const sheet: WorkSheet = {
      '!ref': 'A1:E1',
      A1: { t: 'n', v: 1234.5, z: '0.00' },
      B1: { t: 'n', v: 0.125, z: '0.0%' },
      C1: { t: 'n', v: 45292, z: 'yyyy-mm-dd' },
      D1: { t: 'b', v: true },
      E1: { t: 'n', v: 3, f: '1+2' },
    }
    expect(readWorkbook(encode([['格式', sheet]]))).toEqual([
      { name: '格式', rows: [['1234.50', '12.5%', '2024-01-01', 'TRUE', '3']] },
    ])
  })

  it('keeps untrusted cell content as text rather than producing HTML', () => {
    const cells = ['<img src=x onerror=alert(1)>', '</td><script>evil()</script>', '&<>"']
    expect(readWorkbook(encode([['文本', utils.aoa_to_sheet([cells])]]))).toEqual([
      { name: '文本', rows: [cells] },
    ])
  })

  it('keeps empty sheets selectable', () => {
    expect(readWorkbook(encode([['空表', utils.aoa_to_sheet([])]]))).toEqual([
      { name: '空表', rows: [] },
    ])
  })

  it('treats prototype-related cell values and sheet names as data', () => {
    const before = Object.getOwnPropertyDescriptors(Object.prototype)
    const cells = ['__proto__', 'constructor', 'prototype']
    const result = readWorkbook(encode([['constructor', utils.aoa_to_sheet([cells])]]))
    expect(result).toEqual([{ name: 'constructor', rows: [cells] }])
    expect(Object.getOwnPropertyDescriptors(Object.prototype)).toEqual(before)
  })

  it('rejects a truncated ZIP instead of inventing an empty workbook', () => {
    const valid = encode([['数据', utils.aoa_to_sheet([['value']])]])
    expect(() => readWorkbook(valid.slice(0, 32))).toThrow()
  })
})
