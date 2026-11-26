package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/RedHatInsights/rhc-osdk-utils/utils"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	configAssetsPollInterval          = 10 * time.Second
	configAssetsCacheMaxAge           = "60"
	configAssetsHashAnnotation        = "config-hash"
	configAssetsPublicationAnnotation = "config-publication"
	configAssetsImageAnnotation       = "valpop-image"
	configAssetsComponent             = "frontend-config"
	configAssetsUploadLabel           = "frontend.cloud.redhat.com/config-upload"
	// configMapMaxBytes is the API limit on a ConfigMap's data.
	configMapMaxBytes = 1 << 20
)

// Keep the base inventory explicit; locale messages are generated separately.
// Caddyfile, widgets and layouts are not part of this publisher.
var baseConfigAssets = []string{
	"fed-modules.json", "bundles.json", "search-index.json", "api-specs.json",
	"service-tiles.json", "sso-config.json", "exports.json", "locales.json",
}

func selectConfigAssets(data map[string]string) (map[string]string, error) {
	files := map[string]string{}
	for name, contents := range data {
		if !slices.Contains(baseConfigAssets, name) && !isMessagesFile(name) {
			continue
		}
		if !json.Valid([]byte(contents)) {
			return nil, fmt.Errorf("config asset %s contains invalid JSON", name)
		}
		files[name] = contents
	}
	if manifest, ok := files["locales.json"]; ok {
		var entries []localeEntry
		if err := json.Unmarshal([]byte(manifest), &entries); err != nil {
			return nil, fmt.Errorf("invalid locale manifest: %w", err)
		}
		for _, entry := range entries {
			if entry.ID != sourceLocale {
				if entry.File != messagesFileName(entry.ID) || files[entry.File] == "" {
					return nil, fmt.Errorf("locale %s references missing messages %s", entry.ID, entry.File)
				}
			}
		}
	}
	var registry map[string]json.RawMessage
	if err := json.Unmarshal([]byte(files["fed-modules.json"]), &registry); err != nil || registry == nil {
		return nil, fmt.Errorf("config assets require a fed-modules.json object")
	}
	return files, nil
}

// Preserve a single upload Job while keeping each immutable ConfigMap below the
// API limit. Several valid per-locale ConfigMaps can exceed that limit together.
func splitConfigAssets(files map[string]string) ([]map[string]string, error) {
	parts := []map[string]string{{}}
	size := 0
	for _, name := range slices.Sorted(maps.Keys(files)) {
		contents := files[name]
		n := len(name) + len(contents)
		if n > configMapMaxBytes {
			return nil, fmt.Errorf("config asset %s exceeds the ConfigMap size limit", name)
		}
		if size+n > configMapMaxBytes {
			parts = append(parts, map[string]string{})
			size = 0
		}
		parts[len(parts)-1][name] = contents
		size += n
	}
	return parts, nil
}

func configAssetsSnapshotName(base string, part int) string {
	if part == 0 {
		return base
	}
	return base + "-part-" + strconv.Itoa(part)
}

// deleteConfigAssetsSnapshotsFrom deletes this upload's snapshot parts numbered
// keep and above; keep 0 deletes them all. Called only after the old upload Job
// and its pods have gone away. Scope the query to this upload, not every
// publisher resource in the namespace. Labels remain discoverable even if
// replacement of an immutable part was interrupted.
func (r *FrontendReconciliation) deleteConfigAssetsSnapshotsFrom(reader client.Reader, nn types.NamespacedName, keep int) error {
	list := &v1.ConfigMapList{}
	if err := reader.List(r.Ctx, list, client.InNamespace(nn.Namespace), client.MatchingLabels{configAssetsUploadLabel: nn.Name}); err != nil {
		return err
	}
	// Pre-localization snapshots have no upload label; include that exact name.
	base := &v1.ConfigMap{}
	if err := reader.Get(r.Ctx, nn, base); err == nil {
		if base.Labels[configAssetsUploadLabel] != nn.Name {
			list.Items = append(list.Items, *base)
		}
	} else if !k8serr.IsNotFound(err) {
		return err
	}
	wanted := map[string]bool{}
	for i := range keep {
		wanted[configAssetsSnapshotName(nn.Name, i)] = true
	}
	for _, cm := range list.Items {
		if !wanted[cm.Name] && r.ownsConfigAsset(&cm) {
			if err := r.Client.Delete(r.Ctx, &cm, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &cm.UID}}); client.IgnoreNotFound(err) != nil {
				return err
			}
		}
	}
	return nil
}

