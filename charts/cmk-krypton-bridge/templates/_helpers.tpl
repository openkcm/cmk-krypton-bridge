{{- define "cmk-krypton-bridge.fullname" -}}
{{ .Release.Name }}-cmk-krypton-bridge
{{- end }}

{{- define "cmk-krypton-bridge.labels" -}}
app.kubernetes.io/name: cmk-krypton-bridge
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion }}
{{- end }}

{{- define "cmk-krypton-bridge.selectorLabels" -}}
app.kubernetes.io/name: cmk-krypton-bridge
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
