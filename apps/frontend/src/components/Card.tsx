import type { ReactNode } from 'react';

/** The frosted panel that holds the player. */
export function Card({ children }: { children: ReactNode }) {
  return (
    <div className="card relative flex w-full flex-col items-center overflow-hidden p-6 text-center">{children}</div>
  );
}
