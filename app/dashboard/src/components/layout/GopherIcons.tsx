import type { ReactNode } from 'react';

interface GopherIconProps {
  size?: number;
  className?: string;
}

interface GopherFrameProps extends GopherIconProps {
  children: ReactNode;
}

function GopherFrame({ size = 16, className, children }: GopherFrameProps) {
  return (
    <svg
      viewBox="0 0 24 24"
      width={size}
      height={size}
      className={className}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <circle cx="8" cy="6" r="1.7" fill="currentColor" fillOpacity="0.14" />
      <circle cx="16" cy="6" r="1.7" fill="currentColor" fillOpacity="0.14" />
      <circle cx="12" cy="10.5" r="5.2" fill="currentColor" fillOpacity="0.1" />
      <circle cx="10.2" cy="10.1" r="0.55" fill="currentColor" stroke="none" />
      <circle cx="13.8" cy="10.1" r="0.55" fill="currentColor" stroke="none" />
      <ellipse cx="12" cy="12.2" rx="1.2" ry="0.95" fill="currentColor" fillOpacity="0.2" stroke="none" />
      <path d="M10.9 13.4c.32.36.74.56 1.1.56.37 0 .79-.2 1.1-.56" />
      <rect x="6.1" y="14.4" width="11.8" height="6.8" rx="2.8" fill="currentColor" fillOpacity="0.08" />
      {children}
    </svg>
  );
}

export function GopherServerIcon({ size = 16, className }: GopherIconProps) {
  return (
    <GopherFrame size={size} className={className}>
      <rect x="8.8" y="16.1" width="6.4" height="3.9" rx="0.9" />
      <line x1="9.7" y1="17.3" x2="14.3" y2="17.3" />
      <line x1="9.7" y1="18.6" x2="14.3" y2="18.6" />
    </GopherFrame>
  );
}

export function GopherAndroidIcon({ size = 16, className }: GopherIconProps) {
  return (
    <GopherFrame size={size} className={className}>
      <path d="M9.4 17.1a2.6 2.6 0 0 1 5.2 0v2.5H9.4z" />
      <line x1="10.6" y1="16.2" x2="10.1" y2="15.3" />
      <line x1="13.4" y1="16.2" x2="13.9" y2="15.3" />
      <circle cx="10.9" cy="17.8" r="0.3" fill="currentColor" stroke="none" />
      <circle cx="13.1" cy="17.8" r="0.3" fill="currentColor" stroke="none" />
    </GopherFrame>
  );
}

export function GopherDashboardIcon({ size = 16, className }: GopherIconProps) {
  return (
    <GopherFrame size={size} className={className}>
      <rect x="8.9" y="17.9" width="1.6" height="1.9" rx="0.4" />
      <rect x="11.2" y="17.1" width="1.6" height="2.7" rx="0.4" />
      <rect x="13.5" y="16.3" width="1.6" height="3.5" rx="0.4" />
      <path d="M8.9 16.8l2-1.1 1.8.5 2.5-1.7" />
    </GopherFrame>
  );
}
