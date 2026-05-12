import { TradePanel } from './trade-panel';

export interface BetSlipSelection {
  outcomeId: string;
  label: string;
  matchName: string;
}

interface BetSlipProps {
  selection: BetSlipSelection | null;
  onClose: () => void;
  onTradeExecuted: () => void;
}

export function BetSlip({ selection, onClose, onTradeExecuted }: BetSlipProps) {
  return (
    <div className="flex flex-col h-full bg-sidebar">
      <div className="shrink-0 flex items-start justify-between gap-2 px-4 py-3 bg-sidebar border-b border-border">
        <div className="min-w-0 flex-1">
          <p className="font-mono text-[10px] font-semibold tracking-[0.2em] text-muted-foreground">
            投注单
          </p>
          {selection ? (
            <>
              <p className="text-[13px] font-semibold text-primary mt-1 truncate">
                ▸ {selection.label}
              </p>
              <p className="text-[11px] text-muted-foreground mt-0.5 truncate">
                {selection.matchName}
              </p>
            </>
          ) : (
            <p className="text-[11px] text-muted-foreground mt-1 truncate">
              暂无选项
            </p>
          )}
        </div>
        {selection && (
          <button
            onClick={onClose}
            className="shrink-0 w-11 h-11 md:w-6 md:h-6 flex items-center justify-center rounded-sm border border-border text-muted-foreground hover:text-foreground hover:bg-surface transition-colors text-lg md:text-base leading-none"
            aria-label="清空投注单"
          >
            ×
          </button>
        )}
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto">
        {selection ? (
          <TradePanel
            outcomeId={selection.outcomeId}
            outcomeLabel={selection.label}
            onTradeExecuted={onTradeExecuted}
            hideHeader
          />
        ) : (
          <div className="h-full flex flex-col items-center justify-center px-6 py-10 text-center">
            <div className="w-12 h-12 rounded-full border border-dashed border-border flex items-center justify-center text-muted-foreground text-2xl leading-none mb-4">
              +
            </div>
            <p className="text-[12px] text-muted-foreground leading-relaxed">
              点击盘口将选项加入投注单
            </p>
          </div>
        )}
      </div>
    </div>
  );
}
