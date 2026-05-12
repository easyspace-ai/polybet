import {
  get1X2,
  getSpreadMLTotal,
  isAmericanSport,
  getBestOdds,
  formatDate,
  type MatchGroup,
  type OutcomeRow,
  type BetSlipSelection,
} from '../lib/marketUtils';
import { cn } from '../lib/utils';
import { formatOdds } from '../lib/oddsFormat';
import { useOddsFormat } from '../hooks/useOddsFormat';
import { VenueLogo } from '../components/VenueLogo';
import { OddsLegend } from '../components/OddsLegend';

interface BestCardProps {
  outcome: OutcomeRow;
  matchName: string;
  columnLabel?: string;
  selection: BetSlipSelection | null;
  onOddsClick: (outcomeId: string, label: string, matchName: string) => void;
}

function BestCard({ outcome, matchName, columnLabel, selection, onOddsClick }: BestCardProps) {
  const [format] = useOddsFormat();
  const best = getBestOdds(outcome);
  const decimal = best && best.impliedOdds > 0 ? formatOdds(best.impliedOdds, format) : null;
  const accentBorder = decimal ? 'border-l-tm-poly' : 'border-l-tm-bd';
  const isSelected = selection?.outcomeId === outcome.outcomeId;

  return (
    <button
      onClick={() => decimal && onOddsClick(outcome.outcomeId, outcome.label, matchName)}
      disabled={!decimal}
      className={cn(
        'w-full flex items-center justify-between gap-2 px-3 py-2.5 bg-tm-bg-el border border-tm-bd border-l-2',
        accentBorder,
        'rounded-[var(--tm-rad)] text-left transition-all',
        decimal && 'hover:bg-tm-bg-el/80 hover:border-tm-bd-st cursor-pointer',
        isSelected && 'ring-1 ring-tm-poly/50 bg-tm-poly/10',
      )}
    >
      <div className="flex items-center gap-2 min-w-0">
        {columnLabel && (
          <span className="font-mono text-[9px] text-tm-tx-mut shrink-0">{columnLabel}</span>
        )}
        <span className="text-[12px] font-medium text-tm-tx truncate">{outcome.label}</span>
      </div>
      <div className="flex items-center gap-2 shrink-0">
        {decimal ? (
          <>
            <VenueLogo size={18} />
            <span className="font-mono text-[15px] font-semibold text-tm-tx tabular-nums w-[56px] text-right">{decimal}</span>
          </>
        ) : (
          <span className="font-mono text-[13px] text-tm-tx-mut tabular-nums w-[56px] text-right">—</span>
        )}
      </div>
    </button>
  );
}

function DetailSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="mb-6">
      <div className="font-mono text-[10px] font-semibold tracking-[0.2em] text-tm-tx-dim pb-1.5 mb-2.5 border-b border-tm-bd">
        {title}
      </div>
      {children}
    </div>
  );
}

interface MatchDetailProps {
  group: MatchGroup;
  selection: BetSlipSelection | null;
  onBack: () => void;
  onOddsClick: (outcomeId: string, label: string, matchName: string) => void;
}

export function MatchDetail({ group, selection, onBack, onOddsClick }: MatchDetailProps) {
  const { home, draw, away } = get1X2(group);
  const { mlHome, mlAway } = getSpreadMLTotal(group);
  const american = isAmericanSport(group);

  return (
    <div className="flex flex-col h-full bg-tm-bg">
      <div className="sticky top-0 z-20 shrink-0 flex items-center gap-3 px-4 min-h-10 py-1.5 bg-tm-bg-sunk border-b border-tm-bd">
        <button
          onClick={onBack}
          className="font-mono text-[11px] text-tm-tx-dim hover:text-tm-tx transition-colors shrink-0"
        >
          ← 返回
        </button>
        <div className="flex-1 min-w-0">
          <p className="text-[13px] font-semibold text-tm-tx truncate">{group.name}</p>
        </div>
        <p className="font-mono text-[10px] text-tm-tx-mut shrink-0 tracking-wider uppercase">
          {group.sport} · {group.league} · {formatDate(group.startTime)}
        </p>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto p-4">
        <div className="mb-4 flex justify-end">
          <OddsLegend />
        </div>

        {american && (mlHome || mlAway) && (
          <DetailSection title="胜负盘（独赢）">
            <div className="grid grid-cols-2 gap-2">
              {[mlHome, mlAway].map((outcome, i) =>
                outcome ? (
                  <BestCard
                    key={outcome.outcomeId}
                    outcome={outcome}
                    matchName={group.name}
                    selection={selection}
                    onOddsClick={onOddsClick}
                  />
                ) : (
                  <div key={i} />
                ),
              )}
            </div>
          </DetailSection>
        )}

        {!american && (home || draw || away) && (
          <DetailSection title="独赢 · 1X2">
            <div className="grid grid-cols-3 gap-2.5">
              {[
                { yes: home, col: '1' },
                { yes: draw, col: 'X' },
                { yes: away, col: '2' },
              ].map(({ yes, col }, i) => (
                <div key={i} className="flex flex-col gap-1.5">
                  {yes ? (
                    <BestCard
                      outcome={yes}
                      matchName={group.name}
                      columnLabel={col}
                      selection={selection}
                      onOddsClick={onOddsClick}
                    />
                  ) : (
                    <div className="h-[42px] border border-tm-bd rounded-[var(--tm-rad)] bg-tm-bg-el/40" />
                  )}
                </div>
              ))}
            </div>
          </DetailSection>
        )}
      </div>
    </div>
  );
}
