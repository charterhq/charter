import { Command } from "cmdk";
import { useEffect } from "react";

export type Action = { id: string; label: string; hint?: string; run: () => void };

// Every action is one search away: the "three clicks" rule.
export function CommandPalette(props: { open: boolean; onOpenChange: (o: boolean) => void; actions: Action[] }) {
  const { open, onOpenChange } = props;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        onOpenChange(!open);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onOpenChange]);

  return (
    <Command.Dialog
      open={open}
      onOpenChange={onOpenChange}
      label="Команды"
      overlayClassName="fixed inset-0 bg-black/40 backdrop-blur-[2px]"
      contentClassName="fixed left-1/2 top-[18%] w-[min(560px,92vw)] -translate-x-1/2 overflow-hidden rounded-xl border bg-popover shadow-floating"
    >
      <Command.Input
        placeholder="Что сделать?"
        className="w-full border-b bg-transparent px-4 py-3 text-[15px] outline-none placeholder:text-faint"
      />
      <Command.List className="max-h-72 overflow-y-auto p-1.5">
        <Command.Empty className="px-3 py-6 text-center text-[15px] text-faint">Ничего не нашлось</Command.Empty>
        {props.actions.map((a) => (
          <Command.Item
            key={a.id}
            onSelect={() => {
              a.run();
              onOpenChange(false);
            }}
            className="flex cursor-pointer items-center justify-between rounded-md px-3 py-2 text-[15px] text-muted-foreground data-[selected=true]:bg-surface data-[selected=true]:text-foreground"
          >
            {a.label}
            {a.hint && <span className="text-[13px] text-faint">{a.hint}</span>}
          </Command.Item>
        ))}
      </Command.List>
    </Command.Dialog>
  );
}
