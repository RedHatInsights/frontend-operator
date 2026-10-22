package controllers

import (
	"maps"
	"slices"
	"testing"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func localeTestFrontend(name string, feo bool, locales crd.LocaleCatalogs) crd.Frontend {
	return crd.Frontend{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "console"},
		Spec: crd.FrontendSpec{
			EnvName:          "stage",
			FeoConfigEnabled: feo,
			Module:           &crd.FedModule{},
			Locales:          locales,
		},
	}
}

// withSearch adds search entries with the given ids to a test Frontend.
func withSearch(fe crd.Frontend, ids ...string) crd.Frontend {
	for _, id := range ids {
		fe.Spec.SearchEntries = append(fe.Spec.SearchEntries, &crd.SearchEntry{ID: id, Title: id, Href: "/" + id})
	}
	return fe
}

func checkKeyTable(t *testing.T, table *localeKeyTable, want map[string]string) {
	t.Helper()
	for catalogKey, runtimeKey := range want {
		got, known, ambiguous := table.lookup(catalogKey)
		if !known || ambiguous || got != runtimeKey {
			t.Errorf("%s: got %q (known %v, ambiguous %v), want %q", catalogKey, got, known, ambiguous, runtimeKey)
		}
	}
}

func TestFrontendKeyTable(t *testing.T) {
	fe := localeTestFrontend("insights-rbac", true, nil)
	fe = withSearch(fe, "users")
	fe.Spec.SearchEntries[0].AltTitle = []string{"People", "User accounts"}
	fe.Spec.ServiceTiles = []*crd.ServiceTile{{Section: "security", Group: "access", ID: "user-access"}}
	navItems := []crd.ChromeNavItem{
		{ID: "users"},
		{ID: "access", Expandable: true, Routes: []crd.ChromeNavItem{{ID: "roles", Routes: []crd.ChromeNavItem{{ID: "deep"}}}}},
		{ID: "settings", GroupID: "settings", NavItems: []crd.ChromeNavItem{{ID: "grouped"}}},
		{Title: "no id"},
		{ID: "ref", SegmentRef: &crd.SegmentRef{SegmentID: "s"}},
	}
	fe.Spec.BundleSegments = []*crd.BundleSegment{{BundleID: "iam", SegmentID: "ua", NavItems: &navItems}}

	table := frontendKeyTable(&fe)
	checkKeyTable(t, table, map[string]string{
		"searchEntries.users.title":                                            "search.insights-rbac-stage-users.title",
		"searchEntries.users.description":                                      "search.insights-rbac-stage-users.description",
		"searchEntries.users.alt_title.people":                                 "search.insights-rbac-stage-users.alt_title.people",
		"searchEntries.users.alt_title.user-accounts":                          "search.insights-rbac-stage-users.alt_title.user-accounts",
		"serviceTiles.security.access.user-access.title":                       "tile.insights-rbac.security.access.user-access.title",
		"bundleSegments.iam.ua.navItems.users.title":                           "nav.insights-rbac.iam.ua.users.title",
		"bundleSegments.iam.ua.navItems.access.routes.roles.product":           "nav.insights-rbac.iam.ua.access.routes.roles.product",
		"bundleSegments.iam.ua.navItems.access.routes.roles.routes.deep.title": "nav.insights-rbac.iam.ua.access.routes.roles.routes.deep.title",
		"bundleSegments.iam.ua.navItems.settings.title":                        "nav.insights-rbac.iam.ua.settings.title",
		"bundleSegments.iam.ua.navItems.settings.navItems.grouped.title":       "nav.insights-rbac.iam.ua.settings.navItems.grouped.title",
		// module name falls back to the camelCase Frontend name without a moduleID
		"module.defaultDocumentTitle": "module.insightsRbac.defaultDocumentTitle",
	})
	for _, catalogKey := range []string{
		"searchEntries.users.href",
		"searchEntries.users.alt_title",
		"searchEntries.nobody.title",
		"bundleSegments.iam.ua.navItems.users.href",
		"bundleSegments.iam.ua.navItems.ref.title",
		"module.title",
	} {
		if got, known, _ := table.lookup(catalogKey); known {
			t.Errorf("%s: want unknown, got %q", catalogKey, got)
		}
	}

	noModule := localeTestFrontend("rbac", true, nil)
	noModule.Spec.Module = nil
	if _, known, _ := frontendKeyTable(&noModule).lookup("module.defaultDocumentTitle"); known {
		t.Error("module key known for a Frontend without a module")
	}
}

