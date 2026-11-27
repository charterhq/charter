import type { ChatModelAdapter } from "@assistant-ui/react";
import { draft, type BriefField } from "./store";

// Stand-in for the builder agent until the core has the web conversation API.
// It walks the request questions one at a time and fills the draft on the canvas.
const STEPS: { field: BriefField; next: string }[] = [
  { field: "goal", next: "Понял цель. На что агент реагирует – что запускает его работу?" },
  { field: "trigger", next: "Записал. Что он делает – и чего не делает никогда?" },
  { field: "duties", next: "Хорошо. Кого звать, если проблема подтвердилась?" },
  { field: "escalation", next: "Заявка собрана – она на холсте. Посмотри и подтверди этап." },
];

export const builderMock: ChatModelAdapter = {
  async *run({ messages, abortSignal }) {
    const userTurns = messages.filter((m) => m.role === "user");
    const last = userTurns.at(-1);
    const text = last?.content.map((p) => (p.type === "text" ? p.text : "")).join(" ").trim() ?? "";
    const step = STEPS[Math.min(userTurns.length - 1, STEPS.length - 1)];

    draft.set((d) => ({ brief: { ...d.brief, [step.field]: text } }));
    const name = step.field === "goal" ? guessName(text) : null;
    if (name) draft.set({ name });

    let out = "";
    for (const word of step.next.split(" ")) {
      if (abortSignal.aborted) return;
      await new Promise((r) => setTimeout(r, 35));
      out += (out ? " " : "") + word;
      yield { content: [{ type: "text", text: out }] };
    }
  },
};

// Naming the agent is the real builder's job; the mock only recognises the first example.
function guessName(text: string): string | null {
  return /алерт/i.test(text) ? "alert-triage" : null;
}
