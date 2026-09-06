/** Local-dev base URL for labor-performance's own REST API. Mirrors
 *  e2e-tests/env.sh's port-offset convention (each service's own :8080
 *  default, offset by index -- FACILITY=8081, INVENTORY=8082, WES=8083,
 *  FULFILLMENT=8084, WORKFORCE=8085, ORDER=8086, PROCESS_PATH=8087,
 *  LABOR=8088). */
export const LABOR_API_BASE = "http://localhost:8088";
