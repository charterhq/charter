import { ChevronDown, ListChecks, RotateCcw } from "lucide-react";
import { useEffect, useId, useState, type ReactNode } from "react";
import { Collapse } from "./Collapse";
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
  // What the step did: a tool call and its output, a diff to approve. The step opens to show it.
  details?: ReactNode;
  // Open from the start, for a step that waits on a person.
  defaultOpen?: boolean;
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

const enter = "animate-[fade-in_.15s_cubic-bezier(.16,1,.3,1)]";

export function AgentRun({ title, agent, state, elapsed, steps, result, actions, onRetry, compact }: AgentRunProps) {
  const t = useT();
  const live = state === "queued" || state === "working" || state === "waiting";
  // While the run goes the steps are in view; when it is done they fold into one line the reader can
  // open. A failed run keeps them open: the failed step is the news.
  const done = state === "done";
  const [folded, setFolded] = useState(done);
  useEffect(() => setFolded(done), [done]);
  const listId = useId();
  const chevrons = steps.some((s) => s.details);

  const status = (
    <span
      key={state}
      role="status"
      className={cn("ml-auto flex shrink-0 items-center gap-1.5 text-[13px] text-muted-foreground", enter, state === "failed" && "text-destructive", state === "waiting" && "text-warning")}
    >
      {live && state !== "waiting" ? <Spinner size={13} /> : <StatusIcon size={13} status={state === "done" ? "done" : state === "failed" ? "error" : "waiting"} />}
      {t.blocks.run[state]}
    </span>
  );
  const time = elapsed && <span className="shrink-0 text-[13px] tabular-nums text-faint">{elapsed}</span>;

  return (
    <section
      aria-busy={live || undefined}
      className={cn("min-w-0 overflow-hidden", !compact && "rounded-xl border bg-card shadow-panel")}
    >
      <header className={cn("grid grid-cols-[minmax(0,1fr)] gap-0.5", compact ? "pb-2" : "border-b py-3 pl-4 pr-3")}>
        <div className="flex min-h-8 items-center gap-3">
          <h3 className="min-w-0 flex-1 truncate text-[14px] font-semibold">{title}</h3>
          {!agent && status}
          {!agent && time}
          {onRetry && !live && (
            <button
              type="button"
              onClick={onRetry}
              aria-label={t.blocks.retry}
              title={t.blocks.retry}
              className="-my-1 grid size-8 shrink-0 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-surface hover:text-foreground"
            >
              <RotateCcw size={15} aria-hidden />
            </button>
          )}
        </div>
        {agent && (
          <div className={cn("flex min-w-0 items-center gap-3 text-[13px] text-muted-foreground", !compact && "pr-2")}>
            <span className="flex min-w-0 items-center gap-1.5 truncate">{agent}</span>
            {status}
            {time}
          </div>
        )}
      </header>

      {steps.length > 0 && (
        <div className={cn(!compact && "px-2 py-1.5")}>
          <Collapse open={!live}>
            <button
              type="button"
              aria-expanded={!folded}
              aria-controls={listId}
              onClick={() => setFolded(!folded)}
              className={cn("flex min-h-9 w-full items-center gap-2.5 rounded-lg text-left text-[13px] text-muted-foreground transition-colors", !compact && "px-2 hover:bg-surface")}
            >
              <ListChecks size={15} aria-hidden className="shrink-0 text-faint" />
              <span className="min-w-0 flex-1 truncate">
                {t.blocks.steps(steps.length)}
                {elapsed && ` · ${elapsed}`}
              </span>
              <Chevron open={!folded} />
            </button>
          </Collapse>
          <Collapse id={listId} open={live || !folded}>
            <ol>
              {steps.map((s) => (
                <Step key={s.id} step={s} chevron={chevrons} compact={compact} />
              ))}
            </ol>
          </Collapse>
        </div>
      )}
      <Collapse>
        {result && <div className={cn("text-[13px]", enter, compact ? "pt-2" : "border-t px-4 py-3")}>{result}</div>}
        {actions && <footer className={cn("flex flex-wrap justify-end gap-2", enter, compact ? "pt-2" : "border-t px-4 py-3")}>{actions}</footer>}
      </Collapse>
    </section>
  );
}

function Chevron({ open }: { open: boolean }) {
  return (
    <ChevronDown
      size={15}
      aria-hidden
      className={cn("shrink-0 text-faint transition-transform duration-200 ease-[cubic-bezier(.22,1,.36,1)]", open && "rotate-180")}
    />
  );
}

function Step({ step: s, chevron, compact }: { step: RunStep; chevron: boolean; compact?: boolean }) {
  const [open, setOpen] = useState(!!s.defaultOpen);
  const id = useId();
  const head = (
    <>
      {s.icon && (
        <span aria-hidden className="grid size-4 shrink-0 place-items-center text-faint">
          {s.icon}
        </span>
      )}
      <span
        className={cn(
          "min-w-0 flex-1 truncate transition-colors duration-200",
          s.status === "running" ? "text-foreground" : s.status === "todo" ? "text-faint" : "text-muted-foreground",
        )}
      >
        {s.title}
      </span>
      {s.meta && <span className="shrink-0 text-[12px] tabular-nums text-faint">{s.meta}</span>}
      <StatusIcon status={s.status} />
      {chevron && (s.details ? <Chevron open={open} /> : <span aria-hidden className="w-[15px] shrink-0" />)}
    </>
  );
  const row = cn("flex min-h-9 w-full items-center gap-2.5 rounded-lg text-left text-[13px]", !compact && "px-2");
  return (
    <li className={enter}>
      {s.details ? (
        <>
          <button type="button" aria-expanded={open} aria-controls={id} onClick={() => setOpen(!open)} className={cn(row, "transition-colors", !compact && "hover:bg-surface")}>
            {head}
          </button>
          <Collapse id={id} open={open}>
            <div className={cn("pb-2 pt-0.5 text-[13px]", !compact && "px-2", s.icon && (compact ? "pl-[26px]" : "pl-[34px]"))}>{s.details}</div>
          </Collapse>
        </>
      ) : (
        <div className={row}>{head}</div>
      )}
    </li>
  );
}

// RunOutput shows what a tool got and gave back: the call on top, the output below, both monospace.
// Long lines scroll inside it and never widen the card.
export function RunOutput({ call, children }: { call: ReactNode; children?: ReactNode }) {
  return (
    <div className="overflow-hidden rounded-lg border bg-surface font-mono text-[12px] [contain:inline-size]">
      <div className={cn("truncate px-3 py-2 text-foreground", children != null && "border-b")}>{call}</div>
      {children != null && <pre className="overflow-x-auto whitespace-pre px-3 py-2 leading-5 text-muted-foreground">{children}</pre>}
    </div>
  );
}
