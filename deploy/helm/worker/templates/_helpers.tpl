{{- define "relentless-worker.name" -}}
relentless-worker
{{- end -}}

{{- define "relentless-worker.fullname" -}}
{{ include "relentless-worker.name" . }}
{{- end -}}
