// The Charter boat: soft pixels on a 12x12 grid, sea palette.
// The two portholes are its eyes. In the light theme the eyes go dark and the sail a shade deeper,
// in the dark theme the mast goes light: otherwise the face or the mast disappears into the background.
const PALETTE: Record<string, string> = {
  f: "fill-[#c6dcf1] dark:fill-[#d7e8f6]", // sail
  r: "fill-[#cf6f6a]", // flag
  k: "fill-[#1b2a3a] dark:fill-[#8aa4bf]", // mast
  d: "fill-[#2f5f8f]", // hull
  s: "fill-[#5b9bd5]", // water and the porthole band
  w: "fill-[#1b2a3a] dark:fill-[#f4f8fc]", // eyes
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

// Below 24px a cell is under two pixels: square cells stay crisp, rounded ones blur.
export function Boat({ size = 20, bob = false }: { size?: number; bob?: boolean }) {
  const rx = size < 24 ? 0 : 0.28;
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 12 12"
      aria-hidden
      shapeRendering={rx ? undefined : "crispEdges"}
      style={bob ? { animation: "bob 3.2s ease-in-out infinite" } : undefined}
    >
      {BOAT.flatMap((row, y) =>
        [...row].map((c, x) =>
          PALETTE[c] ? <rect key={`${x}-${y}`} x={x} y={y} width={1} height={1} rx={rx} className={PALETTE[c]} /> : null,
        ),
      )}
    </svg>
  );
}
