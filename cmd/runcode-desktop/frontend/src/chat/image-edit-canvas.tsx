// 中央图片标注画板：底图 + 归一化操作栈；保存为同 generation 目录下的 edited-*.png。
import { useEffect, useRef, useState } from 'react'
import { errText, saveImageGenerationEdit, type ImageGenerationArtifact } from '@/core/bridge'
import { ConfirmDialog } from '@/ui/confirm-dialog'
import { BTN, BTN_PRIMARY } from '@/ui/tokens'
import {
  IMAGE_EDIT_COLORS, IMAGE_EDIT_MAX_BYTES, appendStroke, appendText, fitEditOutputSize,
  initialEditState, isDirty, redo, selectColor, selectTool, undo, type ImageEditState, type NormPoint,
} from './image-edit-ops'

type Props = {
  sessionId: string
  sourcePath: string
  imageUrl: string
  naturalWidth: number
  naturalHeight: number
  onClose: () => void
  onSaved: (art: ImageGenerationArtifact) => void
  /** 嵌入栅格生图 host：不自建 fixed 遮罩与放弃确认。 */
  hosted?: boolean
  onDirtyChange?: (dirty: boolean) => void
}

export function ImageEditCanvas({
  sessionId, sourcePath, imageUrl, naturalWidth, naturalHeight, onClose, onSaved,
  hosted = false, onDirtyChange,
}: Props) {
  const [state, setState] = useState<ImageEditState>(() => initialEditState())
  const [confirmClose, setConfirmClose] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [textDraft, setTextDraft] = useState('')
  const stageRef = useRef<HTMLDivElement>(null)
  const drawing = useRef<NormPoint[] | null>(null)
  const imgRef = useRef<HTMLImageElement>(null)

  useEffect(() => {
    onDirtyChange?.(isDirty(state))
  }, [state.operations.length, onDirtyChange, state])

  useEffect(() => {
    if (hosted) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation()
        requestClose()
        return
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'z') {
        e.preventDefault()
        setState((s) => (e.shiftKey ? redo(s) : undo(s)))
      }
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- requestClose closes over latest dirty
  }, [state.operations.length, hosted])

  function requestClose() {
    if (hosted) {
      onClose()
      return
    }
    if (isDirty(state)) setConfirmClose(true)
    else onClose()
  }

  function toNorm(clientX: number, clientY: number): NormPoint | null {
    const el = stageRef.current
    if (!el) return null
    const r = el.getBoundingClientRect()
    if (r.width <= 0 || r.height <= 0) return null
    return { x: (clientX - r.left) / r.width, y: (clientY - r.top) / r.height }
  }

  function onPointerDown(e: React.PointerEvent) {
    if (state.tool === 'text') {
      const p = toNorm(e.clientX, e.clientY)
      if (!p) return
      const text = textDraft.trim() || window.prompt('输入标注文字') || ''
      setState((s) => appendText(s, text, p.x, p.y))
      setTextDraft('')
      return
    }
    const p = toNorm(e.clientX, e.clientY)
    if (!p) return
    drawing.current = [p]
    ;(e.target as HTMLElement).setPointerCapture?.(e.pointerId)
  }

  function onPointerMove(e: React.PointerEvent) {
    if (!drawing.current) return
    const p = toNorm(e.clientX, e.clientY)
    if (!p) return
    drawing.current = [...drawing.current, p]
  }

  function onPointerUp() {
    if (!drawing.current) return
    const pts = drawing.current
    drawing.current = null
    setState((s) => appendStroke(s, pts))
  }

  async function onSave() {
    if (busy) return
    setError('')
    setBusy(true)
    try {
      const pngBase64 = await composePngBase64(imageUrl, naturalWidth, naturalHeight, state)
      // 粗估：base64 长度约 4/3 字节；超限直接拒，不发宿主。
      const approxBytes = Math.floor((pngBase64.length * 3) / 4)
      if (approxBytes > IMAGE_EDIT_MAX_BYTES || pngBase64.length > IMAGE_EDIT_MAX_BYTES) {
        setError('合成结果超过 16 MiB 上限，请减少标注或使用更小原图')
        return
      }
      const art = await saveImageGenerationEdit({ sessionId, sourcePath, pngBase64 })
      onSaved(art)
      onClose()
    } catch (e) {
      setError(errText(e) || '保存失败')
    } finally {
      setBusy(false)
    }
  }

  const toolbar = (
    <>
      <div className={`flex items-center gap-2 px-4 py-3 border-b border-line2${hosted ? '' : ''}`}>
        {!hosted ? <span className="text-[14px] font-medium text-ink flex-none">标注图片</span> : null}
        <div className="flex items-center gap-1.5 flex-wrap">
          {IMAGE_EDIT_COLORS.map((c) => (
            <button
              key={c}
              type="button"
              aria-label={`颜色 ${c}`}
              className={`w-5 h-5 rounded-full border ${state.selectedColor === c ? 'border-ink ring-2 ring-primary/40' : 'border-line2'}`}
              style={{ background: c }}
              onClick={() => setState((s) => selectColor(s, c))}
            />
          ))}
        </div>
        <div className="flex items-center gap-1 ml-2">
          <button type="button" className={`${BTN} !px-2 !py-1 text-[12px] ${state.tool === 'pen' ? 'border-primary text-primaryink' : ''}`} onClick={() => setState((s) => selectTool(s, 'pen'))}>画笔</button>
          <button type="button" className={`${BTN} !px-2 !py-1 text-[12px] ${state.tool === 'text' ? 'border-primary text-primaryink' : ''}`} onClick={() => setState((s) => selectTool(s, 'text'))}>文字</button>
          <button type="button" className={`${BTN} !px-2 !py-1 text-[12px]`} onClick={() => setState(undo)} disabled={state.operations.length === 0}>撤销</button>
          <button type="button" className={`${BTN} !px-2 !py-1 text-[12px]`} onClick={() => setState(redo)} disabled={state.redo.length === 0}>重做</button>
        </div>
        <div className="flex-1" />
        {!hosted ? <button type="button" className={`${BTN} !px-3`} onClick={requestClose}>关闭</button> : null}
        <button type="button" className={`${BTN} ${BTN_PRIMARY} !px-3`} disabled={busy} onClick={() => { void onSave() }}>保存副本</button>
      </div>

      {state.tool === 'text' && (
        <div className="px-4 py-2 border-b border-line2 flex gap-2 items-center">
          <input
            className="flex-1 border border-line2 rounded-btn px-2 py-1 text-[13px] bg-surface"
            placeholder="输入文字后点击画面放置"
            value={textDraft}
            onChange={(e) => setTextDraft(e.target.value)}
            maxLength={200}
          />
        </div>
      )}

      {error ? <div className="px-4 py-2 text-[12px] text-amberink border-b border-line2">{error}</div> : null}

      <div className="flex-1 overflow-auto p-4 bg-surface2 flex items-center justify-center min-h-[280px]">
        <div
          ref={stageRef}
          data-edit-stage
          className="relative max-w-full max-h-[70vh] touch-none select-none cursor-crosshair"
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          onPointerCancel={onPointerUp}
        >
          <img ref={imgRef} src={imageUrl} alt="" className="block max-w-full max-h-[70vh] rounded-lg" draggable={false} />
          <svg className="absolute inset-0 w-full h-full pointer-events-none" viewBox="0 0 1 1" preserveAspectRatio="none">
            {state.operations.map((op, i) => {
              if (op.kind === 'stroke') {
                const d = op.points.map((p, idx) => `${idx === 0 ? 'M' : 'L'}${p.x} ${p.y}`).join(' ')
                return <path key={i} d={d} fill="none" stroke={op.color} strokeWidth={op.width} strokeLinecap="round" strokeLinejoin="round" />
              }
              return (
                <text key={i} x={op.x} y={op.y} fill={op.color} fontSize={op.fontSize} style={{ fontFamily: 'IBM Plex Sans, sans-serif' }}>
                  {op.text}
                </text>
              )
            })}
          </svg>
        </div>
      </div>
    </>
  )

  if (hosted) {
    return <div className="h-full flex flex-col overflow-hidden bg-surface">{toolbar}</div>
  }

  return (
    <div className="fixed inset-0 z-40 bg-[rgba(30,33,50,0.45)] backdrop-blur-[2px] flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-label="图片标注">
      <div className="w-[min(920px,96vw)] max-h-[92vh] bg-surface rounded-2xl shadow-modal flex flex-col overflow-hidden">
        {toolbar}
      </div>
      {confirmClose && (
        <ConfirmDialog
          title="放弃未保存的标注？"
          message="当前标注尚未保存，关闭后将丢失。"
          confirmLabel="放弃"
          onConfirm={() => { setConfirmClose(false); onClose() }}
          onCancel={() => setConfirmClose(false)}
        />
      )}
    </div>
  )
}

