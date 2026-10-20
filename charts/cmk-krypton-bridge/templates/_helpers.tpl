{{/*
Full name of all resources: fullnameOverride, else `<release name>-cmk-krypton-bridge`, as in
chart 0.1.0, so that an upgrade keeps the resource names.
*/}}
{{- define "cmk-krypton-bridge.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-cmk-krypton-bridge" .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/*
Namespace for all resources. Defaults to the release namespace.
See https://github.com/helm/helm/issues/5358
*/}}
{{- define "cmk-krypton-bridge.namespace" -}}
{{- .Values.namespace | default .Release.Namespace }}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "cmk-krypton-bridge.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Version of the deployed application: the image tag if set, else the chart's appVersion.
*/}}
{{- define "cmk-krypton-bridge.version" -}}
{{- (.Values.image.tag | default .Chart.AppVersion) | toString | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "cmk-krypton-bridge.labels" -}}
helm.sh/chart: {{ include "cmk-krypton-bridge.chart" . }}
{{ include "cmk-krypton-bridge.selectorLabels" . }}
{{- with include "cmk-krypton-bridge.version" . }}
app.kubernetes.io/version: {{ . | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: openkcm
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Selector labels. Do not change them: a Deployment's selector cannot be changed on upgrade.
*/}}
{{- define "cmk-krypton-bridge.selectorLabels" -}}
app.kubernetes.io/name: cmk-krypton-bridge
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Name of the service account to use.
*/}}
{{- define "cmk-krypton-bridge.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "cmk-krypton-bridge.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Image reference: registry and repository, then the digest if set, else the tag. The tag
defaults to the chart's appVersion, the image version this chart release is made for.
Call it with (tuple .Values.image .Chart.AppVersion).
*/}}
{{- define "cmk-krypton-bridge.image" -}}
{{- $image := index . 0 -}}
{{- $appVersion := index . 1 -}}
{{- $repository := $image.repository -}}
{{- if $image.registry -}}{{- $repository = printf "%s/%s" $image.registry $image.repository -}}{{- end -}}
{{- if $image.digest -}}
{{- printf "%s@%s" $repository (toString $image.digest) -}}
{{- else -}}
{{- printf "%s:%s" $repository (toString ($image.tag | default $appVersion)) -}}
{{- end -}}
{{- end }}