func (r *FrontendReconciliation) generateConfigAssetsJobName() string {
	return configAssetsJobName(r.Frontend.Spec.EnvName)
}

func configAssetsJobName(environment string) string {
	name := environment + "-config-assets"
	if len(name) > 63 {
		// Retain a hash of the complete environment name within the Job name limit.
		digest := sha256.Sum256([]byte(name))
		name = fmt.Sprintf("%.40s-%x-config-assets", environment, digest[:4])
	}
	return name
}

func (r *FrontendReconciliation) configAssetsReader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}
	return r.Client
}

func (r *FrontendReconciliation) ownsConfigAsset(obj client.Object) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.Kind == "FrontendEnvironment" && ref.UID == r.FrontendEnvironment.UID {
			return true
		}
	}
	return false
}

// reconcileConfigAssetsJob runs after ApplyAll and pruning, independently of
// frontend image deployment. Keep one Job per environment/namespace.
// Foreground deletion prevents a previous uploader from finishing after its
// replacement. The immutable snapshot binds the Job's identity to its bytes.
func (r *FrontendReconciliation) reconcileConfigAssetsJob() (ctrl.Result, error) {
	if !r.FrontendEnvironment.Spec.EnablePushCache {
		return r.cleanupConfigAssets()
	}
	wait := ctrl.Result{RequeueAfter: configAssetsPollInterval}
	nn := types.NamespacedName{Name: r.generateConfigAssetsJobName(), Namespace: r.Frontend.Namespace}
	job := &batchv1.Job{}
	err := r.configAssetsReader().Get(r.Ctx, nn, job)
	if err != nil && !k8serr.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	exists := err == nil
	if exists {
		if !r.ownsConfigAsset(job) {
			return ctrl.Result{}, fmt.Errorf("config upload Job %s is not owned by this environment", nn)
		}
		if !job.DeletionTimestamp.IsZero() {
			return wait, nil
		}
	}
	if r.FrontendEnvironment.Spec.ValpopImage == "" {
		return ctrl.Result{}, fmt.Errorf("ValpopImage is required when EnablePushCache is enabled")
	}
	files, err := selectConfigAssets(r.configAssets)
	if err != nil {
		return ctrl.Result{}, err
	}
	store, err := getObjectStoreConfig(r.Ctx, r.Client, r.Frontend.Namespace)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := ensurePushCacheCredentialsSecret(r.Ctx, r.Client, nn.Namespace, *store.AccessKey, *store.SecretKey); err != nil {
		return ctrl.Result{}, err
	}
	contentHash, err := createConfigmapHash([]map[string]string{files})
	if err != nil {
		return ctrl.Result{}, err
	}
	// Include destination/cache policy, never unrelated ConfigMaps or credentials.
	publicationHash, err := createConfigmapHash([]map[string]string{files, {
		"bucket": *store.Name, "hostname": *store.Endpoint, "port": *store.Port,
		"tls": strconv.FormatBool(*store.TLS), "cache-max-age": configAssetsCacheMaxAge,
	}})
	if err != nil {
		return ctrl.Result{}, err
	}
	snapshots, err := splitConfigAssets(files)
	if err != nil {
		return ctrl.Result{}, err
	}
	desired := r.buildConfigAssetsJob(nn, len(snapshots), contentHash, publicationHash, store)
	if exists {
		if configAssetsJobIsStale(job, desired) {
			return wait, r.deleteConfigAssetsJob(job)
		}
		for _, condition := range job.Status.Conditions {
			if condition.Status != v1.ConditionTrue {
				continue
			}
			switch condition.Type {
			case batchv1.JobFailed:
				if err := r.deleteConfigAssetsJob(job); err != nil {
					return ctrl.Result{}, err
				}
				// Surface the failure through the existing Frontend condition and
				// controller backoff. A matching hash alone does not mean success.
				return ctrl.Result{}, fmt.Errorf("config upload Job %s failed (%s); retrying", nn, condition.Reason)
			case batchv1.JobComplete:
				return ctrl.Result{}, nil
			}
		}
		// These Jobs belong to the environment, not the Owns(Job) Frontend watch.
		return wait, nil
	}
	// Read through the API: the cache can lag behind recent snapshot writes.
	if err := r.deleteConfigAssetsSnapshotsFrom(r.configAssetsReader(), nn, len(snapshots)); err != nil {
		return wait, err
	}
	for i, snapshot := range snapshots {
		key := types.NamespacedName{Name: configAssetsSnapshotName(nn.Name, i), Namespace: nn.Namespace}
		ready, err := r.ensureConfigAssetsSnapshot(key, snapshot)
		if err != nil || !ready {
			return wait, err
		}
	}
	if err := r.Client.Create(r.Ctx, desired); err != nil {
		if k8serr.IsAlreadyExists(err) {
			return wait, nil
		}
		return ctrl.Result{}, err
	}
	r.Log.Info("Created config assets upload Job", "job", nn, "configHash", contentHash)
	return wait, nil
}

