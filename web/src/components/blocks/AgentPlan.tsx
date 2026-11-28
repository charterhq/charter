import { GripVertical, Pause, Play, Plus, X } from "lucide-react";
import { m, useDragControls, useMotionValue } from "motion/react";
import { useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { Button } from "../arc/button/button";
import { Collapse } from "./Collapse";
import { StatusIcon, type StepStatus } from "./status";
import { useT } from "@/i18n";
import { cn } from "@/lib/utils";

// The plan an agent writes before it acts. While it is a draft, people rename, drag or move with
// Alt+Arrow, remove and add steps; once approved it turns into a live checklist with a progress bar,
// what the current step is doing under it, and how long each step took. Data and changes go through props.

export type PlanStep = {
  id: string;
  title: string;
  status: StepStatus;
  // How long the step took, already formatted.
  meta?: ReactNode;
  // A line under the title: "Added by agent".
  note?: ReactNode;
  // What the step is doing right now; shows under it while it runs.
  activity?: ReactNode;
};

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
  paused?: boolean;
  onPause?: () => void;
  onResume?: () => void;
};

export function AgentPlan({ goal, author, steps, draft, onChange, onApprove, onKeepPlanning, paused, onPause, onResume }: AgentPlanProps) {
  const t = useT();
  const editable = draft && !!onChange;
  const done = steps.filter((s) => s.status === "done" || s.status === "skipped").length;
  const current = steps.findIndex((s) => s.status === "running" || s.status === "waiting");
  const finished = !draft && done === steps.length && steps.length > 0;
  const [adding, setAdding] = useState("");
  const hint = useId();

  const move = (i: number, by: number) => {
    const j = i + by;
    if (j < 0 || j >= steps.length) return;
    const next = [...steps];
    const [s] = next.splice(i, 1);
    next.splice(j, 0, s);
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
      <header className="relative flex items-start gap-3 border-b py-3 pl-4 pr-4">
        <div className="min-w-0 flex-1">
          <div className="text-[13px] text-muted-foreground">
            <span className="font-semibold text-foreground">{t.blocks.plan}</span> · {t.blocks.steps(steps.length)}
          </div>
          <h3 className="mt-0.5 text-[14px] text-foreground">{goal}</h3>
        </div>
        {draft ? (
          author && <div className="flex shrink-0 items-center gap-1.5 text-[12px] text-faint">{author}</div>
        ) : (
          <div className="shrink-0 text-[12px] tabular-nums text-faint">{t.blocks.ofTotal(done, steps.length)}</div>
        )}
        {!draft && (
          <div
            role="progressbar"
            aria-label={t.blocks.planProgress}
            aria-valuemin={0}
            aria-valuemax={steps.length}
            aria-valuenow={done}
            className="absolute inset-x-0 -bottom-px h-0.5"
          >
            <div
              className="h-full origin-left bg-foreground/70 transition-transform duration-500 ease-[cubic-bezier(.22,1,.36,1)]"
              style={{ transform: `scaleX(${steps.length ? done / steps.length : 0})` }}
            />
          </div>
        )}
      </header>

      {editable && (
        <p id={hint} className="sr-only">
          {t.blocks.planHint}
        </p>
      )}
      <ol className="px-2 py-2" aria-label={t.blocks.plan}>
        {editable
          ? steps.map((s, i) => (
              <DraftRow
                key={s.id}
                step={s}
                index={i}
                count={steps.length}
                hint={hint}
                onMove={(by) => move(i, by)}
                onRename={(title) => onChange!(steps.map((x) => (x.id === s.id ? { ...x, title } : x)))}
                onRemove={() => onChange!(steps.filter((x) => x.id !== s.id))}
              />
            ))
          : steps.map((s, i) => <LiveRow key={s.id} step={s} index={i} draft={draft} />)}
        {editable && (
          <li className="flex min-h-9 items-center gap-2 px-1 text-[14px]">
            <Plus size={15} aria-hidden className="mx-1.5 shrink-0 text-faint" />
            <input
              value={adding}
              aria-label={t.blocks.addStep}
              placeholder={t.blocks.addStep}
              onChange={(e) => setAdding(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && add()}
              onBlur={add}
              className="min-w-0 flex-1 rounded-md bg-transparent px-2 py-1 outline-none transition-colors placeholder:text-faint hover:bg-surface/60 focus:bg-surface"
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
      {!draft && (current >= 0 || finished || onPause || onResume) && (
        <footer className="flex min-h-14 flex-wrap items-center justify-between gap-2 border-t px-4 py-2.5 text-[13px] text-muted-foreground">
          <span role="status">{finished ? t.blocks.planDone : paused ? t.blocks.planPaused : current >= 0 ? t.blocks.workingOnStep(current + 1) : null}</span>
          {!finished &&
            (paused
              ? onResume && (
                  <Button size="sm" variant="secondary" onClick={onResume}>
                    <Play size={13} aria-hidden /> {t.blocks.resume}
                  </Button>
                )
              : onPause && (
                  <Button size="sm" variant="secondary" onClick={onPause}>
                    <Pause size={13} aria-hidden /> {t.blocks.pause}
                  </Button>
                ))}
        </footer>
      )}
    </section>
  );
}

function LiveRow({ step: s, index, draft }: { step: PlanStep; index: number; draft?: boolean }) {
  return (
    <li className="px-2 py-1.5 text-[14px]">
      <div className="flex items-start gap-3 leading-5">
        {draft ? (
          <span aria-hidden className="w-4 shrink-0 text-center text-[12px] leading-5 tabular-nums text-faint">
            {index + 1}
          </span>
        ) : (
          <span className="grid h-5 w-4 shrink-0 place-items-center">
            <StatusIcon status={s.status} size={16} disc />
          </span>
        )}
        <span
          className={cn(
            "min-w-0 flex-1 transition-colors duration-200",
            s.status === "running" ? "text-muted-foreground" : s.status === "done" ? "text-foreground/85" : s.status === "skipped" ? "text-faint line-through" : "text-foreground",
          )}
        >
          {s.title}
        </span>
        {s.meta && <span className="shrink-0 text-[12px] leading-5 tabular-nums text-faint">{s.meta}</span>}
      </div>
      {s.note && <div className="mt-0.5 flex items-center gap-1.5 pl-7 text-[12px] text-faint">{s.note}</div>}
      {s.activity != null && (
        <Collapse open={s.status === "running"}>
          <div className="space-y-0.5 pb-0.5 pl-7 pt-1 text-[12px] text-muted-foreground">{s.activity}</div>
        </Collapse>
      )}
    </li>
  );
}

// A draft step: a grip to drag it, its title to rename in place, a cross to remove it. Dragging swaps
// it with a neighbour once it passes half of that neighbour; the rest slide with a layout animation.
function DraftRow({
  step: s,
  index,
  count,
  hint,
  onMove,
  onRename,
  onRemove,
}: {
  step: PlanStep;
  index: number;
  count: number;
  hint: string;
  onMove: (by: number) => void;
  onRename: (title: string) => void;
  onRemove: () => void;
}) {
  const t = useT();
  const controls = useDragControls();
  const y = useMotionValue(0);
  const row = useRef<HTMLLIElement>(null);
  const before = useRef(s.title);
  const [dragging, setDragging] = useState(false);
  const keys = (e: KeyboardEvent) => {
    if (e.altKey && (e.key === "ArrowUp" || e.key === "ArrowDown")) {
      e.preventDefault();
      onMove(e.key === "ArrowUp" ? -1 : 1);
    }
  };
  return (
    <m.li
      ref={row}
      layout="position"
      drag="y"
      dragListener={false}
      dragControls={controls}
      dragSnapToOrigin
      style={{ y }}
      onDragStart={() => setDragging(true)}
      onDragEnd={() => setDragging(false)}
      onDrag={() => {
        const h = row.current?.offsetHeight ?? 36;
        if (y.get() > h / 2 && index < count - 1) onMove(1);
        else if (y.get() < -h / 2 && index > 0) onMove(-1);
      }}
      className={cn("group relative flex min-h-9 items-center gap-2 rounded-lg px-1 text-[14px]", dragging && "z-10 bg-card shadow-[var(--elevation-raised)]")}
    >
      <button
        type="button"
        aria-label={t.blocks.moveStep(index + 1, s.title)}
        aria-describedby={hint}
        onPointerDown={(e) => {
          e.preventDefault();
          controls.start(e);
        }}
        onKeyDown={keys}
        className="grid size-7 shrink-0 cursor-grab touch-none place-items-center rounded-md text-faint transition-colors hover:text-foreground active:cursor-grabbing"
      >
        <GripVertical size={15} aria-hidden />
      </button>
      <input
        value={s.title}
        aria-label={t.blocks.stepN(index + 1)}
        aria-describedby={hint}
        placeholder={t.blocks.describeStep}
        onFocus={() => (before.current = s.title)}
        onChange={(e) => onRename(e.target.value)}
        onKeyDown={(e) => {
          keys(e);
          if (e.key === "Escape") {
            onRename(before.current);
            e.currentTarget.blur();
          }
          if (e.key === "Enter") e.currentTarget.blur();
        }}
        className="min-w-0 flex-1 rounded-md bg-transparent px-2 py-1 outline-none transition-colors placeholder:text-faint hover:bg-surface/60 focus:bg-surface"
      />
      <button
        type="button"
        aria-label={t.blocks.removeStep(s.title)}
        title={t.blocks.removeStep(s.title)}
        onClick={onRemove}
        className="grid size-7 shrink-0 place-items-center rounded-md text-faint transition-[opacity,color] hover:text-foreground group-focus-within:opacity-100 group-hover:opacity-100 sm:opacity-0"
      >
        <X size={14} aria-hidden />
      </button>
    </m.li>
  );
}
