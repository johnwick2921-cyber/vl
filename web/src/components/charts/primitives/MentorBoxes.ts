/*
 * MentorBoxes — a lightweight-charts v5 series primitive that fills each
 * mentor FTGH/FTGL box as a RECTANGLE from its formation bar to its end bar
 * (or the last bar while it is still live). Before m421b a box was drawn as
 * two full-width dotted price lines with no start and no end. The rectangle
 * list comes from chartOverlays.mentorBoxRects (pure, pinned by tests).
 */
import { CanvasRenderingTarget2D } from 'fancy-canvas'
import {
  IChartApi,
  IPrimitivePaneRenderer,
  IPrimitivePaneView,
  ISeriesApi,
  ISeriesPrimitive,
  SeriesAttachedParameter,
  SeriesType,
  Time,
  UTCTimestamp,
} from 'lightweight-charts'
import type { MentorBoxRect } from '../chartOverlays'

interface DrawRect {
  x1: number
  x2: number
  y1: number
  y2: number
  fill: string
  stroke: string
  label: string
}

class BoxRenderer implements IPrimitivePaneRenderer {
  constructor(private readonly rects: DrawRect[]) {}
  draw(target: CanvasRenderingTarget2D): void {
    target.useBitmapCoordinateSpace((scope) => {
      const ctx = scope.context
      const h = scope.horizontalPixelRatio
      const v = scope.verticalPixelRatio
      for (const r of this.rects) {
        const x = Math.round(Math.min(r.x1, r.x2) * h)
        const w = Math.max(1, Math.round(Math.abs(r.x2 - r.x1) * h))
        const y = Math.round(Math.min(r.y1, r.y2) * v)
        const ht = Math.max(1, Math.round(Math.abs(r.y2 - r.y1) * v))
        ctx.fillStyle = r.fill
        ctx.fillRect(x, y, w, ht)
        ctx.strokeStyle = r.stroke
        ctx.lineWidth = Math.max(1, Math.floor(h))
        ctx.strokeRect(x + 0.5, y + 0.5, w, ht)
        ctx.fillStyle = r.stroke
        ctx.font = `${Math.round(10 * v)}px sans-serif`
        ctx.textBaseline = 'top'
        ctx.fillText(r.label, x + Math.round(4 * h), y + Math.round(2 * v))
      }
    })
  }
}

class BoxPaneView implements IPrimitivePaneView {
  private rects: DrawRect[] = []
  constructor(private readonly src: MentorBoxes) {}
  update(): void {
    const chart = this.src.chart()
    const series = this.src.series()
    this.rects = []
    if (!chart || !series) return
    const ts = chart.timeScale()
    for (const b of this.src.data()) {
      const x1 = ts.timeToCoordinate(b.startTime as UTCTimestamp)
      const x2 = ts.timeToCoordinate(b.endTime as UTCTimestamp)
      const y1 = series.priceToCoordinate(b.top)
      const y2 = series.priceToCoordinate(b.bottom)
      if (x1 === null || x2 === null || y1 === null || y2 === null) continue
      this.rects.push({
        x1: x1 as number,
        x2: x2 as number,
        y1: y1 as number,
        y2: y2 as number,
        fill: b.fill,
        stroke: b.color,
        label: b.label,
      })
    }
  }
  renderer(): IPrimitivePaneRenderer {
    return new BoxRenderer(this.rects)
  }
  zOrder(): 'bottom' {
    return 'bottom'
  }
}

export class MentorBoxes implements ISeriesPrimitive<Time> {
  private _chart: IChartApi | undefined
  private _series: ISeriesApi<SeriesType> | undefined
  private _requestUpdate: (() => void) | undefined
  private _data: MentorBoxRect[] = []
  private readonly _views = [new BoxPaneView(this)]

  attached(param: SeriesAttachedParameter<Time>): void {
    this._chart = param.chart as unknown as IChartApi
    this._series = param.series as ISeriesApi<SeriesType>
    this._requestUpdate = param.requestUpdate
    this._requestUpdate?.()
  }
  detached(): void {
    this._requestUpdate = undefined
    this._chart = undefined
    this._series = undefined
  }
  setData(data: MentorBoxRect[]): void {
    this._data = data
    this._requestUpdate?.()
  }
  data(): MentorBoxRect[] {
    return this._data
  }
  chart(): IChartApi | undefined {
    return this._chart
  }
  series(): ISeriesApi<SeriesType> | undefined {
    return this._series
  }
  updateAllViews(): void {
    this._views.forEach((v) => v.update())
  }
  paneViews(): readonly IPrimitivePaneView[] {
    return this._views
  }
}
