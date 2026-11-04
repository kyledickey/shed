import styles from "./Logo.module.css";

/** Logo is the shed mark: a gable shed sticker on an accent tile. */
export function Logo({ size = 28 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" className={styles.logo} aria-label="shed">
      <rect width="64" height="64" rx="16" className={styles.logoTile} />
      <g transform="matrix(1.08 0 0 1.08 -4.06 -4.06)">
        <path d="M19 34 35 20 51 34V51H19Z" className={styles.logoInk} />
        <path d="M16 31 32 17 48 31V48H16Z" className={styles.logoInk} />
        <path d="M16 31 32 17 48 31V48H16Z" className={styles.logoShed} />
        <path d="M27 36h10v12H27z" className={styles.logoDoor} />
      </g>
    </svg>
  );
}
