package controllers

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// English is the source language: its strings live in the base JSON and it
	// never has a messages file.
	sourceLocale = "en"
	// moduleTitleKey is the catalog key of a Frontend's document title.
	moduleTitleKey = "module.defaultDocumentTitle"
	// messagesLocaleLabel marks the per-locale messages ConfigMaps so they can
	// be found for pruning.
	messagesLocaleLabel = "frontend.cloud.redhat.com/messages-locale"
	// maxLocaleMessagesBytes keeps a locale ConfigMap safely under the 1 MiB
	// object limit.
	maxLocaleMessagesBytes = 900 * 1024
	// unadvertisedSinceAnnotation records when a messages ConfigMap was first
	// found neither enabled nor advertised, to time its pruning.
	unadvertisedSinceAnnotation = "frontend.cloud.redhat.com/unadvertised-since"
)

var (
	// localePruneGrace is how long a messages ConfigMap outlives the last
	// locales.json that advertised it, so consumers that still hold the old
	// locales.json (kubelet ConfigMap refresh, cached clients) can read the file.
	localePruneGrace = 15 * time.Minute
	// localeRetryInterval is when a reconcile that could not publish a locale is
	// retried; a failed write does not produce a ConfigMap event.
	localeRetryInterval = time.Minute
)

// localeEntry is one entry of locales.json.
type localeEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Default bool   `json:"default,omitempty"`
	File    string `json:"file,omitempty"`
}

// localeMessages is the generated output for one locale.
type localeMessages struct {
	// Messages maps runtime keys to translated strings.
	Messages map[string]string
	// Collisions lists runtime keys produced by more than one source; they are
	// left out of Messages so Chrome falls back to English for them.
	Collisions []string
	// CollidingSources lists the sources involved in any collision.
	CollidingSources []string
	// Unknown lists catalog keys outside the runtime key scheme, as
	// "<source>: <key>".
	Unknown []string
	// Skipped lists catalog keys without a corresponding English output field.
	Skipped []string
}

// localeReport collects what a reconcile found wrong with the locale output,
// for the Frontend's LocalesValid condition and for events.
type localeReport struct {
	// notAdvertised explains each enabled locale left out of locales.json.
	notAdvertised []string
	// keyProblems are the unknown or colliding keys of each Frontend, by
	// frontendSource.
	keyProblems map[string][]string
	// frontends are the Frontends of the environment, whose conditions all
	// depend on this report.
	frontends []crd.Frontend
}

func messagesFileName(locale string) string {
	return "messages." + locale + ".json"
}

// messagesConfigMapName is the name of a locale's ConfigMap next to the base
// ConfigMap named baseName. ConfigMap names are lowercase, locale ids are not.
func messagesConfigMapName(baseName, locale string) string {
	return baseName + "-messages-" + strings.ToLower(locale)
}

type localeConfigMapLocation struct {
	base     types.NamespacedName
	messages types.NamespacedName
}

func (r *FrontendReconciliation) localeConfigMapLocations(baseName, locale string) []localeConfigMapLocation {
	bases := r.localeBaseConfigMaps(baseName)
	locations := make([]localeConfigMapLocation, 0, len(bases))
	for _, base := range bases {
		locations = append(locations, localeConfigMapLocation{
			base:     base,
			messages: types.NamespacedName{Name: messagesConfigMapName(base.Name, locale), Namespace: base.Namespace},
		})
	}
	return locations
}

func (r *FrontendReconciliation) localeBaseConfigMaps(baseName string) []types.NamespacedName {
	bases := []types.NamespacedName{{Name: r.FrontendEnvironment.Name, Namespace: r.Frontend.Namespace}}
	for _, namespace := range r.FrontendEnvironment.Spec.TargetNamespaces {
		bases = append(bases, types.NamespacedName{Name: baseName, Namespace: namespace})
	}
	seen := map[types.NamespacedName]bool{}
	unique := make([]types.NamespacedName, 0, len(bases))
	for _, base := range bases {
		if seen[base] {
			continue
		}
		seen[base] = true
		unique = append(unique, base)
	}
	return unique
}