async function composePngBase64(
  imageUrl: string,
  naturalWidth: number,
  naturalHeight: number,
  state: ImageEditState,
): Promise<string> {
  const img = await loadImage(imageUrl)
  const { w, h } = fitEditOutputSize(naturalWidth || img.naturalWidth || 1, naturalHeight || img.naturalHeight || 1)
  const canvas = document.createElement('canvas')
  canvas.width = w
  canvas.height = h
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('无法创建画布')
  ctx.drawImage(img, 0, 0, w, h)
  for (const op of state.operations) {
    if (op.kind === 'stroke') {
      if (op.points.length < 2) continue
      ctx.strokeStyle = op.color
      ctx.lineWidth = Math.max(1, op.width * Math.min(w, h))
      ctx.lineCap = 'round'
      ctx.lineJoin = 'round'
      ctx.beginPath()
      op.points.forEach((p, i) => {
        const x = p.x * w
        const y = p.y * h
        if (i === 0) ctx.moveTo(x, y)
        else ctx.lineTo(x, y)
      })
      ctx.stroke()
    } else {
      ctx.fillStyle = op.color
      ctx.font = `${Math.max(12, op.fontSize * Math.min(w, h))}px "IBM Plex Sans", sans-serif`
      ctx.fillText(op.text, op.x * w, op.y * h)
    }
  }
  const dataUrl = canvas.toDataURL('image/png')
  const i = dataUrl.indexOf(',')
  return i >= 0 ? dataUrl.slice(i + 1) : dataUrl
}

function loadImage(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.onload = () => resolve(img)
    img.onerror = () => reject(new Error('无法加载底图'))
    img.src = url
  })
}
