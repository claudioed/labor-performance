import { useState, type FormEvent } from "react";
import { Card, StatusPill, useFetch } from "@warehouse/ui-kit";
import { apiPost, ApiError } from "../api";
import { LABOR_API_BASE } from "../config";
import type { Scorecard, Standard, TaskType, TaskTypePerformance } from "../types";
import {
  FormRow,
  InlineError,
  InlineSuccess,
  SubmitButton,
  TextField,
} from "../components/formkit";

const TASK_TYPES: TaskType[] = ["PICK", "PACK", "SLAM"];

function formatPct(pct: number | null): string {
  return pct === null ? "—" : `${pct.toFixed(1)}%`;
}

function formatSeconds(seconds: number | null): string {
  return seconds === null ? "—" : `${seconds.toFixed(1)}s`;
}

/**
 * The single screen for this remote. labor-performance has three
 * distinct read/write surfaces rather than one flat resource
 * (process-path-management's shape): engineered standards (the only
 * write endpoint -- TaskPerformance itself is written exclusively by
 * the Kafka consumer, never over REST, see api.ts's own doc comment),
 * one associate's scorecard (lookup by id -- there is no "list all
 * associates" endpoint, so this is a search box, not a table), and
 * fleet-wide task-type performance (one of three closed enum values,
 * so a dropdown rather than free text). Kept as three Cards on one
 * screen rather than three routes since none of the three has enough
 * of its own surface to justify a sub-nav, mirroring process-path-
 * management's own single-screen precedent for a similarly-thin
 * bounded context.
 */
