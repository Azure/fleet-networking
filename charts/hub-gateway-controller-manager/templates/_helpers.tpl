{{- define "hub-gateway-controller-manager.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "hub-gateway-controller-manager.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "hub-gateway-controller-manager.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "hub-gateway-controller-manager.selectorLabels" -}}
app.kubernetes.io/name: {{ include "hub-gateway-controller-manager.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "hub-gateway-controller-manager.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | quote }}
{{ include "hub-gateway-controller-manager.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "hub-gateway-controller-manager.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "hub-gateway-controller-manager.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- required "serviceAccount.name is required when reusing an account" (.Values.serviceAccount.name | trim) -}}
{{- end -}}
{{- end -}}

{{- define "hub-gateway-controller-manager.clusterRoleName" -}}
{{- printf "%s-%s" .Release.Namespace (include "hub-gateway-controller-manager.fullname" .) -}}
{{- end -}}

{{- define "hub-gateway-controller-manager.validate" -}}
{{- $_ := required "image.repository must select an explicitly built/published image" (.Values.image.repository | trim) -}}
{{- $_ := required "image.tag must select an explicitly built/published image" (.Values.image.tag | trim) -}}
{{- if .Values.afd.enabled -}}
{{- $_ := required "afd.resourceGroup is required when AFD is enabled" (.Values.afd.resourceGroup | trim) -}}
{{- $_ := required "afd.cloudConfigSecret.name is required when AFD is enabled" (.Values.afd.cloudConfigSecret.name | trim) -}}
{{- $_ := required "afd.cloudConfigSecret.key is required when AFD is enabled" (.Values.afd.cloudConfigSecret.key | trim) -}}
{{- end -}}
{{- end -}}
