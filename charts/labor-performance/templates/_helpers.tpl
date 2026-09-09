{{/*
Expand the name of the chart.
*/}}
{{- define "labor-performance.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "labor-performance.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart name and version as used by the chart label.
*/}}
{{- define "labor-performance.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "labor-performance.labels" -}}
helm.sh/chart: {{ include "labor-performance.chart" . }}
{{ include "labor-performance.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "labor-performance.selectorLabels" -}}
app.kubernetes.io/name: {{ include "labor-performance.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "labor-performance.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "labor-performance.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Name of the Secret holding DATABASE_URL, when the chart creates its own.
*/}}
{{- define "labor-performance.databaseSecretName" -}}
{{- if .Values.database.existingSecret }}
{{- .Values.database.existingSecret }}
{{- else }}
{{- include "labor-performance.fullname" . }}-database
{{- end }}
{{- end }}

{{/*
Name of the Secret holding API_READ_KEY / API_READWRITE_KEY (ADR 0011), when
the chart creates its own.
*/}}
{{- define "labor-performance.authSecretName" -}}
{{- if .Values.auth.existingSecret }}
{{- .Values.auth.existingSecret }}
{{- else }}
{{- include "labor-performance.fullname" . }}-auth
{{- end }}
{{- end }}

{{/*
Whether any REST auth Secret is wired at all (own keys or an existing Secret).
*/}}
{{- define "labor-performance.authSecretEnabled" -}}
{{- if or .Values.auth.readKey .Values.auth.readWriteKey .Values.auth.existingSecret }}true{{- end }}
{{- end }}

{{/*
The env entries every REST-serving container gets for the auth keys. Rendered
only when a Secret is wired; the keys are optional so a Secret carrying only
one of them still mounts cleanly.
*/}}
{{- define "labor-performance.authEnv" -}}
- name: AUTH_MODE
  value: {{ .Values.auth.mode | quote }}
{{- if include "labor-performance.authSecretEnabled" . }}
- name: API_READ_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "labor-performance.authSecretName" . }}
      key: API_READ_KEY
      optional: true
- name: API_READWRITE_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "labor-performance.authSecretName" . }}
      key: API_READWRITE_KEY
      optional: true
{{- end }}
{{- end }}

{{/*
Fully qualified name of the analytics projector deployment (ADR-0007).
*/}}
{{- define "labor-performance.projectorFullname" -}}
{{- include "labor-performance.fullname" . }}-projector
{{- end }}

{{/*
Fully qualified name of the analytics reports deployment/service (ADR-0007).
*/}}
{{- define "labor-performance.reportsFullname" -}}
{{- include "labor-performance.fullname" . }}-reports
{{- end }}

{{/*
Name of the Secret holding the analytics DSNs, when the chart creates its own.
*/}}
{{- define "labor-performance.analyticsSecretName" -}}
{{- if .Values.analytics.database.existingSecret }}
{{- .Values.analytics.database.existingSecret }}
{{- else }}
{{- include "labor-performance.fullname" . }}-analytics
{{- end }}
{{- end }}
