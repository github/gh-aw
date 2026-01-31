package workflow

// ProjectView defines a project view configuration
type ProjectView struct {
	Name          string `yaml:"name" json:"name"`
	Layout        string `yaml:"layout" json:"layout"`
	Filter        string `yaml:"filter,omitempty" json:"filter,omitempty"`
	VisibleFields []int  `yaml:"visible-fields,omitempty" json:"visible_fields,omitempty"`
	Description   string `yaml:"description,omitempty" json:"description,omitempty"`
}

// ProjectFieldDefinition defines a project custom field configuration
// used by create_project operation=create_fields.
type ProjectFieldDefinition struct {
	Name     string   `yaml:"name" json:"name"`
	DataType string   `yaml:"data-type" json:"data_type"`
	Options  []string `yaml:"options,omitempty" json:"options,omitempty"`
}
