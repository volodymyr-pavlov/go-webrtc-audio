/**
 * The name of the station, set as text so that it takes the font and the colour of its place. The letters are spaced
 * out, as on a radio dial, and every size works because nothing is drawn.
 */
export function Wordmark({ className }: { className?: string }) {
  return (
    <span className={`inline-flex items-center text-3xl font-light tracking-[0.35em] uppercase ${className ?? ''}`}>
      Go radio
    </span>
  );
}
