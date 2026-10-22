package controllers

import (
	"fmt"
	"strings"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	localUtil "github.com/RedHatInsights/frontend-operator/controllers/utils"
)

// Catalog keys are written by the i18n tooling relative to one spec
// (Frontend or FrontendEnvironment). Runtime keys are what Chrome looks up in
// messages.<locale>.json; each starts with a namespace Chrome can rebuild from a
// single object in the generated JSON, so no array position is ever involved.
//
//	searchEntries.<id>.<field>                     -> search.<frontend>-<env>-<id>.<field>
//	searchEntries.<id>.alt_title.<slug>            -> search.<frontend>-<env>-<id>.alt_title.<slug>
//	serviceTiles.<section>.<group>.<id>.<field>    -> tile.<frontend>.<section>.<group>.<id>.<field>
//	bundleSegments.<b>.<s>.navItems.<id>[.routes.<id>|.navItems.<id>]*.<field>
//	                                               -> nav.<frontend>.<b>.<s>.<id>[.routes.<id>|.navItems.<id>]*.<field>
//	navigationSegments.<s>.navItems.<id>[.routes.<id>|.navItems.<id>]*.<field>
//	                                               -> navseg.<frontend>.<s>.<id>[...].<field>
//	widgetRegistry.<scope>.<module>.<import>.title -> widget.<frontend>.<scope>.<module>.<import>.title
//	widgetRegistry.<scope>.<module>.<import>.headerLink.title
//	                                               -> widget.<frontend>.<scope>.<module>.<import>.headerLink.title
//	baseWidgetLayouts.<name>.displayName           -> widgetLayout.<frontend>.<name>.displayName
//	module.defaultDocumentTitle                    -> module.<moduleName>.defaultDocumentTitle
//
// A navigation segment is keyed by the Frontend that defines it, so its items
// keep one key in every bundle that references it. Widgets have no id: <module>
// and <import> are aliasSlug of module and importName ("default" when unset).
//
// Environment keys:
//
//	bundles.<id>.<field>                           -> bundle.<id>.<field>
//	serviceCategories.<id>.title                   -> serviceCategory.<id>.title
//	serviceCategories.<id>.groups.<gid>.title      -> serviceCategory.<id>.group.<gid>.title
//
// Ids may contain dots, so a catalog key cannot be parsed back into its parts.
// Instead the key tables below are built by walking the spec, enumerating the
// same items as `feo extract` in i18n-tooling (feo-template.ts), and catalog
// keys are looked up in them.
//
// Nav items cannot be matched by position in Chrome (chrome-service moves
// generated items into the legacy navigation tree by id), so the generated
// bundles carry each item's runtime key prefix in `i18nKey`; see stampNavI18nKeys.

// localeKeyTable maps the catalog keys of one source to runtime keys.
type localeKeyTable struct {
	keys map[string]string
	// runtimeKeysByCatalog retains every runtime key reached by a catalog key.
	runtimeKeysByCatalog map[string]map[string]struct{}
	// ambiguous holds catalog keys that more than one spec item of the source
	// produces, directly or through the same runtime key; their translations
	// are treated as colliding.
	ambiguous        map[string]bool
	catalogByRuntime map[string]string
}

func newLocaleKeyTable() *localeKeyTable {
	return &localeKeyTable{
		keys:                 map[string]string{},
		runtimeKeysByCatalog: map[string]map[string]struct{}{},
		ambiguous:            map[string]bool{},
		catalogByRuntime:     map[string]string{},
	}
}

func (t *localeKeyTable) add(catalogKey, runtimeKey string) {
	if t.runtimeKeysByCatalog[catalogKey] == nil {
		t.runtimeKeysByCatalog[catalogKey] = map[string]struct{}{}
	}
	t.runtimeKeysByCatalog[catalogKey][runtimeKey] = struct{}{}

	if prev, exists := t.keys[catalogKey]; exists {
		t.ambiguous[catalogKey] = true
		if prev != runtimeKey {
			t.ambiguous[t.catalogByRuntime[prev]] = true
			// still record that this catalog key reaches runtimeKey, so a later
			// catalog key with the same runtime key is seen as colliding
			if other, exists := t.catalogByRuntime[runtimeKey]; exists {
				t.ambiguous[other] = true
			} else {
				t.catalogByRuntime[runtimeKey] = catalogKey
			}
		}
		return
	}
	if other, exists := t.catalogByRuntime[runtimeKey]; exists {
		t.ambiguous[catalogKey] = true
		t.ambiguous[other] = true
	}
	t.keys[catalogKey] = runtimeKey
	t.catalogByRuntime[runtimeKey] = catalogKey
}

