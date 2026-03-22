import { useEffect, useRef, useState } from 'react';
import type { UiTarget } from '../../types/inspector';

interface Props {
  targets: UiTarget[];
  videoSize: { width: number; height: number };
  canvasElement: HTMLCanvasElement | null;
  hoveredTarget: UiTarget | null;
  selectedTarget: UiTarget | null;
  onHover: (target: UiTarget | null) => void;
  onSelect: (target: UiTarget) => void;
}

function deviceToOverlay(
  bounds: [number, number, number, number],
  videoSize: { width: number; height: number },
  canvasSize: { width: number; height: number },
): { left: number; top: number; width: number; height: number } {
  const scaleX = canvasSize.width / videoSize.width;
  const scaleY = canvasSize.height / videoSize.height;
  return {
    left: bounds[0] * scaleX,
    top: bounds[1] * scaleY,
    width: (bounds[2] - bounds[0]) * scaleX,
    height: (bounds[3] - bounds[1]) * scaleY,
  };
}

function targetLabel(t: UiTarget): string {
  if (t.semanticKey) return t.semanticKey;
  if (t.text && t.text.length <= 30) return t.text;
  if (t.text) return t.text.slice(0, 27) + '...';
  if (t.resourceId) {
    const short = t.resourceId.split('/').pop() ?? t.resourceId;
    return short;
  }
  return t.uiRole;
}

export function InspectorOverlay({
  targets,
  videoSize,
  canvasElement,
  hoveredTarget,
  selectedTarget,
  onHover,
  onSelect,
}: Props) {
  const overlayRef = useRef<HTMLDivElement>(null);
  const [canvasSize, setCanvasSize] = useState({ width: 0, height: 0 });

  // Track canvas displayed size via ResizeObserver
  useEffect(() => {
    if (!canvasElement) return;
    const update = () => {
      const rect = canvasElement.getBoundingClientRect();
      setCanvasSize({ width: rect.width, height: rect.height });
    };
    update();
    const ro = new ResizeObserver(update);
    ro.observe(canvasElement);
    return () => ro.disconnect();
  }, [canvasElement]);

  if (!canvasElement || !videoSize.width || !videoSize.height || !canvasSize.width) {
    return null;
  }

  return (
    <div
      ref={overlayRef}
      onPointerDown={(e) => e.stopPropagation()}
      onPointerMove={(e) => e.stopPropagation()}
      onPointerUp={(e) => e.stopPropagation()}
      style={{
        position: 'absolute',
        top: canvasElement.offsetTop,
        left: canvasElement.offsetLeft,
        width: canvasSize.width,
        height: canvasSize.height,
        pointerEvents: 'auto',
        cursor: 'crosshair',
        zIndex: 10,
      }}
    >
      {targets.map((target) => {
        const pos = deviceToOverlay(target.bounds, videoSize, canvasSize);
        if (pos.width < 1 || pos.height < 1) return null;

        const isHovered = hoveredTarget?.targetId === target.targetId;
        const isSelected = selectedTarget?.targetId === target.targetId;

        let borderColor = 'rgba(59,130,246,0.3)';
        let borderWidth = 1;
        let background = 'transparent';

        if (isSelected) {
          borderColor = 'rgba(34,197,94,0.7)';
          borderWidth = 2;
          background = 'rgba(34,197,94,0.1)';
        } else if (isHovered) {
          borderColor = 'rgba(59,130,246,0.7)';
          borderWidth = 2;
          background = 'rgba(59,130,246,0.08)';
        }

        return (
          <div
            key={target.targetId}
            onPointerEnter={() => onHover(target)}
            onPointerLeave={() => onHover(null)}
            onPointerDown={(e) => {
              e.stopPropagation();
              onSelect(target);
            }}
            style={{
              position: 'absolute',
              left: pos.left,
              top: pos.top,
              width: pos.width,
              height: pos.height,
              border: `${borderWidth}px solid ${borderColor}`,
              background,
              boxSizing: 'border-box',
              transition: 'border-color 100ms, background 100ms',
            }}
          >
            {isHovered && (
              <span
                style={{
                  position: 'absolute',
                  bottom: '100%',
                  left: 0,
                  marginBottom: 2,
                  padding: '1px 5px',
                  fontSize: '0.6rem',
                  lineHeight: 1.4,
                  fontFamily: 'Fira Code, ui-monospace, monospace',
                  background: 'rgba(0,0,0,0.85)',
                  color: '#93c5fd',
                  borderRadius: 3,
                  whiteSpace: 'nowrap',
                  pointerEvents: 'none',
                  zIndex: 20,
                  maxWidth: 220,
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                }}
              >
                {targetLabel(target)}
              </span>
            )}
          </div>
        );
      })}
    </div>
  );
}