// Navigation segments are keyed by their owning Frontend; widgets, which have
// no id, by scope and the slugs of module and importName, as feo extract does.
func TestFrontendKeyTableSegmentsWidgetsLayouts(t *testing.T) {
	fe := localeTestFrontend("vulnerability", true, nil)
	segmentItems := []crd.ChromeNavItem{
		{ID: "vulnerability", Expandable: true, Routes: []crd.ChromeNavItem{{ID: "cves"}}},
		{ID: "group", GroupID: "g", NavItems: []crd.ChromeNavItem{{ID: "child"}}},
		{ID: "ref", SegmentRef: &crd.SegmentRef{FrontendName: "other", SegmentID: "s"}},
	}
	fe.Spec.NavigationSegments = []*crd.NavigationSegment{{SegmentID: "vulnerability-nav", NavItems: &segmentItems}}
	fe.Spec.WidgetRegistry = []*crd.WidgetModuleFederationMetadata{
		{Scope: "landing", Module: "./OpenShiftWidget"},
		{Scope: "landing", Module: "./OpenShiftWidget", ImportName: "AIWidget"},
		{Scope: "landing", Module: "./--"},
	}
	fe.Spec.BaseWidgetLayouts = []*crd.BaseWidgetDashboardTemplate{{Name: "landingPage"}}

	table := frontendKeyTable(&fe)
	checkKeyTable(t, table, map[string]string{
		"navigationSegments.vulnerability-nav.navItems.vulnerability.title":             "navseg.vulnerability.vulnerability-nav.vulnerability.title",
		"navigationSegments.vulnerability-nav.navItems.vulnerability.routes.cves.title": "navseg.vulnerability.vulnerability-nav.vulnerability.routes.cves.title",
		"navigationSegments.vulnerability-nav.navItems.group.navItems.child.product":    "navseg.vulnerability.vulnerability-nav.group.navItems.child.product",
		"widgetRegistry.landing.openshiftwidget.default.title":                          "widget.vulnerability.landing.openshiftwidget.default.title",
		"widgetRegistry.landing.openshiftwidget.default.headerLink.title":               "widget.vulnerability.landing.openshiftwidget.default.headerLink.title",
		"widgetRegistry.landing.openshiftwidget.aiwidget.title":                         "widget.vulnerability.landing.openshiftwidget.aiwidget.title",
		"baseWidgetLayouts.landingPage.displayName":                                     "widgetLayout.vulnerability.landingPage.displayName",
	})
	for _, catalogKey := range []string{
		"navigationSegments.vulnerability-nav.navItems.ref.title",
		"widgetRegistry.landing..default.title",
		"baseWidgetLayouts.landingPage.name",
	} {
		if got, known, _ := table.lookup(catalogKey); known {
			t.Errorf("%s: want unknown, got %q", catalogKey, got)
		}
	}
}

