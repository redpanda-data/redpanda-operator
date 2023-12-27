
# Manually building crds
kustomize build "https://github.com/fluxcd/source-controller//config/crd&?ref=<version>" -o source-controller.yaml
kustomize build "https://github.com/fluxcd/helm-controller//config/crd?ref=<version>" -o helm-controller.yaml