export function LaborPerformanceScreen() {
  // --- Define standard ---
  const [standardTaskType, setStandardTaskType] = useState<TaskType>("PICK");
  const [expectedSeconds, setExpectedSeconds] = useState("");
  const [defineError, setDefineError] = useState<string | null>(null);
  const [defineSuccess, setDefineSuccess] = useState<string | null>(null);
  const [defining, setDefining] = useState(false);

  async function onDefineStandard(e: FormEvent) {
    e.preventDefault();
    setDefineError(null);
    setDefineSuccess(null);
    setDefining(true);
    try {
      const standard = await apiPost<Standard>("/standards", {
        taskType: standardTaskType,
        expectedSeconds: Number(expectedSeconds),
      });
      setDefineSuccess(
        `Standard for ${standard.taskType} set to ${standard.expectedSeconds}s.`,
      );
      setExpectedSeconds("");
    } catch (err) {
      setDefineError(err instanceof ApiError ? err.message : "Failed to define standard.");
    } finally {
      setDefining(false);
    }
  }

  // --- Associate scorecard lookup ---
  const [associateIdInput, setAssociateIdInput] = useState("");
  const [queriedAssociateId, setQueriedAssociateId] = useState<string | null>(null);
  const {
    data: scorecard,
    loading: scorecardLoading,
    error: scorecardError,
  } = useFetch<Scorecard>(
    queriedAssociateId
      ? `${LABOR_API_BASE}/associates/${encodeURIComponent(queriedAssociateId)}/scorecard`
      : null,
  );

  function onLookupAssociate(e: FormEvent) {
    e.preventDefault();
    setQueriedAssociateId(associateIdInput.trim());
  }

  // --- Fleet-wide task-type performance ---
  const [performanceTaskType, setPerformanceTaskType] = useState<TaskType>("PICK");
  const { data: taskTypePerformance, loading: performanceLoading } =
    useFetch<TaskTypePerformance>(
      `${LABOR_API_BASE}/task-types/${performanceTaskType}/performance`,
    );

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "var(--wh-space-5)" }}>
      <div>
        <h1 style={{ fontSize: "var(--wh-font-size-2xl)", margin: 0 }}>Labor performance</h1>
        <p style={{ color: "var(--wh-color-text-muted)", marginTop: 4 }}>
          labor-performance · engineered labor standards and actual-vs-standard scoring
        </p>
      </div>

      <Card title="Define engineered standard">
        <form
          onSubmit={onDefineStandard}
          style={{ display: "flex", flexDirection: "column", gap: "var(--wh-space-3)" }}
        >
          <FormRow>
            <label style={{ display: "flex", flexDirection: "column", gap: 4 }}>
              <span
                style={{
                  fontSize: "var(--wh-font-size-xs)",
                  color: "var(--wh-color-text-muted)",
                  fontWeight: 600,
                }}
              >
                Task type
              </span>
              <select
                value={standardTaskType}
                onChange={(e) => setStandardTaskType(e.target.value as TaskType)}
                style={{
                  padding: "8px 10px",
                  borderRadius: "var(--wh-radius-md)",
                  border: "1px solid var(--wh-color-border)",
                  background: "var(--wh-color-bg-sunken)",
                  color: "var(--wh-color-text)",
                  fontSize: "var(--wh-font-size-sm)",
                }}
              >
                {TASK_TYPES.map((tt) => (
                  <option key={tt} value={tt}>
                    {tt}
                  </option>
                ))}
              </select>
            </label>
            <TextField
              label="Expected seconds"
              type="number"
              value={expectedSeconds}
              onChange={setExpectedSeconds}
              placeholder="45"
              required
            />
            <SubmitButton disabled={defining || !expectedSeconds.trim()}>
              {defining ? "Saving…" : "Set standard"}
            </SubmitButton>
          </FormRow>
          <InlineError message={defineError} />
          <InlineSuccess message={defineSuccess} />
        </form>
      </Card>

      <Card title="Associate scorecard">
        <form
          onSubmit={onLookupAssociate}
          style={{ display: "flex", flexDirection: "column", gap: "var(--wh-space-3)" }}
        >
          <FormRow>
            <TextField
              label="Associate ID"
              value={associateIdInput}
              onChange={setAssociateIdInput}
              placeholder="assoc-42"
              required
            />
            <SubmitButton disabled={!associateIdInput.trim()}>Look up</SubmitButton>
          </FormRow>
        </form>

        {scorecardLoading && (
          <p style={{ color: "var(--wh-color-text-muted)" }}>Loading…</p>
        )}
        {scorecardError && (
          <InlineError
            message={
              scorecardError.message.includes("404")
                ? `No task performance recorded for ${queriedAssociateId}.`
                : scorecardError.message
            }
          />
        )}
        {scorecard && !scorecardLoading && (
          <div style={{ display: "flex", flexDirection: "column", gap: "var(--wh-space-3)" }}>
            <div style={{ display: "flex", gap: "var(--wh-space-5)", flexWrap: "wrap" }}>
              <div>
                <div style={{ fontSize: "var(--wh-font-size-xs)", color: "var(--wh-color-text-muted)" }}>
                  Task count
                </div>
                <div style={{ fontSize: "var(--wh-font-size-lg)", fontWeight: 600 }}>
                  {scorecard.taskCount}
                </div>
              </div>
              <div>
                <div style={{ fontSize: "var(--wh-font-size-xs)", color: "var(--wh-color-text-muted)" }}>
                  Mean efficiency
                </div>
                <div style={{ fontSize: "var(--wh-font-size-lg)", fontWeight: 600 }}>
                  {formatPct(scorecard.meanEfficiencyPct)}
                </div>
              </div>
              <div>
                <div style={{ fontSize: "var(--wh-font-size-xs)", color: "var(--wh-color-text-muted)" }}>
                  Trend
                </div>
                <StatusPill status={scorecard.trend} size="sm" />
              </div>
              {scorecard.coachingFlag && (
                <div>
                  <div style={{ fontSize: "var(--wh-font-size-xs)", color: "var(--wh-color-text-muted)" }}>
                    Coaching
                  </div>
                  <StatusPill status="Coaching flagged" tone="warning" size="sm" />
                </div>
              )}
            </div>
            {Object.keys(scorecard.byTaskType).length > 0 && (
              <table style={{ width: "100%", borderCollapse: "collapse" }}>
                <thead>
                  <tr>
                    <th style={{ textAlign: "left", fontSize: "var(--wh-font-size-xs)", color: "var(--wh-color-text-faint)" }}>
                      Task type
                    </th>
                    <th style={{ textAlign: "left", fontSize: "var(--wh-font-size-xs)", color: "var(--wh-color-text-faint)" }}>
                      Task count
                    </th>
                    <th style={{ textAlign: "left", fontSize: "var(--wh-font-size-xs)", color: "var(--wh-color-text-faint)" }}>
                      Mean efficiency
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {Object.entries(scorecard.byTaskType).map(([taskType, breakdown]) => (
                    <tr key={taskType}>
                      <td style={{ padding: "var(--wh-space-2) 0" }}>{taskType}</td>
                      <td style={{ padding: "var(--wh-space-2) 0" }}>{breakdown.taskCount}</td>
                      <td style={{ padding: "var(--wh-space-2) 0" }}>
                        {formatPct(breakdown.meanEfficiencyPct)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        )}
      </Card>

      <Card title="Fleet-wide task-type performance">
        <div style={{ display: "flex", flexDirection: "column", gap: "var(--wh-space-3)" }}>
          <label style={{ display: "flex", flexDirection: "column", gap: 4, maxWidth: 200 }}>
            <span
              style={{
                fontSize: "var(--wh-font-size-xs)",
                color: "var(--wh-color-text-muted)",
                fontWeight: 600,
              }}
            >
              Task type
            </span>
            <select
              value={performanceTaskType}
              onChange={(e) => setPerformanceTaskType(e.target.value as TaskType)}
              style={{
                padding: "8px 10px",
                borderRadius: "var(--wh-radius-md)",
                border: "1px solid var(--wh-color-border)",
                background: "var(--wh-color-bg-sunken)",
                color: "var(--wh-color-text)",
                fontSize: "var(--wh-font-size-sm)",
              }}
            >
              {TASK_TYPES.map((tt) => (
                <option key={tt} value={tt}>
                  {tt}
                </option>
              ))}
            </select>
          </label>
          {performanceLoading && (
            <p style={{ color: "var(--wh-color-text-muted)" }}>Loading…</p>
          )}
          {taskTypePerformance && !performanceLoading && (
            <div style={{ display: "flex", gap: "var(--wh-space-5)", flexWrap: "wrap" }}>
              <div>
                <div style={{ fontSize: "var(--wh-font-size-xs)", color: "var(--wh-color-text-muted)" }}>
                  Task count
                </div>
                <div style={{ fontSize: "var(--wh-font-size-lg)", fontWeight: 600 }}>
                  {taskTypePerformance.taskCount}
                </div>
              </div>
              <div>
                <div style={{ fontSize: "var(--wh-font-size-xs)", color: "var(--wh-color-text-muted)" }}>
                  Mean efficiency
                </div>
                <div style={{ fontSize: "var(--wh-font-size-lg)", fontWeight: 600 }}>
                  {formatPct(taskTypePerformance.meanEfficiencyPct)}
                </div>
              </div>
              <div>
                <div style={{ fontSize: "var(--wh-font-size-xs)", color: "var(--wh-color-text-muted)" }}>
                  Mean actual duration
                </div>
                <div style={{ fontSize: "var(--wh-font-size-lg)", fontWeight: 600 }}>
                  {formatSeconds(taskTypePerformance.meanActualSeconds)}
                </div>
              </div>
            </div>
          )}
        </div>
      </Card>
    </div>
  );
}