// Ids may contain dots: keys come from walking the spec, never from parsing.
func TestFrontendKeyTableDottedIDs(t *testing.T) {
	ros := withSearch(localeTestFrontend("ros", true, nil), "RHEL.ros")
	checkKeyTable(t, frontendKeyTable(&ros), map[string]string{
		"searchEntries.RHEL.ros.title":       "search.ros-stage-RHEL.ros.title",
		"searchEntries.RHEL.ros.description": "search.ros-stage-RHEL.ros.description",
	})

	hcs := localeTestFrontend("hcs", true, nil)
	navItems := []crd.ChromeNavItem{{ID: "hybrid-committed-spend.nav", Expandable: true, Routes: []crd.ChromeNavItem{
		{ID: "hybrid-committed-spend.overview"}, {ID: "hybrid-committed-spend.details"},
	}}}
	hcs.Spec.BundleSegments = []*crd.BundleSegment{{BundleID: "insights", SegmentID: "hcs", NavItems: &navItems}}
	checkKeyTable(t, frontendKeyTable(&hcs), map[string]string{
		"bundleSegments.insights.hcs.navItems.hybrid-committed-spend.nav.title":                                        "nav.hcs.insights.hcs.hybrid-committed-spend.nav.title",
		"bundleSegments.insights.hcs.navItems.hybrid-committed-spend.nav.routes.hybrid-committed-spend.overview.title": "nav.hcs.insights.hcs.hybrid-committed-spend.nav.routes.hybrid-committed-spend.overview.title",
		"bundleSegments.insights.hcs.navItems.hybrid-committed-spend.nav.routes.hybrid-committed-spend.details.title":  "nav.hcs.insights.hcs.hybrid-committed-spend.nav.routes.hybrid-committed-spend.details.title",
	})
	// the stamps agree with the table
	stamped := stampNavI18nKeys(navItems, navKeyPrefix("hcs", "insights", "hcs"))
	if got := stamped[0].Routes[1].I18nKey; got != "nav.hcs.insights.hcs.hybrid-committed-spend.nav.routes.hybrid-committed-spend.details" {
		t.Errorf("stamp: got %q", got)
	}
}

// Two spec items of one Frontend that produce the same catalog key make it
// ambiguous: its translation is left out and reported, never overwritten.
func TestFrontendKeyTablePathologicalIDs(t *testing.T) {
	// search entry "a.alt_title" with field title, next to entry "a" with alias "Title"
	fe := withSearch(localeTestFrontend("app", true, nil), "a", "a.alt_title")
	fe.Spec.SearchEntries[0].AltTitle = []string{"Title"}
	// nav id "x.routes.y" next to route y under x
	navItems := []crd.ChromeNavItem{
		{ID: "x", Expandable: true, Routes: []crd.ChromeNavItem{{ID: "y"}}},
		{ID: "x.routes.y"},
	}
	fe.Spec.BundleSegments = []*crd.BundleSegment{{BundleID: "b", SegmentID: "s", NavItems: &navItems}}
	fe.Spec.Locales = crd.LocaleCatalogs{"fr": {
		"searchEntries.a.alt_title.title":              "Titre",
		"bundleSegments.b.s.navItems.x.routes.y.title": "Y",
		"searchEntries.a.title":                        "A",
	}}

	table := frontendKeyTable(&fe)
	for _, catalogKey := range []string{"searchEntries.a.alt_title.title", "bundleSegments.b.s.navItems.x.routes.y.title"} {
		if _, known, ambiguous := table.lookup(catalogKey); !known || !ambiguous {
			t.Errorf("%s: want known and ambiguous, got known %v, ambiguous %v", catalogKey, known, ambiguous)
		}
	}

	built := buildLocaleMessages("fr", &crd.FrontendList{Items: []crd.Frontend{fe}}, &crd.FrontendEnvironment{ObjectMeta: metav1.ObjectMeta{Name: "stage"}})
	if len(built.Messages) != 1 || built.Messages["search.app-stage-a.title"] != "A" {
		t.Errorf("messages: got %v", built.Messages)
	}
	if !slices.Equal(built.Collisions, []string{"nav.app.b.s.x.routes.y.title", "search.app-stage-a.alt_title.title"}) {
		t.Errorf("collisions: got %v", built.Collisions)
	}
	if !slices.Equal(built.CollidingSources, []string{"Frontend/console/app"}) {
		t.Errorf("colliding sources: got %v", built.CollidingSources)
	}
}

