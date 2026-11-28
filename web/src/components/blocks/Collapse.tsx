import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { cn } from "@/lib/utils";

// Collapse follows the height of its content with a CSS transition: a row that comes in, a panel that
// opens or closes moves the rest down smoothly instead of jumping. A ResizeObserver reads the content
// height when it changes; nothing runs between changes, and Reduce motion makes the move instant.
export function Collapse({ open = true, id, className, children }: { open?: boolean; id?: string; className?: string; children: ReactNode }) {
  const inner = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState<number | null>(null);
  useLayoutEffect(() => {
    const el = inner.current;
    if (!el) return;
    setHeight(el.offsetHeight);
    const ro = new ResizeObserver(() => setHeight(el.offsetHeight));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return (
    <div
      id={id}
      inert={!open}
      style={{ height: open ? (height ?? "auto") : 0 }}
      className={cn("overflow-clip transition-[height] duration-[400ms] ease-[cubic-bezier(.16,1,.3,1)]", open && "[overflow-clip-margin:4px]", className)}
    >
      <div ref={inner}>{children}</div>
    </div>
  );
}
