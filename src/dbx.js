/**
 * The one place that talks to the Go sidecar.
 *
 * The host bridge is absent when the page is opened directly in a browser, so
 * every call reports that instead of throwing: the calendar can then still
 * render with the data it computes locally.
 */
const bridge = typeof window !== "undefined" ? window.dbxPlugin : undefined;

export const bridgeAvailable = Boolean(bridge);

/** Host locale, or "en" outside a host. */
export function hostLocale() {
  return bridge?.locale ?? bridge?.context?.locale ?? "en";
}

export async function whenReady() {
  if (!bridge) return null;
  try {
    return (await bridge.ready) ?? null;
  } catch {
    return null;
  }
}

/**
 * Invokes a sidecar method. Resolves to an object carrying `ok:false` on
 * failure, so callers branch on data rather than on exceptions.
 */
export async function invoke(method, params = {}) {
  if (!bridge?.invoke) {
    return { ok: false, error: "The DBX host bridge is not available" };
  }
  try {
    return { ok: true, value: await bridge.invoke(method, params) };
  } catch (error) {
    return { ok: false, error: error?.message ?? String(error) };
  }
}
