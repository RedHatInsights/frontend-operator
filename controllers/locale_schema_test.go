package controllers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Schema tests for the localization fields (RHCLOUD-52083):
// Frontend.spec.locales, FrontendEnvironment.spec.enabledLocales and
// FrontendEnvironment.spec.locales. Rejection cases use server-side dry-run so
// the API server validates the CRD schema without persisting (or reconciling)
// anything.
var _ = ginkgo.Describe("Localization API schema", func() {
	ctx := context.Background()

	tier1 := crd.LocaleCatalogs{
		"fr":    {"searchEntries.rbac-roles.title": "Rôles", "bundles.rhel.title": "Red Hat Enterprise Linux"},
		"ko":    {"searchEntries.rbac-roles.title": "역할"},
		"ja":    {"searchEntries.rbac-roles.title": "ロール"},
		"zh-CN": {"searchEntries.rbac-roles.title": "角色"},
	}

	newFrontend := func(name string) *crd.Frontend {
		return &crd.Frontend{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: crd.FrontendSpec{
				EnvName:        "locale-schema-env",
				Title:          name,
				DeploymentRepo: "https://github.com/RedHatInsights/" + name,
				Frontend:       crd.FrontendInfo{Paths: []string{"/apps/" + name}},
			},
		}
	}

	newEnv := func(name string) *crd.FrontendEnvironment {
		return &crd.FrontendEnvironment{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       crd.FrontendEnvironmentSpec{SSO: "https://sso.example.com/auth"},
		}
	}

	// unstructuredCR builds a CR from raw spec fields so malformed values that
	// the typed structs cannot express (non-string messages, wrong nesting) can
	// still be submitted.
	unstructuredCR := func(kind, name, namespace string, spec map[string]any) *unstructured.Unstructured {
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "cloud.redhat.com/v1alpha1",
			"kind":       kind,
			"metadata":   map[string]any{"name": name},
			"spec":       spec,
		}}
		if namespace != "" {
			u.SetNamespace(namespace)
		}
		return u
	}

	dryRun := func(obj client.Object) error {
		return k8sClient.Create(ctx, obj, client.DryRunAll)
	}

	ginkgo.Context("Frontend.spec.locales", func() {
		ginkgo.It("accepts tier-1 locale maps", func() {
			fe := newFrontend("locale-ok")
			fe.Spec.Locales = tier1
			gomega.Expect(dryRun(fe)).To(gomega.Succeed())
		})

		ginkgo.It("accepts script and region subtags in canonical casing", func() {
			fe := newFrontend("locale-subtags")
			fe.Spec.Locales = crd.LocaleCatalogs{
				"pt-BR":   {"a.b": "x"},
				"zh-Hant": {"a.b": "x"},
				"es-419":  {"a.b": "x"},
			}
			gomega.Expect(dryRun(fe)).To(gomega.Succeed())
		})

		ginkgo.It("rejects English as a translation", func() {
			fe := newFrontend("locale-en")
			fe.Spec.Locales = crd.LocaleCatalogs{"en": {"a.b": "x"}}
			gomega.Expect(dryRun(fe)).To(gomega.MatchError(gomega.ContainSubstring("English is the default locale")))
		})

		ginkgo.DescribeTable("rejects malformed locale identifiers",
			func(id string) {
				fe := newFrontend("locale-bad-id")
				fe.Spec.Locales = crd.LocaleCatalogs{id: {"a.b": "x"}}
				gomega.Expect(dryRun(fe)).To(gomega.MatchError(gomega.ContainSubstring("canonical BCP 47 casing")))
			},
			ginkgo.Entry("wrong region casing", "zh-cn"),
			ginkgo.Entry("underscore separator", "zh_CN"),
			ginkgo.Entry("uppercase language", "FR"),
			ginkgo.Entry("empty", ""),
			ginkgo.Entry("path traversal", "../fr"),
			ginkgo.Entry("too long", "abcdefghijklmnopqrstuvwxyz"),
		)

		ginkgo.DescribeTable("rejects malformed message keys",
			func(key string) {
				fe := newFrontend("locale-bad-key")
				fe.Spec.Locales = crd.LocaleCatalogs{"fr": {key: "x"}}
				gomega.Expect(dryRun(fe)).To(gomega.MatchError(gomega.ContainSubstring("message keys must be dot-separated paths")))
			},
			ginkgo.Entry("empty", ""),
			ginkgo.Entry("leading dot", ".a"),
			ginkgo.Entry("trailing dot", "a."),
			ginkgo.Entry("empty segment", "a..b"),
			ginkgo.Entry("whitespace", "a b"),
			ginkgo.Entry("slash", "a/b"),
			ginkgo.Entry("too long", strings.Repeat("a", 300)),
		)

		ginkgo.It("rejects messages longer than the limit", func() {
			fe := newFrontend("locale-long-message")
			fe.Spec.Locales = crd.LocaleCatalogs{"fr": {"a.b": crd.LocaleMessage(strings.Repeat("x", 4097))}}
			gomega.Expect(dryRun(fe)).To(gomega.HaveOccurred())
		})

		ginkgo.DescribeTable("rejects non-string message values",
			func(value any) {
				u := unstructuredCR("Frontend", "locale-non-string", "default", map[string]any{
					"envName":        "locale-schema-env",
					"title":          "locale-non-string",
					"deploymentRepo": "https://github.com/RedHatInsights/locale-non-string",
					"frontend":       map[string]any{"paths": []any{"/apps/x"}},
					"locales":        map[string]any{"fr": map[string]any{"a.b": value}},
				})
				gomega.Expect(dryRun(u)).To(gomega.MatchError(gomega.ContainSubstring("must be of type string")))
			},
			ginkgo.Entry("number", int64(1)),
			ginkgo.Entry("boolean", true),
			ginkgo.Entry("object", map[string]any{"one": "x"}),
			ginkgo.Entry("list", []any{"x"}),
		)

		ginkgo.DescribeTable("rejects malformed maps",
			func(locales any) {
				u := unstructuredCR("Frontend", "locale-bad-map", "default", map[string]any{
					"envName":        "locale-schema-env",
					"title":          "locale-bad-map",
					"deploymentRepo": "https://github.com/RedHatInsights/locale-bad-map",
					"frontend":       map[string]any{"paths": []any{"/apps/x"}},
					"locales":        locales,
				})
				gomega.Expect(dryRun(u)).To(gomega.HaveOccurred())
			},
			ginkgo.Entry("locale maps to a string", map[string]any{"fr": "Rôles"}),
			ginkgo.Entry("locale maps to a list", map[string]any{"fr": []any{"Rôles"}}),
			ginkgo.Entry("locales is a list", []any{"fr"}),
			ginkgo.Entry("locales is a string", "fr"),
		)

		ginkgo.It("rejects more locales than the limit", func() {
			fe := newFrontend("locale-too-many")
			fe.Spec.Locales = crd.LocaleCatalogs{}
			for _, a := range "abcdefgh" {
				for _, b := range "abcde" {
					fe.Spec.Locales[string(a)+string(b)] = crd.LocaleMessages{"a.b": "x"}
				}
			}
			gomega.Expect(len(fe.Spec.Locales)).To(gomega.BeNumerically(">", 32))
			gomega.Expect(dryRun(fe)).To(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("FrontendEnvironment.spec.enabledLocales", func() {
		ginkgo.It("accepts {id, name} entries", func() {
			env := newEnv("locale-enabled-ok")
			env.Spec.EnabledLocales = []crd.EnabledLocale{
				{ID: "fr", Name: "Français"},
				{ID: "ko", Name: "한국어"},
				{ID: "ja", Name: "日本語"},
				{ID: "zh-CN", Name: "简体中文"},
			}
			gomega.Expect(dryRun(env)).To(gomega.Succeed())
		})

		ginkgo.It("rejects English", func() {
			env := newEnv("locale-enabled-en")
			env.Spec.EnabledLocales = []crd.EnabledLocale{{ID: "en", Name: "English"}}
			gomega.Expect(dryRun(env)).To(gomega.MatchError(gomega.ContainSubstring("English is the default locale")))
		})

		ginkgo.It("rejects malformed ids", func() {
			env := newEnv("locale-enabled-bad-id")
			env.Spec.EnabledLocales = []crd.EnabledLocale{{ID: "zh-cn", Name: "简体中文"}}
			gomega.Expect(dryRun(env)).To(gomega.HaveOccurred())
		})

		ginkgo.It("rejects an empty display name", func() {
			env := newEnv("locale-enabled-no-name")
			env.Spec.EnabledLocales = []crd.EnabledLocale{{ID: "fr"}}
			gomega.Expect(dryRun(env)).To(gomega.HaveOccurred())
		})

		ginkgo.It("rejects duplicate ids", func() {
			env := newEnv("locale-enabled-dup")
			env.Spec.EnabledLocales = []crd.EnabledLocale{{ID: "fr", Name: "Français"}, {ID: "fr", Name: "French"}}
			gomega.Expect(dryRun(env)).To(gomega.MatchError(gomega.ContainSubstring("Duplicate value")))
		})

		ginkgo.It("rejects bare string tags", func() {
			u := unstructuredCR("FrontendEnvironment", "locale-enabled-strings", "", map[string]any{
				"sso":            "https://sso.example.com/auth",
				"enabledLocales": []any{"fr", "ko"},
			})
			gomega.Expect(dryRun(u)).To(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("FrontendEnvironment.spec.locales", func() {
		ginkgo.It("accepts tier-1 locale maps", func() {
			env := newEnv("locale-env-ok")
			env.Spec.Locales = tier1
			gomega.Expect(dryRun(env)).To(gomega.Succeed())
		})

		ginkgo.It("rejects English as a translation", func() {
			env := newEnv("locale-env-en")
			env.Spec.Locales = crd.LocaleCatalogs{"en": {"bundles.rhel.title": "x"}}
			gomega.Expect(dryRun(env)).To(gomega.MatchError(gomega.ContainSubstring("English is the default locale")))
		})

		ginkgo.It("rejects malformed keys", func() {
			env := newEnv("locale-env-bad-key")
			env.Spec.Locales = crd.LocaleCatalogs{"fr": {"bundles..title": "x"}}
			gomega.Expect(dryRun(env)).To(gomega.HaveOccurred())
		})

		ginkgo.It("rejects non-string message values", func() {
			u := unstructuredCR("FrontendEnvironment", "locale-env-non-string", "", map[string]any{
				"sso":     "https://sso.example.com/auth",
				"locales": map[string]any{"fr": map[string]any{"bundles.rhel.title": int64(1)}},
			})
			gomega.Expect(dryRun(u)).To(gomega.MatchError(gomega.ContainSubstring("must be of type string")))
		})
	})

	ginkgo.Context("existing resources without localization fields", func() {
		ginkgo.It("round-trips without gaining localization fields or changed defaults", func() {
			env := newEnv("locale-schema-env")
			env.Spec.Bundles = &[]crd.FrontendBundles{{ID: "rhel", Title: "Red Hat Enterprise Linux"}}
			gomega.Expect(k8sClient.Create(ctx, env)).To(gomega.Succeed())
			defer func() { gomega.Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, env))).To(gomega.Succeed()) }()

			fe := newFrontend("locale-roundtrip")
			gomega.Expect(k8sClient.Create(ctx, fe)).To(gomega.Succeed())
			defer func() { gomega.Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, fe))).To(gomega.Succeed()) }()

			gotEnv := &crd.FrontendEnvironment{}
			gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: env.Name}, gotEnv)).To(gomega.Succeed())
			gomega.Expect(gotEnv.Spec.Locales).To(gomega.BeNil())
			gomega.Expect(gotEnv.Spec.EnabledLocales).To(gomega.BeNil())
			gomega.Expect(gotEnv.Spec).To(gomega.Equal(env.Spec))

			gotFe := &crd.Frontend{}
			gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fe.Name, Namespace: fe.Namespace}, gotFe)).To(gomega.Succeed())
			gomega.Expect(gotFe.Spec.Locales).To(gomega.BeNil())

			for _, kind := range []struct{ gvk, name, ns string }{
				{"FrontendEnvironment", env.Name, ""},
				{"Frontend", fe.Name, fe.Namespace},
			} {
				raw := &unstructured.Unstructured{}
				raw.SetAPIVersion("cloud.redhat.com/v1alpha1")
				raw.SetKind(kind.gvk)
				gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: kind.name, Namespace: kind.ns}, raw)).To(gomega.Succeed())
				spec, _, _ := unstructured.NestedMap(raw.Object, "spec")
				gomega.Expect(spec).NotTo(gomega.HaveKey("locales"))
				gomega.Expect(spec).NotTo(gomega.HaveKey("enabledLocales"))
			}
		})

		ginkgo.It("persists and returns localization fields unchanged", func() {
			env := newEnv("locale-persist-env")
			env.Spec.EnabledLocales = []crd.EnabledLocale{{ID: "fr", Name: "Français"}, {ID: "zh-CN", Name: "简体中文"}}
			env.Spec.Locales = crd.LocaleCatalogs{"fr": {"bundles.rhel.title": "RHEL"}}
			gomega.Expect(k8sClient.Create(ctx, env)).To(gomega.Succeed())
			defer func() { gomega.Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, env))).To(gomega.Succeed()) }()

			got := &crd.FrontendEnvironment{}
			gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: env.Name}, got)).To(gomega.Succeed())
			gomega.Expect(got.Spec.EnabledLocales).To(gomega.Equal(env.Spec.EnabledLocales))
			gomega.Expect(got.Spec.Locales).To(gomega.Equal(env.Spec.Locales))
		})
	})

	ginkgo.Context("server-side apply", func() {
		ginkgo.It("merges enabledLocales entries by id across field owners", func() {
			name := "locale-ssa-env"
			apply := func(owner, id, label string) {
				obj := unstructuredCR("FrontendEnvironment", name, "", map[string]any{
					"sso":            "https://sso.example.com/auth",
					"enabledLocales": []any{map[string]any{"id": id, "name": label}},
				})
				gomega.Expect(k8sClient.Patch(ctx, obj, client.Apply, client.FieldOwner(owner))).To(gomega.Succeed())
			}
			defer func() {
				gomega.Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, newEnv(name)))).To(gomega.Succeed())
			}()

			apply("locale-ssa-fr", "fr", "Français")
			apply("locale-ssa-ko", "ko", "한국어")

			got := &crd.FrontendEnvironment{}
			gomega.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, got)).To(gomega.Succeed())
			gomega.Expect(got.Spec.EnabledLocales).To(gomega.ConsistOf(
				crd.EnabledLocale{ID: "fr", Name: "Français"},
				crd.EnabledLocale{ID: "ko", Name: "한국어"},
			))
		})
	})

	ginkgo.Context("examples/localization.yaml", func() {
		ginkgo.It("is accepted by the API server", func() {
			data, err := os.ReadFile("../examples/localization.yaml")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
			kinds := []string{}
			for {
				u := &unstructured.Unstructured{}
				if err := decoder.Decode(&u.Object); errors.Is(err, io.EOF) {
					break
				} else {
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
				}
				if len(u.Object) == 0 {
					continue
				}
				if u.GetKind() == "Frontend" {
					u.SetNamespace("default")
				}
				gomega.Expect(dryRun(u)).To(gomega.Succeed(), "kind %s", u.GetKind())
				kinds = append(kinds, u.GetKind())
			}
			gomega.Expect(kinds).To(gomega.ConsistOf("FrontendEnvironment", "Frontend"))
		})
	})
})
