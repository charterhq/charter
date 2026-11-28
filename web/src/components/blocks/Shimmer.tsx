import { useEffect, useRef, useState, type ReactNode } from "react";
import { cn } from "@/lib/utils";

// Text with a light that runs over it while the work it names is going on. The light is a gradient
// clipped to the glyphs, moved by a CSS animation: one sweep, then a short rest. It stops off screen and
// under Reduce motion, where the text stays in its plain color.
export function Shimmer({ active = true, className, children }: { active?: boolean; className?: string; children: ReactNode }) {
  const el = useRef<HTMLSpanElement>(null);
  const [seen, setSeen] = useState(true);
  useEffect(() => {
    const node = el.current;
    if (!active || !node || typeof IntersectionObserver === "undefined") return;
    const io = new IntersectionObserver(([e]) => setSeen(e.isIntersecting), { rootMargin: "64px" });
    io.observe(node);
    return () => io.disconnect();
  }, [active]);
  return (
    <span ref={el} className={cn(active && "text-shimmer", active && !seen && "[animation-play-state:paused]", className)}>
      {children}
    </span>
  );
}
