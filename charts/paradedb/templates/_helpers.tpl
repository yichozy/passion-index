{{/*
Expand the name of the chart.
*/}}
{{- define "paradedb.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name. We truncate at 63 chars because
some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "paradedb.fullname" -}}
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
chart name + version, used in labels.
*/}}
{{- define "paradedb.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "paradedb.labels" -}}
helm.sh/chart: {{ include "paradedb.chart" . }}
{{ include "paradedb.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: database
{{- end }}

{{/*
Selector labels — shared across StatefulSet/Service/Pod templates.
*/}}
{{- define "paradedb.selectorLabels" -}}
app.kubernetes.io/name: {{ include "paradedb.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Secret name to reference for DB credentials. When existingSecret is set,
use that; otherwise fall back to secret.name (fixed default).
*/}}
{{- define "paradedb.secretName" -}}
{{- if .Values.secret.existingSecret }}
{{- .Values.secret.existingSecret }}
{{- else }}
{{- .Values.secret.name | default "paradedb-pg" }}
{{- end }}
{{- end }}

{{/*
DB user. Empty falls back to "postgres" to match the postgres image's
entrypoint default (${POSTGRES_USER:-postgres}).
*/}}
{{- define "paradedb.user" -}}
{{- .Values.database.user | default "postgres" }}
{{- end }}

{{/*
DB name. Empty falls back to the resolved user — mirrors the postgres
image which defaults POSTGRES_DB to whatever POSTGRES_USER became.
*/}}
{{- define "paradedb.database" -}}
{{- .Values.database.name | default (include "paradedb.user" .) }}
{{- end }}

{{/*
Resolve the DB password. Priority:
  1. database.password from values (operator-supplied)
  2. password already stored in the live Secret (preserved across upgrades)
  3. freshly generated 32-char random string (first install)

The `lookup` call returns empty during `helm template` (offline render), so
the rendered manifest will show a new random value. This is fine — on real
install/upgrade against a cluster, lookup hits the live Secret and the
existing password is preserved. Once installed, prefer pinning
database.password to make renders deterministic.
*/}}
{{- define "paradedb.password" -}}
{{- if .Values.database.password }}
{{- .Values.database.password }}
{{- else }}
{{- $existing := lookup "v1" "Secret" .Release.Namespace (include "paradedb.secretName" .) }}
{{- if $existing }}
{{- if index $existing.data "password" }}
{{- index $existing.data "password" | b64dec }}
{{- else }}
{{- randAlphaNum 32 }}
{{- end }}
{{- else }}
{{- randAlphaNum 32 }}
{{- end }}
{{- end }}
{{- end }}
