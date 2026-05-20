import { Copy } from 'lucide-react';
import { useAccounts } from '@/hooks/useAccounts';
import { abbreviateAddress } from '@/lib/formatAddress';
import { toast } from 'sonner';

export function WalletHeaderStrip() {
  const { accounts, loading } = useAccounts();
  const active = accounts.find((a) => a.isActive) ?? accounts[0];
  const name = active?.name ?? '—';
  const addr = active?.funderAddress?.trim() ?? '';

  const copyAddr = async () => {
    if (!addr) return;
    try {
      await navigator.clipboard.writeText(addr);
      toast.success('已复制钱包地址');
    } catch {
      toast.error('复制失败');
    }
  };

  if (loading && !active) {
    return (
      <span className="text-[11px] text-muted-foreground font-mono">钱包加载中…</span>
    );
  }

  if (!active) {
    return (
      <span className="text-[11px] text-muted-foreground">未选择钱包</span>
    );
  }

  return (
    <div className="flex items-center gap-2 min-w-0 max-w-[320px]">
      <span className="text-[12px] font-medium text-foreground truncate">{name}</span>
      {addr && (
        <>
          <span className="text-border">·</span>
          <button
            type="button"
            onClick={copyAddr}
            title={addr}
            className="flex items-center gap-1 text-[11px] font-mono text-muted-foreground hover:text-foreground transition shrink-0"
          >
            {abbreviateAddress(addr)}
            <Copy className="size-3 opacity-60" />
          </button>
        </>
      )}
    </div>
  );
}
