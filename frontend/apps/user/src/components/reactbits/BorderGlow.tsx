import type { CSSProperties, PointerEvent, ReactNode } from 'react';
import { useMemo, useState } from 'react';
import clsx from 'clsx';

type BorderGlowProps = {
  children: ReactNode;
  edgeSensitivity?: number;
  glowColor?: string;
  backgroundColor?: string;
  borderRadius?: number;
  glowRadius?: number;
  glowIntensity?: number;
  coneSpread?: number;
  animated?: boolean;
  colors?: string[];
  className?: string;
};

export default function BorderGlow({
  children,
  edgeSensitivity = 30,
  glowColor = '40 80 80',
  backgroundColor = '#120F17',
  borderRadius = 28,
  glowRadius = 40,
  glowIntensity = 1,
  coneSpread = 25,
  animated = false,
  colors = ['#c084fc', '#f472b6', '#38bdf8'],
  className,
}: BorderGlowProps) {
  const [glow, setGlow] = useState({ x: 0, y: 0, opacity: animated ? 0.28 : 0 });
  const palette = colors.length ? colors : ['#c084fc', '#f472b6', '#38bdf8'];

  const ringGradient = useMemo(() => {
    const stops = palette.map((color, index) => `${color} ${Math.round((index + 1) * coneSpread)}deg`).join(', ');
    return `conic-gradient(from 180deg at var(--border-glow-x) var(--border-glow-y), transparent 0deg, ${stops}, transparent ${Math.min(360, coneSpread * (palette.length + 1))}deg, transparent 360deg)`;
  }, [coneSpread, palette]);

  const handlePointerMove = (event: PointerEvent<HTMLDivElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect();
    const x = event.clientX - bounds.left;
    const y = event.clientY - bounds.top;
    const edgeDistance = Math.min(x, y, bounds.width - x, bounds.height - y);
    const edgeFactor = Math.max(0, 1 - edgeDistance / Math.max(1, edgeSensitivity));
    setGlow({
      x,
      y,
      opacity: Math.min(1, edgeFactor * glowIntensity + (animated ? 0.12 : 0)),
    });
  };

  const style = {
    '--border-glow-x': `${glow.x}px`,
    '--border-glow-y': `${glow.y}px`,
    '--border-glow-opacity': String(glow.opacity),
    '--border-glow-radius': `${borderRadius}px`,
    '--border-glow-bg': backgroundColor,
    '--border-glow-rgb': glowColor,
    '--border-glow-size': `${glowRadius}px`,
    '--border-glow-ring': ringGradient,
  } as CSSProperties;

  return (
    <div
      className={clsx('border-glow', animated && 'border-glow--animated', className)}
      style={style}
      onPointerMove={handlePointerMove}
      onPointerLeave={() => setGlow((current) => ({ ...current, opacity: animated ? 0.28 : 0 }))}
    >
      <div className="border-glow__content">{children}</div>
    </div>
  );
}