func TestAliasSlug(t *testing.T) {
	cases := map[string]string{
		"People":                    "people",
		"User accounts":             "user-accounts",
		"  --Red Hat® OpenShift!  ": "red-hat-openshift",
		"ACS":                       "acs",
		"Register RHEL":             "register-rhel",
		"日本語":                       "",
		"İstanbul":                  "i-stanbul", // JavaScript lowercases U+0130 to i plus a combining dot
		"a__b":                      "a-b",
		// expected values computed with the tooling's JavaScript aliasSlug
		"Ⅻ roman":  "roman",
		"K kelvin": "k-kelvin",
		"ß straße": "stra-e",
		"Σίσυφος":  "",
		"x😀y":      "x-y",
	}
	for alias, want := range cases {
		if got := aliasSlug(alias); got != want {
			t.Errorf("%q: got %q, want %q", alias, got, want)
		}
	}
}

// Aliases with the same slug (acs/ACS, hosts/hosts) share one key and one
// translation instead of failing.
func TestFrontendKeyTableSameSlugAliases(t *testing.T) {
	fe := withSearch(localeTestFrontend("acs", true, nil), "acs", "hosts")
	fe.Spec.SearchEntries[0].AltTitle = []string{"acs", "ACS"}
	fe.Spec.SearchEntries[1].AltTitle = []string{"hosts", "hosts"}
	checkKeyTable(t, frontendKeyTable(&fe), map[string]string{
		"searchEntries.acs.alt_title.acs":     "search.acs-stage-acs.alt_title.acs",
		"searchEntries.hosts.alt_title.hosts": "search.acs-stage-hosts.alt_title.hosts",
	})
}

func TestEnvironmentKeyTable(t *testing.T) {
	env := &crd.FrontendEnvironment{Spec: crd.FrontendEnvironmentSpec{
		Bundles:           &[]crd.FrontendBundles{{ID: "iam"}, {ID: "rhel.edge"}},
		ServiceCategories: &[]crd.FrontendServiceCategory{{ID: "security", Groups: []crd.FrontendServiceCategoryGroup{{ID: "a"}}}},
	}}
	table := environmentKeyTable(env)
	checkKeyTable(t, table, map[string]string{
		"bundles.iam.title":                         "bundle.iam.title",
		"bundles.iam.description":                   "bundle.iam.description",
		"bundles.rhel.edge.title":                   "bundle.rhel.edge.title",
		"serviceCategories.security.title":          "serviceCategory.security.title",
		"serviceCategories.security.groups.a.title": "serviceCategory.security.group.a.title",
	})
	for _, catalogKey := range []string{"bundles.iam", "bundles.other.title", "serviceCategories.security.description", "serviceCategories.security.groups.a.description"} {
		if got, known, _ := table.lookup(catalogKey); known {
			t.Errorf("%s: want unknown, got %q", catalogKey, got)
		}
	}
}

// A catalog key produced twice with different runtime keys must not hide that
// one of those runtime keys is also produced by a third catalog key: category
// "a.groups.b" and group b of category "a" share a catalog key, and group b of
// category "a" shares its runtime key with category "a.group.b".
func TestEnvironmentKeyTableDuplicateCatalogKeyHidesNoCollision(t *testing.T) {
	a := crd.FrontendServiceCategory{ID: "a", Groups: []crd.FrontendServiceCategoryGroup{{ID: "b"}}}
	aGroupsB := crd.FrontendServiceCategory{ID: "a.groups.b"}
	aGroupB := crd.FrontendServiceCategory{ID: "a.group.b"}
	for name, categories := range map[string][]crd.FrontendServiceCategory{
		"duplicate first": {aGroupsB, a, aGroupB},
		"duplicate last":  {aGroupB, a, aGroupsB},
		"shared first":    {a, aGroupB, aGroupsB},
	} {
		t.Run(name, func(t *testing.T) {
			table := environmentKeyTable(&crd.FrontendEnvironment{Spec: crd.FrontendEnvironmentSpec{ServiceCategories: &categories}})
			for _, catalogKey := range []string{"serviceCategories.a.groups.b.title", "serviceCategories.a.group.b.title"} {
				if _, known, ambiguous := table.lookup(catalogKey); !known || !ambiguous {
					t.Errorf("%s: want known and ambiguous, got known=%v ambiguous=%v", catalogKey, known, ambiguous)
				}
			}
			if _, _, ambiguous := table.lookup("serviceCategories.a.title"); ambiguous {
				t.Error("serviceCategories.a.title: unrelated key marked ambiguous")
			}

			env := &crd.FrontendEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: "stage"},
				Spec: crd.FrontendEnvironmentSpec{
					ServiceCategories: &categories,
					Locales:           crd.LocaleCatalogs{"fr": {"serviceCategories.a.group.b.title": "B", "serviceCategories.a.title": "A"}},
				},
			}
			built := buildLocaleMessages("fr", &crd.FrontendList{}, env)
			if want := map[string]string{"serviceCategory.a.title": "A"}; !maps.Equal(built.Messages, want) {
				t.Errorf("messages: got %v, want %v", built.Messages, want)
			}
		})
	}
}

