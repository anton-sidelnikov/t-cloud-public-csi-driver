# EVS Manual Test Manifests

Manual EVS validation is now split by scenario:

- [common](manual/evs/common/README.md): filesystem, raw block, expansion, and reclaim-policy checks
- [snapshot](manual/evs/snapshot/README.md): ordered snapshot create and restore workflow

Compatibility entrypoint:

```bash
kubectl apply -k deploy/manual/evs
```

That command applies the `common` scenario bundle.
