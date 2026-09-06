import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@warehouse/ui-kit/tokens.css";
import App from "./App";

/** Standalone dev entry -- lets labor-mfe run and be worked on in
 *  isolation (npm run dev, :5187) without the shell attached. The shell
 *  imports App.tsx directly via Module Federation and provides its own
 *  chrome; this file is dev-only scaffolding. No BrowserRouter here since
 *  this remote (like process-path-mfe) has no internal sub-routes -- it
 *  is a single screen. */
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <div style={{ padding: "var(--wh-space-5)" }}>
      <App />
    </div>
  </StrictMode>,
);
