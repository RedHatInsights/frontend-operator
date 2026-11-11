package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
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
)

// Keep this inventory explicit: the base ConfigMap also contains a Caddyfile and
// a locale manifest whose messages are separate ConfigMaps. Those, widgets and
// layouts are not part of this base-config publisher.
func selectConfigAssets(data map[string]string) (map[string]string, error) {
	files := map[string]string{}
	for _, name := range []string{
		"fed-modules.json", "bundles.json", "search-index.json", "api-specs.json",
		"service-tiles.json", "sso-config.json", "exports.json",
	} {
		if contents, ok := data[name]; ok {
			if !json.Valid([]byte(contents)) {
				return nil, fmt.Errorf("config asset %s contains invalid JSON", name)
			}
			files[name] = contents
		}
	}
	var registry map[string]json.RawMessage
	if err := json.Unmarshal([]byte(files["fed-modules.json"]), &registry); err != nil || registry == nil {
		return nil, fmt.Errorf("config assets require a fed-modules.json object")
	}
	return files, nil
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
	if !r.FrontendEnvironment.Spec.EnableConfigAssets {
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
		return ctrl.Result{}, fmt.Errorf("ValpopImage is required when EnableConfigAssets is enabled")
	}
	cutoff := r.FrontendEnvironment.Spec.DeployCutoffTimestampConfigAssets
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
	// Include destination/cache policy and the manual refresh token, never unrelated
	// ConfigMaps or credentials. A cutoff change must bypass valpop's deduplication.
	publicationHash, err := createConfigmapHash([]map[string]string{files, {
		"bucket": *store.Name, "hostname": *store.Endpoint, "port": *store.Port,
		"tls": strconv.FormatBool(*store.TLS), "cache-max-age": configAssetsCacheMaxAge, "cutoff": cutoff,
	}})
	if err != nil {
		return ctrl.Result{}, err
	}
	desired := r.buildConfigAssetsJob(nn, contentHash, publicationHash, store)
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
	ready, err := r.ensureConfigAssetsSnapshot(nn, files)
	if err != nil || !ready {
		return wait, err
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
	return ctrl.Result{}, r.deleteConfigAssetsSnapshot(nn)
}

func (r *FrontendReconciliation) deleteConfigAssetsJob(job *batchv1.Job) error {
	propagation := metav1.DeletePropagationForeground
	err := r.Client.Delete(r.Ctx, job, &client.DeleteOptions{
		PropagationPolicy: &propagation, Preconditions: &metav1.Preconditions{UID: &job.UID},
	})
	return client.IgnoreNotFound(err)
}

func (r *FrontendReconciliation) deleteConfigAssetsSnapshot(nn types.NamespacedName) error {
	cm := &v1.ConfigMap{}
	if err := r.Client.Get(r.Ctx, nn, cm); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !r.ownsConfigAsset(cm) {
		return nil
	}
	return client.IgnoreNotFound(r.Client.Delete(r.Ctx, cm, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &cm.UID}}))
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
	return map[string]string{"frontendenv": r.Frontend.Spec.EnvName, "component": configAssetsComponent}
}

// A Frontend can leave an environment by changing envName or being deleted.
// Discover only publisher resources in its namespace, and check live Frontends
// before deleting anything shared. Finalization waits for foreground Job cleanup.
func (r *FrontendReconciler) pruneOrphanedConfigAssets(ctx context.Context, namespace string) (ctrl.Result, error) {
	opts := []client.ListOption{client.InNamespace(namespace), client.MatchingLabels{"component": configAssetsComponent}}
	jobs, snapshots := &batchv1.JobList{}, &v1.ConfigMapList{}
	if err := r.Client.List(ctx, jobs, opts...); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Client.List(ctx, snapshots, opts...); err != nil {
		return ctrl.Result{}, err
	}
	owners := map[types.UID]metav1.OwnerReference{}
	collectOwner := func(obj client.Object) {
		for _, owner := range obj.GetOwnerReferences() {
			if owner.APIVersion == crd.GroupVersion.String() && owner.Kind == "FrontendEnvironment" &&
				owner.UID != "" && obj.GetName() == configAssetsJobName(owner.Name) {
				owners[owner.UID] = owner
			}
		}
	}
	for i := range jobs.Items {
		collectOwner(&jobs.Items[i])
	}
	for i := range snapshots.Items {
		collectOwner(&snapshots.Items[i])
	}
	if len(owners) == 0 {
		return ctrl.Result{}, nil
	}
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}
	frontends := &crd.FrontendList{}
	if err := reader.List(ctx, frontends, client.InNamespace(namespace)); err != nil {
		return ctrl.Result{}, err
	}
	for _, frontend := range frontends.Items {
		if !frontend.DeletionTimestamp.IsZero() {
			continue
		}
		for uid, owner := range owners {
			if frontend.Spec.EnvName == owner.Name {
				delete(owners, uid)
			}
		}
	}
	result := ctrl.Result{}
	for _, owner := range owners {
		reconciliation := &FrontendReconciliation{
			Ctx: ctx, Client: r.Client,
			Frontend:            &crd.Frontend{ObjectMeta: metav1.ObjectMeta{Namespace: namespace}, Spec: crd.FrontendSpec{EnvName: owner.Name}},
			FrontendEnvironment: &crd.FrontendEnvironment{ObjectMeta: metav1.ObjectMeta{UID: owner.UID}},
		}
		pending, err := reconciliation.cleanupConfigAssets()
		if err != nil {
			return ctrl.Result{}, err
		}
		if pending.RequeueAfter > 0 {
			result = pending
		}
	}
	return result, nil
}

func (r *FrontendReconciliation) buildConfigAssetsJob(nn types.NamespacedName, contentHash, publicationHash string, store *ObjectStoreBucket) *batchv1.Job {
	image := r.FrontendEnvironment.Spec.ValpopImage
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
						{Name: "config", VolumeSource: v1.VolumeSource{ConfigMap: &v1.ConfigMapVolumeSource{LocalObjectReference: v1.LocalObjectReference{Name: nn.Name}}}},
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
