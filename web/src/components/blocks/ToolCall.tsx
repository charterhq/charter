import { ChevronDown } from "lucide-react";
import { useId, useState, type ReactNode } from "react";
import { StatusIcon, type StepStatus } from "./status";
import { useT } from "@/i18n";
import { cn } from "@/lib/utils";

// A tool call in a transcript: the tool's mark and the call in plain words, its state, and the
// input and output a click away. Data comes through props; the block knows nothing about agents.

export type ToolCallProps = {
  // The tool's mark: an icon or a small logo.
  icon?: ReactNode;
  title: ReactNode;
  // Short facts after the title: "4 results · 1.2s".
  meta?: ReactNode;
  status: StepStatus;
  // Input, output or an error; the row opens to show them.
  details?: ReactNode;
  // Instead of details, the row can lead somewhere: the title becomes a button.
  onOpen?: () => void;
  openLabel?: string;
};

export function ToolCall({ icon, title, meta, status, details, onOpen, openLabel }: ToolCallProps) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const id = useId();
  const row = (
    <>
      {icon && (
        <span aria-hidden className="grid size-4 shrink-0 place-items-center text-muted-foreground">
          {icon}
        </span>
      )}
      <span className={cn("min-w-0 flex-1 truncate text-left", status === "running" ? "text-muted-foreground" : "text-foreground")}>{title}</span>
      {meta && <span className="hidden shrink-0 text-[12px] tabular-nums text-faint sm:inline">{meta}</span>}
      <StatusIcon status={status} />
    </>
  );
  const rowClass = "flex min-h-10 w-full items-center gap-2.5 px-3.5 text-[13px]";
  return (
    <li className="list-none">
      {details ? (
        <button
          type="button"
          aria-expanded={open}
          aria-controls={id}
          title={open ? t.blocks.hideDetails : t.blocks.showDetails}
          onClick={() => setOpen(!open)}
          className={cn(rowClass, "hover:bg-surface/70 focus-visible:bg-surface/70")}
        >
          {row}
          <ChevronDown size={14} aria-hidden className={cn("shrink-0 text-faint transition-transform duration-200", open && "rotate-180")} />
        </button>
      ) : onOpen ? (
        <button type="button" onClick={onOpen} aria-label={openLabel} className={cn(rowClass, "hover:bg-surface/70 focus-visible:bg-surface/70")}>
          {row}
        </button>
      ) : (
        <div className={rowClass}>{row}</div>
      )}
      {details && open && (
        <div id={id} className="animate-[fade-in_.16s_ease-out] border-t border-dashed bg-surface/50 px-3.5 py-2.5 text-[12px] text-muted-foreground [overflow-wrap:anywhere]">
          {details}
        </div>
      )}
    </li>
  );
}

// ToolCallGroup gathers the calls of one step under a header with their marks; it folds when done.
export function ToolCallGroup({
  title,
  icons = [],
  children,
  defaultOpen = true,
}: {
  title: ReactNode;
  icons?: ReactNode[];
  children: ReactNode;
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);
  const id = useId();
  return (
    <div className="overflow-hidden rounded-xl border bg-card shadow-[var(--elevation-resting)]">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen(!open)}
        className="flex min-h-10 w-full items-center gap-2.5 px-3.5 text-[13px] text-muted-foreground hover:text-foreground"
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
        <ChevronDown size={14} aria-hidden className={cn("shrink-0 text-faint transition-transform duration-200", open && "rotate-180")} />
      </button>
      {open && (
        <ul id={id} className="divide-y border-t">
          {children}
        </ul>
      )}
    </div>
  );
}
