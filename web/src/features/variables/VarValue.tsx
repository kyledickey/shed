import styles from "./Variables.module.css";

/** VarValue renders a value with ${{ }} references as crayon chips. */
export function VarValue({ value }: { value: string }) {
  const parts = value.split(/(\$\{\{\s*[^}]+\s*\}\})/g);
  return (
    <>
      {parts.map((p, i) =>
        p.startsWith("${{") ? (
          <span key={i} className={styles.ref} data-tone={p.includes(".") ? "sky" : "grape"}>
            {p}
          </span>
        ) : (
          p
        ),
      )}
    </>
  );
}
