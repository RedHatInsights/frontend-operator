package v1alpha1

// A base template which users can "fork" and customize for their own dashboards.
type BaseWidgetDashboardTemplate struct {
	Name           string         `json:"name"`           // The name of the dashboard template
	DisplayName    string         `json:"displayName"`    // The display name of the dashboard template
	TemplateConfig TemplateConfig `json:"templateConfig"` // The configuration of the dashboard template
}

// TemplateConfig defines the configuration for different screen sizes.
type TemplateConfig struct {
	Sm []WidgetTemplateConfigItem `json:"sm"` // Small screen configuration items
	Md []WidgetTemplateConfigItem `json:"md"` // Medium screen configuration items
	Lg []WidgetTemplateConfigItem `json:"lg"` // Large screen configuration items
	Xl []WidgetTemplateConfigItem `json:"xl"` // Extra large screen configuration items
}

// WidgetTemplateConfigItem represents a single widget's configuration within the grid.
type WidgetTemplateConfigItem struct {
	W      int    `json:"w"`                // The width of the widget in the grid
	H      int    `json:"h"`                // The height of the widget in the grid
	MaxH   *int   `json:"maxH,omitempty"`   // The maximum height of the widget in the grid
	MinH   *int   `json:"minH,omitempty"`   // The minimum height of the widget in the grid
	X      int    `json:"x"`                // The x position of the widget in the grid
	Y      int    `json:"y"`                // The y position of the widget in the grid
	I      string `json:"i"`                // The unique identifier of the widget
	Static *bool  `json:"static,omitempty"` // Whether the widget is locked in the grid
	Title  string `json:"title,omitempty"`  // The title of the widget
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
	Width     int `json:"w" yaml:"w"`
	Height    int `json:"h" yaml:"h"`
	MaxHeight int `json:"maxH" yaml:"maxH"`
	MinHeight int `json:"minH" yaml:"minH"`
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
