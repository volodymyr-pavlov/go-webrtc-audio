// A soft diagonal gradient from green, through teal and indigo, to violet. The layer is far bigger than the screen, so
// the screen shows only the middle of it, and sliding the layer along the diagonal moves green or violet into view.
const gradient = 'bg-backdrop';

/**
 * The background of the whole page: a smooth gradient from green to violet whose colour slowly moves across the screen,
 * as if the page were lit from a light that drifts. Only a transform changes, so the browser does it on the graphics
 * card, and the movement stops for people who have asked their system to reduce motion.
 */
export function Backdrop() {
  return (
    <div aria-hidden className="pointer-events-none fixed inset-0 -z-10 overflow-hidden bg-green-200">
      <div className={`absolute -inset-[55%] will-change-transform motion-safe:animate-gradient-shift ${gradient}`} />
    </div>
  );
}
