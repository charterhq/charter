import { AuiIf, ComposerPrimitive, MessagePrimitive, ThreadPrimitive } from "@assistant-ui/react";
import { ArrowUp } from "lucide-react";
import { Boat } from "./Mascot";

export function Chat() {
  return (
    <section className="flex min-h-0 flex-col rounded-xl border bg-card shadow-panel">
      <div className="flex items-center gap-2 border-b px-4 py-2.5 text-xs font-medium text-faint">
        <span className="size-[7px] rounded-full bg-success shadow-[0_0_0_3px_color-mix(in_oklab,var(--success)_20%,transparent)]" />
        Билдер · новый агент
      </div>
      <ThreadPrimitive.Root className="flex min-h-0 flex-1 flex-col">
        <ThreadPrimitive.Viewport className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4">
          <AuiIf condition={(s) => s.thread.isEmpty}>
            <Bot>
              Привет! Расскажи своими словами, что должен делать агент. Можно кидать ссылки, примеры и картинки – я
              разложу все по шагам и задам вопросы по одному.
            </Bot>
          </AuiIf>
          <ThreadPrimitive.Messages>
            {({ message }) =>
              message.role === "user" ? (
                <MessagePrimitive.Root className="max-w-[92%] self-end rounded-xl rounded-br-sm border bg-surface px-3 py-2 text-[13.5px]">
                  <MessagePrimitive.Parts />
                </MessagePrimitive.Root>
              ) : (
                <MessagePrimitive.Root>
                  <Bot>
                    <MessagePrimitive.Parts />
                  </Bot>
                </MessagePrimitive.Root>
              )
            }
          </ThreadPrimitive.Messages>
        </ThreadPrimitive.Viewport>
        <ComposerPrimitive.Root className="m-3 flex items-end gap-2 rounded-xl border bg-surface p-1.5 pl-3 focus-within:border-primary">
          <ComposerPrimitive.Input
            rows={1}
            placeholder="Напиши, что хочешь…"
            className="max-h-40 min-h-8 flex-1 resize-none bg-transparent py-1.5 text-[13.5px] outline-none placeholder:text-faint"
          />
          <ComposerPrimitive.Send className="grid size-8 place-items-center rounded-lg bg-primary text-primary-foreground hover:bg-primary-hover disabled:opacity-40">
            <ArrowUp size={15} />
          </ComposerPrimitive.Send>
        </ComposerPrimitive.Root>
      </ThreadPrimitive.Root>
    </section>
  );
}

function Bot({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex items-start gap-2.5">
      <span className="grid size-7 shrink-0 place-items-center rounded-lg border bg-surface">
        <Boat size={18} />
      </span>
      <div className="pt-0.5 text-[13.5px]">
        <div className="mb-0.5 text-[11px] font-semibold text-sky">Билдер</div>
        {children}
      </div>
    </div>
  );
}
