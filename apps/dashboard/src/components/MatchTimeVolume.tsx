import { formatKickoffET } from '@/lib/kickoffTime';
import { formatVolumeAmount } from '@/lib/formatVolume';

interface Props {
  startTime: string;
  eventVolume?: number | null;
  compact?: boolean;
}

/** Polymarket-style kickoff (ET) + volume with explicit labels. */
export function MatchTimeVolume({ startTime, eventVolume, compact = false }: Props) {
  const vol = formatVolumeAmount(eventVolume);
  const labelClass = compact ? 'text-[9px]' : 'text-[10px]';
  const timeClass = compact ? 'text-[11px]' : 'text-[12px]';
  const volClass = compact ? 'text-[10px]' : 'text-[11px]';

  return (
    <div className={`flex flex-col ${compact ? 'gap-1' : 'gap-1.5'} min-w-[100px]`}>
      <div className="flex items-baseline gap-1.5">
        <span className={`${labelClass} text-muted-foreground shrink-0`}>开赛</span>
        <span className={`${timeClass} font-medium text-foreground tabular-nums`}>
          {formatKickoffET(startTime)}
        </span>
      </div>
      <div className="flex items-baseline gap-1.5">
        <span className={`${labelClass} text-muted-foreground shrink-0`}>交易量</span>
        <span className={`${volClass} font-mono text-muted-foreground tabular-nums`}>
          {vol ?? '—'}
        </span>
      </div>
    </div>
  );
}
