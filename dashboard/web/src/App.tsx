import AppShell from "./components/AppShell";
import Header from "./components/Header";
import ConnectionStatusIndicator from "./components/ConnectionStatusIndicator";
import { ToastProvider } from "./components/ToastContainer";

// Top-level dashboard component.
//
// Plan 04-12 ships the chrome (AppShell + Header + ConnectionStatusIndicator + Toast).
// Plan 04-13 replaces the placeholder pane divs with PlanningDAGView (left) and
// ExecutionDAGView (right), and adds the resizable divider + TaskDetailDrawer.
// Plan 04-15 wires the ProjectPicker into the Header slot.
//
// The 50/50 grid here is the simplest split that lets the chrome render against
// realistic content; plan 04-13 swaps `grid-cols-2` for the flex+splitRatio shape.
export default function App() {
  return (
    <ToastProvider>
      <AppShell
        header={
          <Header connectionStatus={<ConnectionStatusIndicator state="connected" />} />
        }
      >
        <div className="grid h-full grid-cols-2 gap-2 p-4">
          <div className="flex h-full items-center justify-center rounded border border-[var(--color-border-subtle)] bg-[var(--color-surface-raised)] text-[var(--color-text-muted)]">
            Planning DAG placeholder (plan 04-13)
          </div>
          <div className="flex h-full items-center justify-center rounded border border-[var(--color-border-subtle)] bg-[var(--color-surface-raised)] text-[var(--color-text-muted)]">
            Execution DAG placeholder (plan 04-13)
          </div>
        </div>
      </AppShell>
    </ToastProvider>
  );
}
