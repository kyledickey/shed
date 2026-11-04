import styles from "./Variables.module.css";

/** VarValue renders a value with its ${{ }} references set off in outlined chips. */
export function VarValue({ value }: { value: string }) {
  const parts = value.split(/(\$\{\{\s*[^}]+\s*\}\})/g);
  return (
    <>
      {parts.map((p, i) =>
        p.startsWith("${{") ? (
          <span key={i} className={styles.ref}>
            {p}
          </span>
        ) : (
          p
        ),
      )}
    </>
  );
}