func frontendSource(frontend *crd.Frontend) string {
	return "Frontend/" + frontend.Namespace + "/" + frontend.Name
}

type localeKeySource struct {
	name    string
	table   *localeKeyTable
	locales crd.LocaleCatalogs
	// allowed, when set, limits the source to these catalog keys: the parts of
	// a Frontend without feoConfigEnabled that still reach the English output.
	allowed map[string]bool
	skipAll bool
}

// limited reports whether a catalog key of the source is left out.
func (ks localeKeySource) limited(catalogKey string) bool {
	return ks.allowed != nil && !ks.allowed[catalogKey]
}

func buildLocaleKeySources(feList *crd.FrontendList, env *crd.FrontendEnvironment) []localeKeySource {
	keySources := []localeKeySource{{
		name:    "FrontendEnvironment/" + env.Name,
		table:   environmentKeyTable(env),
		locales: env.Spec.Locales,
	}}
	frontends := append([]crd.Frontend{}, feList.Items...)
	referenced := referencedNavSegments(feList)
	sort.Slice(frontends, func(i, j int) bool {
		if frontends[i].Namespace != frontends[j].Namespace {
			return frontends[i].Namespace < frontends[j].Namespace
		}
		return frontends[i].Name < frontends[j].Name
	})
	for i := range frontends {
		frontend := &frontends[i]
		if frontend.Spec.EnvName != env.Name || frontend.DeletionTimestamp != nil {
			continue
		}
		source := localeKeySource{name: frontendSource(frontend), locales: frontend.Spec.Locales}
		if frontend.Spec.FeoConfigEnabled {
			source.table = frontendKeyTable(frontend)
			keySources = append(keySources, source)
			continue
		}
		// Without feoConfigEnabled, only chrome's document title and the
		// navigation segments that enabled Frontends reference reach the output.
		allowed := map[string]bool{}
		if frontend.Name == "chrome" && frontend.Spec.Module != nil {
			allowed[moduleTitleKey] = true
		}
		segments := newLocaleKeyTable()
		addNavigationSegmentKeys(segments, frontend, func(segmentID string) bool {
			return referenced[frontend.Name][segmentID]
		})
		for catalogKey := range segments.keys {
			allowed[catalogKey] = true
		}
		if len(allowed) == 0 {
			source.skipAll = true
			keySources = append(keySources, source)
			continue
		}
		source.table = frontendKeyTable(frontend)
		source.allowed = allowed
		keySources = append(keySources, source)
	}
	return keySources
}

// buildLocaleMessages merges the environment's strings with those of every
// eligible Frontend for one locale, under runtime keys. Eligibility matches the
// English output: search entries, tiles and nav come only from Frontends with
// feoConfigEnabled, document titles also from chrome, and navigation segments
// from any Frontend whose segment an enabled Frontend's bundle references. Output is deterministic:
// Frontends are visited in name order and keys are sorted on marshal.
func buildLocaleMessages(locale string, feList *crd.FrontendList, env *crd.FrontendEnvironment) localeMessages {
	return buildLocaleMessagesFromSources(locale, buildLocaleKeySources(feList, env))
}

