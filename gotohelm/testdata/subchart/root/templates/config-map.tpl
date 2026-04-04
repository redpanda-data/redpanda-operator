apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Chart.Name }}-{{ .Release.Name }}
data:
  values: |
    {{- toYaml .Values | nindent 4 }}