// lookup returns the runtime key of a catalog key, whether the source knows
// the key at all, and whether it is ambiguous.
func (t *localeKeyTable) lookup(catalogKey string) (runtimeKey string, known, ambiguous bool) {
	runtimeKey, known = t.keys[catalogKey]
	return runtimeKey, known, t.ambiguous[catalogKey]
}

// frontendKeyTable enumerates the translatable fields of a Frontend.
func frontendKeyTable(frontend *crd.Frontend) *localeKeyTable {
	t := newLocaleKeyTable()
	for _, entry := range frontend.Spec.SearchEntries {
		if entry == nil || entry.ID == "" {
			continue
		}
		catalog := "searchEntries." + entry.ID
		runtime := "search." + fmt.Sprintf("%s-%s-%s", frontend.Name, frontend.Spec.EnvName, entry.ID)
		for _, field := range []string{"title", "description"} {
			t.add(catalog+"."+field, runtime+"."+field)
		}
		// aliases with the same slug share one key and one translation
		slugs := map[string]bool{}
		for _, alias := range entry.AltTitle {
			slug := aliasSlug(alias)
			if slug == "" || slugs[slug] {
				continue
			}
			slugs[slug] = true
			t.add(catalog+".alt_title."+slug, runtime+".alt_title."+slug)
		}
	}
	for _, tile := range frontend.Spec.ServiceTiles {
		if tile == nil || tile.ID == "" {
			continue
		}
		catalog := strings.Join([]string{"serviceTiles", tile.Section, tile.Group, tile.ID}, ".")
		runtime := strings.Join([]string{"tile", frontend.Name, tile.Section, tile.Group, tile.ID}, ".")
		for _, field := range []string{"title", "description"} {
			t.add(catalog+"."+field, runtime+"."+field)
		}
	}
	for _, segment := range frontend.Spec.BundleSegments {
		if segment == nil || segment.NavItems == nil {
			continue
		}
		addNavKeys(t, *segment.NavItems,
			"bundleSegments."+segment.BundleID+"."+segment.SegmentID+".navItems",
			navKeyPrefix(frontend.Name, segment.BundleID, segment.SegmentID))
	}
	for _, segment := range frontend.Spec.NavigationSegments {
		if segment == nil || segment.NavItems == nil {
			continue
		}
		addNavKeys(t, *segment.NavItems,
			"navigationSegments."+segment.SegmentID+".navItems",
			navSegmentKeyPrefix(frontend.Name, segment.SegmentID))
	}
	for _, widget := range frontend.Spec.WidgetRegistry {
		if widget == nil {
			continue
		}
		catalog, runtime, ok := widgetKeys(frontend.Name, widget)
		if !ok {
			continue
		}
		t.add(catalog+".title", runtime+".title")
		t.add(catalog+".headerLink.title", runtime+".headerLink.title")
	}
	for _, layout := range frontend.Spec.BaseWidgetLayouts {
		if layout == nil || layout.Name == "" {
			continue
		}
		t.add("baseWidgetLayouts."+layout.Name+".displayName", widgetLayoutKeyPrefix(frontend.Name, layout.Name)+".displayName")
	}
	if frontend.Spec.Module != nil {
		t.add(moduleTitleKey, "module."+moduleName(frontend)+".defaultDocumentTitle")
	}
	return t
}

// addNavKeys mirrors stampNavI18nKeys: items with an id, their nested routes
// and their group children get keys; segment references and items without an
// id do not.
func addNavKeys(t *localeKeyTable, navItems []crd.ChromeNavItem, catalogPrefix, runtimePrefix string) {
	for _, navItem := range navItems {
		if navItem.ID == "" || navItem.HasSegmentRef() {
			continue
		}
		catalog := catalogPrefix + "." + navItem.ID
		runtime := runtimePrefix + "." + navItem.ID
		for _, field := range []string{"title", "product"} {
			t.add(catalog+"."+field, runtime+"."+field)
		}
		if navItem.Routes != nil {
			addNavKeys(t, navItem.Routes, catalog+".routes", runtime+".routes")
		}
		if navItem.NavItems != nil {
			addNavKeys(t, navItem.NavItems, catalog+".navItems", runtime+".navItems")
		}
	}
}

