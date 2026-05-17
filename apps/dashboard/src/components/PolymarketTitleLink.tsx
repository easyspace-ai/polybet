import { ExternalLink } from "lucide-react";
import { resolvePolymarketEventUrl } from "@/lib/polymarketLinks";
import { cn } from "@/lib/utils";

interface Props {
  title: string;
  officialUrl?: string | null;
  polySlug?: string | null;
  className?: string;
  titleClassName?: string;
}

/** Market-style title link to polymarket.com/event/… when a slug or officialUrl is known. */
export function PolymarketTitleLink({
  title,
  officialUrl,
  polySlug,
  className,
  titleClassName,
}: Props) {
  const href = resolvePolymarketEventUrl(officialUrl, polySlug);
  if (!href) {
    return <span className={cn("truncate", titleClassName)}>{title}</span>;
  }
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className={cn("inline-flex items-center gap-1 min-w-0 group", className)}
      onClick={(e) => e.stopPropagation()}
    >
      <span className={cn("truncate group-hover:text-brand transition-colors", titleClassName)}>{title}</span>
      <ExternalLink className="size-3 shrink-0 text-muted-foreground opacity-70 group-hover:opacity-100 group-hover:text-brand transition-opacity" />
    </a>
  );
}
