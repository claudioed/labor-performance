import { LaborPerformanceScreen } from "./screens/LaborPerformanceScreen";

/** Exposed as labor_mfe/App via Module Federation. Routed under /labor/*
 *  by the shell.
 *
 *  labor-performance has three distinct read/write surfaces (engineered
 *  standards, one associate's scorecard, fleet-wide task-type
 *  performance) rather than a single flat resource the way
 *  process-path-management is -- see LaborPerformanceScreen's own doc
 *  comment for why these stay one screen with three cards rather than
 *  three separate routes. */
export default function App() {
  return <LaborPerformanceScreen />;
}
