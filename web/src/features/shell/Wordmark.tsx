import styles from "./Wordmark.module.css";

export function Wordmark() {
  return (
    <span className={styles.wordmark}>
      <svg width="18" height="18" viewBox="0 0 32 32" aria-hidden>
        <rect width="32" height="32" rx="7" fill="currentColor" />
        <path
          d="M8 15.5 16 9l8 6.5V23H8z"
          fill="none"
          stroke="var(--bg)"
          strokeWidth="2.5"
          strokeLinejoin="round"
        />
      </svg>
      shed
    </span>
  );
}
