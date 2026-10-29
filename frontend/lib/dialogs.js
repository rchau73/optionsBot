// The trading dialogs open on this page (#446): the NPS card is never shown
// while a manual close or a rebalance dialog is open (surveys.md §1). Each of
// those dialogs counts itself in while mounted (useBlockingDialog).
import { useEffect } from "react";

let open = 0;

/** True while a manual close or rebalance dialog is open. */
export function blockingDialogOpen() {
  return open > 0;
}

/** Counts the calling dialog as open while it is mounted. */
export function useBlockingDialog() {
  useEffect(() => {
    open += 1;
    return () => {
      open -= 1;
    };
  }, []);
}