func TestStampNavI18nKeysIgnoresSpecValues(t *testing.T) {
	items := clearNavI18nKeys([]crd.ChromeNavItem{
		{ID: "a", I18nKey: "spoofed", Routes: []crd.ChromeNavItem{{ID: "b", I18nKey: "spoofed"}}},
		{I18nKey: "spoofed"},
		{SegmentRef: &crd.SegmentRef{SegmentID: "s"}, ID: "ref"},
		{ID: "g", GroupID: "g", NavItems: []crd.ChromeNavItem{{ID: "c", I18nKey: "spoofed"}}},
	})
	stamped := stampNavI18nKeys(items, "nav.fe.b.s")
	if got := stamped[3].NavItems[0].I18nKey; got != "nav.fe.b.s.g.navItems.c" {
		t.Errorf("group child: got %q", got)
	}
	if got := stamped[0].I18nKey; got != "nav.fe.b.s.a" {
		t.Errorf("item: got %q", got)
	}
	if got := stamped[0].Routes[0].I18nKey; got != "nav.fe.b.s.a.routes.b" {
		t.Errorf("route: got %q", got)
	}
	if stamped[1].I18nKey != "" || stamped[2].I18nKey != "" {
		t.Errorf("item without id or with segmentRef got a key: %q, %q", stamped[1].I18nKey, stamped[2].I18nKey)
	}
}

func TestBuildLocaleMessages(t *testing.T) {
	env := &crd.FrontendEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "stage"},
		Spec: crd.FrontendEnvironmentSpec{
			Bundles: &[]crd.FrontendBundles{{ID: "iam"}},
			Locales: crd.LocaleCatalogs{"fr": {"bundles.iam.title": "IAM", "bundles.iam.icon": "x"}},
		},
	}
	feList := &crd.FrontendList{Items: []crd.Frontend{
		// two Frontends sharing a moduleID produce the same module key
		localeTestFrontend("b", true, crd.LocaleCatalogs{"fr": {"module.defaultDocumentTitle": "B"}}),
		withSearch(localeTestFrontend("a", true, crd.LocaleCatalogs{"fr": {"module.defaultDocumentTitle": "A", "searchEntries.x.title": "X"}}), "x"),
		// not part of the English output, so not part of the translations either
		withSearch(localeTestFrontend("off", false, crd.LocaleCatalogs{"fr": {"searchEntries.y.title": "Y"}}), "y"),
		// chrome contributes its document title even without feoConfigEnabled
		withSearch(localeTestFrontend("chrome", false, crd.LocaleCatalogs{"fr": {"module.defaultDocumentTitle": "Console", "searchEntries.z.title": "Z"}}), "z"),
	}}
	for i := range feList.Items[:2] {
		feList.Items[i].Spec.Module.ModuleID = "shared"
	}

	built := buildLocaleMessages("fr", feList, env)
	want := map[string]string{
		"bundle.iam.title":                   "IAM",
		"search.a-stage-x.title":             "X",
		"module.chrome.defaultDocumentTitle": "Console",
	}
	if len(built.Messages) != len(want) {
		t.Errorf("messages: got %v, want %v", built.Messages, want)
	}
	for k, v := range want {
		if built.Messages[k] != v {
			t.Errorf("%s: got %q, want %q", k, built.Messages[k], v)
		}
	}
	if !slices.Equal(built.Collisions, []string{"module.shared.defaultDocumentTitle"}) {
		t.Errorf("collisions: got %v", built.Collisions)
	}
	if !slices.Equal(built.Unknown, []string{"FrontendEnvironment/stage: bundles.iam.icon"}) {
		t.Errorf("unknown: got %v", built.Unknown)
	}
	if want := []string{"Frontend/console/chrome: searchEntries.z.title", "Frontend/console/off: searchEntries.y.title"}; !slices.Equal(built.Skipped, want) {
		t.Errorf("skipped: got %v, want %v", built.Skipped, want)
	}

	// deterministic regardless of list order
	slices.Reverse(feList.Items)
	again := buildLocaleMessages("fr", feList, env)
	if len(again.Messages) != len(built.Messages) || !slices.Equal(again.Collisions, built.Collisions) {
		t.Errorf("output depends on Frontend order: %v vs %v", again, built)
	}
}

