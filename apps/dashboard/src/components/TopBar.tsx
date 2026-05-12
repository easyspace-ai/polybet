import { Moon, Sun } from "lucide-react";
import { useTheme } from "@/lib/theme";

interface Props {
  title: string;
  subtitle?: React.ReactNode;
  actions?: React.ReactNode;
}

export function TopBar({ title, subtitle, actions }: Props) {
  const { theme, toggle } = useTheme();
  return (
    <header className="h-14 px-6 flex items-center justify-between border-b border-border bg-background/80 backdrop-blur-md sticky top-0 z-30">
      <div className="flex items-center gap-4">
        <h1 className="text-[14px] font-semibold tracking-tight">{title}</h1>
        {subtitle && (
          <>
            <div className="h-3 w-px bg-border" />
            <div className="text-[11px] text-muted-foreground font-mono flex items-center gap-3">{subtitle}</div>
          </>
        )}
      </div>
      <div className="flex items-center gap-3">
        {actions}
        <button
          onClick={toggle}
          aria-label="切换主题"
          className="size-8 rounded-md border border-border bg-surface hover:bg-accent transition-all flex items-center justify-center"
        >
          {theme === "dark" ? <Sun className="size-3.5" /> : <Moon className="size-3.5" />}
        </button>
      </div>
    </header>
  );
}
