import { ArrowDown, ArrowUp, Plus, X } from "lucide-react";
import { useState, type ReactNode } from "react";
import { Button } from "../arc/button/button";
import { StatusIcon, type StepStatus } from "./status";
import { useT } from "@/i18n";
import { cn } from "@/lib/utils";

// The plan an agent writes before it acts. While it is a draft, people rename, reorder, remove and add
// steps; once approved it turns into a live checklist. Data and changes go through props.

export type PlanStep = { id: string; title: string; status: StepStatus };

export type AgentPlanProps = {
  goal: ReactNode;
  // Who planned: a mark and a model name.
  author?: ReactNode;
  steps: PlanStep[];
  // A draft can be edited and approved; without onChange the plan is read-only.
  draft?: boolean;
  onChange?: (steps: PlanStep[]) => void;
  onApprove?: () => void;
  onKeepPlanning?: () => void;
};

export function AgentPlan({ goal, author, steps, draft, onChange, onApprove, onKeepPlanning }: AgentPlanProps) {
  const t = useT();
  const editable = draft && !!onChange;
  const done = steps.filter((s) => s.status === "done").length;
  const [adding, setAdding] = useState("");

  const move = (i: number, by: number) => {
    const next = [...steps];
    const [s] = next.splice(i, 1);
    next.splice(i + by, 0, s);
    onChange?.(next);
  };
  const add = () => {
    const title = adding.trim();
    if (!title) return;
    onChange?.([...steps, { id: `step-${Date.now()}`, title, status: "todo" }]);
    setAdding("");
  };

  return (
    <section className="overflow-hidden rounded-xl border bg-card shadow-panel">
      <header className="flex flex-wrap items-start gap-x-3 gap-y-1 border-b px-4 py-3">
        <div className="min-w-0 flex-1 basis-56">
          <div className="text-[13px] text-muted-foreground">
            <span className="font-semibold text-foreground">{t.blocks.plan}</span> · {draft ? t.blocks.steps(steps.length) : t.blocks.stepsDone(done, steps.length)}
          </div>
          <h3 className="mt-0.5 text-[14px] text-foreground">{goal}</h3>
        </div>
        {author && <div className="flex shrink-0 items-center gap-1.5 text-[12px] text-faint">{author}</div>}
      </header>

      <ol className="px-2 py-2">
        {steps.map((s, i) => (
          <li key={s.id} className="group flex min-h-9 items-center gap-2.5 rounded-md px-2 text-[14px] hover:bg-surface/60">
            {draft ? (
              <span aria-hidden className="w-4 shrink-0 text-center text-[12px] tabular-nums text-faint">
                {i + 1}
              </span>
            ) : (
              <StatusIcon status={s.status} />
            )}
            {editable ? (
              <input
                value={s.title}
                aria-label={`${t.blocks.plan} ${i + 1}`}
                onChange={(e) => onChange!(steps.map((x) => (x.id === s.id ? { ...x, title: e.target.value } : x)))}
                className="min-w-0 flex-1 rounded bg-transparent py-1 outline-none focus:bg-surface"
              />
            ) : (
              <span className={cn("min-w-0 flex-1", s.status === "done" && "text-muted-foreground", s.status === "skipped" && "text-faint line-through")}>{s.title}</span>
            )}
            {editable && (
              <span className="hidden shrink-0 gap-0.5 group-focus-within:flex sm:group-hover:flex">
                <IconButton label={t.blocks.moveUp(s.title)} disabled={i === 0} onClick={() => move(i, -1)}>
                  <ArrowUp size={13} />
                </IconButton>
                <IconButton label={t.blocks.moveDown(s.title)} disabled={i === steps.length - 1} onClick={() => move(i, 1)}>
                  <ArrowDown size={13} />
                </IconButton>
                <IconButton label={t.blocks.removeStep(s.title)} onClick={() => onChange!(steps.filter((x) => x.id !== s.id))}>
                  <X size={13} />
                </IconButton>
              </span>
            )}
          </li>
        ))}
        {editable && (
          <li className="flex min-h-9 items-center gap-2.5 px-2 text-[14px]">
            <Plus size={14} aria-hidden className="w-4 shrink-0 text-faint" />
            <input
              value={adding}
              aria-label={t.blocks.addStep}
              placeholder={t.blocks.addStep}
              onChange={(e) => setAdding(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && add()}
              onBlur={add}
              className="min-w-0 flex-1 bg-transparent py-1 outline-none placeholder:text-faint"
            />
          </li>
        )}
      </ol>

      {draft && (onApprove || onKeepPlanning) && (
        <footer className="flex flex-wrap justify-end gap-2 border-t px-4 py-3">
          {onKeepPlanning && (
            <Button variant="secondary" size="sm" onClick={onKeepPlanning}>
              {t.blocks.keepPlanning}
            </Button>
          )}
          {onApprove && (
            <Button size="sm" onClick={onApprove} disabled={steps.length === 0}>
              {t.blocks.approvePlan}
            </Button>
          )}
        </footer>
      )}
    </section>
  );
}

function IconButton({ label, children, disabled, onClick }: { label: string; children: ReactNode; disabled?: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onClick}
      className="grid size-7 place-items-center rounded-md text-faint hover:bg-card hover:text-foreground disabled:opacity-30 disabled:hover:bg-transparent"
    >
      {children}
    </button>
  );
}
