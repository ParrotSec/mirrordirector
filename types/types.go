package types

type MirrorsYAML struct {
	Mirrors []struct {
		Url string `yaml:"url"`
		Down  bool
	} `yaml:"mirrors"`
}
