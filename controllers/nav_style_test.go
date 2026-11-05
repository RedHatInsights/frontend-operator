package controllers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSetupBundlesNavStyle(t *testing.T) {
	for _, tc := range []struct {
		name   string
		bundle string
		want   string
	}{
		{
			name:   "omitted",
			bundle: `{"id":"lightwell","title":"Lightwell"}`,
			want:   `[{"id":"lightwell","title":"Lightwell","navItems":[]}]`,
		},
		{
			name:   "empty",
			bundle: `{"id":"lightwell","title":"Lightwell","navStyle":""}`,
			want:   `[{"id":"lightwell","title":"Lightwell","navItems":[]}]`,
		},
		{
			name:   "flat without frontend contributions",
			bundle: `{"id":"lightwell","title":"Lightwell","navStyle":"flat"}`,
			want:   `[{"id":"lightwell","title":"Lightwell","navItems":[],"navStyle":"flat"}]`,
		},
		{
			name:   "future style passes through",
			bundle: `{"id":"lightwell","title":"Lightwell","navStyle":"future-style"}`,
			want:   `[{"id":"lightwell","title":"Lightwell","navItems":[],"navStyle":"future-style"}]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var env crd.FrontendEnvironment
			if err := json.Unmarshal([]byte(`{"bundles":[`+tc.bundle+`]}`), &env.Spec); err != nil {
				t.Fatal(err)
			}
			bundles, skipped, err := setupBundlesData(&crd.FrontendList{}, env)
			g := gomega.NewWithT(t)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(skipped).To(gomega.BeEmpty())
			data, err := json.Marshal(bundles)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(string(data)).To(gomega.MatchJSON(tc.want))
		})
	}
}

func bundleNavStyleFrontend(name string, position uint) *crd.Frontend {
	return &crd.Frontend{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: crd.FrontendSpec{
			EnvName:          "bundle-nav-style-env",
			FeoConfigEnabled: true,
			Frontend:         crd.FrontendInfo{Paths: []string{"/apps/" + name}},
			Module: &crd.FedModule{
				ModuleID:         name,
				ManifestLocation: "/apps/" + name + "/fed-mods.json",
			},
			BundleSegments: []*crd.BundleSegment{{
				SegmentID: name,
				BundleID:  "lightwell",
				Position:  position,
				NavItems: &[]crd.ChromeNavItem{{
					ID: name, Title: name, Href: "/lightwell/" + name,
				}},
			}},
		},
	}
}

func TestBundleNavStyleIsIndependentOfFrontends(t *testing.T) {
	first := bundleNavStyleFrontend("first", 100)
	second := bundleNavStyleFrontend("second", 200)
	// One frontend contributes to bundles with different navigation styles.
	first.Spec.BundleSegments = append(first.Spec.BundleSegments, &crd.BundleSegment{
		SegmentID: "standard", BundleID: "insights", Position: 100,
		NavItems: &[]crd.ChromeNavItem{{Title: "Content", Href: "/insights/content"}},
	})
	env := crd.FrontendEnvironment{Spec: crd.FrontendEnvironmentSpec{
		Bundles: &[]crd.FrontendBundles{
			{ID: "lightwell", Title: "Lightwell", NavStyle: "flat"},
			{ID: "insights", Title: "Insights"},
		},
	}}
	for _, tc := range []struct {
		name      string
		frontends []crd.Frontend
		wantRefs  []string
	}{
		{name: "multiple frontends", frontends: []crd.Frontend{*first, *second}, wantRefs: []string{"first", "second"}},
		{name: "reversed order", frontends: []crd.Frontend{*second, *first}, wantRefs: []string{"first", "second"}},
		{name: "frontend removed", frontends: []crd.Frontend{*first}, wantRefs: []string{"first"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frontends := &crd.FrontendList{Items: tc.frontends}
			bundles, skipped, err := setupBundlesData(frontends, env)
			g := gomega.NewWithT(t)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(skipped).To(gomega.BeEmpty())
			g.Expect(bundles).To(gomega.HaveLen(2))
			g.Expect(bundles[0].NavStyle).To(gomega.Equal("flat"))
			g.Expect(bundles[1].NavStyle).To(gomega.BeEmpty())
			g.Expect(bundles[1].NavItems).To(gomega.HaveLen(1))
			refs := []string{}
			for _, item := range bundles[0].NavItems {
				refs = append(refs, item.FrontendRef)
			}
			g.Expect(refs).To(gomega.Equal(tc.wantRefs))
		})
	}
}

var _ = ginkgo.Describe("Bundle navigation style", func() {
	ginkgo.It("persists the environment setting and reconciles changes for all contributing frontends", func() {
		ctx := context.Background()
		env := &crd.FrontendEnvironment{
			ObjectMeta: metav1.ObjectMeta{Name: "bundle-nav-style-env"},
			Spec: crd.FrontendEnvironmentSpec{
				SSO:      "https://sso.example.com",
				Hostname: "console.example.com",
				Bundles: &[]crd.FrontendBundles{
					{ID: "lightwell", Title: "Lightwell", NavStyle: "flat"},
					{ID: "insights", Title: "Insights"},
				},
			},
		}
		gomega.Expect(k8sClient.Create(ctx, env)).To(gomega.Succeed())
		ginkgo.DeferCleanup(func() { gomega.Expect(k8sClient.Delete(ctx, env)).To(gomega.Succeed()) })

		for i, name := range []string{"nav-style-first", "nav-style-second"} {
			frontend := bundleNavStyleFrontend(name, uint(i+1)*100)
			gomega.Expect(k8sClient.Create(ctx, frontend)).To(gomega.Succeed())
			ginkgo.DeferCleanup(func() { gomega.Expect(k8sClient.Delete(ctx, frontend)).To(gomega.Succeed()) })
		}

		expectNavStyle := func(want string) {
			gomega.Eventually(func(g gomega.Gomega) {
				stored := &crd.FrontendEnvironment{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(env), stored)).To(gomega.Succeed())
				g.Expect(stored.Spec.Bundles).NotTo(gomega.BeNil())
				g.Expect((*stored.Spec.Bundles)[0].NavStyle).To(gomega.Equal(want))
				configMap := &v1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: env.Name, Namespace: "default"}, configMap)).To(gomega.Succeed())
				var bundles []map[string]json.RawMessage
				g.Expect(json.Unmarshal([]byte(configMap.Data["bundles.json"]), &bundles)).To(gomega.Succeed())
				g.Expect(bundles).To(gomega.HaveLen(2))
				g.Expect(bundles[0]["id"]).To(gomega.Equal(json.RawMessage(`"lightwell"`)))
				g.Expect(bundles[1]).NotTo(gomega.HaveKey("navStyle"))
				var navItems []crd.ChromeNavItem
				g.Expect(json.Unmarshal(bundles[0]["navItems"], &navItems)).To(gomega.Succeed())
				g.Expect(navItems).To(gomega.HaveLen(2))
				g.Expect(navItems[0].FrontendRef).To(gomega.Equal("nav-style-first"))
				g.Expect(navItems[1].FrontendRef).To(gomega.Equal("nav-style-second"))
				if want == "" {
					g.Expect(bundles[0]).NotTo(gomega.HaveKey("navStyle"))
				} else {
					g.Expect(bundles[0]["navStyle"]).To(gomega.Equal(json.RawMessage(`"flat"`)))
				}
				var modules map[string]map[string]json.RawMessage
				g.Expect(json.Unmarshal([]byte(configMap.Data["fed-modules.json"]), &modules)).To(gomega.Succeed())
				g.Expect(modules).To(gomega.HaveLen(2))
				for _, module := range modules {
					g.Expect(module).NotTo(gomega.HaveKey("navStyle"))
				}
			}, 20*time.Second, 100*time.Millisecond).Should(gomega.Succeed())
		}

		expectNavStyle("flat")
		for _, change := range []struct {
			patch string
			want  string
		}{
			{patch: `[{"op":"replace","path":"/spec/bundles/0/navStyle","value":""}]`, want: ""},
			{patch: `[{"op":"add","path":"/spec/bundles/0/navStyle","value":"flat"}]`, want: "flat"},
			{patch: `[{"op":"remove","path":"/spec/bundles/0/navStyle"}]`, want: ""},
		} {
			ginkgo.By("updating the FrontendEnvironment with " + change.patch)
			gomega.Expect(k8sClient.Patch(ctx, env, client.RawPatch(types.JSONPatchType, []byte(change.patch)))).To(gomega.Succeed())
			expectNavStyle(change.want)
		}
	})
})
