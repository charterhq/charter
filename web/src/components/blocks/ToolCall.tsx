import { ChevronDown, RotateCcw } from "lucide-react";
import { Children, useId, useState, type ReactNode } from "react";
import { Button } from "../arc/button/button";
import { Collapse } from "./Collapse";
import { CopyButton } from "./CopyButton";
import { StatusIcon, type StepStatus } from "./status";
import { useT } from "@/i18n";
import { cn } from "@/lib/utils";

// A tool call in a transcript: the tool's mark and the call in plain words, its state, and the
// input and output a click away. A failed call says why under the title and offers a retry, a call
// that waits on a person offers allow and deny. Data comes through props; the block knows nothing
// about agents.

export type ToolCallProps = {
  // The tool's mark: an icon or a small logo.
  icon?: ReactNode;
  title: ReactNode;
  // Short facts after the title: "4 results · 1.2s".
  meta?: ReactNode;
  status: StepStatus;
  // Why it failed or what it waits for; shows under the title.
  message?: ReactNode;
  // What the call got: argument names and values.
  input?: Record<string, ReactNode>;
  // What it gave back.
  output?: ReactNode;
  // Plain text for the copy buttons of the sections.
  inputText?: string;
  outputText?: string;
  onRetry?: () => void;
  onAllow?: () => void;
  onDeny?: () => void;
  // Instead of details, the row can lead somewhere: the whole row becomes a button.
  onOpen?: () => void;
  openLabel?: string;
  defaultOpen?: boolean;
};

export function ToolCall(p: ToolCallProps) {
  const { icon, title, meta, status, message, input, output, onOpen, openLabel } = p;
  const t = useT();
  const [open, setOpen] = useState(!!p.defaultOpen);
  const id = useId();
  const failed = status === "error";
  const hasInput = !!input && Object.keys(input).length > 0;
  const body = hasInput || output != null || (failed && message != null);
  const actions =
    failed && p.onRetry ? (
      <Button size="sm" variant="secondary" onClick={p.onRetry} aria-label={`${t.blocks.retryCall}: ${typeof title === "string" ? title : ""}`.trim()}>
        <RotateCcw size={13} aria-hidden /> {t.blocks.retryCall}
      </Button>
    ) : status === "waiting" && (p.onAllow || p.onDeny) ? (
      <>
        {p.onDeny && (
          <Button size="sm" variant="secondary" onClick={p.onDeny}>
            {t.blocks.deny}
          </Button>
        )}
        {p.onAllow && (
          <Button size="sm" onClick={p.onAllow}>
            {t.blocks.allow}
          </Button>
        )}
      </>
    ) : null;

  // The row: a button stretched over it opens the body, the actions sit above that button.
  const toggle = body || onOpen;
  return (
    <li className="list-none">
      <div className={cn("relative flex min-h-11 flex-wrap items-center gap-x-2.5 gap-y-2 px-3.5 py-2 text-[13px]", toggle && "transition-colors has-[>button:hover]:bg-surface/60")}>
        {icon && (
          <span aria-hidden className="grid size-4 shrink-0 place-items-center self-start pt-[3px] text-muted-foreground">
            {icon}
          </span>
        )}
        <div className="order-1 min-w-0 flex-1">
          <div className={cn("truncate", status === "running" ? "text-muted-foreground" : "text-foreground")}>{title}</div>
          {message != null && <div className="mt-0.5 text-[12px] text-muted-foreground [overflow-wrap:anywhere]">{message}</div>}
        </div>
        {actions ? (
          <span className="relative z-10 order-2 flex shrink-0 gap-1.5 max-sm:order-5 max-sm:basis-full max-sm:pl-[26px]">{actions}</span>
        ) : (
          meta && <span className="order-2 hidden shrink-0 text-[12px] tabular-nums text-faint sm:inline">{meta}</span>
        )}
        <span className="order-3 inline-grid">
          <StatusIcon status={status} />
        </span>
        {body && <ChevronDown size={15} aria-hidden className={cn("order-4 shrink-0 text-faint transition-transform duration-200 ease-[cubic-bezier(.22,1,.36,1)]", open && "rotate-180")} />}
        {toggle && (
          <button
            type="button"
            aria-expanded={body ? open : undefined}
            aria-controls={body ? id : undefined}
            aria-label={onOpen && !body ? openLabel : undefined}
            title={body ? (open ? t.blocks.hideDetails : t.blocks.showDetails) : undefined}
            onClick={() => (body ? setOpen(!open) : onOpen?.())}
            className="absolute inset-0 rounded-[inherit] outline-offset-[-2px]"
          >
            {body && <span className="sr-only">{title}</span>}
          </button>
        )}
      </div>
      {body && (
        <Collapse id={id} open={open}>
          <div className={cn("space-y-3 pb-3 pr-3.5", icon ? "pl-10" : "pl-3.5")}>
            {hasInput && (
              <Section label={t.blocks.callInput} copy={p.inputText}>
                <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-5 gap-y-1">
                  {Object.entries(input!).map(([k, v]) => (
                    <div key={k} className="contents">
                      <dt className="text-faint">{k}</dt>
                      <dd className="text-foreground [overflow-wrap:anywhere]">{v}</dd>
                    </div>
                  ))}
                </dl>
              </Section>
            )}
            {failed && message != null ? (
              <Section label={t.blocks.callError} copy={typeof message === "string" ? message : undefined}>
                <p className="font-sans text-[13px] text-foreground">{message}</p>
              </Section>
            ) : (
              output != null && (
                <Section label={t.blocks.callOutput} copy={p.outputText}>
                  {output}
                </Section>
              )
            )}
          </div>
        </Collapse>
      )}
    </li>
  );
}