func configAssetsJobIsStale(current, desired *batchv1.Job) bool {
	actual, wanted := current.Spec.Template.Annotations, desired.Spec.Template.Annotations
	for _, key := range []string{configAssetsHashAnnotation, configAssetsPublicationAnnotation, configAssetsImageAnnotation} {
		if actual[key] != wanted[key] {
			return true
		}
	}
	return !isJobFinished(current) && actual[PushCacheCredentialsChecksumAnnotation] != wanted[PushCacheCredentialsChecksumAnnotation]
}

// Disabled environments use the cache and ignore name collisions with resources
// they do not own. Only previously managed uploads need cleanup.
func (r *FrontendReconciliation) cleanupConfigAssets() (ctrl.Result, error) {
	nn := types.NamespacedName{Name: r.generateConfigAssetsJobName(), Namespace: r.Frontend.Namespace}
	job := &batchv1.Job{}
	if err := r.Client.Get(r.Ctx, nn, job); err != nil {
		if !k8serr.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	} else if r.ownsConfigAsset(job) {
		wait := ctrl.Result{RequeueAfter: configAssetsPollInterval}
		if !job.DeletionTimestamp.IsZero() {
			return wait, nil
		}
		return wait, r.deleteConfigAssetsJob(job)
	}
	// This runs on every reconcile of a disabled environment; the cache is enough.
	return ctrl.Result{}, r.deleteConfigAssetsSnapshotsFrom(r.Client, nn, 0)
}

func (r *FrontendReconciliation) deleteConfigAssetsJob(job *batchv1.Job) error {
	propagation := metav1.DeletePropagationForeground
	err := r.Client.Delete(r.Ctx, job, &client.DeleteOptions{
		PropagationPolicy: &propagation, Preconditions: &metav1.Preconditions{UID: &job.UID},
	})
	return client.IgnoreNotFound(err)
}

func (r *FrontendReconciliation) ensureConfigAssetsSnapshot(nn types.NamespacedName, files map[string]string) (bool, error) {
	cm := &v1.ConfigMap{}
	err := r.configAssetsReader().Get(r.Ctx, nn, cm)
	if err == nil {
		if !r.ownsConfigAsset(cm) {
			return false, fmt.Errorf("config snapshot %s is not owned by this environment", nn)
		}
		if !cm.DeletionTimestamp.IsZero() {
			return false, nil
		}
		if cm.Immutable != nil && *cm.Immutable && maps.Equal(cm.Data, files) && len(cm.BinaryData) == 0 {
			return true, nil
		}
		return false, client.IgnoreNotFound(r.Client.Delete(r.Ctx, cm))
	}
	if !k8serr.IsNotFound(err) {
		return false, err
	}
	cm = &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: nn.Namespace, Labels: r.configAssetsLabels(),
			OwnerReferences: []metav1.OwnerReference{r.FrontendEnvironment.MakeOwnerReference()}},
		Immutable: utils.TruePtr(), Data: maps.Clone(files),
	}
	return true, r.Client.Create(r.Ctx, cm)
}

func (r *FrontendReconciliation) configAssetsLabels() map[string]string {
	return map[string]string{"frontendenv": r.Frontend.Spec.EnvName, "component": configAssetsComponent,
		configAssetsUploadLabel: r.generateConfigAssetsJobName()}
}

