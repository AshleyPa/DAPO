import type { CSSProperties } from 'react';
import clsx from 'clsx';

type ShinyTextProps = {
  text: string;
  speed?: number;
  delay?: number;
  color?: string;
  shineColor?: string;
  spread?: number;
  direction?: 'left' | 'right';
  yoyo?: boolean;
  pauseOnHover?: boolean;
  disabled?: boolean;
  className?: string;
};

export default function ShinyText({
  text,
  speed = 1.2,
  delay = 1,
  color = '#b5b5b5',
  shineColor = '#d2ffb6',
  spread = 160,
  direction = 'left',
  yoyo = false,
  pauseOnHover = false,
  disabled = false,
  className,
}: ShinyTextProps) {
  const style = {
    '--shiny-text-color': color,
    '--shiny-text-shine': shineColor,
    '--shiny-text-spread': `${spread}%`,
    '--shiny-text-speed': `${speed}s`,
    '--shiny-text-delay': `${delay}s`,
    '--shiny-text-from': direction === 'left' ? '120%' : '-120%',
    '--shiny-text-to': direction === 'left' ? '-120%' : '120%',
  } as CSSProperties;

  return (
    <span
      className={clsx(
        'shiny-text',
        yoyo && 'shiny-text--yoyo',
        pauseOnHover && 'shiny-text--pause-on-hover',
        disabled && 'shiny-text--disabled',
        className,
      )}
      style={style}
    >
      {text}
    </span>
  );
}
