{{- define "wyrd.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "wyrd.fullname" -}}
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

{{- define "wyrd.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "wyrd.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "wyrd.selectorLabels" -}}
app.kubernetes.io/name: {{ include "wyrd.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "wyrd.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "wyrd.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "wyrd.image" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) }}
{{- end }}

{{/*
The Qdrant base URL: the bundled chart's service, or the one given.
*/}}
{{- define "wyrd.qdrantURL" -}}
{{- if .Values.qdrant.enabled }}
{{- $name := default "qdrant" .Values.qdrant.nameOverride }}
{{- if .Values.qdrant.fullnameOverride }}
{{- printf "http://%s:6333" .Values.qdrant.fullnameOverride }}
{{- else if contains $name .Release.Name }}
{{- printf "http://%s:6333" (.Release.Name | trunc 63 | trimSuffix "-") }}
{{- else }}
{{- printf "http://%s-%s:6333" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- else }}
{{- required "qdrantConnection.url is required when qdrant.enabled is false" .Values.qdrantConnection.url }}
{{- end }}
{{- end }}

{{/*
Values that cannot work together are refused at render time, where the
operator reads them, rather than as a pod that never becomes ready.
*/}}
{{- define "wyrd.validate" -}}
{{- $provider := .Values.embedder.provider }}
{{- if not (has $provider (list "vertex" "openai" "ollama" "noop")) }}
{{- fail (printf "embedder.provider %q is not one of vertex, openai, ollama, noop" $provider) }}
{{- end }}
{{- if and .Values.requireRemote (eq $provider "noop") }}
{{- fail "embedder.provider noop cannot be used with requireRemote: it produces no real embeddings" }}
{{- end }}
{{- if and (eq $provider "vertex") (not .Values.embedder.vertex.project) }}
{{- fail "embedder.vertex.project is required for the vertex provider" }}
{{- end }}
{{- if and (eq $provider "vertex") (not .Values.embedder.dim) }}
{{- fail "embedder.dim is required for the vertex provider" }}
{{- end }}
{{- if and (eq $provider "openai") (not .Values.embedder.apiKeySecret.name) }}
{{- fail "embedder.apiKeySecret.name is required for the openai provider" }}
{{- end }}
{{- if not .Values.qdrantConnection.collection }}
{{- fail "qdrantConnection.collection is required" }}
{{- end }}
{{- end }}

{{/*
The environment the daemon and the collection bootstrap share: where the
embedder and Qdrant are.
*/}}
{{- define "wyrd.env" -}}
- name: WYRD_EMBED_PROVIDER
  value: {{ .Values.embedder.provider | quote }}
{{- with .Values.embedder.model }}
- name: WYRD_EMBED_MODEL
  value: {{ . | quote }}
{{- end }}
{{- if .Values.embedder.dim }}
- name: WYRD_EMBED_DIM
  value: {{ .Values.embedder.dim | quote }}
{{- end }}
{{- with .Values.embedder.concurrency }}
- name: WYRD_EMBED_CONCURRENCY
  value: {{ . | quote }}
{{- end }}
{{- with .Values.embedder.endpoint }}
- name: WYRD_EMBED_ENDPOINT
  value: {{ . | quote }}
{{- end }}
{{- if eq .Values.embedder.provider "vertex" }}
- name: WYRD_VERTEX_PROJECT
  value: {{ .Values.embedder.vertex.project | quote }}
- name: WYRD_VERTEX_LOCATION
  value: {{ .Values.embedder.vertex.location | quote }}
{{- if .Values.embedder.vertex.credentialsSecret.name }}
- name: GOOGLE_APPLICATION_CREDENTIALS
  value: /var/run/secrets/wyrd/google/{{ .Values.embedder.vertex.credentialsSecret.key }}
{{- end }}
{{- end }}
{{- if .Values.embedder.apiKeySecret.name }}
- name: WYRD_EMBED_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.embedder.apiKeySecret.name }}
      key: {{ .Values.embedder.apiKeySecret.key }}
{{- end }}
- name: WYRD_QDRANT_URL
  value: {{ include "wyrd.qdrantURL" . | quote }}
- name: WYRD_QDRANT_COLLECTION
  value: {{ .Values.qdrantConnection.collection | quote }}
{{- if .Values.qdrantConnection.apiKeySecret.name }}
- name: WYRD_QDRANT_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.qdrantConnection.apiKeySecret.name }}
      key: {{ .Values.qdrantConnection.apiKeySecret.key }}
{{- end }}
{{- with .Values.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{- define "wyrd.credentialMounts" -}}
{{- if and (eq .Values.embedder.provider "vertex") .Values.embedder.vertex.credentialsSecret.name }}
- name: google-credentials
  mountPath: /var/run/secrets/wyrd/google
  readOnly: true
{{- end }}
{{- end }}
