# RWX Storage Driver Matrix

TIDE's `tide-projects` PersistentVolumeClaim is `ReadWriteMany` by default
(see `charts/tide/templates/projects-pvc.yaml` and `charts/tide/values.yaml`
`workspaces.pvc.accessModes: [ReadWriteMany]`) so multiple Pods can mount the
same workspace volume concurrently — the Phase 2 dispatch contract depends on
this for `envelope-writer-init` + the subagent main container + the
`credproxy` sidecar to share `/workspace` within a Task, and for downstream
Tasks across waves to read the artifacts written by upstream Tasks without
copying through etcd.

The chart sets `storageClassName: ""` (empty) so the cluster's default
StorageClass is used. This works out of the box for `kind`, `minikube`, and
single-node dev clusters that ship a default RWO provisioner like
`rancher.io/local-path` (override `accessModes: [ReadWriteOnce]` in that
case). For multi-node production deployments, operators must explicitly set
`storageClassName` to a CSI driver that supports `ReadWriteMany`. The matrix
below covers the five drivers TIDE has been tested against or is known to
work behind (via upstream driver's documented RWX support).

| Driver                                           | Cloud        | Access Modes | Provisioning      | Performance Class            | Cross-AZ | Notes                                                                                                                                |
| ------------------------------------------------ | ------------ | ------------ | ----------------- | ---------------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| AWS EFS (`efs.csi.aws.com`)                      | AWS          | RWO + RWX    | Dynamic + Static  | Bursting / Provisioned       | Yes      | Regional file system; the only AWS-native RWX option for general Kubernetes workloads. Throughput scales with size or provisioned MB/s. |
| GCP Filestore (`filestore.csi.storage.gke.io`)   | GCP          | RWO + RWX    | Dynamic + Static  | Basic / Premium / Enterprise | Zonal\*  | Native GKE integration; Premium tier requires ≥ 2.5 TiB minimum. Multi-region uses Filestore Enterprise. `\*` regional only on Enterprise. |
| Azure Files (`file.csi.azure.com`)               | Azure        | RWO + RWX    | Dynamic + Static  | Standard / Premium           | Yes      | SMB or NFS protocol; NFS recommended for POSIX semantics in TIDE workspaces (subagent artifacts include shell-friendly files / symlinks). |
| csi-driver-nfs (`nfs.csi.k8s.io`)                | Any          | RWO + RWX    | Dynamic (over NFS)| Depends on backing NFS       | N/A      | Bring-your-own NFS server. Common for on-prem / bare-metal. Operator manages the NFS server lifecycle separately from the cluster.   |
| Longhorn (`driver.longhorn.io`)                  | Any          | RWO + RWX\*  | Dynamic           | Depends on backing disks     | Yes      | Cloud-native distributed block storage. `\*` RWX requires Longhorn's NFS share-manager feature (RWO is the default mode).            |

## Setting the StorageClass

Via Helm values:

```yaml
workspaces:
  pvc:
    storageClassName: "efs-sc"        # or filestore-csi-rwx, azurefile-csi-premium, nfs-csi, longhorn
    accessModes: [ReadWriteMany]
```

Or via direct PVC patch (if installed without re-templating):

```bash
kubectl patch pvc tide-projects -n tide-system \
  -p '{"spec":{"storageClassName":"efs-sc"}}'
```

## Verification

After setting the StorageClass, verify the PVC binds:

```bash
kubectl get pvc tide-projects -n tide-system
# Expected: STATUS = Bound
```

If the PVC stays `Pending`, confirm the StorageClass exists in-cluster:

```bash
kubectl get storageclass
```

And inspect the CSI controller pod logs for provisioning errors:

```bash
kubectl logs -n kube-system -l app=<driver-controller-label> --tail=50
```

For drivers that mount external file systems (EFS, Filestore, Azure Files),
verify that the cluster's worker-node IAM / service principal grants
permission to access the file-system resource. The CSI controller's logs
surface mount failures with the underlying cloud-API error.

## Phase 04.1 deferral note

This document was authored during Phase 04.1 Plan 13 (Wave 7 UAT closeout)
to close Phase 03 UAT item 5 / ROADMAP SC-13 second clause "docs enumerate
the matrix" requirement. The matrix is informational — TIDE does NOT bundle
any of these CSI drivers in the Helm chart; operators install the driver
appropriate to their cluster before applying the TIDE chart.

Phase 5 (`DIST-04` distribution docs) may extend this with cloud-specific
install recipes per driver; the matrix itself is the Phase 04.1 deliverable.
