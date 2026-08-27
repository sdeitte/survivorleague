import { SvgXml } from 'react-native-svg';
import { survivorLeagueLogoSvg } from '../assets/survivorLeagueLogoSvg';

// The brand mark: the real logo (assets/survivorLeagueLogoSvg.ts, sourced
// from web/public/survivor-league-logo.svg) — a traced black football
// emblem with blood-red "Survivor League" lettering baked into the artwork
// itself. Mirrors web/src/components/BrandWordmark.tsx. size is the
// rendered width in px; height follows the SVG's native 16:9 (1600x900)
// aspect ratio automatically.
export function BrandWordmark({ size = 96 }: { size?: number }) {
  return <SvgXml xml={survivorLeagueLogoSvg} width={size} height={size * (900 / 1600)} />;
}
