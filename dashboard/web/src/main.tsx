import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import App from "./App";

// React 18 createRoot entry point. Wrapped in StrictMode for dev-time double-render
// detection — important for surfaces like SSE EventSource cleanup (plan 04-16) so
// we catch listener-leak shapes in development.
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