// Finalization only needs to clean up the deleting Frontend's shared upload.
// Generated configuration is rebuilt by the normal reconciliation path; do not
// scan publisher Jobs and ConfigMaps on every Frontend reconcile.
func (r *FrontendReconciler) cleanupConfigAssetsForDeletedFrontend(ctx context.Context, frontend *crd.Frontend) (ctrl.Result, error) {
	environment := &crd.FrontendEnvironment{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: frontend.Spec.EnvName}, environment); err != nil {
		// Kubernetes garbage collection handles resources whose environment is gone.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}
	peers := &crd.FrontendList{}
	if err := reader.List(ctx, peers, client.InNamespace(frontend.Namespace)); err != nil {
		return ctrl.Result{}, err
	}
	for _, peer := range peers.Items {
		if peer.Name != frontend.Name && peer.Spec.EnvName == frontend.Spec.EnvName && peer.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, nil
		}
	}
	reconciliation := &FrontendReconciliation{
		Ctx: ctx, Client: r.Client, Frontend: frontend, FrontendEnvironment: environment,
	}
	return reconciliation.cleanupConfigAssets()
}

func (r *FrontendReconciliation) buildConfigAssetsJob(nn types.NamespacedName, snapshotParts int, contentHash, publicationHash string, store *ObjectStoreBucket) *batchv1.Job {
	image := r.FrontendEnvironment.Spec.ValpopImage
	snapshots := make([]v1.VolumeProjection, 0, snapshotParts)
	for i := range snapshotParts {
		snapshots = append(snapshots, v1.VolumeProjection{ConfigMap: &v1.ConfigMapProjection{
			LocalObjectReference: v1.LocalObjectReference{Name: configAssetsSnapshotName(nn.Name, i)},
		}})
	}
	minRecords := 3
	if r.FrontendEnvironment.Spec.MinAssetRecords != nil {
		minRecords = *r.FrontendEnvironment.Spec.MinAssetRecords
	}
	labels := r.configAssetsLabels()
	resources := v1.ResourceRequirements{
		Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m"), v1.ResourceMemory: resource.MustParse("128Mi")},
		Limits:   v1.ResourceList{v1.ResourceCPU: resource.MustParse("200m"), v1.ResourceMemory: resource.MustParse("256Mi")},
	}
	deadline := int64(300)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: nn.Namespace, Labels: labels,
			OwnerReferences: []metav1.OwnerReference{r.FrontendEnvironment.MakeOwnerReference()}},
		Spec: batchv1.JobSpec{
			Completions: utils.Int32Ptr(1), BackoffLimit: utils.Int32Ptr(3), ActiveDeadlineSeconds: &deadline,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: map[string]string{
					configAssetsHashAnnotation: contentHash, configAssetsPublicationAnnotation: publicationHash, configAssetsImageAnnotation: image,
					PushCacheCredentialsChecksumAnnotation: objectStoreCredentialChecksum(store),
				}},
				Spec: v1.PodSpec{
					RestartPolicy: v1.RestartPolicyNever,
					Volumes: []v1.Volume{
						{Name: "config", VolumeSource: v1.VolumeSource{Projected: &v1.ProjectedVolumeSource{Sources: snapshots}}},
						{Name: "assets", VolumeSource: v1.VolumeSource{EmptyDir: &v1.EmptyDirVolumeSource{}}},
					},
					InitContainers: []v1.Container{{
						Name: "stage-config", Image: image, Resources: resources,
						Command:      []string{"/bin/sh", "-ec", "cp /config/*.json /assets/"},
						VolumeMounts: []v1.VolumeMount{{Name: "config", MountPath: "/config", ReadOnly: true}, {Name: "assets", MountPath: "/assets"}},
					}},
					Containers: []v1.Container{{
						Name: "valpop-config", Image: image, Resources: resources,
						Command: []string{"valpop"},
						Args: []string{"populate", "-r", "config", "-s", "/assets", "-i", "config:" + publicationHash,
							"--valpop-image", image, "--cache-max-age", configAssetsCacheMaxAge, "--timeout", "172800",
							"--min-asset-records", strconv.Itoa(minRecords), "--bucket", *store.Name,
							"--hostname", *store.Endpoint, "--port", *store.Port, "--secure=" + strconv.FormatBool(*store.TLS),
							"--username", "$(PUSHCACHE_AWS_ACCESS_KEY_ID)", "--password", "$(PUSHCACHE_AWS_SECRET_ACCESS_KEY)"},
						Env:          pushCacheCredentialEnvVars(),
						VolumeMounts: []v1.VolumeMount{{Name: "assets", MountPath: "/assets", ReadOnly: true}},
					}},
				},
			},
		},
	}
}
