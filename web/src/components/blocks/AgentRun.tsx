import { RotateCcw } from "lucide-react";
import type { ReactNode } from "react";
import { Spinner, StatusIcon, type StepStatus } from "./status";
import { useT } from "@/i18n";
import { cn } from "@/lib/utils";

// One run of an agent as a card: what it works on, who works, its state, the steps as they come,
// and the result when it ends. Data comes through props.

export type RunState = "queued" | "working" | "waiting" | "done" | "failed";

export type RunStep = {
  id: string;
  icon?: ReactNode;
  title: ReactNode;
  meta?: ReactNode;
  status: StepStatus;
};

export type AgentRunProps = {
  title: ReactNode;
  // Who runs: a mark and a name.
  agent?: ReactNode;
  state: RunState;
  // How long it took or has been running, already formatted.
  elapsed?: string;
  steps: RunStep[];
  // What came out: changed fields, a link, an error.
  result?: ReactNode;
  actions?: ReactNode;
  onRetry?: () => void;
  // Without the frame and the header, for a run inside a chat message.
  compact?: boolean;
};

export function AgentRun({ title, agent, state, elapsed, steps, result, actions, onRetry, compact }: AgentRunProps) {
  const t = useT();
  const live = state === "queued" || state === "working" || state === "waiting";
  return (
    <section
      aria-busy={live || undefined}
      className={cn("min-w-0 overflow-hidden", !compact && "rounded-xl border bg-card shadow-panel")}
    >
      <header className={cn("flex items-start gap-3", compact ? "pb-2" : "border-b px-4 py-3")}>
        <div className={cn("min-w-0 flex-1", !agent && "flex flex-wrap items-baseline gap-x-3")}>
          <h3 className="truncate text-[14px] font-semibold">{title}</h3>
          <div className={cn("flex items-center gap-3 text-[13px] text-muted-foreground", agent ? "mt-0.5" : "ml-auto")}>
            {agent && <span className="flex min-w-0 items-center gap-1.5 truncate">{agent}</span>}
            <span role="status" className={cn("ml-auto flex shrink-0 items-center gap-1.5", state === "failed" && "text-destructive", state === "waiting" && "text-warning")}>
              {live && state !== "waiting" ? <Spinner size={13} /> : <StatusIcon size={13} status={state === "done" ? "done" : state === "failed" ? "error" : "waiting"} />}
              {t.blocks.run[state]}
            </span>
            {elapsed && <span className="shrink-0 tabular-nums text-faint">{elapsed}</span>}
          </div>
        </div>
        {onRetry && !live && (
          <button
            type="button"
            onClick={onRetry}
            aria-label={t.blocks.retry}
            title={t.blocks.retry}
            className="grid size-8 shrink-0 place-items-center rounded-md text-muted-foreground hover:bg-surface hover:text-foreground"
          >
            <RotateCcw size={15} aria-hidden />
          </button>
        )}
      </header>
      {steps.length > 0 && (
        <ol className={cn("relative", compact ? "space-y-1" : "px-4 py-2")}>
          {steps.map((s) => (
            <li key={s.id} className="flex min-h-8 animate-[fade-in_.2s_ease-out] items-center gap-2.5 text-[13px]">
              {s.icon && (
                <span aria-hidden className="grid size-4 shrink-0 place-items-center text-faint">
                  {s.icon}
                </span>
              )}
              <span className={cn("min-w-0 flex-1 truncate", s.status === "running" ? "text-foreground" : s.status === "todo" ? "text-faint" : "text-muted-foreground")}>
                {s.title}
              </span>
              {s.meta && <span className="shrink-0 text-[12px] tabular-nums text-faint">{s.meta}</span>}
              <StatusIcon status={s.status} />
            </li>
          ))}
        </ol>
      )}
      {result && <div className={cn("text-[13px]", compact ? "pt-2" : "border-t px-4 py-3")}>{result}</div>}
      {actions && <footer className={cn("flex flex-wrap justify-end gap-2", compact ? "pt-2" : "border-t px-4 py-3")}>{actions}</footer>}
    </section>
  );
}
