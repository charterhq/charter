import type { ChatModelAdapter } from "@assistant-ui/react";
import { BRIEF_QUESTIONS, draft, missingFields, updateRoom, type BriefField } from "./store";

// Stand-in for the builder agent until the core has the web conversation API.
// The answer goes to the field the builder asked about in this room; the next question is
// whatever the brief still lacks. So it does not matter in which room or order things are said.
const ACK: Record<BriefField, string> = {
  goal: "Понял цель.",
  trigger: "Записал, когда он срабатывает.",
  duties: "Хорошо, границы понятны.",
  escalation: "Записал, кого звать.",
};

export function builderFor(roomId: string): ChatModelAdapter {
  return {
    async *run({ messages, abortSignal }) {
      const d = draft.get();
      const room = d.rooms.find((r) => r.id === roomId);
      const last = messages.filter((m) => m.role === "user").at(-1);
      const text = last?.content.map((p) => (p.type === "text" ? p.text : "")).join(" ").trim() ?? "";
      const field = guessField(text) ?? room?.asking ?? missingFields(d)[0];

      if (field && text) {
        draft.set((s) => ({ brief: { ...s.brief, [field]: text }, lastFilled: field }));
        if (field === "goal" && /алерт/i.test(text)) draft.set({ name: "alert-triage" });
      }
      const next = missingFields(draft.get())[0];
      const reply = [
        field ? ACK[field] : "Записал.",
        next ? BRIEF_QUESTIONS[next] : "Бриф собран – он на холсте. Посмотри и подтверди заявку.",
      ].join(" ");

      let out = "";
      for (const word of reply.split(" ")) {
        if (abortSignal.aborted) return;
        await new Promise((r) => setTimeout(r, 30));
        out += (out ? " " : "") + word;
        yield { content: [{ type: "text", text: out }] };
      }

      const history = messages.map((m) => ({
        role: m.role as "user" | "assistant",
        text: m.content.map((p) => (p.type === "text" ? p.text : "")).join(""),
      }));
      updateRoom(roomId, {
        asking: next,
        messages: [...history, { role: "assistant", text: out }],
        ...(room?.title === "Новая беседа" && text ? { title: text.length > 38 ? text.slice(0, 38) + "…" : text } : {}),
      });
    },
  };
}

// The real builder decides which field an answer belongs to; the mock guesses by words,
// so an answer lands in the right field whatever was asked.
function guessField(text: string): BriefField | null {
  if (/звать|позвать|эскал|дежурн|@/i.test(text)) return "escalation";
  if (/не делает|никогда|не жмет|не чинит|проверяет|смотрит/i.test(text)) return "duties";
  if (/реагир|срабатыв|прилетает|каждый алерт|по событию|по расписанию/i.test(text) && !/хочу/i.test(text)) return "trigger";
  return null;
}
