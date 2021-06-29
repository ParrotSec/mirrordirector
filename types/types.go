package types

type Mirror struct {
	Name string `yaml:"name"`
	Url string `yaml:"url"`
	BlockedCountries []string `yaml:"blocked_countries"`
	Down bool
}

type Country struct {
	Mirrors []Mirror `yaml:"mirrors"`
}

type Continent struct {
	Countries map[string]Country `yaml:",inline"`
}

type MirrorsYAML struct {
	Continents map[string]Continent `yaml:"continents"`
}
