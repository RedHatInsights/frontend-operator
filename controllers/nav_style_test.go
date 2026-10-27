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

func TestSetupFedModulesNavStyle(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec string
		want string
	}{
		{
			name: "omitted",
			spec: `{}`,
			want: `{"testFrontend":{"manifestLocation":"/apps/test/fed-mods.json","fullProfile":false}}`,
		},
		{
			name: "empty",
			spec: `{"navStyle":""}`,
			want: `{"testFrontend":{"manifestLocation":"/apps/test/fed-mods.json","fullProfile":false}}`,
		},
		{
			name: "flat",
			spec: `{"navStyle":"flat"}`,
			want: `{"testFrontend":{"manifestLocation":"/apps/test/fed-mods.json","fullProfile":false,"navStyle":"flat"}}`,
		},
		{
			name: "future style passes through",
			spec: `{"navStyle":"future-style"}`,
			want: `{"testFrontend":{"manifestLocation":"/apps/test/fed-mods.json","fullProfile":false,"navStyle":"future-style"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frontend := crd.Frontend{ObjectMeta: metav1.ObjectMeta{Name: "test-frontend"}}
			if err := json.Unmarshal([]byte(tc.spec), &frontend.Spec); err != nil {
				t.Fatal(err)
			}
			frontend.Spec.FeoConfigEnabled = true
			frontend.Spec.Module = &crd.FedModule{
				ManifestLocation: "/apps/test/fed-mods.json",
				// The top-level spec is authoritative, including when it is empty.
				NavStyle: "stale-style",
			}
			frontends := &crd.FrontendList{Items: []crd.Frontend{frontend}}
			modules := map[string]crd.FedModule{}
			if err := setupFedModules(&crd.FrontendEnvironment{}, frontends, modules); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(modules)
			if err != nil {
				t.Fatal(err)
			}
			gomega.NewWithT(t).Expect(string(data)).To(gomega.MatchJSON(tc.want))
			if frontends.Items[0].Spec.Module.NavStyle != "stale-style" {
				t.Error("generating configuration mutated the Frontend module")
			}
		})
	}
}

var _ = ginkgo.Describe("Frontend navigation style", func() {
	ginkgo.It("preserves navStyle through the API and reconciles changes without affecting other apps", func() {
		ctx := context.Background()
		env := &crd.FrontendEnvironment{
			ObjectMeta: metav1.ObjectMeta{Name: "nav-style-env"},
			Spec: crd.FrontendEnvironmentSpec{
				SSO:      "https://sso.example.com",
				Hostname: "console.example.com",
			},
		}
		gomega.Expect(k8sClient.Create(ctx, env)).To(gomega.Succeed())
		ginkgo.DeferCleanup(func() { gomega.Expect(k8sClient.Delete(ctx, env)).To(gomega.Succeed()) })

		frontend := &crd.Frontend{
			ObjectMeta: metav1.ObjectMeta{Name: "nav-style-frontend", Namespace: "default"},
			Spec: crd.FrontendSpec{
				EnvName:          env.Name,
				FeoConfigEnabled: true,
				NavStyle:         "flat",
				Frontend:         crd.FrontendInfo{Paths: []string{"/apps/nav-style"}},
				Module: &crd.FedModule{
					ModuleID:         "navStyleApp",
					ManifestLocation: "/apps/nav-style/fed-mods.json",
				},
			},
		}
		legacy := frontend.DeepCopy()
		legacy.Name = "nav-style-legacy"
		legacy.Spec.NavStyle = ""
		legacy.Spec.Module.ModuleID = "legacyApp"
		for _, resource := range []*crd.Frontend{frontend, legacy} {
			gomega.Expect(k8sClient.Create(ctx, resource)).To(gomega.Succeed())
			ginkgo.DeferCleanup(func() { gomega.Expect(k8sClient.Delete(ctx, resource)).To(gomega.Succeed()) })
		}

		expectNavStyle := func(want string) {
			gomega.Eventually(func(g gomega.Gomega) {
				stored := &crd.Frontend{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(frontend), stored)).To(gomega.Succeed())
				g.Expect(stored.Spec.NavStyle).To(gomega.Equal(want))
				configMap := &v1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: env.Name, Namespace: frontend.Namespace}, configMap)).To(gomega.Succeed())
				var modules map[string]map[string]json.RawMessage
				g.Expect(json.Unmarshal([]byte(configMap.Data["fed-modules.json"]), &modules)).To(gomega.Succeed())
				g.Expect(modules).To(gomega.HaveKey("navStyleApp"))
				g.Expect(modules).To(gomega.HaveKey("legacyApp"))
				g.Expect(modules["legacyApp"]).NotTo(gomega.HaveKey("navStyle"))
				if want == "" {
					g.Expect(modules["navStyleApp"]).NotTo(gomega.HaveKey("navStyle"))
				} else {
					g.Expect(modules["navStyleApp"]["navStyle"]).To(gomega.Equal(json.RawMessage(`"flat"`)))
				}
			}, 20*time.Second, 100*time.Millisecond).Should(gomega.Succeed())
		}

		expectNavStyle("flat")
		for _, change := range []struct {
			patch string
			want  string
		}{
			{patch: `{"spec":{"navStyle":""}}`, want: ""},
			{patch: `{"spec":{"navStyle":"flat"}}`, want: "flat"},
			{patch: `{"spec":{"navStyle":null}}`, want: ""},
		} {
			ginkgo.By("applying " + change.patch)
			gomega.Expect(k8sClient.Patch(ctx, frontend, client.RawPatch(types.MergePatchType, []byte(change.patch)))).To(gomega.Succeed())
			expectNavStyle(change.want)
		}
	})
})