// Two same-name Frontends in different namespaces produce the same search key.
// A translation from only one of them would also apply to the other's entry,
// so it is a collision even though only one source translates it.
func TestBuildLocaleMessagesCollisionWithoutTranslation(t *testing.T) {
	env := &crd.FrontendEnvironment{ObjectMeta: metav1.ObjectMeta{Name: "stage"}}
	translated := withSearch(localeTestFrontend("app", true, crd.LocaleCatalogs{"fr": {"searchEntries.x.title": "X", "searchEntries.y.title": "Y"}}), "x", "y")
	translated.Namespace = "ns1"
	untranslated := withSearch(localeTestFrontend("app", true, nil), "x")
	untranslated.Namespace = "ns2"
	// a Frontend outside the English output owns no keys
	off := withSearch(localeTestFrontend("app", false, nil), "y")
	off.Namespace = "ns3"

	built := buildLocaleMessages("fr", &crd.FrontendList{Items: []crd.Frontend{untranslated, translated, off}}, env)
	if want := map[string]string{"search.app-stage-y.title": "Y"}; !maps.Equal(built.Messages, want) {
		t.Errorf("messages: got %v, want %v", built.Messages, want)
	}
	if !slices.Equal(built.Collisions, []string{"search.app-stage-x.title"}) {
		t.Errorf("collisions: got %v", built.Collisions)
	}
	if !slices.Equal(built.CollidingSources, []string{"Frontend/ns1/app", "Frontend/ns2/app"}) {
		t.Errorf("colliding sources: got %v", built.CollidingSources)
	}
}

func TestBuildLocaleMessagesDuplicateCatalogKeyTracksEveryRuntimeKey(t *testing.T) {
	const catalogKey = "bundleSegments.b.s.navItems.x.navItems.y.title"
	first := localeTestFrontend("app", true, crd.LocaleCatalogs{"fr": {catalogKey: "Y"}})
	first.Namespace = "ns1"
	first.Spec.BundleSegments = []*crd.BundleSegment{
		{BundleID: "b", SegmentID: "s.navItems.x", NavItems: &[]crd.ChromeNavItem{{ID: "y"}}},
		{BundleID: "b", SegmentID: "s", NavItems: &[]crd.ChromeNavItem{{ID: "x.navItems.y"}}},
	}
	second := localeTestFrontend("app", true, crd.LocaleCatalogs{"fr": {catalogKey: "Y"}})
	second.Namespace = "ns2"
	second.Spec.BundleSegments = []*crd.BundleSegment{
		{BundleID: "b", SegmentID: "s", NavItems: &[]crd.ChromeNavItem{{ID: "x.navItems.y"}}},
	}

	built := buildLocaleMessages("fr", &crd.FrontendList{Items: []crd.Frontend{first, second}}, &crd.FrontendEnvironment{ObjectMeta: metav1.ObjectMeta{Name: "stage"}})
	wantCollisions := []string{"nav.app.b.s.navItems.x.y.title", "nav.app.b.s.x.navItems.y.title"}
	if !slices.Equal(built.Collisions, wantCollisions) {
		t.Errorf("collisions: got %v, want %v", built.Collisions, wantCollisions)
	}
	if len(built.Messages) != 0 {
		t.Errorf("ambiguous translations were published: %v", built.Messages)
	}
	if want := []string{"Frontend/ns1/app", "Frontend/ns2/app"}; !slices.Equal(built.CollidingSources, want) {
		t.Errorf("colliding sources: got %v, want %v", built.CollidingSources, want)
	}
}

