import { FileCode2 } from "lucide-react";
import { t as messages, useT } from "@/i18n";
import { focusField, label, type BriefField, type Draft } from "@/lib/store";
import { cn } from "@/lib/utils";

// A preview of the package the brief turns into, in the v1 format (schemas/*.schema.json).
// Lines that come from the brief name their field: they light up when filled and lead to the card.
// The rest of the required fields are decided on the "Description" stage, so the preview says so
// instead of inventing values.

type Line = { text: string; field?: BriefField; pending?: boolean; note?: boolean; label?: string };

const SCENARIO = "main";

// Brief fields the preview reads (docs/templates/brief.md of the platform): the job, when it starts,
// and what it does by itself and never. A builder package with other names shows these as pending.
const F = { work: "work", trigger: "trigger", limits: "limits" };

// The "Agent package" artifact. Before the request is confirmed it opens as a read-only draft.
export function Contract({ d, draftOnly }: { d: Draft; draftOnly: boolean }) {
  const t = useT();
  const files: { path: string; lines: Line[] }[] = [
    { path: "agent.yml", lines: agentLines(d) },
    { path: `scenarios/${SCENARIO}/scenario.yml`, lines: scenarioLines(d) },
  ];

  return (
    <article aria-labelledby="package-title" className="mx-auto max-w-2xl">
      <div className="flex items-center gap-2">
        <h2 id="package-title" className="text-lg font-semibold tracking-tight">
          {t.contract.title}
        </h2>
        {draftOnly && (
          <span className="rounded-full border border-dashed border-border-strong px-2 py-px text-[12px] text-muted-foreground">
            {t.contract.draft}
          </span>
        )}
      </div>
      <p className="mb-6 mt-1 text-[14px] text-faint">
        {draftOnly ? t.contract.draftIntro : t.contract.intro}
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
                <YamlLine key={i} line={l.field ? { ...l, label: label(d, l.field) } : l} />
              ))}
            </pre>
          </figure>
        ))}
      </div>
    </article>
  );
}

function YamlLine({ line }: { line: Line }) {
  const t = useT();
  // Wrapped lines hang under their own key or list item, like an editor with soft wrap.
  const hang = (line.text.match(/^ */)?.[0].length ?? 0) + (/^ *- /.test(line.text) ? 2 : 0);
  const style = { paddingLeft: `calc(1rem + ${hang}ch)`, textIndent: `-${hang}ch` };
  const body = (
    <>
      <span className={cn(line.pending || line.note ? "text-faint" : "text-foreground")}>{line.text}</span>
      {line.field && (
        <span className="ml-3 hidden rounded bg-card px-1.5 font-sans text-[11px] text-muted-foreground group-hover:inline">
          {line.label}
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
      title={t.contract.toField(line.label ?? "")}
      className={cn(
        "group block w-full border-l-2 pr-4 text-left hover:bg-card",
        line.pending ? "border-transparent" : "border-success/70",
      )}
    >
      {body}
    </button>
  );
}

const text = (v: unknown) => (typeof v === "string" ? v.trim() : "");

// Lines are built outside React; Contract redraws on a language switch, so they follow it.
function agentLines(d: Draft): Line[] {
  const c = messages().contract;
  const work = text(d.brief[F.work]);
  const limits = (d.brief[F.limits] ?? {}) as Record<string, unknown>;
  const does = sentences(text(limits.self));
  const never = sentences(text(limits.never));
  const team = text(d.attrs.team) || c.team;
  const owner = d.members.find((m) => m.role === "owner")?.person ?? c.owner;
  return [
    { text: "apiVersion: charter.dev/v1" },
    { text: "kind: Agent" },
    { text: "metadata:" },
    { text: `  name: ${slug(d.name)}` },
    { text: `  team: ${team}` },
    { text: `  owner: ${owner}` },
    { text: "  version: 0.1.0" },
    { text: "  status: draft" },
    { text: "spec:" },
    { text: "  goals:" },
    work ? { text: `    - ${q(work)}`, field: F.work } : { text: `    ${c.goals}`, field: F.work, pending: true },
    { text: "  duties:" },
    ...(does.length
      ? does.map((s): Line => ({ text: `    - ${q(s)}`, field: F.limits }))
      : [{ text: `    ${c.duties}`, field: F.limits, pending: true }]),
    { text: "  not_duties:" },
    ...(never.length
      ? never.map((s): Line => ({ text: `    - ${q(s)}`, field: F.limits }))
      : [{ text: `    ${c.notDuties}`, field: F.limits, pending: true }]),
    { text: "  scenarios:" },
    { text: `    - name: ${SCENARIO}` },
    { text: `  ${c.agentLater(messages().stages.description)}`, note: true },
  ];
}

function scenarioLines(d: Draft): Line[] {
  const c = messages().contract;
  const trigger = text(d.brief[F.trigger]);
  return [
    { text: "apiVersion: charter.dev/v1" },
    { text: "kind: Scenario" },
    { text: "metadata:" },
    { text: `  name: ${SCENARIO}` },
    { text: "spec:" },
    { text: "  triggers:" },
    trigger
      ? { text: `    - ${guessTrigger(trigger)}  # "${clip(trigger, 48)}"`, field: F.trigger }
      : { text: `    ${c.trigger}`, field: F.trigger, pending: true },
    { text: `  ${c.scenarioLater(messages().stages.description)}`, note: true },
  ];
}

// The builder lays the brief into the package; the preview only splits a field into sentences.
// The trigger guess reads both languages: the brief is in whatever language people write.
function sentences(t: string) {
  return t
    .split(/(?<=[.!?;])\s+|\n+/)
    .map((s) => s.trim().replace(/^[-•]\s*/, "").replace(/[.;]$/, ""))
    .filter(Boolean);
}

function guessTrigger(text: string) {
  if (/расписан|кажд(ый|ое|ую) (день|час|утро|недел)|по утрам|ежеднев|cron|schedule|every (day|hour|morning|week)|daily|hourly/i.test(text)) return "{type: schedule, source: daily}";
  if (/пишут|сообщени|в чате|спрашива|упомина|message|in (the )?chat|asks?\b|mention/i.test(text)) return "{type: message, source: chat}";
  if (/по запросу|вызыва|api|on request|\bcalls?\b/i.test(text)) return "{type: call, source: api}";
  return `{type: event, source: ${/алерт|alert/i.test(text) ? "alerts" : "events"}}`;
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
