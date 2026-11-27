import { FileCode2 } from "lucide-react";
import { BRIEF_LABELS, focusField, type BriefField, type Draft } from "@/lib/store";
import { cn } from "@/lib/utils";

// A preview of the package the brief turns into, in the v1 format (schemas/*.schema.json).
// Lines that come from the brief name their field: they light up when filled and lead to the card.
// The rest of the required fields are decided on the "Описание" stage, so the preview says so
// instead of inventing values.

type Line = { text: string; field?: BriefField; pending?: boolean; note?: boolean };

const SCENARIO = "main";

// The "Пакет агента" artifact. Before the request is confirmed it opens as a read-only draft.
export const PACKAGE = { stage: 1, artifact: 0 };

export function Contract({ d }: { d: Draft }) {
  const draftOnly = d.stage < PACKAGE.stage;
  const files: { path: string; lines: Line[] }[] = [
    { path: "agent.yml", lines: agentLines(d) },
    { path: `scenarios/${SCENARIO}/scenario.yml`, lines: scenarioLines(d) },
  ];

  return (
    <article aria-labelledby="package-title" className="mx-auto max-w-2xl">
      <div className="flex items-center gap-2">
        <h2 id="package-title" className="text-lg font-semibold tracking-tight">
          Пакет агента
        </h2>
        {draftOnly && (
          <span className="rounded-full border border-dashed border-border-strong px-2 py-px text-[12px] text-muted-foreground">
            черновик
          </span>
        )}
      </div>
      <p className="mb-6 mt-1 text-[14px] text-faint">
        {draftOnly
          ? "Соберется после заявки. Сейчас показано то, что уже есть в брифе: строка из брифа ведет к своему полю."
          : "Пакет в формате v1: его проверяет agentctl validate и загружает ядро."}
      </p>
      <div className="space-y-3">
        {files.map((f) => (
          <figure key={f.path} className="overflow-hidden rounded-xl border bg-surface">
            <figcaption className="flex items-center gap-2 border-b px-3 py-2 font-mono text-[12px] text-muted-foreground">
              <FileCode2 size={13} aria-hidden />
              {f.path}
            </figcaption>
            <pre className="whitespace-pre-wrap py-2 font-mono text-[12.5px] leading-[1.7] [overflow-wrap:anywhere]">
              {f.lines.map((l, i) => (
                <YamlLine key={i} line={l} />
              ))}
            </pre>
          </figure>
        ))}
      </div>
    </article>
  );
}

function YamlLine({ line }: { line: Line }) {
  // Wrapped lines hang under their own key or list item, like an editor with soft wrap.
  const hang = (line.text.match(/^ */)?.[0].length ?? 0) + (/^ *- /.test(line.text) ? 2 : 0);
  const style = { paddingLeft: `calc(1rem + ${hang}ch)`, textIndent: `-${hang}ch` };
  const body = (
    <>
      <span className={cn(line.pending || line.note ? "text-faint" : "text-foreground")}>{line.text}</span>
      {line.field && (
        <span className="ml-3 hidden rounded bg-card px-1.5 font-sans text-[11px] text-muted-foreground group-hover:inline">
          {BRIEF_LABELS[line.field]}
        </span>
      )}
    </>
  );
  if (!line.field) return <div style={style} className="pr-4">{body}</div>;
  return (
    <button
      type="button"
      onClick={() => focusField(line.field!)}
      style={{ ...style, paddingLeft: `calc(1rem - 2px + ${hang}ch)` }}
      title={`К полю «${BRIEF_LABELS[line.field]}»`}
      className={cn(
        "group block w-full border-l-2 pr-4 text-left hover:bg-card",
        line.pending ? "border-transparent" : "border-success/70",
      )}
    >
      {body}
    </button>
  );
}

function agentLines(d: Draft): Line[] {
  const { goal, duties, escalation } = d.brief;
  const { does, doesNot } = splitDuties(duties);
  return [
    { text: "apiVersion: charter.dev/v1" },
    { text: "kind: Agent" },
    { text: "metadata:" },
    { text: `  name: ${slug(d.name)}` },
    { text: "  team: sre" },
    { text: "  owner: e.azimov" },
    { text: "  version: 0.1.0" },
    { text: "  status: shadow  # первый выпуск идет в тень" },
    { text: "spec:" },
    { text: "  goals:" },
    goal ? { text: `    - ${q(goal)}`, field: "goal" } : { text: "    # ← Зачем", field: "goal", pending: true },
    { text: "  duties:" },
    ...(does.length
      ? does.map((s): Line => ({ text: `    - ${q(s)}`, field: "duties" }))
      : [{ text: "    # ← Что делает", field: "duties" as const, pending: true }]),
    { text: "  not_duties:" },
    ...(doesNot.length
      ? doesNot.map((s): Line => ({ text: `    - ${q(s)}`, field: "duties" }))
      : [{ text: "    # ← Чего не делает", field: "duties" as const, pending: true }]),
    { text: "  knowledge:" },
    escalation
      ? { text: `    - {path: knowledge/people.yml}  # кого звать: ${escalation}`, field: "escalation" }
      : { text: "    # ← Кого звать: knowledge/people.yml", field: "escalation", pending: true },
    { text: "  scenarios:" },
    { text: `    - name: ${SCENARIO}` },
    { text: "  # description, persona, model, data_classes, budgets, memory, fallback – на этапе «Описание»", note: true },
  ];
}

function scenarioLines(d: Draft): Line[] {
  const { trigger } = d.brief;
  return [
    { text: "apiVersion: charter.dev/v1" },
    { text: "kind: Scenario" },
    { text: "metadata:" },
    { text: `  name: ${SCENARIO}` },
    { text: "spec:" },
    { text: "  triggers:" },
    trigger
      ? { text: `    - ${guessTrigger(trigger)}  # «${clip(trigger, 48)}»`, field: "trigger" }
      : { text: "    # ← Когда срабатывает", field: "trigger", pending: true },
    { text: "  # схемы входа и выхода, инструкции, autonomy, eval – на этапе «Описание»", note: true },
  ];
}

// The builder will split duties properly; the preview splits by sentences and negations.
function splitDuties(text?: string) {
  const parts = (text ?? "")
    .split(/(?<=[.!?;])\s+|\s*,\s*(?=но\s|а\s)/i)
    .map((s) => s.trim().replace(/[.;]$/, ""))
    .filter(Boolean);
  const neg = /(^|\s)(не|никогда|нельзя)\s/i;
  return { does: parts.filter((s) => !neg.test(s)), doesNot: parts.filter((s) => neg.test(s)) };
}

function guessTrigger(text: string) {
  if (/расписан|кажд(ый|ое|ую) (день|час|утро|недел)|по утрам|ежеднев|cron/i.test(text)) return "{type: schedule, source: daily}";
  if (/пишут|сообщени|в чате|спрашива|упомина/i.test(text)) return "{type: message, source: chat}";
  if (/по запросу|вызыва|api/i.test(text)) return "{type: call, source: api}";
  return `{type: event, source: ${/алерт/i.test(text) ? "alerts" : "events"}}`;
}

function slug(name: string) {
  return /^[a-z0-9-]+$/.test(name) ? name : "new-agent";
}

// Plain words stay bare; anything YAML could misread is quoted.
function q(s: string) {
  return /^[\p{L}\p{N} ,.()«»–-]+$/u.test(s) && !/^[-\s]/.test(s) ? s : JSON.stringify(s);
}

function clip(s: string, n: number) {
  return s.length > n ? s.slice(0, n - 1) + "…" : s;
}
