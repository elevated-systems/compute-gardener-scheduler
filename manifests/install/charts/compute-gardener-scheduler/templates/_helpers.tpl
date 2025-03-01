{{/* Define fullname */}}
{{- define "compute-gardener-scheduler.fullname" -}}
{{ .Values.scheduler.name }}
{{- end -}}


{{/* Define common labels */}}
{{- define "compute-gardener-scheduler.labels" -}}
app: {{ .Values.scheduler.name }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion }}
{{- end -}}

{{/* Define common selector labels */}}
{{- define "compute-gardener-scheduler.selectorLabels" -}}
app: {{ .Values.scheduler.name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}