func TestLocalesJSONEntries(t *testing.T) {
	enabled := []crd.EnabledLocale{
		{ID: "en", Name: "English"},
		{ID: "fr", Name: "Français", Default: true},
		{ID: "ja", Name: "日本語"},
	}

	all := localesJSONEntries(enabled, map[string]bool{"en": true, "fr": true, "ja": true})
	want := []localeEntry{
		{ID: "en", Name: "English"},
		{ID: "fr", Name: "Français", Default: true, File: "messages.fr.json"},
		{ID: "ja", Name: "日本語", File: "messages.ja.json"},
	}
	if !slices.Equal(all, want) {
		t.Errorf("all advertised: got %v, want %v", all, want)
	}

	// the default is not advertised: English takes over
	partial := localesJSONEntries(enabled, map[string]bool{"en": true, "ja": true})
	want = []localeEntry{
		{ID: "en", Name: "English", Default: true},
		{ID: "ja", Name: "日本語", File: "messages.ja.json"},
	}
	if !slices.Equal(partial, want) {
		t.Errorf("default not advertised: got %v, want %v", partial, want)
	}
}

func TestMessagesConfigMapName(t *testing.T) {
	if got := messagesConfigMapName("feo-context-cfg-beta", "zh-CN"); got != "feo-context-cfg-beta-messages-zh-cn" {
		t.Errorf("got %q", got)
	}
}

