import { cn } from '../lib/utils';
import { VenueLogo } from './VenueLogo';

interface OddsLegendProps {
  className?: string;
}

export function OddsLegend({ className }: OddsLegendProps) {
  return (
    <div
      className={cn(
        'inline-flex items-center gap-2 font-mono text-[10px] tracking-wider text-tm-tx-mut',
        className,
      )}
      title="盘口来自 Polymarket CLOB"
    >
      <span className="text-tm-tx-dim">报价</span>
      <span className="inline-flex items-center gap-1">
        <span className="inline-block w-2 h-2 rounded-sm bg-tm-poly" />
        <VenueLogo size={12} />
        <span className="text-tm-poly">POLY</span>
      </span>
    </div>
  );
}
