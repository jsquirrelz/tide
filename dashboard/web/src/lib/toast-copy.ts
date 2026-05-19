// Locked toast copy strings from UI-SPEC §Copywriting Contract (verbatim).
// Emitters import these constants — never inline the strings — so the gsd-ui-checker
// can grep a single file to verify the contract.

export const TOAST_COPY = {
  clipboardCopySuccess: {
    title: "Command copied",
    body: (command: string) =>
      `Paste in your terminal to run: ${command}` as const,
  },
  clipboardCopyFailure: {
    title: "Couldn't copy",
    body: (command: string) =>
      `Clipboard API blocked. Command: ${command}` as const,
    duration: 8000,
  },
  sseReconnecting: {
    title: "Reconnecting…",
    body: "Stream lost — retrying with exponential backoff.",
  },
  sseReconnected: {
    title: "Reconnected",
    body: "Live updates resumed.",
  },
  sseDisconnectPersistent: {
    title: "Backend unreachable",
    body: "Unable to reach the dashboard backend. Try: kubectl logs deploy/tide-dashboard",
    sticky: true,
  },
} as const;
