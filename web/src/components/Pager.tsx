import { Button } from "@cloudflare/kumo";
import { CaretLeftIcon, CaretRightIcon } from "@phosphor-icons/react";
import styles from "./Pager.module.css";

type PagerProps = {
  /** How many records the page on screen holds. */
  count: number;
  hasPrevious: boolean;
  hasNext: boolean;
  onPrevious: () => void;
  onNext: () => void;
  /** A page is loading; the buttons wait for it. */
  busy?: boolean;
};

/**
 * Pager is the footer of a record list paged by cursor: how many rows are
 * shown, and Previous and Next. Lists with a single page leave it out.
 */
export function Pager({ count, hasPrevious, hasNext, onPrevious, onNext, busy }: PagerProps) {
  return (
    <div className={styles.pager}>
      <span className={styles.count}>Showing {count}</span>
      <span className={styles.buttons}>
        <Button
          size="sm"
          variant="secondary"
          icon={<CaretLeftIcon size={12} />}
          disabled={!hasPrevious || busy}
          onClick={onPrevious}
        >
          Previous
        </Button>
        <Button size="sm" variant="secondary" disabled={!hasNext || busy} onClick={onNext}>
          Next
          <CaretRightIcon size={12} />
        </Button>
      </span>
    </div>
  );
}
