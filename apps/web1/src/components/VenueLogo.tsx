import { cn } from '../lib/utils';

interface VenueLogoProps {
  size?: number;
  className?: string;
}

/** Polymarket mark (token / venue indicator). */
export function VenueLogo({ size = 14, className }: VenueLogoProps) {
  const scale = 1.6;
  return (
    <span
      className={cn('inline-flex items-center justify-center shrink-0', className)}
      style={{ width: size, height: size }}
    >
      <img
        src="/icon-white.png"
        alt="Polymarket"
        width={size}
        height={size}
        style={{ width: size, height: size, transform: `scale(${scale})` }}
        className="inline-block object-contain"
      />
    </span>
  );
}
