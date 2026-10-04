import { createContext, useContext, useMemo, useState, type ReactNode } from "react";

type RedeployHint = {
  pending: boolean;
  markPending: () => void;
  clear: () => void;
};

const Context = createContext<RedeployHint | null>(null);

/** Tracks whether saved settings are waiting for a redeploy. Key it by service id. */
export function RedeployHintProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState(false);
  const value = useMemo(
    () => ({ pending, markPending: () => setPending(true), clear: () => setPending(false) }),
    [pending],
  );
  return <Context.Provider value={value}>{children}</Context.Provider>;
}

export function useRedeployHint(): RedeployHint {
  const ctx = useContext(Context);
  if (!ctx) throw new Error("useRedeployHint must be used inside RedeployHintProvider");
  return ctx;
}
