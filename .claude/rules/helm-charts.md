# Helm Chart Rules

## cert-manager Integration

cert-manager is **optional** in all charts. The default must not require cert-manager to be installed.

### values.yaml structure

```yaml
tls:
  # Pre-provisioned Secret containing tls.crt and tls.key.
  # When set the chart uses this directly; no Certificate is created.
  existingSecret: ""
  # DNS names included in the server certificate (used by cert-manager).
  dnsNames:
    - <service-name>
    - <service-name>.<namespace>.svc.cluster.local

certManager:
  enabled: false          # opt-in, never default to true
  issuerRef:
    name: cluster-issuer
    kind: ClusterIssuer
```

Any other TLS fields specific to a chart (e.g. `clientCASecretName` for mTLS) live under `tls:`.

### _helpers.tpl — tlsSecretName helper

Every chart must define a `<chart>.tlsSecretName` helper that returns the existing secret when set, otherwise a generated name:

```
{{- define "<chart>.tlsSecretName" -}}
{{- if .Values.tls.existingSecret }}
{{- .Values.tls.existingSecret }}
{{- else }}
{{- printf "%s-tls" (include "<chart>.fullname" .) }}
{{- end }}
{{- end }}
```

### templates/certificate.yaml

Guard on `certManager.enabled`, never on `tls.issuerRef`:

```yaml
{{- if .Values.certManager.enabled }}
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: {{ include "<chart>.fullname" . }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "<chart>.labels" . | nindent 4 }}
spec:
  secretName: {{ include "<chart>.tlsSecretName" . }}
  issuerRef:
    name: {{ .Values.certManager.issuerRef.name }}
    kind: {{ .Values.certManager.issuerRef.kind | default "ClusterIssuer" }}
  dnsNames:
    {{- toYaml .Values.tls.dnsNames | nindent 4 }}
  usages:
    - server auth
  duration: 8760h
  renewBefore: 720h
{{- end }}
```

### Deployment volumes

Always use the `tlsSecretName` helper — never hardcode the secret name:

```yaml
volumes:
  - name: tls
    secret:
      secretName: {{ include "<chart>.tlsSecretName" . }}
```

## General Helm Conventions

- Reference sibling controllers (`../mqtt-device-controller`, `../mqtt-bridge-controller`) as the canonical pattern source when adding new chart features.
- Feature flags follow the pattern: `<feature>.enabled: false` as the safe default (prometheus.serviceMonitor, prometheus.rules, certManager, crds.install).
- CRD installation is controlled by `crds.install: true` (or `installCRD: true` — align with the chart's existing key).
- All namespace fields default to `""` which templates resolve to `.Release.Namespace`.

## OCI Image Repository

The image repository is always `ghcr.io/hauke-cloud/<application>` — never include the intermediate `iot/` path segment:

```yaml
# CORRECT:
image:
  repository: ghcr.io/hauke-cloud/valve-controller

# WRONG:
image:
  repository: ghcr.io/hauke-cloud/iot/valve-controller
```

## Chart Location

Helm charts are stored at `deployments/helm/<application>` within the repository root — not under `charts/`, `helm/`, or any other path:

```
deployments/helm/valve-controller/
deployments/helm/mqtt-device-controller/
```