function Section({ label, copy, children }: { label: string; copy?: string; children: ReactNode }) {
  const t = useT();
  return (
    <div role="group" aria-label={label}>
      <div className="mb-1.5 flex min-h-6 items-center justify-between text-[12px] text-muted-foreground">
        <span aria-hidden>{label}</span>
        {copy !== undefined && <CopyButton small text={copy} label={t.blocks.copySection(label)} />}
      </div>
      <div className="overflow-x-auto rounded-lg bg-surface px-3 py-2.5 font-mono text-[12px] leading-5 [contain:inline-size]">{children}</div>
    </div>
  );
}

// ToolCallGroup gathers the calls of one step under a header with their marks; it folds.
export function ToolCallGroup({
  title,
  icons = [],
  failed = 0,
  children,
  defaultOpen = true,
}: {
  title: ReactNode;
  icons?: ReactNode[];
  // How many calls failed; shown on the right of the header.
  failed?: number;
  children: ReactNode;
  defaultOpen?: boolean;
}) {
  const t = useT();
  const [open, setOpen] = useState(defaultOpen);
  const id = useId();
  return (
    <div className="overflow-hidden rounded-xl border bg-card shadow-[var(--elevation-resting)]">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen(!open)}
        className="flex min-h-11 w-full items-center gap-2.5 px-3.5 text-[13px] text-muted-foreground transition-colors hover:bg-surface/60 hover:text-foreground"
      >
        {icons.length > 0 && (
          <span aria-hidden className="flex shrink-0 -space-x-1">
            {icons.slice(0, 3).map((icon, i) => (
              <span key={i} className="grid size-5 place-items-center rounded-full border bg-card">
                {icon}
              </span>
            ))}
          </span>
        )}
        <span className="min-w-0 flex-1 truncate text-left">{title}</span>
        {failed > 0 && <span className="shrink-0 text-[12px] text-destructive">{t.blocks.failedCount(failed)}</span>}
        <ChevronDown size={15} aria-hidden className={cn("shrink-0 text-faint transition-transform duration-200 ease-[cubic-bezier(.22,1,.36,1)]", open && "rotate-180")} />
      </button>
      <Collapse id={id} open={open}>
        <ul className={cn("border-t py-1", Children.count(children) === 0 && "hidden")}>{children}</ul>
      </Collapse>
    </div>
  );
}
