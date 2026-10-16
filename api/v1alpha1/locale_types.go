/*
Copyright 2021 RedHatInsights.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

// LocaleMessage is a single translated string.
// +kubebuilder:validation:MaxLength=4096
type LocaleMessage string

// LocaleMessages maps catalog keys to translated strings for one locale. Keys
// are flat identifiers made of dot-separated segments, such as
// `searchEntries.rbac-roles.title`. The dots are part of the key, not a nesting
// path, so `a.b` and `a.b.c` are independent keys.
// +kubebuilder:validation:MaxProperties=2048
// +kubebuilder:validation:XValidation:rule="self.all(k, size(k) <= 256 && k.matches('^[A-Za-z0-9][A-Za-z0-9_-]*([.][A-Za-z0-9][A-Za-z0-9_-]*)*$'))",message="message keys must be dot-separated paths of letters, digits, '_' and '-' (e.g. searchEntries.rbac-roles.title)"
type LocaleMessages map[string]LocaleMessage

// LocaleCatalogs maps a locale identifier to the translated strings for that
// locale. Locale identifiers use canonical BCP 47 casing (`fr`, `ko`, `ja`,
// `zh-CN`). English is the default and is never listed as a translation.
// +kubebuilder:validation:MaxProperties=32
// +kubebuilder:validation:XValidation:rule="self.all(l, size(l) <= 16 && l.matches('^[a-z]{2,3}(-[A-Z][a-z]{3})?(-([A-Z]{2}|[0-9]{3}))?$'))",message="locale identifiers must use canonical BCP 47 casing, e.g. fr, ko, ja, zh-CN, zh-Hant-TW"
// +kubebuilder:validation:XValidation:rule="!('en' in self)",message="English is the default locale and must not be listed as a translation"
type LocaleCatalogs map[string]LocaleMessages

// EnabledLocale is a locale that an environment generates and offers to users.
// It carries what the generated locales.json needs for each entry.
type EnabledLocale struct {
	// Locale identifier in canonical BCP 47 casing (e.g. `fr`, `ko`, `ja`, `zh-CN`).
	// English is the default and must not be listed.
	// +kubebuilder:validation:MaxLength=16
	// +kubebuilder:validation:Pattern=`^[a-z]{2,3}(-[A-Z][a-z]{3})?(-([A-Z]{2}|[0-9]{3}))?$`
	// +kubebuilder:validation:XValidation:rule="self != 'en'",message="English is the default locale and must not be listed"
	ID string `json:"id" yaml:"id"`
	// Display name of the locale, shown to users in the locale picker
	// (typically the native name, e.g. `Français`).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	Name string `json:"name" yaml:"name"`
}
