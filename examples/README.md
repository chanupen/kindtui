# Example kind configs

| File | Cluster |
| --- | --- |
| [kind-cluster-config.yaml](kind-cluster-config.yaml) | 1 control plane + 2 workers |
| [kind-all-options.yaml](kind-all-options.yaml) | 1 control plane + 3 workers with kubeadm patches (reference for the available fields) |

Use one with the **repo config** create shape:

```sh
KIND_CONFIG=examples/kind-cluster-config.yaml kindtui
```

Or run kindtui from a directory that contains (or whose parent contains) a file named `kind-cluster-config.yaml`, `kind-config.yaml`, `kind.yaml`, or `kind.yml`.

Without kindtui, the same cluster is:

```sh
kind create cluster --name lab --config examples/kind-cluster-config.yaml
```

The full config reference is at https://kind.sigs.k8s.io/docs/user/configuration/.
