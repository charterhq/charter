import { Canvas } from "./Canvas";
import { Chat } from "./Chat";
import { useT } from "@/i18n";
import { counted, draft, useDraft } from "@/lib/store";
import { cn } from "@/lib/utils";

// The builder: the chat and the agent. Below lg they are two tabs; from lg up they sit side by side.
export default function Builder() {
  const t = useT();
  const d = useDraft();
  const pane = d.pane;
  const setPane = (p: typeof pane) => draft.set({ pane: p });
  const total = counted(d).length;
  const filled = total - d.missing.length;
  return (
    <main className="flex min-h-0 flex-1 flex-col gap-3 p-3 lg:grid lg:grid-cols-[minmax(300px,1fr)_minmax(0,2fr)] lg:gap-4 lg:p-4">
      <div role="tablist" aria-label={t.app.tabs} className="grid shrink-0 grid-cols-2 gap-1 rounded-lg border bg-surface p-1 lg:hidden">
        {(
          [
            ["chat", t.app.chatTab],
            ["agent", t.app.agentTab(filled, total)],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            role="tab"
            aria-selected={pane === id}
            onClick={() => setPane(id)}
            className={cn(
              "rounded-md py-1.5 text-[14px] font-medium tabular-nums text-muted-foreground",
              pane === id && "bg-card text-foreground shadow-[inset_0_0_0_1px_var(--border)]",
            )}
          >
            {label}
          </button>
        ))}
      </div>
      <div className={cn("min-h-0 flex-1 flex-col lg:flex", pane === "chat" ? "flex" : "hidden")}>
        <Chat />
      </div>
      <div className={cn("min-h-0 flex-1 flex-col lg:flex", pane === "agent" ? "flex" : "hidden")}>
        <Canvas />
      </div>
    </main>
  );
}
