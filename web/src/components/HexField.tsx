import { useEffect, useRef } from 'react'

const GLYPHS = '0123456789abcdef:'

type Particle = {
  x: number
  y: number
  vx: number
  vy: number
  ch: string
  a: number
}

export default function HexField() {
  const ref = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    const canvas = ref.current
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return

    const mouse = { x: -9999, y: -9999 }
    const onMove = (e: PointerEvent) => {
      mouse.x = e.clientX
      mouse.y = e.clientY
    }
    window.addEventListener('pointermove', onMove)

    let w = 0
    let h = 0
    let particles: Particle[] = []
    const resize = () => {
      w = canvas.width = window.innerWidth * devicePixelRatio
      h = canvas.height = window.innerHeight * devicePixelRatio
      canvas.style.width = window.innerWidth + 'px'
      canvas.style.height = window.innerHeight + 'px'
      ctx.setTransform(devicePixelRatio, 0, 0, devicePixelRatio, 0, 0)
      const n = Math.min(110, Math.floor((window.innerWidth * window.innerHeight) / 14000))
      particles = Array.from({ length: n }, () => ({
        x: Math.random() * window.innerWidth,
        y: Math.random() * window.innerHeight,
        vx: (Math.random() - 0.5) * 0.28,
        vy: (Math.random() - 0.5) * 0.28,
        ch: GLYPHS[Math.floor(Math.random() * GLYPHS.length)],
        a: 0.12 + Math.random() * 0.35,
      }))
    }
    resize()
    window.addEventListener('resize', resize)

    let raf = 0
    const tick = () => {
      ctx.clearRect(0, 0, window.innerWidth, window.innerHeight)
      ctx.font = '12px "Azeret Mono", monospace'
      for (let i = 0; i < particles.length; i++) {
        const p = particles[i]
        const dx = p.x - mouse.x
        const dy = p.y - mouse.y
        const d2 = dx * dx + dy * dy
        if (d2 < 22000) {
          const f = 18 / Math.max(40, Math.sqrt(d2))
          p.vx += dx * f * 0.002
          p.vy += dy * f * 0.002
        }
        p.vx *= 0.99
        p.vy *= 0.99
        p.x += p.vx
        p.y += p.vy
        if (p.x < -20) p.x = window.innerWidth + 20
        if (p.x > window.innerWidth + 20) p.x = -20
        if (p.y < -20) p.y = window.innerHeight + 20
        if (p.y > window.innerHeight + 20) p.y = -20
        ctx.fillStyle = `rgba(61,255,232,${p.a})`
        ctx.fillText(p.ch, p.x, p.y)
      }
      ctx.strokeStyle = 'rgba(61,255,232,0.06)'
      ctx.lineWidth = 1
      for (let i = 0; i < particles.length; i++) {
        for (let j = i + 1; j < particles.length; j++) {
          const a = particles[i]
          const b = particles[j]
          const dx = a.x - b.x
          const dy = a.y - b.y
          const d2 = dx * dx + dy * dy
          if (d2 < 9000) {
            ctx.globalAlpha = 1 - d2 / 9000
            ctx.beginPath()
            ctx.moveTo(a.x, a.y)
            ctx.lineTo(b.x, b.y)
            ctx.stroke()
          }
        }
      }
      ctx.globalAlpha = 1
      raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)
    return () => {
      cancelAnimationFrame(raf)
      window.removeEventListener('resize', resize)
      window.removeEventListener('pointermove', onMove)
    }
  }, [])

  return <canvas ref={ref} className="hex-field" aria-hidden />
}
