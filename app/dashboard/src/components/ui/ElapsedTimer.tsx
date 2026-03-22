import { useEffect, useRef, useState } from 'react';

export function ElapsedTimer({ running }: { running: boolean }) {
  const [elapsed, setElapsed] = useState(0);
  const startRef = useRef<number>(Date.now());

  useEffect(() => {
    if (!running) { setElapsed(0); return; }
    startRef.current = Date.now();
    const id = setInterval(() => setElapsed(Math.floor((Date.now() - startRef.current) / 1000)), 500);
    return () => clearInterval(id);
  }, [running]);

  if (!running) return null;
  const m = Math.floor(elapsed / 60);
  const s = elapsed % 60;
  return (
    <span style={{ color: 'var(--muted)', fontFamily: 'Fira Code, monospace', fontSize: '0.72rem' }}>
      {m > 0 ? `${m}m ` : ''}{s}s
    </span>
  );
}
