import { Circle, CircleAlert, Hand } from "lucide-react";
import { useRef } from "react";
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

// A check whose stroke draws itself when it comes in after another state.
function Check({ size, draw }: { size: number; draw: boolean }) {
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden className="text-success">
      <path d="M3.25 8.5 6.5 11.75 12.75 4.75" pathLength={1} strokeDasharray={1} className={cn(draw && "animate-[draw_.2s_cubic-bezier(.22,1,.36,1)_40ms_backwards]")} />
    </svg>
  );
}

// StatusIcon shows a step state with a mark and names it for screen readers. A state that changes
// pops in: the new mark grows from 60% and fades in, a check draws itself. The first state just shows.
export function StatusIcon({ status, size = 14 }: { status: StepStatus; size?: number }) {
  const t = useT();
  const moved = useRef(false);
  const initial = useRef(status).current;
  if (status !== initial) moved.current = true; // from the first change on, every state pops in
  const changed = moved.current;
  const label = t.blocks.status[status === "error" ? "error" : status];
  return (
    <span role="img" aria-label={label} title={label} className="inline-grid shrink-0 place-items-center">
      <span key={status} className={cn("inline-grid place-items-center", changed && "animate-[status-in_.2s_cubic-bezier(.16,1,.3,1)]")}>
        {status === "running" ? (
          <Spinner size={size} />
        ) : status === "done" ? (
          <Check size={size} draw={changed} />
        ) : status === "error" ? (
          <CircleAlert size={size} strokeWidth={2} className="text-destructive" aria-hidden />
        ) : status === "waiting" ? (
          <Hand size={size} strokeWidth={2} className="text-warning" aria-hidden />
        ) : (
          <Circle size={size - 2} strokeWidth={1.75} className={cn("text-border-strong", status === "skipped" && "opacity-50")} aria-hidden />
        )}
      </span>
    </span>
  );
}
