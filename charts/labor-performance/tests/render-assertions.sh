#!/usr/bin/env sh
set -eu

chart_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tmp_dir=$(mktemp -d)
trap 'rm -r "$tmp_dir"' EXIT

helm template labor-performance "$chart_dir" >"$tmp_dir/default.yaml"
if grep -q 'name: labor-performance-frontend' "$tmp_dir/default.yaml"; then
  echo "frontend resources rendered while frontend.enabled=false" >&2
  exit 1
fi

helm template labor-performance "$chart_dir" \
  --set frontend.enabled=true \
  --set frontend.image.tag=test >"$tmp_dir/enabled.yaml"

assert_component() {
  template=$1
  component=$2
  output=$3
  helm template labor-performance "$chart_dir" \
    --set frontend.enabled=true \
    --set frontend.image.tag=test \
    --show-only "$template" >"$output"
  grep -q "app.kubernetes.io/component: $component" "$output"
}

assert_component templates/deployment.yaml api "$tmp_dir/api-deployment.yaml"
assert_component templates/service.yaml api "$tmp_dir/api-service.yaml"
assert_component templates/frontend-deployment.yaml frontend "$tmp_dir/frontend-deployment.yaml"
assert_component templates/frontend-service.yaml frontend "$tmp_dir/frontend-service.yaml"

grep -q 'type: ClusterIP' "$tmp_dir/frontend-service.yaml"
if grep -Eq 'kind: (Ingress|HTTPRoute)|type: (NodePort|LoadBalancer)' "$tmp_dir/frontend"*.yaml; then
  echo "frontend render contains a forbidden public exposure resource" >&2
  exit 1
fi

api_selector=$(grep 'app.kubernetes.io/component:' "$tmp_dir/api-service.yaml" | tail -n 1)
frontend_selector=$(grep 'app.kubernetes.io/component:' "$tmp_dir/frontend-service.yaml" | tail -n 1)
if [ "$api_selector" = "$frontend_selector" ]; then
  echo "API and frontend Service selectors are not disjoint" >&2
  exit 1
fi

echo "chart render assertions passed"
