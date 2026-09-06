import { describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse, server } from "../test/mocks/server";
import { LABOR_API_BASE } from "../config";
import { LaborPerformanceScreen } from "./LaborPerformanceScreen";

describe("LaborPerformanceScreen", () => {
  it("defines a standard and shows a success message", async () => {
    server.use(
      http.post(`${LABOR_API_BASE}/standards`, async ({ request }) => {
        const body = (await request.json()) as { taskType: string; expectedSeconds: number };
        return HttpResponse.json(
          {
            taskType: body.taskType,
            expectedSeconds: body.expectedSeconds,
            effectiveFrom: "2026-09-06T00:00:00Z",
          },
          { status: 201 },
        );
      }),
      http.get(`${LABOR_API_BASE}/task-types/PICK/performance`, () =>
        HttpResponse.json({
          taskType: "PICK",
          taskCount: 0,
          meanEfficiencyPct: null,
          meanActualSeconds: null,
        }),
      ),
    );

    render(<LaborPerformanceScreen />);
    await userEvent.type(screen.getByLabelText("Expected seconds *"), "45");
    await userEvent.click(screen.getByRole("button", { name: "Set standard" }));

    await waitFor(() =>
      expect(screen.getByText("Standard for PICK set to 45s.")).toBeInTheDocument(),
    );
  });

  it("shows the RFC 7807 problem detail when defining a standard fails", async () => {
    server.use(
      http.post(`${LABOR_API_BASE}/standards`, () =>
        HttpResponse.json(
          {
            type: "https://errors.labor-performance.warehouse-systems.dev/invalid-expected-seconds",
            title: "Expected seconds must be greater than zero",
            status: 422,
            detail: "expected seconds must be greater than zero",
          },
          { status: 422 },
        ),
      ),
      http.get(`${LABOR_API_BASE}/task-types/PICK/performance`, () =>
        HttpResponse.json({
          taskType: "PICK",
          taskCount: 0,
          meanEfficiencyPct: null,
          meanActualSeconds: null,
        }),
      ),
    );

    render(<LaborPerformanceScreen />);
    await userEvent.type(screen.getByLabelText("Expected seconds *"), "0");
    await userEvent.click(screen.getByRole("button", { name: "Set standard" }));

    expect(
      await screen.findByText("expected seconds must be greater than zero"),
    ).toBeInTheDocument();
  });

  it("looks up an associate's scorecard and shows its breakdown", async () => {
    server.use(
      http.get(`${LABOR_API_BASE}/associates/assoc-42/scorecard`, () =>
        HttpResponse.json({
          associateId: "assoc-42",
          taskCount: 12,
          meanEfficiencyPct: 91.5,
          byTaskType: {
            PICK: { taskCount: 10, meanEfficiencyPct: 92.0 },
            PACK: { taskCount: 2, meanEfficiencyPct: 88.0 },
          },
          trend: "IMPROVING",
          coachingFlag: false,
        }),
      ),
      http.get(`${LABOR_API_BASE}/task-types/PICK/performance`, () =>
        HttpResponse.json({
          taskType: "PICK",
          taskCount: 0,
          meanEfficiencyPct: null,
          meanActualSeconds: null,
        }),
      ),
    );

    render(<LaborPerformanceScreen />);
    await userEvent.type(screen.getByLabelText("Associate ID *"), "assoc-42");
    await userEvent.click(screen.getByRole("button", { name: "Look up" }));

    expect(await screen.findByText("12")).toBeInTheDocument();
    expect(screen.getByText("91.5%")).toBeInTheDocument();
    expect(screen.getByText("IMPROVING")).toBeInTheDocument();
  });

  it("shows a not-found message for an associate with no recorded tasks", async () => {
    server.use(
      http.get(`${LABOR_API_BASE}/associates/assoc-unknown/scorecard`, () =>
        HttpResponse.json(
          {
            type: "https://errors.labor-performance.warehouse-systems.dev/associate-not-found",
            title: "No task performance recorded for this associate",
            status: 404,
            detail: "no task performance recorded for this associate",
          },
          { status: 404 },
        ),
      ),
      http.get(`${LABOR_API_BASE}/task-types/PICK/performance`, () =>
        HttpResponse.json({
          taskType: "PICK",
          taskCount: 0,
          meanEfficiencyPct: null,
          meanActualSeconds: null,
        }),
      ),
    );

    render(<LaborPerformanceScreen />);
    await userEvent.type(screen.getByLabelText("Associate ID *"), "assoc-unknown");
    await userEvent.click(screen.getByRole("button", { name: "Look up" }));

    expect(
      await screen.findByText("No task performance recorded for assoc-unknown."),
    ).toBeInTheDocument();
  });

  it("shows fleet-wide task-type performance for the selected task type", async () => {
    server.use(
      http.get(`${LABOR_API_BASE}/task-types/PICK/performance`, () =>
        HttpResponse.json({
          taskType: "PICK",
          taskCount: 340,
          meanEfficiencyPct: 96.2,
          meanActualSeconds: 42.8,
        }),
      ),
    );

    render(<LaborPerformanceScreen />);

    expect(await screen.findByText("340")).toBeInTheDocument();
    expect(screen.getByText("96.2%")).toBeInTheDocument();
    expect(screen.getByText("42.8s")).toBeInTheDocument();
  });
});
