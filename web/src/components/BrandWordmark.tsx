// The brand mark: the real logo (public/survivor-league-logo.svg) — a
// traced black football emblem with blood-red "Survivor League" lettering
// baked into the artwork itself. size is the rendered width in px; height
// follows the SVG's native 16:9 (1600x900) aspect ratio automatically.
export function BrandWordmark({ size = 96 }: { size?: number }) {
  return (
    <img
      src="/survivor-league-logo.svg"
      alt="Survivor League"
      className="shrink-0"
      style={{ width: size, height: size * (900 / 1600) }}
    />
  )
}
