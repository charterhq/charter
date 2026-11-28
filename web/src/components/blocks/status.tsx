import { Check, Circle, Hand, X } from "lucide-react";
import { useT } from "@/i18n";
import { cn } from "@/lib/utils";

// Blocks share one set of step states: an agent's run, a plan, a tool call, a search.
export type StepStatus = "todo" | "running" | "waiting" | "done" | "error" | "skipped";

// A spinner made of a ring and an arc that turns with a CSS transform; Reduce motion stops it.
export function Spinner({ size = 14, className }: { size?: number; className?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" aria-hidden className={cn("shrink-0 animate-spin text-muted-foreground [animation-duration:900ms]", className)}>
      <circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" strokeOpacity=".22" strokeWidth="1.75" />
      <path d="M8 2a6 6 0 0 1 6 6" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" />
    </svg>
  );
}

// StatusIcon shows a step state with a mark and names it for screen readers.
export function StatusIcon({ status, size = 14 }: { status: StepStatus; size?: number }) {
  const t = useT();
  const label = t.blocks.status[status === "error" ? "error" : status];
  return (
    <span role="img" aria-label={label} title={label} className="inline-grid shrink-0 place-items-center">
      {status === "running" ? (
        <Spinner size={size} />
      ) : status === "done" ? (
        <Check size={size} strokeWidth={2.25} className="text-success" aria-hidden />
      ) : status === "error" ? (
        <X size={size} strokeWidth={2.25} className="text-destructive" aria-hidden />
      ) : status === "waiting" ? (
        <Hand size={size} strokeWidth={2} className="text-warning" aria-hidden />
      ) : (
        <Circle size={size - 2} strokeWidth={1.75} className={cn("text-border-strong", status === "skipped" && "opacity-50")} aria-hidden />
      )}
    </span>
  );
}
