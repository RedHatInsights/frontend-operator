package controllers

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
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
	// messagesLocaleLabel marks the per-locale messages ConfigMaps, for
	// consumers and for pruning.
	messagesLocaleLabel = "frontend.cloud.redhat.com/messages-locale"
	// maxLocaleMessagesBytes keeps a locale ConfigMap safely under the 1 MiB
	// object limit.
	maxLocaleMessagesBytes = 900 * 1024
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

func messagesFileName(locale string) string {
	return "messages." + locale + ".json"
}

// messagesConfigMapName is the name of a locale's ConfigMap next to the base
// ConfigMap named baseName. ConfigMap names are lowercase, locale ids are not.
func messagesConfigMapName(baseName, locale string) string {
	return baseName + "-messages-" + strings.ToLower(locale)
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

// publishedLocaleIDs lists, sorted, the non-English locales that get a
// messages file: every enabled locale, so locales.json never lists a missing
// file, and every locale that the environment or one of its Frontends has
// strings for. Consumers only request the locales advertised in locales.json.
func publishedLocaleIDs(feList *crd.FrontendList, env *crd.FrontendEnvironment) []string {
	ids := map[string]bool{}
	for _, enabled := range env.Spec.EnabledLocales {
		ids[enabled.ID] = true
	}
	for id := range env.Spec.Locales {
		ids[id] = true
	}
	for _, frontend := range feList.Items {
		if frontend.Spec.EnvName != env.Name || frontend.DeletionTimestamp != nil {
			continue
		}
		for id := range frontend.Spec.Locales {
			ids[id] = true
		}
	}
	delete(ids, sourceLocale)
	return slices.Sorted(maps.Keys(ids))
}

// setupLocaleMessages builds the messages file of every published locale,
// keyed by locale. A locale whose messages exceed maxLocaleMessagesBytes is
// left out and logged: its ConfigMap would be rejected and fail the reconcile.
func (r *FrontendReconciliation) setupLocaleMessages(feList *crd.FrontendList) (map[string]string, error) {
	ids := publishedLocaleIDs(feList, r.FrontendEnvironment)
	if len(ids) == 0 {
		return nil, nil
	}
	keySources := buildLocaleKeySources(feList, r.FrontendEnvironment)
	messages := map[string]string{}
	for _, id := range ids {
		built := buildLocaleMessagesFromSources(id, keySources)
		r.logLocaleKeys(id, built)
		data, err := json.Marshal(built.Messages)
		if err != nil {
			return nil, err
		}
		if len(data) > maxLocaleMessagesBytes {
			r.logLocale("locale %s left out: messages are %d bytes, limit %d", id, len(data), maxLocaleMessagesBytes)
			continue
		}
		messages[id] = string(data)
	}
	return messages, nil
}

// advertisedLocales returns the locales.json entries for the published
// messages, or nil when the environment enables no locales.
func (r *FrontendReconciliation) advertisedLocales(messages map[string]string) []localeEntry {
	enabled := r.FrontendEnvironment.Spec.EnabledLocales
	if len(enabled) == 0 {
		return nil
	}
	advertised := map[string]bool{sourceLocale: true}
	for id := range messages {
		advertised[id] = true
	}
	return localesJSONEntries(enabled, advertised)
}

// createLocaleMessagesConfigMap creates the ConfigMap holding one locale's
// messages beside the base ConfigMap baseNN.
func (r *FrontendReconciliation) createLocaleMessagesConfigMap(baseNN types.NamespacedName, locale, data string) error {
	nn := types.NamespacedName{Name: messagesConfigMapName(baseNN.Name, locale), Namespace: baseNN.Namespace}
	cfgMap := &v1.ConfigMap{}
	if err := r.Cache.Create(CoreConfig, nn, cfgMap); err != nil {
		return err
	}

	// no recycle annotation: a translation change must not restart consumers
	cfgMap = r.setupConfigMapWithLabels(nn, false)
	cfgMap.Labels[messagesLocaleLabel] = locale
	cfgMap.SetOwnerReferences([]metav1.OwnerReference{r.FrontendEnvironment.MakeOwnerReference()})
	cfgMap.Data = map[string]string{messagesFileName(locale): data}

	return r.Cache.Update(CoreConfig, cfgMap)
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

// pruneLocaleConfigMaps deletes this environment's messages ConfigMaps beside
// the given base ConfigMaps whose locale has no messages any more.
func (r *FrontendReconciliation) pruneLocaleConfigMaps(bases []types.NamespacedName, messages map[string]string) error {
	env := r.FrontendEnvironment
	for _, baseNN := range bases {
		list := &v1.ConfigMapList{}
		if err := r.Client.List(r.Ctx, list, client.InNamespace(baseNN.Namespace), client.MatchingLabels{"frontendenv": env.GetLabels()["frontendenv"]}, client.HasLabels{messagesLocaleLabel}); err != nil {
			return err
		}
		for i := range list.Items {
			cfgMap := &list.Items[i]
			locale := cfgMap.Labels[messagesLocaleLabel]
			if _, ok := messages[locale]; ok || cfgMap.Name != messagesConfigMapName(baseNN.Name, locale) || !ownedBy(cfgMap, env) {
				continue
			}
			r.Log.Info("Pruning locale messages", "namespace", cfgMap.Namespace, "name", cfgMap.Name)
			if err := r.Client.Delete(r.Ctx, cfgMap); client.IgnoreNotFound(err) != nil {
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

// logLocaleKeys logs the keys of one locale that were left out of its
// messages, like the other generators log what they skip.
func (r *FrontendReconciliation) logLocaleKeys(locale string, built localeMessages) {
	if len(built.Collisions) > 0 {
		r.logLocale("locale %s: %d keys produced by more than one source were left out: %s (sources: %s)", locale, len(built.Collisions), truncateList(built.Collisions), truncateList(built.CollidingSources))
	}
	if len(built.Unknown) > 0 {
		r.logLocale("locale %s: %d catalog keys outside the runtime key scheme were dropped: %s", locale, len(built.Unknown), truncateList(built.Unknown))
	}
	if len(built.Skipped) > 0 {
		r.logLocale("locale %s: %d catalog keys without English output were skipped: %s", locale, len(built.Skipped), truncateList(built.Skipped))
	}
	r.Log.Info("Locale messages built", "locale", locale, "keys", len(built.Messages))
}

func (r *FrontendReconciliation) logLocale(format string, args ...any) {
	r.Log.Info(fmt.Sprintf(format, args...), "environment", r.FrontendEnvironment.Name)
}

func truncateList(items []string) string {
	const limit = 10
	if len(items) <= limit {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:limit], ", ") + fmt.Sprintf(", and %d more", len(items)-limit)
}
