package v1alpha1

// A base template which users can "fork" and customize for their own dashboards.
type BaseWidgetDashboardTemplate struct {
	Name           string         `json:"name" yaml:"name"`                                   // The name of the dashboard template
	DisplayName    string         `json:"displayName" yaml:"displayName"`                     // The display name of the dashboard template
	TemplateConfig TemplateConfig `json:"templateConfig" yaml:"templateConfig"`               // The configuration of the dashboard template
	FrontendRef    string         `json:"frontendRef,omitempty" yaml:"frontendRef,omitempty"` // The frontend reference for the dashboard template
}

// TemplateConfig defines the configuration for different screen sizes.
type TemplateConfig struct {
	Sm []WidgetTemplateConfigItem `json:"sm" yaml:"sm"` // Small screen configuration items
	Md []WidgetTemplateConfigItem `json:"md" yaml:"md"` // Medium screen configuration items
	Lg []WidgetTemplateConfigItem `json:"lg" yaml:"lg"` // Large screen configuration items
	Xl []WidgetTemplateConfigItem `json:"xl" yaml:"xl"` // Extra large screen configuration items
}

// WidgetTemplateConfigItem represents a single widget's configuration within the grid.
type WidgetTemplateConfigItem struct {
	W    *int `json:"w" yaml:"w"`                           // The width of the widget in the grid
	H    *int `json:"h" yaml:"h"`                           // The height of the widget in the grid
	MaxH *int `json:"maxH,omitempty" yaml:"maxH,omitempty"` // The maximum height of the widget in the grid
	MinH *int `json:"minH,omitempty" yaml:"minH,omitempty"` // The minimum height of the widget in the grid
	// The original coordinates of the widget are x and y, we have to use CX/CY because in some YAML parsers y is a reserved keyword and translates to a boolean value.
	// CX and CY are used to avoid this issue.
	CX     *int   `json:"cx" yaml:"cx"`     // The x position of the widget in the grid
	CY     *int   `json:"cy" yaml:"cy"`     // The y position of the widget in the grid
	I      string `json:"i"`                // The unique identifier of the widget
	Static *bool  `json:"static,omitempty"` // Whether the widget is locked in the grid
}

type WidgetHeaderLink struct {
	Title string `json:"title" yaml:"title"`
	Href  string `json:"href" yaml:"href"`
}

type WidgetConfiguration struct {
	Title       string           `json:"title"`
	Icon        string           `json:"icon,omitempty"`
	HeaderLink  WidgetHeaderLink `json:"headerLink,omitempty"`
	Permissions []Permission     `json:"permissions,omitempty"`
}

type WidgetBaseDimensions struct {
	Width     *int `json:"w" yaml:"w"`
	Height    *int `json:"h" yaml:"h"`
	MaxHeight *int `json:"maxH,omitempty" yaml:"maxH,omitempty"`
	MinHeight *int `json:"minH,omitempty" yaml:"minH,omitempty"`
}

type WidgetModuleFederationMetadata struct {
	Scope       string               `json:"scope"`
	Module      string               `json:"module"`
	ImportName  string               `json:"importName,omitempty"`
	FeatureFlag string               `json:"featureFlag,omitempty"`
	Defaults    WidgetBaseDimensions `json:"defaults"`
	Config      WidgetConfiguration  `json:"config"`
	FrontendRef string               `json:"frontendRef,omitempty" yaml:"frontendRef,omitempty"`
}
