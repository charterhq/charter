import { Check, Copy } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { useT } from "@/i18n";
import { cn } from "@/lib/utils";

// CopyButton puts text on the clipboard and shows a check for a moment.
// icon replaces the copy mark, for a button that copies a link.
export function CopyButton({ text, label, small, icon, className }: { text: string; label?: string; small?: boolean; icon?: ReactNode; className?: string }) {
  const t = useT();
  const [done, setDone] = useState(false);
  useEffect(() => {
    if (!done) return;
    const id = window.setTimeout(() => setDone(false), 1500);
    return () => window.clearTimeout(id);
  }, [done]);
  const name = done ? t.blocks.copied : (label ?? t.blocks.copy);
  return (
    <button
      type="button"
      aria-label={name}
      title={name}
      onClick={() => void navigator.clipboard?.writeText(text).then(() => setDone(true), () => undefined)}
      className={cn(
        "grid shrink-0 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-surface hover:text-foreground",
        small ? "size-6" : "size-8",
        className,
      )}
    >
      {done ? <Check size={small ? 13 : 14} className="text-success" aria-hidden /> : (icon ?? <Copy size={small ? 13 : 14} aria-hidden />)}
    </button>
  );
}
