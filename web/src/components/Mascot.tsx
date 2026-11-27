// The Charter boat: soft pixels on a 12x12 grid, sea palette.
const PALETTE: Record<string, string> = {
  f: "#d7e8f6", // sail
  r: "#cf6f6a", // flag
  k: "#1b2a3a", // mast
  d: "#2f5f8f", // hull
  s: "#5b9bd5", // water
  w: "#f4f8fc", // portholes
};

const BOAT = [
  "......f.....",
  ".....ff.....",
  "....fff.....",
  "...ffff.r...",
  "..fffff.rr..",
  ".ffffff.rrr.",
  "......k.....",
  "ddddddddddd.",
  ".dsswsswssd.",
  "..dddddddd..",
  "s..s..s..s..",
  ".ss.ss.ss.ss",
];

export function Boat({ size = 20, bob = false }: { size?: number; bob?: boolean }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 12 12"
      aria-hidden
      style={bob ? { animation: "bob 3.2s ease-in-out infinite" } : undefined}
    >
      {BOAT.flatMap((row, y) =>
        [...row].map((c, x) =>
          PALETTE[c] ? <rect key={`${x}-${y}`} x={x} y={y} width={1} height={1} rx={0.28} fill={PALETTE[c]} /> : null,
        ),
      )}
    </svg>
  );
}