func buildLocaleMessagesFromSources(locale string, keySources []localeKeySource) localeMessages {
	out := localeMessages{Messages: map[string]string{}}
	owners := map[string]map[string]struct{}{}
	collided := map[string]bool{}
	colliding := map[string]bool{}

	collide := func(source, runtimeKey string) {
		colliding[source] = true
		if !collided[runtimeKey] {
			collided[runtimeKey] = true
			delete(out.Messages, runtimeKey)
			out.Collisions = append(out.Collisions, runtimeKey)
		}
		for owner := range owners[runtimeKey] {
			colliding[owner] = true
		}
	}
	add := func(source string, table *localeKeyTable, catalogKey string, value crd.LocaleMessage) {
		runtimeKey, known, ambiguous := table.lookup(catalogKey)
		switch {
		case !known:
			out.Unknown = append(out.Unknown, source+": "+catalogKey)
		case ambiguous || len(owners[runtimeKey]) > 1 || collided[runtimeKey]:
			// several spec items of one source, or several sources, produce
			// this key
			collide(source, runtimeKey)
		default:
			out.Messages[runtimeKey] = string(value)
		}
	}

	// A runtime key that several sources produce collides even when only one of
	// them translates it: the translation would apply to every source's item.
	for _, ks := range keySources {
		if ks.skipAll {
			for _, catalogKey := range slices.Sorted(maps.Keys(ks.locales[locale])) {
				out.Skipped = append(out.Skipped, ks.name+": "+catalogKey)
			}
			continue
		}
		for catalogKey, runtimeKeys := range ks.table.runtimeKeysByCatalog {
			if ks.limited(catalogKey) {
				continue
			}
			for runtimeKey := range runtimeKeys {
				if owners[runtimeKey] == nil {
					owners[runtimeKey] = map[string]struct{}{}
				}
				owners[runtimeKey][ks.name] = struct{}{}
			}
		}
	}

	for _, ks := range keySources {
		if ks.skipAll {
			continue
		}
		catalog := ks.locales[locale]
		for _, catalogKey := range slices.Sorted(maps.Keys(catalog)) {
			if ks.limited(catalogKey) {
				out.Skipped = append(out.Skipped, ks.name+": "+catalogKey)
				continue
			}
			add(ks.name, ks.table, catalogKey, catalog[catalogKey])
		}
	}

	sort.Strings(out.Collisions)
	sort.Strings(out.Skipped)
	for source := range colliding {
		out.CollidingSources = append(out.CollidingSources, source)
	}
	sort.Strings(out.CollidingSources)
	return out
}

// setupLocaleConfigMaps writes one messages ConfigMap per enabled non-English
// locale beside the source and target base ConfigMaps, and returns the locales.json
// entries to advertise. It runs before the base ConfigMaps are written, and
// advertises a locale only when its messages are written everywhere.
//
// It writes directly rather than through the resource cache: the cache applies
// at the end of the reconcile, after the base ConfigMap.
func (r *FrontendReconciliation) setupLocaleConfigMaps(feList *crd.FrontendList, baseName string) ([]localeEntry, error) {
	env := r.FrontendEnvironment
	r.localeReport = localeReport{keyProblems: map[string][]string{}}
	for _, frontend := range feList.Items {
		if frontend.Spec.EnvName == env.Name && frontend.DeletionTimestamp == nil {
			r.localeReport.frontends = append(r.localeReport.frontends, frontend)
		}
	}
	advertised := map[string]bool{}
	if len(env.Spec.EnabledLocales) > 0 {
		advertised[sourceLocale] = true
	}
	var keySources []localeKeySource
	for _, enabled := range env.Spec.EnabledLocales {
		if enabled.ID != sourceLocale {
			keySources = buildLocaleKeySources(feList, env)
			break
		}
	}
	for _, enabled := range env.Spec.EnabledLocales {
		if enabled.ID == sourceLocale {
			continue
		}
		built := buildLocaleMessagesFromSources(enabled.ID, keySources)
		r.reportLocaleKeys(enabled.ID, built)

		data, err := json.Marshal(built.Messages)
		if err != nil {
			return nil, err
		}
		if len(data) > maxLocaleMessagesBytes {
			r.notAdvertised("LocaleMessagesOversize", "locale %s not advertised: messages are %d bytes, limit %d", enabled.ID, len(data), maxLocaleMessagesBytes)
			continue
		}
		if len(env.Spec.TargetNamespaces) == 0 {
			r.notAdvertised("LocaleMessagesNoTarget", "locale %s not advertised: the environment has no targetNamespaces to write messages to", enabled.ID)
			continue
		}

		locations := r.localeConfigMapLocations(baseName, enabled.ID)
		written := true
		for _, location := range locations {
			err := r.writeLocaleConfigMap(location.messages, enabled.ID, string(data))
			if k8serr.IsConflict(err) {
				// a stale read, not a bad locale: retry the reconcile instead of
				// dropping the locale from locales.json and restarting consumers
				return nil, err
			}
			if err != nil {
				message := fmt.Sprintf("locale %s: writing messages to namespace %s failed: %v", enabled.ID, location.messages.Namespace, err)
				r.requeueLocalesAfter(localeRetryInterval)
				written = false
				if r.localeWasPublished(enabled.ID, locations) {
					r.localeEvent("LocaleMessagesWriteFailed", "%s; retaining previously published messages", message)
					advertised[enabled.ID] = true
				} else {
					r.notAdvertised("LocaleMessagesWriteFailed", "locale %s not advertised: writing messages to namespace %s failed: %v", enabled.ID, location.messages.Namespace, err)
				}
				break
			}
		}
		if written {
			advertised[enabled.ID] = true
		}
	}

	if err := r.pruneLocaleConfigMaps(baseName, advertised); err != nil {
		return nil, err
	}

	if len(env.Spec.EnabledLocales) == 0 {
		return nil, nil
	}
	return localesJSONEntries(env.Spec.EnabledLocales, advertised), nil
}

