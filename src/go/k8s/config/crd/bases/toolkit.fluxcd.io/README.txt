
# Manually building crds
```
kustomize build "https://github.com/fluxcd/source-controller//config/crd?ref=<version>" -o source-controller.yaml
```
and
```
kustomize build "https://github.com/fluxcd/helm-controller//config/crd?ref=<version>" -o helm-controller.yaml
```

# Use Makefile
Run at level where crd exists

```
make update-external-crds
```