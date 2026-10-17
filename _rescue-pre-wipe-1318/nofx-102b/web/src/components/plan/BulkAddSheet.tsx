// P5.2 — bulk add: one text box, multi-line natural entry → parsed staged rows →
// preview → ONE Save (each row → a sticky owner level; appears next read with 👤).
// Same parser as the blind-mark grammar (bulkParse.ts).

import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { toast } from 'sonner'
import type { Language } from '../../i18n/translations'
import type { RealignChange } from '../../lib/api/plan'
import { tp } from '../../i18n/plan-translations'
import { api } from '../../lib/api'
import { guardedCall } from '../../lib/api/guarded'
import { parseBulkLevels } from './bulkParse'
import { fmtPrice } from './levelState'

interface Props {
  open: boolean
  traderId: string
  symbol: string
  language: Language
  onClose: () => void
  onSaved: (change?: RealignChange) => void
}

export function BulkAddSheet({
  open,
  traderId,
  symbol,
  language,
  onClose,
  onSaved,
}: Props) {
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const ref = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    if (!open) return
    setText('')
    setBusy(false)
    setTimeout(() => ref.current?.focus(), 30)
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null

  const rows = parseBulkLevels(text)

  const save = async () => {
    if (!rows.length || busy) return
    setBusy(true)
    let ok = 0
    let firstErr = ''
    for (const r of rows) {
      const label = r.type || '👤'
      // Never-latch (reset hotfix class): one thrown request must not freeze
      // the sheet mid-batch — it counts as a row failure and the loop goes on.
      const g = await guardedCall(() =>
        api.addOwnerLevel(
          traderId,
          { price: r.price, label, note: r.note },
          symbol
        )
      )
      if (g.ok && g.value.ok) ok++
      else if (!firstErr) firstErr = (g.ok ? g.value.error : g.error) || ''
    }
    setBusy(false)
    if (ok > 0) {
      toast.success(tp('overlayApplied', language), {
        description: `${ok}/${rows.length}`,
      })
      // W13 — ONE re-examination for the whole batch, never per row.
      onSaved({
        kind: 'bulk-add',
        summary: `bulk-added ${ok} level(s): ${rows
          .slice(0, 6)
          .map((r) => `${r.type || 'level'} ${r.price}`)
          .join(' · ')}`,
        batch_count: ok,
      })
      onClose()
    } else {
      toast.error(tp('saveFailed', language), { description: firstErr })
    }
  }

  const sheet = (
    <div
      className="fixed inset-0 z-[60] flex items-end sm:items-center justify-center"
      style={{ background: 'rgba(0,0,0,0.6)' }}
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={tp('bulkAdd', language)}
        className="vl-sheet-in w-full sm:max-w-[430px]"
        style={{
          background: 'var(--vl-card-2)',
          border: '1px solid var(--vl-gold-line)',
          borderRadius: '16px 16px 14px 14px',
          overflow: 'hidden',
          fontFamily: 'var(--vl-font-ui)',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <div
          className="flex items-center gap-2 px-4 py-3"
          style={{ borderBottom: '1px solid var(--vl-hair)' }}
        >
          <span
            style={{
              fontFamily: 'var(--vl-font-display)',
              fontSize: 18,
              fontWeight: 600,
              flex: 1,
              color: 'var(--vl-ivory)',
            }}
          >
            {tp('bulkAdd', language)}
          </span>
          <button
            onClick={onClose}
            aria-label={tp('cancel', language)}
            style={{ color: 'var(--vl-muted)' }}
          >
            ✕
          </button>
        </div>
        <div className="px-4 py-3 flex flex-col gap-2">
          <span className="text-[10px]" style={{ color: 'var(--vl-faint)' }}>
            {tp('bulkAddHint', language)}
          </span>
          <textarea
            ref={ref}
            value={text}
            onChange={(e) => setText(e.target.value)}
            rows={5}
            placeholder={
              '30246 D-zone strong 4h OB\n30150-30152 S-zone\n30000 RN magnet'
            }
            className="vl-num text-[12px] px-3 py-2"
            style={{
              background: 'rgba(201,162,75,0.05)',
              border: '1px solid var(--vl-gold-line)',
              borderRadius: 8,
              color: 'var(--vl-ivory)',
              resize: 'vertical',
            }}
          />
          {/* preview */}
          {rows.length > 0 && (
            <div
              style={{
                border: '1px solid var(--vl-hair)',
                borderRadius: 8,
                overflow: 'hidden',
              }}
            >
              <div
                className="px-2.5 py-1 text-[9px] font-bold tracking-widest"
                style={{
                  color: 'var(--vl-gold)',
                  background: 'var(--vl-gold-dim)',
                }}
              >
                {tp('preview', language)} · {rows.length}
              </div>
              {rows.map((r, i) => (
                <div
                  key={i}
                  className="flex items-center gap-2 px-2.5 py-1 text-[11px]"
                  style={{
                    borderTop: i ? '1px solid var(--vl-hair)' : undefined,
                  }}
                >
                  <span className="vl-num" style={{ color: 'var(--vl-ivory)' }}>
                    {fmtPrice(r.price)}
                    {r.priceHi ? `–${fmtPrice(r.priceHi)}` : ''}
                  </span>
                  {r.type && (
                    <span style={{ color: 'var(--vl-gold)' }}>{r.type}</span>
                  )}
                  <span
                    className="truncate"
                    style={{ color: 'var(--vl-muted)' }}
                  >
                    {r.note}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
        <div
          className="flex gap-2 px-4 py-3"
          style={{ borderTop: '1px solid var(--vl-hair)' }}
        >
          <button
            onClick={onClose}
            className="text-[12px] px-3 py-2.5"
            style={{
              color: 'var(--vl-muted)',
              border: '1px solid var(--vl-hair)',
              borderRadius: 9,
            }}
          >
            {tp('cancel', language)}
          </button>
          <button
            onClick={save}
            disabled={!rows.length || busy}
            className="flex-1 text-[12px] font-semibold px-4 py-2.5"
            style={{
              background:
                rows.length && !busy ? 'var(--vl-gold)' : 'var(--vl-faint)',
              color: 'var(--vl-ink)',
              borderRadius: 9,
              opacity: busy ? 0.6 : 1,
            }}
          >
            {tp('save', language)}
          </button>
        </div>
      </div>
    </div>
  )

  return createPortal(sheet, document.body)
}