// localesJSONEntries lists the enabled locales that are advertised, in spec
// order, with the messages file for each non-English one. If the configured
// default is not advertised, English becomes the default.
func localesJSONEntries(enabled []crd.EnabledLocale, advertised map[string]bool) []localeEntry {
	entries := []localeEntry{}
	hasDefault := false
	for _, l := range enabled {
		if !advertised[l.ID] {
			continue
		}
		entry := localeEntry{ID: l.ID, Name: l.Name, Default: l.Default}
		if l.ID != sourceLocale {
			entry.File = messagesFileName(l.ID)
		}
		hasDefault = hasDefault || l.Default
		entries = append(entries, entry)
	}
	if !hasDefault {
		for i := range entries {
			if entries[i].ID == sourceLocale {
				entries[i].Default = true
			}
		}
	}
	return entries
}

func (r *FrontendReconciliation) writeLocaleConfigMap(nn types.NamespacedName, locale, data string) error {
	cfgMap := &v1.ConfigMap{}
	err := r.Client.Get(r.Ctx, nn, cfgMap)
	if k8serr.IsNotFound(err) {
		cfgMap = &v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: nn.Namespace}}
		r.setLocaleConfigMap(cfgMap, nn, locale, data)
		err = r.Client.Create(r.Ctx, cfgMap)
		if k8serr.IsAlreadyExists(err) {
			return k8serr.NewConflict(v1.Resource("configmaps"), nn.Name, err)
		}
		return err
	}
	if err != nil {
		return err
	}

	updated := cfgMap.DeepCopy()
	r.setLocaleConfigMap(updated, nn, locale, data)
	if equality.Semantic.DeepEqual(cfgMap, updated) {
		return nil
	}
	return r.Client.Update(r.Ctx, updated)
}

func (r *FrontendReconciliation) setLocaleConfigMap(cfgMap *v1.ConfigMap, nn types.NamespacedName, locale, data string) {
	// no recycle annotation: a translation change must not restart consumers
	desired := r.setupConfigMapWithLabels(nn, false)
	if cfgMap.Labels == nil {
		cfgMap.Labels = map[string]string{}
	}
	for k, v := range desired.Labels {
		cfgMap.Labels[k] = v
	}
	cfgMap.Labels[messagesLocaleLabel] = locale
	// advertised again: cancel any pending pruning
	delete(cfgMap.Annotations, unadvertisedSinceAnnotation)
	cfgMap.SetOwnerReferences([]metav1.OwnerReference{r.FrontendEnvironment.MakeOwnerReference()})
	// the key must not also be in BinaryData, or the write is rejected
	cfgMap.BinaryData = nil
	cfgMap.Data = map[string]string{messagesFileName(locale): data}
}

func (r *FrontendReconciliation) localeWasPublished(locale string, locations []localeConfigMapLocation) bool {
	advertised := false
	for _, location := range locations {
		locales, ok := r.storedAdvertisedLocales(location.base)
		if ok && locales[locale] {
			advertised = true
		}
	}
	if !advertised {
		return false
	}
	for _, location := range locations {
		cfgMap := &v1.ConfigMap{}
		if err := r.Client.Get(r.Ctx, location.messages, cfgMap); err != nil {
			return false
		}
		raw, ok := cfgMap.Data[messagesFileName(locale)]
		if !ok || len(raw) > maxLocaleMessagesBytes {
			return false
		}
		messages := map[string]string{}
		if err := json.Unmarshal([]byte(raw), &messages); err != nil || messages == nil {
			return false
		}
	}
	return true
}

