import type { ReactNode } from 'react';
import { useRef, useState } from 'react';

type MagnetProps = {
  children: ReactNode;
  padding?: number;
  disabled?: boolean;
  magnetStrength?: number;
  className?: string;
};

export default function Magnet({ children, padding = 36, disabled = false, magnetStrength = 18, className }: MagnetProps) {
  const ref = useRef<HTMLDivElement | null>(null);
  const [transform, setTransform] = useState('translate3d(0, 0, 0)');

  const handleMove = (event: React.MouseEvent<HTMLDivElement>) => {
    const node = ref.current;
    if (!node || disabled) return;

    const rect = node.getBoundingClientRect();
    const centerX = rect.left + rect.width / 2;
    const centerY = rect.top + rect.height / 2;
    const distanceX = event.clientX - centerX;
    const distanceY = event.clientY - centerY;
    const distance = Math.hypot(distanceX, distanceY);

    if (distance > rect.width / 2 + padding) {
      setTransform('translate3d(0, 0, 0)');
      return;
    }

    const pull = Math.max(0, 1 - distance / (rect.width / 2 + padding));
    const x = (distanceX / Math.max(distance, 1)) * pull * magnetStrength;
    const y = (distanceY / Math.max(distance, 1)) * pull * magnetStrength;
    setTransform(`translate3d(${x.toFixed(2)}px, ${y.toFixed(2)}px, 0)`);
  };

  return (
    <div
      ref={ref}
      className={className}
      onMouseMove={handleMove}
      onMouseLeave={() => setTransform('translate3d(0, 0, 0)')}
      style={{
        display: 'inline-flex',
        transform,
        transition: 'transform 180ms cubic-bezier(.22, 1, .36, 1)',
        willChange: 'transform',
      }}
    >
      {children}
    </div>
  );
}
