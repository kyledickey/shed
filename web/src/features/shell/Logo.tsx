import styles from "./Logo.module.css";

const roof = "M120 206 392 140";
const body = "M148 214 364 162V378H148Z";

/**
 * Logo is the shed mark: a lean-to shed with a slatted door on a raised
 * accent tile, shaded like a primary button. public/favicon.svg is the same
 * drawing in fixed colors.
 */
export function Logo({ size = 28 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 512 512" className={styles.logo} aria-label="shed">
      <clipPath id="shed-logo-face">
        <rect width="512" height="486" rx="163" />
      </clipPath>
      <rect width="512" height="512" rx="163" className={styles.edge} />
      <rect width="512" height="486" rx="163" className={styles.sheen} />
      <rect
        y="20"
        width="512"
        height="486"
        rx="163"
        className={styles.face}
        clipPath="url(#shed-logo-face)"
      />
      <g transform="translate(0 -13)">
        <g transform="translate(0 14)" className={styles.glyphEdge}>
          <path d={roof} className={styles.roof} />
          <path d={body} className={styles.body} />
        </g>
        <g className={styles.glyph}>
          <path d={roof} className={styles.roof} />
          <path d={body} className={styles.body} />
        </g>
        <g className={styles.slat}>
          <rect x="184" y="248" width="144" height="26" rx="13" />
          <rect x="184" y="296" width="144" height="26" rx="13" />
          <rect x="184" y="344" width="144" height="26" rx="13" />
        </g>
      </g>
    </svg>
  );
}