// storedAdvertisedLocales reads the locales listed by a stored base ConfigMap.
// ok is false when the ConfigMap exists but cannot be read or parsed: the
// caller must then assume any locale may still be advertised there.
func (r *FrontendReconciliation) storedAdvertisedLocales(nn types.NamespacedName) (locales map[string]bool, ok bool) {
	locales = map[string]bool{}
	cfgMap := &v1.ConfigMap{}
	err := r.Client.Get(r.Ctx, nn, cfgMap)
	if k8serr.IsNotFound(err) {
		return locales, true
	}
	if err != nil {
		r.Log.Info("Cannot read advertised locales, not pruning", "namespace", nn.Namespace, "name", nn.Name, "error", err.Error())
		return nil, false
	}
	raw, present := cfgMap.Data["locales.json"]
	if !present {
		return locales, true
	}
	entries := []localeEntry{}
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		r.Log.Info("Unreadable locales.json, not pruning", "namespace", nn.Namespace, "name", nn.Name, "error", err.Error())
		return nil, false
	}
	for _, e := range entries {
		locales[e.ID] = true
	}
	return locales, true
}

// pruneLocaleConfigMaps deletes this environment's messages ConfigMaps for
// locales that no locales.json lists any more. Only ConfigMaps next to a source
// or target base ConfigMap that this FrontendEnvironment owns are considered. In each
// namespace a locale is kept while it is advertised now, by the stored source
// base ConfigMap, or by that namespace's stored base ConfigMap; if either stored
// ConfigMap cannot be read, nothing in that namespace is touched and the
// reconcile is retried after localeRetryInterval. The first
// reconcile that finds a candidate stamps it with unadvertisedSinceAnnotation;
// it is deleted once that is older than localePruneGrace, and the reconcile is
// requeued for the earliest such deadline.
func (r *FrontendReconciliation) pruneLocaleConfigMaps(baseName string, advertisedNow map[string]bool) error {
	env := r.FrontendEnvironment
	source, sourceOK := r.storedAdvertisedLocales(types.NamespacedName{Name: r.Frontend.Spec.EnvName, Namespace: r.Frontend.Namespace})
	if !sourceOK {
		// nothing else may trigger a reconcile once the base is readable again
		r.requeueLocalesAfter(localeRetryInterval)
		return nil
	}
	now := time.Now().UTC()
	for _, baseNN := range r.localeBaseConfigMaps(baseName) {
		target, ok := r.storedAdvertisedLocales(baseNN)
		if !ok {
			r.requeueLocalesAfter(localeRetryInterval)
			continue
		}
		list := &v1.ConfigMapList{}
		if err := r.Client.List(r.Ctx, list, client.InNamespace(baseNN.Namespace), client.MatchingLabels{"frontendenv": env.GetLabels()["frontendenv"]}, client.HasLabels{messagesLocaleLabel}); err != nil {
			return err
		}
		for i := range list.Items {
			cfgMap := &list.Items[i]
			locale := cfgMap.Labels[messagesLocaleLabel]
			if !strings.HasPrefix(cfgMap.Name, baseNN.Name+"-messages-") || !ownedBy(cfgMap, env) || advertisedNow[locale] {
				continue
			}
			if source[locale] || target[locale] {
				// no longer advertised by this reconcile, but a stored base still
				// lists it until the cache applies; come back to start the grace
				// period, since nothing else may trigger a reconcile
				r.requeueLocalesAfter(localeRetryInterval)
				continue
			}
			since, err := time.Parse(time.RFC3339, cfgMap.Annotations[unadvertisedSinceAnnotation])
			if err != nil {
				updated := cfgMap.DeepCopy()
				if updated.Annotations == nil {
					updated.Annotations = map[string]string{}
				}
				updated.Annotations[unadvertisedSinceAnnotation] = now.Format(time.RFC3339)
				patch := client.MergeFromWithOptions(cfgMap.DeepCopy(), client.MergeFromWithOptimisticLock{})
				if err := r.Client.Patch(r.Ctx, updated, patch); err != nil {
					if k8serr.IsConflict(err) || k8serr.IsNotFound(err) {
						r.requeueLocalesAfter(localeRetryInterval)
						continue
					}
					return err
				}
				r.requeueLocalesAfter(localePruneGrace)
				continue
			}
			if remaining := since.Add(localePruneGrace).Sub(now); remaining > 0 {
				r.requeueLocalesAfter(remaining)
				continue
			}
			r.Log.Info("Pruning locale messages", "namespace", cfgMap.Namespace, "name", cfgMap.Name, "unadvertisedSince", since)
			resourceVersion := cfgMap.ResourceVersion
			uid := cfgMap.UID
			if err := r.Client.Delete(r.Ctx, cfgMap, client.Preconditions{ResourceVersion: &resourceVersion, UID: &uid}); err != nil {
				if k8serr.IsConflict(err) || k8serr.IsNotFound(err) {
					r.requeueLocalesAfter(localeRetryInterval)
					continue
				}
				return err
			}
		}
	}
	return nil
}

