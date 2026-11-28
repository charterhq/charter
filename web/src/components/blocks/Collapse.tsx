import { animate, useMotionValue, useMotionValueEvent, useReducedMotionConfig, type AnimationPlaybackControls } from "motion/react";
import { createContext, useContext, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { cn } from "@/lib/utils";

// Collapse follows the height of its content: a row that comes in, a panel that opens or closes moves
// the rest down smoothly instead of jumping. At rest the height is auto; a change runs a spring from the
// old height to the new one, then the height goes back to auto. A spring and not a CSS transition: when
// the reader opens and closes fast, the spring turns around with the speed it has, a transition would
// start over with a jolt and a long tail. Code runs only while something moves; Reduce motion skips it.
//
// Collapses nest: a step opens inside a list that grows. While an inner one moves, the outer ones stay
// on auto and move with it in the same frame, instead of chasing it with a spring of their own.

const spring = { type: "spring", visualDuration: 0.3, bounce: 0 } as const;

type Hold = (on: boolean) => void;
const Parent = createContext<Hold | null>(null);

export function Collapse({ open = true, id, className, children }: { open?: boolean; id?: string; className?: string; children: ReactNode }) {
  const hold = useContext(Parent);
  const reduce = useReducedMotionConfig();
  const outer = useRef<HTMLDivElement>(null);
  const inner = useRef<HTMLDivElement>(null);
  const openRef = useRef(open);
  const held = useRef(0); // inner collapses moving right now
  const run = useRef<AnimationPlaybackControls | null>(null);
  const last = useRef(0);
  const [initial] = useState(open ? undefined : 0);
  const height = useMotionValue<number | string>(open ? "auto" : 0);
  useMotionValueEvent(height, "change", (v) => {
    if (outer.current) outer.current.style.height = typeof v === "number" ? `${v}px` : v;
  });

  const own = useMemo<Hold>(
    () => (on) => {
      held.current += on ? 1 : -1;
      hold?.(on);
    },
    [hold],
  );

  const settle = () => {
    if (run.current) {
      run.current = null;
      hold?.(false);
    }
    height.set(openRef.current ? "auto" : 0);
  };

  // move starts from what is on screen; a spring already running keeps its speed and turns around.
  const move = (to: number, from?: number) => {
    const el = outer.current;
    if (!el) return;
    if (!run.current) {
      const now = from ?? el.getBoundingClientRect().height;
      if (Math.abs(now - to) < 0.5 || reduce) return settle();
      height.jump(now);
      hold?.(true);
    }
    run.current = animate(height, to, { ...spring, onComplete: settle });
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
      if (held.current > 0 && !run.current) return;
      move(h, prev);
    });
    ro.observe(el);
    return () => {
      ro.disconnect();
      if (run.current) {
        run.current.stop();
        hold?.(false);
      }
    };
  }, []); // move and settle read refs only

  useLayoutEffect(() => {
    if (openRef.current === open) return;
    openRef.current = open;
    move(open ? (inner.current?.offsetHeight ?? 0) : 0);
  }, [open]);

  return (
    <Parent.Provider value={own}>
      <div ref={outer} id={id} inert={!open} aria-hidden={!open || undefined} style={{ height: initial }} className={cn("overflow-clip", open && "[overflow-clip-margin:4px]", className)}>
        <div ref={inner} className={cn("transition-opacity", open ? "duration-150" : "opacity-0 duration-75")}>
          {children}
        </div>
      </div>
    </Parent.Provider>
  );
}
