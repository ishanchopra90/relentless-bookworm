{{- define "relentless-api.name" -}}
relentless-api
{{- end -}}

{{- define "relentless-api.fullname" -}}
{{ include "relentless-api.name" . }}
{{- end -}}

{{- define "relentless-api.serviceAccountName" -}}
{{- if .Values.serviceAccount.name -}}
{{ .Values.serviceAccount.name }}
{{- else -}}
{{ include "relentless-api.fullname" . }}
{{- end -}}
{{- end -}}
