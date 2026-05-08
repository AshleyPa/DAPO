import { useRef, useState } from 'react';
import clsx from 'clsx';

export type CircularGalleryItem = {
  id: string;
  title: string;
  subtitle?: string;
  image: string;
  prompt?: string;
};

type CircularGalleryProps = {
  items: CircularGalleryItem[];
  onSelect: (item: CircularGalleryItem) => void;
  className?: string;
};

export default function CircularGallery({ items, onSelect, className }: CircularGalleryProps) {
  const scrollerRef = useRef<HTMLDivElement | null>(null);
  const dragRef = useRef<{ x: number; scrollLeft: number; dragging: boolean } | null>(null);
  const [activeIndex, setActiveIndex] = useState(0);

  const updateActive = () => {
    const scroller = scrollerRef.current;
    if (!scroller) return;
    const center = scroller.scrollLeft + scroller.clientWidth / 2;
    let nextIndex = 0;
    let nextDistance = Number.POSITIVE_INFINITY;
    Array.from(scroller.children).forEach((child, index) => {
      const element = child as HTMLElement;
      const itemCenter = element.offsetLeft + element.offsetWidth / 2;
      const distance = Math.abs(itemCenter - center);
      if (distance < nextDistance) {
        nextIndex = index;
        nextDistance = distance;
      }
    });
    setActiveIndex(nextIndex);
  };

  const handlePointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    const scroller = scrollerRef.current;
    const drag = dragRef.current;
    if (!scroller || !drag?.dragging) return;
    scroller.scrollLeft = drag.scrollLeft - (event.clientX - drag.x);
  };

  return (
    <div className={clsx('circular-gallery', className)}>
      <div
        ref={scrollerRef}
        className="circular-gallery__track"
        onScroll={updateActive}
        onWheel={(event) => {
          if (!scrollerRef.current) return;
          if (Math.abs(event.deltaY) <= Math.abs(event.deltaX)) return;
          event.preventDefault();
          scrollerRef.current.scrollLeft += event.deltaY;
        }}
        onPointerDown={(event) => {
          const scroller = scrollerRef.current;
          if (!scroller) return;
          dragRef.current = { x: event.clientX, scrollLeft: scroller.scrollLeft, dragging: true };
          scroller.setPointerCapture(event.pointerId);
        }}
        onPointerMove={handlePointerMove}
        onPointerUp={() => {
          if (dragRef.current) dragRef.current.dragging = false;
        }}
        onPointerCancel={() => {
          if (dragRef.current) dragRef.current.dragging = false;
        }}
      >
        {items.map((item, index) => {
          const distance = Math.min(2, Math.abs(index - activeIndex));
          return (
            <button
              key={item.id}
              type="button"
              className="circular-gallery__card"
              style={{
                transform: `rotateY(${(index - activeIndex) * -9}deg) translateZ(${distance === 0 ? 20 : 0}px) scale(${distance === 0 ? 1 : 0.92})`,
                opacity: distance > 1 ? 0.72 : 1,
              }}
              onClick={() => onSelect(item)}
            >
              <img src={item.image} alt={item.title} loading="lazy" />
              <span className="circular-gallery__shade" />
              <span className="circular-gallery__label">
                <span>{item.title}</span>
                {item.subtitle && <small>{item.subtitle}</small>}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}