func ownedBy(obj metav1.Object, env *crd.FrontendEnvironment) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == env.UID {
			return true
		}
	}
	return false
}

// requeueLocalesAfter asks for the reconcile to run again within d.
func (r *FrontendReconciliation) requeueLocalesAfter(d time.Duration) {
	if d > 0 && (r.requeueAfter == 0 || d < r.requeueAfter) {
		r.requeueAfter = d
	}
}

// reportLocaleKeys records the unknown and colliding keys of one locale: all
// of them as events, and those of each Frontend for its condition.
func (r *FrontendReconciliation) reportLocaleKeys(locale string, built localeMessages) {
	if len(built.Collisions) > 0 {
		r.localeEvent("LocaleKeyCollision", "locale %s: %d keys produced by more than one source were left out: %s", locale, len(built.Collisions), truncateList(built.Collisions))
	}
	if len(built.Unknown) > 0 {
		r.localeEvent("LocaleKeyUnknown", "locale %s: %d catalog keys outside the runtime key scheme were dropped: %s", locale, len(built.Unknown), truncateList(built.Unknown))
	}
	if len(built.Skipped) > 0 {
		r.localeEvent("LocaleKeySkipped", "locale %s: %d catalog keys without English output were skipped: %s", locale, len(built.Skipped), truncateList(built.Skipped))
	}
	r.Log.Info("Locale messages built", "locale", locale, "keys", len(built.Messages))

	recordKeyProblems := func(problems []string, format string) {
		keysBySource := map[string][]string{}
		sources := []string{}
		for _, problem := range problems {
			source, key, _ := strings.Cut(problem, ": ")
			if _, seen := keysBySource[source]; !seen {
				sources = append(sources, source)
			}
			keysBySource[source] = append(keysBySource[source], key)
		}
		for _, source := range sources {
			r.localeReport.keyProblems[source] = append(r.localeReport.keyProblems[source], fmt.Sprintf(format, locale, len(keysBySource[source]), truncateList(keysBySource[source])))
		}
	}
	recordKeyProblems(built.Unknown, "locale %s: %d keys outside the runtime key scheme were dropped: %s")
	recordKeyProblems(built.Skipped, "locale %s: %d catalog keys without English output were skipped: %s")
	for _, source := range built.CollidingSources {
		r.localeReport.keyProblems[source] = append(r.localeReport.keyProblems[source], fmt.Sprintf("locale %s: keys produced by more than one source or spec item were left out", locale))
	}
}

func (r *FrontendReconciliation) notAdvertised(reason, format string, args ...any) {
	r.localeReport.notAdvertised = append(r.localeReport.notAdvertised, fmt.Sprintf(format, args...))
	r.localeEvent(reason, format, args...)
}

