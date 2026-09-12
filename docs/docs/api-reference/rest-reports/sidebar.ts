import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

const sidebar: SidebarsConfig = {
  apisidebar: [
    {
      type: "doc",
      id: "api-reference/rest-reports/labor-performance-reports-api",
    },
    {
      type: "category",
      label: "reports",
      link: {
        type: "doc",
        id: "api-reference/rest-reports/reports",
      },
      items: [
        {
          type: "doc",
          id: "api-reference/rest-reports/get-labor-performance-report",
          label: "Labor Performance Report, keyed by TaskType and UTC hour",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/rest-reports/get-labor-performance-report-freshness",
          label: "How far the read model trails the event stream",
          className: "api-method get",
        },
      ],
    },
    {
      type: "category",
      label: "health",
      link: {
        type: "doc",
        id: "api-reference/rest-reports/health",
      },
      items: [
        {
          type: "doc",
          id: "api-reference/rest-reports/get-reports-healthz",
          label: "Liveness probe",
          className: "api-method get",
        },
      ],
    },
  ],
};

export default sidebar.apisidebar;