// environmentKeyTable enumerates the translatable fields of a
// FrontendEnvironment.
func environmentKeyTable(env *crd.FrontendEnvironment) *localeKeyTable {
	t := newLocaleKeyTable()
	if env.Spec.Bundles != nil {
		for _, bundle := range *env.Spec.Bundles {
			for _, field := range []string{"title", "description"} {
				t.add("bundles."+bundle.ID+"."+field, "bundle."+bundle.ID+"."+field)
			}
		}
	}
	if env.Spec.ServiceCategories != nil {
		for _, category := range *env.Spec.ServiceCategories {
			t.add("serviceCategories."+category.ID+".title", "serviceCategory."+category.ID+".title")
			for _, group := range category.Groups {
				t.add("serviceCategories."+category.ID+".groups."+group.ID+".title", "serviceCategory."+category.ID+".group."+group.ID+".title")
			}
		}
	}
	return t
}

// aliasSlug must match aliasSlug in i18n-tooling's feo-template.ts: lowercase,
// each run of characters outside [a-z0-9] becomes "-", one leading and one
// trailing "-" are dropped. JavaScript lowercases U+0130 to "i" plus a
// combining dot where Go yields a plain "i"; spell that out so slugs agree.
func aliasSlug(alias string) string {
	lower := strings.ToLower(strings.ReplaceAll(alias, "\u0130", "i\u0307"))
	var b strings.Builder
	dash := false
	for _, r := range lower {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(strings.TrimPrefix(b.String(), "-"), "-")
}

// moduleName is the key of a Frontend's module in fed-modules.json.
func moduleName(frontend *crd.Frontend) string {
	if frontend.Spec.Module != nil && frontend.Spec.Module.ModuleID != "" {
		return frontend.Spec.Module.ModuleID
	}
	return localUtil.ToCamelCase(frontend.GetName())
}

func navKeyPrefix(frontendName, bundleID, segmentID string) string {
	return strings.Join([]string{"nav", frontendName, bundleID, segmentID}, ".")
}

func widgetLayoutKeyPrefix(frontendName, name string) string {
	return strings.Join([]string{"widgetLayout", frontendName, name}, ".")
}

func navSegmentKeyPrefix(frontendName, segmentID string) string {
	return strings.Join([]string{"navseg", frontendName, segmentID}, ".")
}

// widgetKeys returns the catalog and runtime key bases of a widget, matching
// the widgetRegistry keys of i18n-tooling's feo-template.ts. ok is false when
// the extractor would reject the widget (a module or importName without an
// ASCII letter or digit).
func widgetKeys(frontendName string, widget *crd.WidgetModuleFederationMetadata) (catalog, runtime string, ok bool) {
	importName := widget.ImportName
	if importName == "" {
		importName = "default"
	}
	module, imported := aliasSlug(widget.Module), aliasSlug(importName)
	if widget.Scope == "" || module == "" || imported == "" {
		return "", "", false
	}
	catalog = strings.Join([]string{"widgetRegistry", widget.Scope, module, imported}, ".")
	runtime = strings.Join([]string{"widget", frontendName, widget.Scope, module, imported}, ".")
	return catalog, runtime, true
}

// stampNavI18nKeys sets i18nKey on the nav items of one bundle or navigation
// segment, their nested routes and their group children, mirroring the catalog
// key path the extractor uses. Items without an id have no catalog key and get
// none.
func stampNavI18nKeys(navItems []crd.ChromeNavItem, prefix string) []crd.ChromeNavItem {
	res := make([]crd.ChromeNavItem, 0, len(navItems))
	for _, navItem := range navItems {
		if navItem.ID != "" && !navItem.HasSegmentRef() {
			navItem.I18nKey = prefix + "." + navItem.ID
			if navItem.Routes != nil {
				navItem.Routes = stampNavI18nKeys(navItem.Routes, navItem.I18nKey+".routes")
			}
			if navItem.NavItems != nil {
				navItem.NavItems = stampNavI18nKeys(navItem.NavItems, navItem.I18nKey+".navItems")
			}
		}
		res = append(res, navItem)
	}
	return res
}

// clearNavI18nKeys drops any i18nKey given in a Frontend spec, so only
// operator-computed keys reach the generated bundles.
func clearNavI18nKeys(navItems []crd.ChromeNavItem) []crd.ChromeNavItem {
	res := make([]crd.ChromeNavItem, 0, len(navItems))
	for _, navItem := range navItems {
		navItem.I18nKey = ""
		if navItem.Routes != nil {
			navItem.Routes = clearNavI18nKeys(navItem.Routes)
		}
		if navItem.NavItems != nil {
			navItem.NavItems = clearNavI18nKeys(navItem.NavItems)
		}
		res = append(res, navItem)
	}
	return res
}