func (r *FrontendReconciliation) localeEvent(reason, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	r.Log.Info(message, "reason", reason, "environment", r.FrontendEnvironment.Name)
	if r.Recorder != nil {
		r.Recorder.Event(r.FrontendEnvironment, v1.EventTypeWarning, reason, message)
	}
}

// localesCondition is the LocalesValid condition for the reconciling Frontend,
// or nil when neither it nor its environment uses localization.
func (r *FrontendReconciliation) localesCondition() *metav1.Condition {
	return r.localesConditionFor(r.Frontend)
}

// peerLocalesConditions are the LocalesValid conditions of the environment's
// other Frontends. Keys of one Frontend can collide with those of another, so a
// reconcile that changes the messages can change their conditions too; setting
// them here keeps them current without reconciling every Frontend of the
// environment. Without enabled locales a condition depends only on its own
// Frontend, so there is nothing to update.
func (r *FrontendReconciliation) peerLocalesConditions() map[types.NamespacedName]*metav1.Condition {
	if len(r.FrontendEnvironment.Spec.EnabledLocales) == 0 {
		return nil
	}
	conditions := map[types.NamespacedName]*metav1.Condition{}
	for i := range r.localeReport.frontends {
		frontend := &r.localeReport.frontends[i]
		if frontend.Namespace == r.Frontend.Namespace && frontend.Name == r.Frontend.Name {
			continue
		}
		conditions[types.NamespacedName{Namespace: frontend.Namespace, Name: frontend.Name}] = r.localesConditionFor(frontend)
	}
	return conditions
}

func (r *FrontendReconciliation) localesConditionFor(frontend *crd.Frontend) *metav1.Condition {
	if len(r.FrontendEnvironment.Spec.EnabledLocales) == 0 && len(frontend.Spec.Locales) == 0 {
		return nil
	}
	keyProblems := r.localeReport.keyProblems[frontendSource(frontend)]
	condition := &metav1.Condition{
		Type:   crd.LocalesValid,
		Status: metav1.ConditionTrue,
		Reason: "Valid",
	}
	switch {
	case len(r.FrontendEnvironment.Spec.EnabledLocales) == 0:
		condition.Reason = "NotEnabled"
		condition.Message = "the environment enables no locales; translations are stored but not published"
	case len(r.localeReport.notAdvertised) > 0:
		condition.Status = metav1.ConditionFalse
		condition.Reason = "LocaleNotAdvertised"
		condition.Message = joinBounded(append(slices.Clone(r.localeReport.notAdvertised), keyProblems...), maxConditionMessageBytes)
	case len(keyProblems) > 0:
		condition.Status = metav1.ConditionFalse
		condition.Reason = "KeysDropped"
		condition.Message = joinBounded(keyProblems, maxConditionMessageBytes)
	}
	return condition
}

// maxConditionMessageBytes keeps the LocalesValid message well under the
// 32768 byte limit of metav1.Condition; a longer one fails the status update.
const maxConditionMessageBytes = 8 * 1024

// joinBounded joins problems with "; " into at most limit bytes, ending with
// "… and N more" when some are left out.
func joinBounded(problems []string, limit int) string {
	var b strings.Builder
	for i, p := range problems {
		sep := ""
		if i > 0 {
			sep = "; "
		}
		suffix := ""
		if rest := len(problems) - i - 1; rest > 0 {
			suffix = fmt.Sprintf("; … and %d more", rest)
		}
		if b.Len()+len(sep)+len(p)+len(suffix) > limit {
			more := fmt.Sprintf("… and %d more", len(problems)-i)
			if b.Len() == 0 {
				// even the first problem does not fit: cut it on a rune boundary
				cut := []rune(p)
				for len(string(cut)) > limit-len(more)-2 {
					cut = cut[:len(cut)-1]
				}
				return string(cut) + "; " + more
			}
			b.WriteString("; " + more)
			return b.String()
		}
		b.WriteString(sep + p)
	}
	return b.String()
}

func truncateList(items []string) string {
	const limit = 10
	if len(items) <= limit {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:limit], ", ") + fmt.Sprintf(", and %d more", len(items)-limit)
}