// Inserting items before existing ones changes no existing runtime key or
// i18nKey: keys come from ids, never from positions.
func TestLocaleKeysInsertionStability(t *testing.T) {
	env := crd.FrontendEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "stage"},
		Spec: crd.FrontendEnvironmentSpec{
			EnabledLocales: []crd.EnabledLocale{{ID: "en", Name: "English", Default: true}, {ID: "fr", Name: "Français"}},
			Bundles:        &[]crd.FrontendBundles{{ID: "iam", Title: "IAM"}},
		},
	}
	frontend := func(insert bool) crd.Frontend {
		routes := []crd.ChromeNavItem{{ID: "roles", Title: "Roles", Href: "/roles"}}
		navItems := []crd.ChromeNavItem{
			{ID: "users", Title: "Users", Href: "/users"},
			{ID: "access", Title: "Access", Expandable: true, Routes: routes},
		}
		search := []*crd.SearchEntry{{ID: "users", Title: "Users", Href: "/users"}}
		if insert {
			navItems[1].Routes = append([]crd.ChromeNavItem{{ID: "new-route", Title: "New", Href: "/new"}}, routes...)
			navItems = append([]crd.ChromeNavItem{{ID: "new-item", Title: "New", Href: "/new"}}, navItems...)
			search = append([]*crd.SearchEntry{{ID: "new-entry", Title: "New", Href: "/new"}}, search...)
		}
		fe := localeTestFrontend("rbac", true, crd.LocaleCatalogs{"fr": {
			"searchEntries.users.title":                                "Utilisateurs",
			"bundleSegments.iam.ua.navItems.users.title":               "Utilisateurs",
			"bundleSegments.iam.ua.navItems.access.routes.roles.title": "Rôles",
		}})
		fe.Spec.SearchEntries = search
		fe.Spec.BundleSegments = []*crd.BundleSegment{{SegmentID: "ua", BundleID: "iam", NavItems: &navItems}}
		return fe
	}

	keys := func(fe crd.Frontend) (map[string]string, map[string]string) {
		feList := &crd.FrontendList{Items: []crd.Frontend{fe}}
		bundles, _, err := setupBundlesData(feList, env)
		if err != nil {
			t.Fatal(err)
		}
		stamps := map[string]string{}
		var walk func([]crd.ChromeNavItem)
		walk = func(items []crd.ChromeNavItem) {
			for _, item := range items {
				stamps[item.ID] = item.I18nKey
				walk(item.Routes)
			}
		}
		walk(bundles[0].NavItems)
		return stamps, buildLocaleMessages("fr", feList, &env).Messages
	}

	stampsBefore, messagesBefore := keys(frontend(false))
	stampsAfter, messagesAfter := keys(frontend(true))
	for id, key := range stampsBefore {
		if stampsAfter[id] != key {
			t.Errorf("i18nKey of %s changed after insertion: %q -> %q", id, key, stampsAfter[id])
		}
	}
	if len(messagesBefore) != 3 {
		t.Errorf("messages: got %v", messagesBefore)
	}
	for k, v := range messagesBefore {
		if messagesAfter[k] != v {
			t.Errorf("message %s changed after insertion: %q -> %q", k, v, messagesAfter[k])
		}
	}
	// the runtime keys line up with the stamps
	for id, key := range map[string]string{"users": "nav.rbac.iam.ua.users", "roles": "nav.rbac.iam.ua.access.routes.roles"} {
		if stampsAfter[id] != key || messagesAfter[key+".title"] == "" {
			t.Errorf("%s: stamp %q, message %q", id, stampsAfter[id], messagesAfter[key+".title"])
		}
	}
}

// Widgets and layouts carry their runtime key prefix, so Chrome never has to
// rebuild the slug keys; spec values are ignored and nothing is stamped
// without locales.
func TestStampWidgetAndLayoutI18nKeys(t *testing.T) {
	fe := localeTestFrontend("rbac", true, nil)
	fe.Spec.WidgetRegistry = []*crd.WidgetModuleFederationMetadata{
		{Scope: "landing", Module: "./RbacWidget", I18nKey: "spoofed"},
		{Scope: "landing", Module: "./--"},
	}
	fe.Spec.BaseWidgetLayouts = []*crd.BaseWidgetDashboardTemplate{{Name: "rbacLanding", I18nKey: "spoofed"}}
	feList := &crd.FrontendList{Items: []crd.Frontend{fe}}

	widgets := setupWidgetRegistry(feList, true)
	keys := map[string]string{}
	for _, widget := range widgets {
		keys[widget.Module] = widget.I18nKey
	}
	if keys["./RbacWidget"] != "widget.rbac.landing.rbacwidget.default" || keys["./--"] != "" {
		t.Errorf("widget keys: %v", keys)
	}
	layouts := setupBaseWidgetDashboardTemplates(feList, true)
	if len(layouts) != 1 || layouts[0].I18nKey != "widgetLayout.rbac.rbacLanding" || layouts[0].Name != "rbac-rbacLanding" {
		t.Errorf("layout: %+v", layouts)
	}
	if fe.Spec.WidgetRegistry[0].I18nKey != "spoofed" || fe.Spec.WidgetRegistry[0].FrontendRef != "" {
		t.Error("the Frontend spec was modified")
	}

	for _, widget := range setupWidgetRegistry(feList, false) {
		if widget.I18nKey != "" {
			t.Errorf("unlocalized widget got %q", widget.I18nKey)
		}
	}
	if got := setupBaseWidgetDashboardTemplates(feList, false)[0].I18nKey; got != "" {
		t.Errorf("unlocalized layout got %q", got)
	}
}
