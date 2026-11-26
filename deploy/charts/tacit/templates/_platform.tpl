{{- define "tacit.namespace" -}}
{{- printf "tacit-%s-%s" .environment .role -}}
{{- end -}}

{{- define "tacit.labels" -}}
{{- dict "app.kubernetes.io/name" "tacit" "tacit.dev/role" . | toJson -}}
{{- end -}}

{{- define "tacit.metadata" -}}
{{- dict "name" .name "namespace" .namespace "labels" (include "tacit.labels" .role | fromJson) | toJson -}}
{{- end -}}

{{- define "tacit.resources" -}}
{{- dict "requests" (dict "cpu" (printf "%dm" (int .requestCPU)) "memory" (printf "%dMi" (int .requestMemory)) "ephemeral-storage" (printf "%dMi" (int .scratch)))
         "limits"   (dict "cpu" (printf "%dm" (int .limitCPU)) "memory" (printf "%dMi" (int .limitMemory)) "ephemeral-storage" (printf "%dMi" (int .scratch))) | toJson -}}
{{- end -}}

{{- define "tacit.pod" -}}
{{- $workload := .workload -}}
{{- $env := list -}}
{{- range $name, $key := $workload.secretKeys -}}
  {{- $env = append $env (dict "name" $name "valueFrom" (dict "secretKeyRef" (dict "name" $.secret "key" $key))) -}}
{{- end -}}
{{- range $name, $value := .env -}}
  {{- $env = append $env (dict "name" $name "value" $value) -}}
{{- end -}}
{{- $container := dict "name" .role "image" .image "imagePullPolicy" "IfNotPresent" "env" $env
      "resources" (include "tacit.resources" $workload.resources | fromJson)
      "securityContext" (dict "allowPrivilegeEscalation" false "readOnlyRootFilesystem" true "capabilities" (dict "drop" (list "ALL"))) -}}
{{- $spec := dict "serviceAccountName" .role "automountServiceAccountToken" false
      "terminationGracePeriodSeconds" $workload.lifecycle.graceSeconds "containers" (list $container)
      "securityContext" (dict "runAsNonRoot" true "runAsUser" $workload.uid "runAsGroup" $workload.gid "fsGroup" $workload.gid
        "seccompProfile" (dict "type" .security.seccompProfile)) -}}
{{- if .args -}}
  {{- $_ := set $container "args" .args -}}
{{- end -}}
{{- if eq $workload.kind "job" -}}
  {{- $_ := set $spec "restartPolicy" "Never" -}}
{{- else -}}
  {{- $_ := set $container "ports" (list (dict "name" "http" "containerPort" $workload.port)) -}}
  {{- $_ := set $container "livenessProbe" (dict "httpGet" (dict "path" $workload.lifecycle.live "port" "http") "timeoutSeconds" 2) -}}
  {{- $_ := set $container "readinessProbe" (dict "httpGet" (dict "path" $workload.lifecycle.ready "port" "http") "timeoutSeconds" 3 "periodSeconds" 5) -}}
  {{- $_ := set $container "startupProbe" (dict "httpGet" (dict "path" $workload.lifecycle.live "port" "http") "periodSeconds" 2 "failureThreshold" 30) -}}
  {{- $_ := set $spec "topologySpreadConstraints" (list (dict "maxSkew" 1 "topologyKey" "topology.kubernetes.io/zone" "whenUnsatisfiable" "ScheduleAnyway" "labelSelector" (dict "matchLabels" (include "tacit.labels" .role | fromJson)))) -}}
{{- end -}}
{{- if $workload.writable -}}
  {{- $volumes := list -}}
  {{- $mounts := list -}}
  {{- range $name, $path := $workload.writable -}}
    {{- $volumes = append $volumes (dict "name" $name "emptyDir" (dict "sizeLimit" (printf "%dMi" (div $workload.resources.scratch (len $workload.writable))))) -}}
    {{- $mounts = append $mounts (dict "name" $name "mountPath" $path) -}}
  {{- end -}}
  {{- $_ := set $spec "volumes" $volumes -}}
  {{- $_ := set $container "volumeMounts" $mounts -}}
{{- end -}}
{{- dict "metadata" (dict "labels" (include "tacit.labels" .role | fromJson)
      "annotations" (dict "tacit.dev/schema-version" (toString .schemaVersion) "tacit.dev/revision" .revision))
    "spec" $spec | toJson -}}
{{- end -}}

{{- define "tacit.peer" -}}
{{- dict "namespaceSelector" (dict "matchLabels" (dict "kubernetes.io/metadata.name" .namespace))
         "podSelector" (dict "matchLabels" .labels) | toJson -}}
{{- end -}}

{{- define "tacit.policy" -}}
{{- $metadata := include "tacit.metadata" . | fromJson -}}
{{- dict "apiVersion" "networking.k8s.io/v1" "kind" "NetworkPolicy" "metadata" $metadata "spec" .spec | toJson -}}
{{- end -}}
