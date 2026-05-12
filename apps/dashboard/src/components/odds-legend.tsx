import { VenueLogo } from './venue-logo';

export function OddsLegend({ className }: { className?: string }) {
  return (
    <div className={`flex items-center gap-1 font-mono text-[10px] text-muted-foreground ${className ?? ''}`}>
      <span>报价</span>
      <VenueLogo size={13} />
      <span>POLY</span>
    </div>
  );
}
