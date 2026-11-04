/// <reference types="node" />
/**
 * card draws a page's link preview, the 1200×630 image that og:image points
 * at. The home page shows a deploy; docs and changelog pages show their title
 * above a row of sheds. The mark comes from public/favicon.svg.
 */
import { Resvg } from "@resvg/resvg-js";
import { readFileSync } from "node:fs";
import type { ReactNode } from "react";
import satori from "satori";

/** Card is what a page's preview says. Without one, it's the home card. */
export type Card = { kick: string; title: string };

const width = 1200;
const height = 630;

const file = (p: string) => readFileSync(new URL(p, import.meta.url));
const fonts = [
  { name: "Runde", data: file("./fonts/OpenRunde-Semibold.ttf"), weight: 600 as const },
  { name: "Runde", data: file("./fonts/OpenRunde-Bold.ttf"), weight: 700 as const },
];

// satori and resvg don't read oklch, so these are the dashboard's accent
// colors (hue 148) in hex.
const green = "#008935";
const ink = "#111e13";

const mark = file("../../public/favicon.svg").toString();
// The mark on a white tile, for the green cards' header.
const swaps: Record<string, string> = {
  "#006523": "#b5d4b9",
  "#46b55f": "#ffffff",
  "#0e9a3f": "#fbfefc",
  "#a9d4ae": "#005f1d",
  "#f7fef7": "#008a2f",
  "#035e23": "#def1e0",
};
const whiteMark = mark.replace(/#[0-9a-f]{6}/g, (c) => swaps[c] ?? c);
// The shed alone, cut from the mark, with its slats showing the card's green.
const glyph = mark.match(/<g id="glyph"[\s\S]*<\/g>/)![0].replace(/#035e23/g, green);
const shed = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="100 97 312 290">${glyph}</svg>`;

const src = (svg: string) => `data:image/svg+xml;base64,${Buffer.from(svg).toString("base64")}`;
const check = src(
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 30 30"><circle cx="15" cy="15" r="15" fill="#0e9a3f"/><path d="M9 15.5l4 4 8-9" fill="none" stroke="#fff" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
);

function Header() {
  return (
    <div
      style={{
        position: "absolute",
        left: 72,
        top: 60,
        display: "flex",
        alignItems: "center",
        gap: 14,
      }}
    >
      <img src={src(whiteMark)} width={48} height={48} />
      <div style={{ fontSize: 34, fontWeight: 700 }}>shed</div>
    </div>
  );
}

function Frame({ children }: { children: ReactNode }) {
  return (
    <div
      style={{
        width,
        height,
        display: "flex",
        position: "relative",
        background: green,
        color: "#fff",
        fontFamily: "Runde",
      }}
    >
      <Header />
      {children}
    </div>
  );
}

// The home card: the pitch beside a deploy that went live.
function Home() {
  const steps = ["Built in 41s", "Health check passed", "HTTPS ready"];
  return (
    <Frame>
      <div
        style={{
          position: "absolute",
          left: 72,
          top: 210,
          width: 540,
          fontSize: 64,
          fontWeight: 700,
          lineHeight: 1.04,
          letterSpacing: -1.6,
        }}
      >
        Push-to-deploy on the server you already have.
      </div>
      <div
        style={{
          position: "absolute",
          left: 650,
          top: 124,
          width: 478,
          display: "flex",
          flexDirection: "column",
          paddingBottom: 8,
          background: "#fff",
          borderRadius: 30,
          boxShadow: "0 10px 0 #b5d4b9",
          color: ink,
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            padding: "28px 32px",
            borderBottom: "2px solid #e3efe4",
          }}
        >
          <div style={{ fontSize: 38, fontWeight: 700 }}>api</div>
          <div
            style={{
              fontSize: 28,
              fontWeight: 700,
              color: "#fff",
              background: "#0e9a3f",
              borderRadius: 12,
              padding: "6px 14px",
              boxShadow: "0 3px 0 #006523",
            }}
          >
            Live
          </div>
        </div>
        {steps.map((s) => (
          <div
            key={s}
            style={{ display: "flex", alignItems: "center", gap: 16, padding: "22px 32px" }}
          >
            <img src={check} width={40} height={40} />
            <div style={{ fontSize: 32, fontWeight: 600 }}>{s}</div>
          </div>
        ))}
      </div>
    </Frame>
  );
}

// A street of sheds along the bottom; the solid one is yours.
const street: [left: number, size: number, yours?: true][] = [
  [60, 120],
  [210, 150],
  [400, 110],
  [540, 190, true],
  [770, 130],
  [930, 160],
  [1110, 120],
];

// A page card: its section and title above the street.
function Page({ kick, title }: Card) {
  return (
    <Frame>
      <div
        style={{
          position: "absolute",
          left: 72,
          top: 150,
          fontSize: 28,
          fontWeight: 600,
          opacity: 0.78,
        }}
      >
        {kick}
      </div>
      <div
        style={{
          position: "absolute",
          left: 72,
          top: 196,
          width: 1056,
          fontSize: title.length > 26 ? 60 : 72,
          fontWeight: 700,
          lineHeight: 1.04,
          letterSpacing: -1.8,
        }}
      >
        {title}
      </div>
      {street.map(([left, size, yours]) => (
        <img
          key={left}
          src={src(shed)}
          width={size}
          height={Math.round((size * 290) / 312)}
          style={{ position: "absolute", left, bottom: 58, opacity: yours ? 1 : 0.22 }}
        />
      ))}
      <div
        style={{
          position: "absolute",
          left: 0,
          bottom: 40,
          width,
          height: 18,
          background: "#006f27",
        }}
      />
      <div
        style={{
          position: "absolute",
          left: 0,
          bottom: 0,
          width,
          height: 40,
          background: "#00792b",
        }}
      />
    </Frame>
  );
}

/** render draws card, or the home card when it's null, as a PNG. */
export async function render(card: Card | null): Promise<Buffer> {
  const svg = await satori(card ? <Page {...card} /> : <Home />, { width, height, fonts });
  return new Resvg(svg, { fitTo: { mode: "width", value: width } }).render().asPng();
}
