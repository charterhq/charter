import { createContext, useContext, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { cn } from "@/lib/utils";

// Collapse follows the height of its content with a CSS transition: a row that comes in, a panel that
// opens or closes moves the rest down smoothly instead of jumping. At rest the height is auto; only a
// change runs a transition from the old height to the new one, then the height goes back to auto.
// A ResizeObserver notices the change; nothing runs between changes, and Reduce motion makes it instant.
//
// Collapses nest: a step opens inside a list that grows. While an inner one moves, the outer ones stay
// on auto and move with it in the same frame, instead of chasing it with a transition of their own.

type Hold = (on: boolean) => void;
const Parent = createContext<Hold | null>(null);

export function Collapse({ open = true, id, className, children }: { open?: boolean; id?: string; className?: string; children: ReactNode }) {
  const hold = useContext(Parent);
  const outer = useRef<HTMLDivElement>(null);
  const inner = useRef<HTMLDivElement>(null);
  const openRef = useRef(open);
  const held = useRef(0); // inner collapses moving right now
  const moving = useRef(false);
  const last = useRef(0);
  const [initial] = useState(open ? undefined : 0);

  const own = useMemo<Hold>(
    () => (on) => {
      held.current += on ? 1 : -1;
      hold?.(on);
    },
    [hold],
  );

  const settle = () => {
    if (moving.current) {
      moving.current = false;
      hold?.(false);
    }
    const el = outer.current;
    if (el) el.style.height = openRef.current ? "auto" : "0px";
  };

  const move = (to: number) => {
    const el = outer.current;
    if (!el) return;
    const from = el.getBoundingClientRect().height;
    if (Math.abs(from - to) < 0.5) return settle();
    el.style.height = `${from}px`;
    void el.offsetHeight; // the start of the transition
    if (!moving.current) {
      moving.current = true;
      hold?.(true);
    }
    el.style.height = `${to}px`;
  };

  useLayoutEffect(() => {
    const el = inner.current;
    if (!el) return;
    last.current = el.offsetHeight;
    const ro = new ResizeObserver(() => {
      const h = el.offsetHeight;
      const prev = last.current;
      last.current = h;
      if (!openRef.current || h === prev) return;
      // An inner collapse is moving: on auto we already follow it frame by frame.
      if (held.current > 0 && !moving.current) return;
      if (outer.current) outer.current.style.height = `${prev}px`;
      move(h);
    });
    ro.observe(el);
    return () => {
      ro.disconnect();
      if (moving.current) hold?.(false);
    };
  }, []);

  useLayoutEffect(() => {
    if (openRef.current === open) return;
    openRef.current = open;
    move(open ? (inner.current?.offsetHeight ?? 0) : 0);
  }, [open]); // move reads refs only

  return (
    <Parent.Provider value={own}>
      <div
        ref={outer}
        id={id}
        inert={!open}
        style={{ height: initial }}
        onTransitionEnd={(e) => e.target === e.currentTarget && e.propertyName === "height" && settle()}
        className={cn("overflow-clip transition-[height] duration-[400ms] ease-[cubic-bezier(.33,1,.68,1)]", open && "[overflow-clip-margin:4px]", className)}
      >
        <div ref={inner}>{children}</div>
      </div>
    </Parent.Provider>
  );
}
